// What each cluster serves and which plugins go with it: custom resource definitions, and the plugins' catalogue.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import {
    ResourceService,
    MetricsService,
    PluginService,
    SettingsService,
} from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { adoptSettings, type Settings } from '../adopt';
import { views } from '../views';
import {
    PLUGIN_OVERVIEW,
    labelFor,
    parsePluginKind,
    pluginKindFor,
    registerPluginViews,
} from '../../catalogue';
import { clusters } from '../health.svelte';
import { notices } from '../notices.svelte';
import { session } from '../session.svelte';
import { detail } from '../detail.svelte';
import { rememberSection } from '../../components/settings/section.svelte';
import { adoptKnownPlugin, adoptPluginCatalogue } from '../../plugins/adopt';
import {
    type KnownPlugin,
    type Plugin,
    type PluginSectionSpec,
    type PluginViewSpec,
} from '../../plugins/types';
import {
    type CustomKinds,
    NOT_LOADED,
    PLUGIN_RECHECK_MS,
    apiGroupKey,
    firstLine,
    message,
    toggled,
} from './helpers';
import { WorkspaceLayout } from './layout.svelte';

export abstract class WorkspacePlugins extends WorkspaceLayout {
    // ----- custom resources ----------------------------------------------

    /** What a context's definitions section should show. */
    customKindsFor(contextId: string): CustomKinds {
        return this.customKinds[contextId] ?? NOT_LOADED;
    }

    /**
     * Fetches the definitions a cluster serves.
     *
     * Called when a cluster first answers -- see askAboutPlugins -- or when
     * its definitions section is opened, and not for every context in the
     * kubeconfig, for the same reason probing is lazy: this is a request to a
     * cluster, and a kubeconfig with twenty contexts should not make twenty of
     * them because the sidebar was expanded. The answer is kept, so opening
     * and closing the section costs nothing after the first time.
     *
     * Asking again over an answer already on screen leaves that answer there
     * until the new one lands. Blanking it would blank everything read from
     * it too -- which plugins this cluster has, and so every panel and button
     * they draw -- only to put it all back a moment later. `quiet` is the
     * background recheck: it also keeps the last answer when the cluster does
     * not reply, rather than trading a good answer for a passing failure.
     */
    async loadCustomKinds(contextId: string, { force = false, quiet = false } = {}): Promise<void> {
        const previous = this.customKindsFor(contextId);
        if (previous.status === 'loading' || this.readingKinds.has(contextId)) return;
        if (!force && previous.status !== 'idle') return;

        const keep = quiet || previous.status === 'ready';
        const generation = this.kindsGenerations.get(contextId) ?? 0;
        const current = () => (this.kindsGenerations.get(contextId) ?? 0) === generation;

        this.readingKinds.add(contextId);
        if (!keep) {
            this.customKinds[contextId] = { status: 'loading', groups: [], message: '' };
        } else if (!quiet) {
            this.rereadingKinds = [...this.rereadingKinds, contextId];
        }
        try {
            const groups = await ResourceService.CustomResourceKinds(contextId);
            if (!current()) return;
            // Normalised here rather than guarded at every use: the generated
            // bindings type both the list and each group's kinds as nullable.
            this.customKinds[contextId] = {
                status: 'ready',
                groups: (groups ?? []).map((g) => ({ group: g.group, kinds: g.kinds ?? [] })),
                message: '',
            };
            // The definitions answer for every plugin that defines a custom
            // resource. The handful that define none are asked about here, on
            // the same trigger and with the same laziness.
            await this.probeCluster(contextId, { quiet, current });
        } catch (err) {
            if (current() && !quiet) {
                this.customKinds[contextId] = { status: 'error', groups: [], message: message(err) };
            }
        } finally {
            if (current()) this.readingKinds.delete(contextId);
            if (this.rereadingKinds.includes(contextId)) {
                this.rereadingKinds = this.rereadingKinds.filter((id) => id !== contextId);
            }
        }
    }

    /** Whether a context's definitions are being read, first time or again. */
    isReadingKinds(contextId: string): boolean {
        return this.customKindsFor(contextId).status === 'loading' || this.rereadingKinds.includes(contextId);
    }

    /**
     * Makes sure every cluster that has answered has been asked which plugins
     * it has.
     *
     * What a plugin draws onto a cluster's own objects -- a Descheduler panel
     * on a pod, an "Allow descheduling" button beside it -- is drawn only once
     * the cluster is known to run the product, so the question cannot wait
     * for the sidebar's Plugins section to be opened: viewing a pod is reason
     * enough. Contexts that have not answered are left alone, which is what
     * keeps this as lazy as the probing that decides they have. Asks nothing
     * of a context that has an answer already.
     */
    askAboutPlugins(contextIds: string[]): void {
        for (const id of contextIds) void this.loadCustomKinds(id);
    }

    /**
     * Asks every connected cluster that has been asked before again, quietly,
     * so a product installed while the app is open brings its plugin to life
     * -- and one removed takes it away -- without anybody pressing refresh.
     * See PLUGIN_RECHECK_MS for how often.
     *
     * An errored answer is asked again too: a cluster that was unreachable a
     * minute ago may not be now.
     */
    recheckPlugins(): void {
        for (const [id, loaded] of Object.entries(this.customKinds)) {
            if (loaded.status !== 'ready' && loaded.status !== 'error') continue;
            if (clusters.of(id).status !== 'connected') continue;
            void this.loadCustomKinds(id, { force: true, quiet: true });
        }
    }

    /**
     * Forgets what a context served and which plugins it had, so the next
     * look at it asks again. A read still in flight is left to land nowhere.
     */
    protected forgetDefinitions(contextId: string): void {
        this.kindsGenerations.set(contextId, (this.kindsGenerations.get(contextId) ?? 0) + 1);
        this.readingKinds.delete(contextId);
        this.rereadingKinds = this.rereadingKinds.filter((id) => id !== contextId);
        const { [contextId]: _kinds, ...kinds } = this.customKinds;
        this.customKinds = kinds;
        const { [contextId]: _probe, ...probes } = this.pluginProbes;
        this.pluginProbes = probes;
    }

    /**
     * Asks the backend what a cluster's definitions cannot say: which known
     * plugins are running here, and which installed ones are not.
     *
     * Flannel is the case this exists for. It defines no custom resources at
     * all, so there is nothing in the definitions to recognise it by: as an
     * offer it was suggested for every cluster, and once installed its row
     * read "installed in this cluster" everywhere, because the kinds it needs
     * -- DaemonSets, Nodes, ConfigMaps -- are kinds every cluster serves. The
     * backend looks for the objects instead, for the few plugins that ask it
     * to, so this is usually one request or none.
     *
     * A failure is not reported: the definitions beside it already say the
     * cluster would not answer, and it leaves every row as it was. A quiet
     * recheck that fails keeps the answer it had.
     */
    private async probeCluster(
        contextId: string,
        { quiet = false, current = () => true }: { quiet?: boolean; current?: () => boolean } = {},
    ): Promise<void> {
        try {
            const probe = await PluginService.Probe(contextId);
            if (!current()) return;
            this.pluginProbes[contextId] = { known: probe?.known ?? [], absent: probe?.absent ?? [] };
        } catch {
            if (!current() || (quiet && this.pluginProbes[contextId])) return;
            this.pluginProbes[contextId] = { known: [], absent: [] };
        }
    }

    /**
     * Whether this plugin is one the definitions cannot answer for: it asks
     * for objects itself, or the known list knows how to find what it is
     * about. Either way a probe is coming, and a row should wait for it.
     */
    private probeDecides(plugin: Plugin): boolean {
        if (plugin.requires.some((req) => !req.optional && req.selector)) return true;
        return this.knownPlugins.some((known) => known.id === plugin.id && known.probed === true);
    }

    /** Whether a known plugin was found running in a cluster by its workload. */
    private detectedIn(contextId: string, id: string): boolean {
        return (this.pluginProbes[contextId]?.known ?? []).includes(id);
    }

    /** Whether one API group's definitions are showing, for one context. */
    isApiGroupExpanded(contextId: string, group: string): boolean {
        return this.expandedApiGroups.includes(apiGroupKey(contextId, group));
    }

    toggleApiGroup(contextId: string, group: string): void {
        this.expandedApiGroups = toggled(this.expandedApiGroups, apiGroupKey(contextId, group));
    }

    // ----- solution plugins ----------------------------------------------

    /** Whether a plugin's views are unfolded under a context in the sidebar. */
    isPluginExpanded(contextId: string, pluginId: string): boolean {
        return this.expandedPlugins.includes(apiGroupKey(contextId, pluginId));
    }

    togglePlugin(contextId: string, pluginId: string): void {
        this.expandedPlugins = toggled(this.expandedPlugins, apiGroupKey(contextId, pluginId));
    }

    /** Whether the plugins this cluster does not have are unfolded in the sidebar. */
    isAbsentPluginsExpanded(contextId: string): boolean {
        return this.expandedAbsentPlugins.includes(contextId);
    }

    toggleAbsentPlugins(contextId: string): void {
        this.expandedAbsentPlugins = toggled(this.expandedAbsentPlugins, contextId);
    }

    /** Opens a plugin's landing page for a context. */
    openPluginOverview(contextId: string, pluginId: string): void {
        this.openTab(contextId, pluginKindFor(pluginId, PLUGIN_OVERVIEW));
    }

    /**
     * Whether a cluster serves what a plugin needs, answered from the custom
     * resource definitions the sidebar has already read.
     *
     * Reusing that list is the point: the definitions are loaded lazily and
     * cached per context, so asking "does this cluster have Argo CD" costs
     * nothing beyond what the definitions section already fetched, and one
     * refresh button updates both. The plugin's own overview asks the cluster
     * directly and is the authority; this is only what decides how the sidebar
     * row reads.
     *
     * `null` means we do not know yet -- the definitions have not been read for
     * this context -- which is deliberately distinct from "no". A row that said
     * "not installed" before it had looked would be wrong more often than
     * right.
     */
    pluginInstalledIn(contextId: string, plugin: Plugin): boolean | null {
        const required = plugin.requires.filter((req) => !req.optional);
        const probed = this.probeDecides(plugin);
        // A plugin that needs nothing is at home in every cluster, and does
        // not have to wait for one to answer to say so.
        if (required.length === 0 && !probed) return true;

        const loaded = this.customKinds[contextId];
        if (!loaded || loaded.status !== 'ready') return null;

        const served = new Set(loaded.groups.flatMap((group) => group.kinds.map((kind) => kind.kind)));

        // Some plugins the definitions cannot answer for at all: a product
        // that defines no custom resources requires only kinds every cluster
        // serves, so taking those as met would read "installed here" in every
        // cluster. The backend looks for the objects instead -- what the
        // manifest asks for, or what the known list knows to look for -- and
        // until it has answered the honest verdict is that we do not know.
        if (probed) {
            const probe = this.pluginProbes[contextId];
            if (!probe) return null;
            if (probe.absent.includes(plugin.id)) return false;
        }

        return required.every((req) => {
            // Only custom resources can be looked up this way. A requirement on
            // a built-in kind -- Deployments, say -- is served by every cluster
            // worth connecting to, so it is taken as met rather than reported
            // as unknown.
            if (!req.kind.startsWith('crd:')) return true;
            return served.has(req.kind);
        });
    }

    /** The plugin a `plugin:` tab kind belongs to, if it is installed. */
    pluginFor(kind: string): Plugin | null {
        const view = parsePluginKind(kind);
        if (!view) return null;
        return this.plugins.find((p) => p.id === view.pluginId) ?? null;
    }

    /**
     * The view a `plugin:` tab kind opens, if its plugin is installed and still
     * declares it. Null for the overview, which no plugin declares.
     */
    pluginViewFor(kind: string): PluginViewSpec | null {
        const view = parsePluginKind(kind);
        if (!view) return null;
        return this.pluginFor(kind)?.views.find((v) => v.id === view.viewId) ?? null;
    }

    /**
     * The enabled plugins worth drawing for one cluster: the ones it is known
     * to have.
     *
     * A plugin is installed on this machine, not in a cluster, and its rows in
     * the sidebar say so -- the ones a cluster does not have are listed for it
     * under "not in this cluster", because that row is how you find out. What
     * a plugin draws *onto the cluster's own objects* is a different matter: a
     * Descheduler panel on every pod of a cluster with no descheduler in it,
     * and an "Allow descheduling" button beside it, are a feature of a product
     * that is not there.
     *
     * So only a yes draws anything. The cluster is asked as soon as it answers
     * at all -- see askAboutPlugins -- so "not asked yet" is a moment, and a
     * panel that appears a moment late is better than one that shows up and
     * then vanishes. The one exception is a cluster that would not say which
     * definitions it serves: it cannot tell us which products it runs either,
     * and taking every plugin away from it for that would be the wrong way
     * round.
     */
    pluginsHereFor(contextId: string): Plugin[] {
        if (this.customKindsFor(contextId).status === 'error') return this.enabledPlugins;
        return this.enabledPlugins.filter((plugin) => this.pluginInstalledIn(contextId, plugin) === true);
    }

    /**
     * The panels enabled plugins draw in the detail view of an object of this
     * kind, each with the plugin it belongs to. Only the plugins whose product
     * this cluster has -- see pluginsHereFor.
     */
    pluginSectionsFor(contextId: string, kind: string): { plugin: Plugin; section: PluginSectionSpec }[] {
        return this.pluginsHereFor(contextId).flatMap((plugin) =>
            (plugin.sections ?? []).filter((s) => s.kind === kind).map((section) => ({ plugin, section })),
        );
    }

    /**
     * Whether an enabled plugin from outside the app puts buttons on this kind
     * in this cluster. One that does takes over from the app's own
     * product-specific buttons for it -- a VM's lifecycle bar -- rather than
     * drawing a second set beside them.
     */
    pluginActsOn(contextId: string, kind: string, opts: { external?: boolean } = {}): boolean {
        return this.pluginsHereFor(contextId).some(
            (p) => (!opts.external || p.origin !== 'builtin') && (p.actions ?? []).some((a) => a.kind === kind),
        );
    }

    /**
     * The kind a tab's rows actually are: for a plugin view, the kind the
     * view lists; for anything else, the kind itself.
     *
     * A row opened from a plugin view has to be described, edited and acted
     * on as what it is -- an Application, not "plugin:argocd/applications",
     * which the backend rightly refuses as a kind. The backend resolves the
     * view for the listing itself, so this is the one place the window has to
     * do the same.
     */
    listedKind(kind: string): string {
        const view = parsePluginKind(kind);
        if (!view) return kind;
        return this.pluginFor(kind)?.views.find((v) => v.id === view.viewId)?.kind ?? kind;
    }

    /**
     * The namespace a plugin view pins itself to, or the empty string when it
     * leaves the tab's own filter free.
     *
     * The table asks before drawing its namespace picker: a view that says
     * "Argo CD's own workloads" is not answering a question about kube-system,
     * and a filter that silently did nothing would be worse than one that is
     * not offered.
     */
    pinnedNamespace(kind: string): string {
        const view = parsePluginKind(kind);
        if (!view) return '';
        const plugin = this.pluginFor(kind);
        return plugin?.views.find((v) => v.id === view.viewId)?.namespace ?? '';
    }

    /**
     * Re-reads the definitions of contexts that already had them, so the sync
     * button is the way back from a cluster that was unreachable when its
     * section was first opened.
     *
     * Without it a failure is permanent: the sidebar asks only while a context
     * has no answer, and an error counts as one. Contexts nobody has opened the
     * section for are left alone, which is what keeps this lazy.
     */
    protected recheckCustomKinds(): void {
        for (const id of Object.keys(this.customKinds)) {
            void this.loadCustomKinds(id, { force: true });
        }
    }

    /** Forgets the definitions of contexts that are no longer in a kubeconfig. */
    protected pruneCustomKinds(): void {
        const known = new Set(this.contexts.map((c) => c.id));
        const kept: Record<string, CustomKinds> = {};
        for (const [id, loaded] of Object.entries(this.customKinds)) {
            if (known.has(id)) kept[id] = loaded;
        }
        this.customKinds = kept;
        const keptProbes: Record<string, { known: string[]; absent: string[] }> = {};
        for (const [id, probe] of Object.entries(this.pluginProbes)) {
            if (known.has(id)) keptProbes[id] = probe;
        }
        this.pluginProbes = keptProbes;
    }

    // ----- preferences ---------------------------------------------------

    /**
     * Reads the plugin catalogue, and tells the nav catalogue how the views it
     * found are titled and iconed.
     *
     * That registration is what lets labelFor() go on being a pure function of
     * a kind string everywhere else in the app: a `crd:` kind carries its own
     * name, but a plugin view's name lives in a file, so it has to be put
     * somewhere the tab bar can reach without knowing plugins exist.
     */
    async loadPlugins(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.List());
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
        } catch (err) {
            notices.fail(`Could not read plugins: ${message(err)}`);
            return;
        }
        this.registerViews();
        this.tellPluginProblems();
        if (this.knownPlugins.length === 0) void this.loadKnownPlugins();
    }

    /**
     * Says once per session, in the status bar, that some plugin would not
     * load. Settings has the reasons, but nobody opens Settings to find out
     * why a plugin they installed is not in the sidebar unless something tells
     * them to look.
     */
    private tellPluginProblems(): void {
        const count = this.pluginProblems.length;
        if (count === 0 || this.toldPluginProblems) return;
        this.toldPluginProblems = true;
        notices.fail(
            count === 1
                ? 'A plugin would not load. Settings → Plugins says why.'
                : `${count} plugin problems. Settings → Plugins says what is wrong.`,
        );
    }

    /** Reads the list of plugins the app knows of. */
    async loadKnownPlugins(): Promise<void> {
        try {
            this.knownPlugins = ((await PluginService.Known()) ?? []).map(adoptKnownPlugin);
        } catch (err) {
            notices.fail(`Could not read the list of known plugins: ${message(err)}`);
        }
    }

    /** Whether a plugin with this id is installed here, switched on or off. */
    hasPlugin(id: string): boolean {
        return this.plugins.some((p) => p.id === id);
    }

    /**
     * Known plugins worth suggesting for one cluster: not installed, not
     * hidden, and with a kind that gives their product away served there.
     *
     * Answered from the definitions the sidebar has already read, as
     * pluginInstalledIn is, so it costs nothing and says nothing until they
     * have been read.
     */
    pluginSuggestionsFor(contextId: string): KnownPlugin[] {
        const loaded = this.customKinds[contextId];
        if (!loaded || loaded.status !== 'ready') return [];
        const served = new Set(loaded.groups.flatMap((group) => group.kinds.map((kind) => kind.kind)));
        const hidden = this.settings.hiddenPluginSuggestions ?? [];
        return this.knownPlugins.filter(
            (known) =>
                !this.hasPlugin(known.id) &&
                !hidden.includes(known.id) &&
                (known.detect.some((kind) => served.has(kind)) || this.detectedIn(contextId, known.id)),
        );
    }

    /**
     * The clusters, by the name the sidebar shows, whose definitions -- where
     * they have been read -- show a known plugin's product running.
     */
    clustersRunning(known: KnownPlugin): string[] {
        return this.orderContexts(this.contexts)
            .filter((context) => {
                const loaded = this.customKinds[context.id];
                if (!loaded || loaded.status !== 'ready') return false;
                if (this.detectedIn(context.id, known.id)) return true;
                return loaded.groups.some((group) => group.kinds.some((kind) => known.detect.includes(kind.kind)));
            })
            .map((context) => this.displayName(context));
    }

    /**
     * The install that failed, and why, in full.
     *
     * The status bar gets the first line of it and is gone a moment later,
     * which is the wrong place for a message whose whole point is what to do
     * next -- an address to check, a repository to make public. So the card
     * that was pressed keeps it until it is dismissed or tried again. Keyed by
     * plugin id, with '' for the address typed into the form.
     */
    pluginInstallFailure = $state<{ id: string; message: string } | null>(null);

    /** Puts a card back to normal: pressing Install again, or dismissing. */
    clearPluginInstallFailure(): void {
        this.pluginInstallFailure = null;
    }

    /**
     * Installs one of the known plugins from the repository the app has for
     * it. Returns whether it worked.
     */
    async installKnownPlugin(id: string): Promise<boolean> {
        const name = this.knownPlugins.find((k) => k.id === id)?.name ?? id;
        this.pluginInstallFailure = null;
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.InstallKnown(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Installed ${name}`);
            return true;
        } catch (err) {
            // A clone that would not load still left its folder behind, and
            // Settings should list it with the reason.
            await this.loadPlugins();
            this.pluginInstallFailure = { id, message: message(err) };
            notices.fail(`Could not install ${name}: ${firstLine(message(err))}`);
            return false;
        }
    }

    /** Stops the sidebar suggesting a known plugin, or lets it again. */
    async hidePluginSuggestion(id: string, hidden = true): Promise<void> {
        try {
            this.settings = adoptSettings(await PluginService.HideSuggestion(id, hidden));
        } catch (err) {
            notices.fail(`Could not save that: ${message(err)}`);
        }
    }

    /** Opens Settings on its Plugins section. */
    openPluginSettings(): void {
        rememberSection('plugins');
        this.openSettings();
    }

    /** Publishes every installed view's label and icon to the nav catalogue. */
    private registerViews(): void {
        registerPluginViews(
            this.plugins.flatMap((plugin) => [
                // The overview is not one of the plugin's declared views -- every
                // plugin has one whether it asks or not -- so it is named here.
                {
                    kind: pluginKindFor(plugin.id, PLUGIN_OVERVIEW),
                    label: plugin.name,
                    icon: plugin.icon,
                },
                ...plugin.views.map((view) => ({
                    kind: pluginKindFor(plugin.id, view.id),
                    label: view.label,
                    icon: view.icon,
                })),
            ]),
        );
    }

    /** Rereads the plugin folders, picking up a file edited since launch. */
    async reloadPlugins(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.Reload());
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            const count = this.plugins.length;
            notices.inform(`${count} plugin${count === 1 ? '' : 's'} available`);
        } catch (err) {
            notices.fail(`Could not read plugins: ${message(err)}`);
        }
    }

    /**
     * Switches a plugin on or off.
     *
     * The wanted state is sent rather than a toggle, so a switch that fires
     * twice cannot end up disagreeing with what is on disk.
     */
    async setPluginEnabled(id: string, enabled: boolean): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.SetEnabled(id, enabled));
            // Refreshed here as well as on load, because this is what decides
            // whether a chart panel is drawn at all: leaving it stale would
            // keep a Metrics heading on the dashboard talking about a
            // Prometheus the user has just switched off.
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
        } catch (err) {
            notices.fail(`Could not ${enabled ? 'enable' : 'disable'} the plugin: ${message(err)}`);
        }
    }

    /** Opens the plugins folder in the file manager, creating it if need be. */
    async revealPluginDir(): Promise<void> {
        try {
            await PluginService.RevealDir();
        } catch (err) {
            notices.fail(`Could not open the plugins folder: ${message(err)}`);
        }
    }

    /** Writes a starter plugin into the plugins folder and reloads. */
    async createExamplePlugin(): Promise<void> {
        try {
            const path = await PluginService.CreateExample();
            await this.loadPlugins();
            notices.inform(`Wrote ${path}`);
        } catch (err) {
            notices.fail(`Could not write a starter plugin: ${message(err)}`);
        }
    }

    /** Opens the native picker and reads plugins from the folder chosen. */
    async addPluginFolder(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.BrowseForFolder());
            this.registerViews();
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not add the folder: ${message(err)}`);
        }
    }

    /**
     * Clones a plugin repository into the plugins folder and reads it. Returns
     * whether it worked, so the form can clear itself only then.
     */
    async installPluginFromGit(url: string): Promise<boolean> {
        this.pluginInstallFailure = null;
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.InstallFromGit(url));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Installed ${url}`);
            return true;
        } catch (err) {
            // A clone that would not load still left its folder behind, and
            // Settings should list it with the reason.
            await this.loadPlugins();
            this.pluginInstallFailure = { id: '', message: message(err) };
            notices.fail(`Could not install the plugin: ${firstLine(message(err))}`);
            return false;
        }
    }

    /** Pulls the repository a plugin was cloned from and reads it again. */
    async updatePluginFromGit(id: string): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.UpdateFromGit(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Updated ${id}`);
        } catch (err) {
            notices.fail(`Could not update the plugin: ${message(err)}`);
        }
    }

    /**
     * What uninstalling a plugin would delete -- its clone, folder or file, and
     * every plugin read from there -- for its card to ask about before
     * anything is deleted. `null` when the app refuses, with the reason shown.
     */
    async pluginRemoval(id: string): Promise<{ path: string; plugins: string[] } | null> {
        try {
            const removal = await PluginService.UninstallPreview(id);
            return { path: removal.path, plugins: removal.plugins ?? [] };
        } catch (err) {
            const name = this.plugins.find((p) => p.id === id)?.name || id;
            notices.fail(`Could not uninstall ${name}: ${message(err)}`);
            return null;
        }
    }

    /**
     * Deletes an installed plugin from the plugins folder and reads the
     * folders again. Returns whether it worked.
     *
     * The card asks first, in the page, with what pluginRemoval says goes: the
     * macOS webview answers window.confirm with a silent "no" and shows
     * nothing, so a native question would never be seen.
     */
    async uninstallPlugin(id: string): Promise<boolean> {
        const name = this.plugins.find((p) => p.id === id)?.name || id;
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.Uninstall(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Uninstalled ${name}`);
            return true;
        } catch (err) {
            notices.fail(`Could not uninstall ${name}: ${message(err)}`);
            return false;
        }
    }

    /** Stops reading plugins from a folder. Nothing on disk is touched. */
    async removePluginFolder(path: string): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.RemoveFolder(path));
            this.registerViews();
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not drop the folder: ${message(err)}`);
        }
    }
}
