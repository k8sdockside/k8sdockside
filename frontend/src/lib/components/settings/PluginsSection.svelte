<!--
  The solution plugins installed on this machine, and everything about
  installing more.

  Deliberately the same shape as the Themes section: same folder, same starter
  file, same reload, same list of what would not load. The two extension points
  are the same promise made twice — drop a JSON file in a folder and the app
  knows about your thing — and someone who has installed a theme should not have
  to learn a second set of motions to install a plugin.

  The web version keeps the same shape with the same limits as Themes: no
  folder of the user's to open or pick, and the plugins installed on the server
  are everyone's, so installing, updating, switching and removing them is for
  administrators. Everyone else still sees what is installed and what it does.
-->
<script lang="ts">
    import { onExternalClick } from '../../links';
    import {
        ANY_PLUGIN,
        arrange,
        categoryCounts,
        categoryOf,
        narrowed,
        PLUGIN_SORTS,
        type PluginQuery,
    } from '../../plugins/categories';
    import { authorOf, knownStanding, standingOf } from '../../plugins/credit';
    import type { KnownPlugin, PluginLink } from '../../plugins/types';
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import PluginMark from '../PluginMark.svelte';
    import PluginCredit from '../PluginCredit.svelte';
    import SettingsSection from './SettingsSection.svelte';

    let showFormat = $state(false);

    /**
     * What the reader is looking for: a word, a category, an order. There are
     * a dozen plugins now and reading all of them is the wrong way to find
     * one, so every list below is drawn through this. The rules themselves --
     * what a word matches, what order a category comes in -- are in
     * lib/plugins/categories.ts, where they are tested.
     */
    let query = $state<PluginQuery>({ ...ANY_PLUGIN });

    /** The repository address being typed, and whether a clone is running. */
    let repoUrl = $state('');
    let cloning = $state(false);
    let updating = $state<string | null>(null);

    async function install(): Promise<void> {
        const url = repoUrl.trim();
        if (!url || cloning) return;
        cloning = true;
        try {
            if (await workspace.installPluginFromGit(url)) repoUrl = '';
        } finally {
            cloning = false;
        }
    }

    /** The known plugin being installed, if one is. */
    let installing = $state<string | null>(null);

    async function installKnown(known: KnownPlugin): Promise<void> {
        if (installing) return;
        installing = known.id;
        try {
            await workspace.installKnownPlugin(known.id);
        } finally {
            installing = null;
        }
    }

    function hiddenSuggestion(id: string): boolean {
        return (workspace.settings.hiddenPluginSuggestions ?? []).includes(id);
    }

    /** "the folder it is in / the file", which is how a problem is found on disk. */
    function shortPath(path: string): string {
        const parts = path.split(/[\\/]/).filter(Boolean);
        return parts.slice(-2).join('/');
    }

    async function update(id: string): Promise<void> {
        updating = id;
        try {
            await workspace.updatePluginFromGit(id);
        } finally {
            updating = null;
        }
    }

    /** The plugin being uninstalled, if one is. */
    let uninstalling = $state<string | null>(null);

    /**
     * The uninstall waiting on an answer: which plugin, and what the app says
     * it deletes. Asked in the card rather than with window.confirm, which the
     * macOS webview answers with a silent "no" and shows nothing.
     */
    let asking = $state<{ id: string; path: string; plugins: string[] } | null>(null);
    /** The question's safe answer, focused so a stray Enter cannot delete. */
    let cancelEl = $state<HTMLButtonElement | null>(null);

    $effect(() => {
        if (asking) cancelEl?.focus();
    });

    async function askUninstall(id: string): Promise<void> {
        if (uninstalling) return;
        const removal = await workspace.pluginRemoval(id);
        if (removal) asking = { id, ...removal };
    }

    async function uninstall(id: string): Promise<void> {
        if (uninstalling) return;
        uninstalling = id;
        try {
            await workspace.uninstallPlugin(id);
        } finally {
            uninstalling = null;
            asking = null;
        }
    }

    /**
     * Whether a plugin was read from the plugins folder, which is what can be
     * uninstalled. A built-in is switched off instead, and a plugin in a folder
     * the user added is theirs -- the app only stops reading that folder. The
     * Go side refuses both whatever this says; this only decides the button.
     */
    function inPluginsDir(plugin: { origin: string }): boolean {
        const dir = workspace.pluginDir.replace(/[\\/]+$/, '');
        return !!dir && (plugin.origin.startsWith(dir + '/') || plugin.origin.startsWith(dir + '\\'));
    }

    /** "3 actions on VirtualMachines · 1 panel" -- what a plugin adds to objects. */
    function objectExtras(plugin: import('../../plugins/types').Plugin): string {
        const parts: string[] = [];
        const acts = plugin.actions?.length ?? 0;
        const secs = plugin.sections?.length ?? 0;
        if (acts > 0) parts.push(`${acts} object action${acts === 1 ? '' : 's'}`);
        if (secs > 0) parts.push(`${secs} detail panel${secs === 1 ? '' : 's'}`);
        return parts.join(' · ');
    }

    let builtin = $derived(workspace.plugins.filter((p) => p.origin === 'builtin'));
    /** Installed into the plugins folder: the app's to update and uninstall. */
    let installed = $derived(workspace.plugins.filter((p) => p.origin !== 'builtin' && inPluginsDir(p)));
    /**
     * Read from a folder the user added -- their own checkout, typically the
     * plugin they are writing. Listed apart, because the app neither updates
     * nor deletes anything in it.
     */
    let watched = $derived(workspace.plugins.filter((p) => p.origin !== 'builtin' && !inPluginsDir(p)));
    /**
     * The known plugins not installed here. One that is installed is already
     * under Installed, with its links and its update button; listing it here
     * as well would only say the same thing twice. One read only from a
     * watched folder is still offered: installing it is how to get the
     * published copy back.
     */
    let known = $derived(
        workspace.knownPlugins.filter((k) => !installed.some((p) => p.id === k.id) && !builtin.some((p) => p.id === k.id)),
    );

    /**
     * Every plugin this page lists, whatever section it is in: what the
     * category counts are taken from, so a count says how many there are
     * rather than how many are in one list.
     */
    let everything = $derived([...known, ...builtin, ...installed, ...watched]);
    let counts = $derived(categoryCounts(everything));

    let shownKnown = $derived(arrange(known, query));
    let shownBuiltin = $derived(arrange(builtin, query));
    let shownInstalled = $derived(arrange(installed, query));
    let shownWatched = $derived(arrange(watched, query));
    let shownTotal = $derived(
        shownKnown.length + shownBuiltin.length + shownInstalled.length + shownWatched.length,
    );
    /** A search that found nothing -- said once, rather than as four empty lists. */
    let foundNothing = $derived(narrowed(query) && shownTotal === 0);

    function pickCategory(id: string): void {
        query.category = query.category === id ? '' : id;
    }

    function clearQuery(): void {
        query.text = '';
        query.category = '';
    }

    /** The watched folder a plugin was read from; '' when it was not. */
    function watchedFolder(plugin: { origin: string }): string {
        return (
            workspace.pluginFolders.find((folder) => {
                const f = folder.replace(/[\\/]+$/, '');
                return plugin.origin.startsWith(f + '/') || plugin.origin.startsWith(f + '\\');
            }) ?? ''
        );
    }

    function fileOf(origin: string): string {
        const at = Math.max(origin.lastIndexOf('/'), origin.lastIndexOf('\\'));
        return at >= 0 ? origin.slice(at + 1) : origin;
    }

    function required(plugin: { requires: { optional: boolean }[] }): number {
        return plugin.requires.filter((r) => !r.optional).length;
    }
</script>

<SettingsSection
    title="Plugins"
    lede="A solution plugin gives something installed in your clusters — Argo CD, cert-manager, KubeVirt — a place of its own in the sidebar, instead of leaving its custom resources scattered through the definitions tree under group names. At heart it is a JSON file naming things the app already knows how to show. It may also bring pages of its own, drawn in a sandboxed frame that reads only the kinds its card lists and asks before changing anything."
>
    <!-- One search, one set of categories and one order over every list below:
         available, built in, installed and watched all narrow together, because
         "where is the one for storage" is not a question about which folder a
         plugin happens to be in. -->
    <div class="finder">
        <div class="search">
            <Icon name="search" size={13} />
            <input
                type="search"
                placeholder="Search plugins by name, subject or author"
                aria-label="Search plugins"
                spellcheck="false"
                autocomplete="off"
                bind:value={query.text}
            />
        </div>
        <label class="sort">
            <span>Sort by</span>
            <select bind:value={query.sort} aria-label="Sort plugins by">
                {#each PLUGIN_SORTS as option (option.id)}
                    <option value={option.id}>{option.label}</option>
                {/each}
            </select>
        </label>
    </div>

    <div class="chips">
        <button class="chip" class:on={query.category === ''} onclick={() => (query.category = '')}>
            All <span class="n">{everything.length}</span>
        </button>
        {#each counts as entry (entry.category.id)}
            <button
                class="chip"
                class:on={query.category === entry.category.id}
                title={entry.category.note}
                onclick={() => pickCategory(entry.category.id)}
            >
                <Icon name={entry.category.icon} size={12} />
                {entry.category.label}
                <span class="n">{entry.count}</span>
            </button>
        {/each}
    </div>

    {#if foundNothing}
        <p class="note nothing">
            No plugin matches that.
            <button class="suggest" onclick={clearQuery}>Show all {everything.length} again</button>
        </p>
    {/if}

    {#if shownKnown.length > 0}
        <h3>Available <span class="tally">{shownKnown.length}</span></h3>
        <p class="note">
            Plugins kept in repositories of their own. Installing one clones it into the plugins folder; its card
            then updates it from there. The sidebar suggests one for any cluster running what it is about.
        </p>
        <div class="gallery wide">
            {#each shownKnown as offer (offer.id)}
                {@render knownCard(offer)}
            {/each}
        </div>
    {/if}

    {#if shownBuiltin.length > 0}
        <h3>Built in <span class="tally">{shownBuiltin.length}</span></h3>
        <div class="gallery">
            {#each shownBuiltin as plugin (plugin.id)}
                {@render card(plugin)}
            {/each}
        </div>
    {/if}

    {#if shownInstalled.length > 0}
        <h3>Installed <span class="tally">{shownInstalled.length}</span></h3>
        <div class="gallery">
            {#each shownInstalled as plugin (plugin.id)}
                {@render card(plugin)}
            {/each}
        </div>
    {/if}

    {#if shownWatched.length > 0}
        <h3>From folders you watch <span class="tally">{shownWatched.length}</span></h3>
        <p class="note">
            Read from the folders under <strong>Extra folders</strong> — your own checkouts, typically a plugin you
            are writing. The app neither updates nor deletes anything in them: pull and edit them yourself, and press
            <strong>Reload</strong> to see the change.
        </p>
        <div class="gallery">
            {#each shownWatched as plugin (plugin.id)}
                {@render card(plugin)}
            {/each}
        </div>
    {/if}

    <h3>Your own plugins</h3>
    <p class="note">
        Drop a <code>.json</code> file into the plugins folder, or a whole folder of them. Files are read from the
        folder itself and one level into any subfolder, exactly as themes are.
    </p>

    <div class="path-row">
        <Icon name="folder" size={13} />
        <span class="path selectable">{workspace.pluginDir || '…'}</span>
        {#if !session.server}
            <button onclick={() => workspace.revealPluginDir()}>Open folder</button>
        {/if}
    </div>

    <div class="actions">
        {#if session.admin}
            <button class="primary" onclick={() => workspace.createExamplePlugin()}>
                <Icon name="plus" size={14} /> Write a starter plugin
            </button>
        {/if}
        {#if !session.server}
            <button onclick={() => workspace.addPluginFolder()}>
                <Icon name="folder-plus" size={14} /> Watch another folder
            </button>
        {/if}
        <button onclick={() => workspace.reloadPlugins()}>
            <Icon name="refresh" size={14} /> Reload
        </button>
    </div>

    {#if session.admin}
        <h3>From a repository</h3>
        <p class="note">
            A plugin kept in a repository of its own — with <code>plugin.json</code> at its root — is cloned into the
            plugins folder, and updated from its card. Needs <code>git</code> on this machine.
        </p>
        <form
            class="repo-row"
            onsubmit={(e) => {
                e.preventDefault();
                void install();
            }}
        >
            <input
                type="text"
                placeholder="https://github.com/you/your-plugin.git"
                spellcheck="false"
                autocomplete="off"
                aria-label="Repository address"
                bind:value={repoUrl}
            />
            <button type="submit" disabled={cloning || !repoUrl.trim()}>
                <Icon name="download" size={13} />
                {cloning ? 'Cloning…' : 'Install'}
            </button>
        </form>
        {@render installFailure('')}
    {/if}

    {#if workspace.pluginFolders.length > 0}
        <h3>Extra folders</h3>
        <ul class="paths">
            {#each workspace.pluginFolders as folder (folder)}
                <li>
                    <Icon name="folder" size={13} />
                    <span class="path selectable">{folder}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removePluginFolder(folder)}
                            title="Stop reading plugins from {folder}"
                            aria-label="Stop reading plugins from {folder}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.pluginProblems.length > 0}
        <h3>Would not load</h3>
        <p class="note">
            Fix what is listed and press <strong>Reload</strong>. To check a plugin without the app, run
            <code>go run github.com/k8sdockside/k8sdockside/cmd/plugincheck@main</code> in its folder.
        </p>
        <ul class="problems">
            {#each workspace.pluginProblems as problem (problem.path + problem.message)}
                <li>
                    <Icon name="alert" size={13} />
                    <div>
                        <span class="file" title={problem.path}>{shortPath(problem.path)}</span>
                        <span class="path selectable">{problem.path}</span>
                        <ul class="reasons selectable">
                            {#each problem.message.split('\n').filter(Boolean) as reason, i (i)}
                                <li>{reason}</li>
                            {/each}
                        </ul>
                    </div>
                </li>
            {/each}
        </ul>
    {/if}

    <button class="disclose" onclick={() => (showFormat = !showFormat)} aria-expanded={showFormat}>
        <Icon name={showFormat ? 'dot' : 'plus'} size={12} />
        {showFormat ? 'Hide' : 'Show'} what a plugin file looks like
    </button>

    {#if showFormat}
        <p class="note">
            A view names a kind the app can already open: a built-in name like <code>deployments</code>, or
            <code>crd:&lt;plural&gt;.&lt;group&gt;</code> for a custom resource. <code>requires</code> is what the
            overview checks the cluster for, and <code>cards</code> are the live counts on it — grouped by a field
            path such as <code>status.health.status</code> or <code>status.conditions[Ready]</code>.
        </p>
        <pre class="example selectable">{`{
    "$schema": "https://raw.githubusercontent.com/k8sdockside/k8sdockside/main/docs/plugin.schema.json",
    "id": "acme",
    "name": "Acme Mesh",
    "version": "1.0.0",
    "minAppVersion": "0.0.15",
    "tagline": "service mesh",
    "icon": "share",
    "links": [
        { "label": "acme.io", "url": "https://acme.io" },
        { "label": "GitHub", "url": "https://github.com/acme/mesh" }
    ],
    "requires": [
        { "kind": "crd:meshes.acme.io", "label": "Meshes" }
    ],
    "views": [
        { "id": "meshes", "label": "Meshes", "icon": "share",
          "kind": "crd:meshes.acme.io" },
        { "id": "control-plane", "label": "Control plane", "icon": "server",
          "kind": "deployments", "namespace": "acme-system",
          "selector": "app.kubernetes.io/part-of=acme" }
    ],
    "cards": [
        { "label": "Meshes", "kind": "crd:meshes.acme.io",
          "groupBy": "status.conditions[Ready]",
          "tones": { "True": "ok", "False": "error" } }
    ]
}`}</pre>
    {/if}
</SettingsSection>

{#snippet card(plugin: import('../../plugins/types').Plugin)}
    <article class="plugin" class:off={plugin.disabled}>
        <header>
            <PluginMark id={plugin.id} icon={plugin.icon} logo={plugin.logo} size={16} />
            <div class="naming">
                <p class="name">{plugin.name}</p>
                <p class="tagline">{plugin.tagline || plugin.id}</p>
            </div>
            <!-- The wanted state is sent rather than a toggle, so a card that
                 fires twice cannot end up disagreeing with what is on disk. -->
            {#if session.admin}
                <label class="switch" title={plugin.disabled ? `Switch ${plugin.name} on` : `Switch ${plugin.name} off`}>
                    <input
                        type="checkbox"
                        checked={!plugin.disabled}
                        onchange={(event) =>
                            void workspace.setPluginEnabled(plugin.id, event.currentTarget.checked)}
                    />
                    <span class="track"><span class="knob"></span></span>
                    <span class="sr-only">{plugin.disabled ? 'Off' : 'On'}</span>
                </label>
            {/if}
        </header>
        <p class="credit-line">
            <PluginCredit author={authorOf(plugin)} authorUrl={plugin.authorUrl} standing={standingOf(plugin)} />
        </p>
        <p class="tags">{@render categoryTag(plugin.category)}</p>
        <p class="counts">
            {#if plugin.version}<span class="version">v{plugin.version.replace(/^v/, '')}</span> ·{/if}
            {plugin.views.length} view{plugin.views.length === 1 ? '' : 's'}
            · {required(plugin)} required kind{required(plugin) === 1 ? '' : 's'}
        </p>
        <!-- A plugin with views of its own runs code, so what that code can
             reach is said here, before any of its views is opened. -->
        {#if plugin.ui}
            <p class="counts" title={plugin.ui.readable.join(', ')}>
                Own views · reads {plugin.ui.readable.length} kind{plugin.ui.readable.length === 1 ? '' : 's'}
                {#if plugin.ui.write}· may ask to change them{/if}
                {#if plugin.ui.registries}· asks registries about the images it sees{/if}
            </p>
            {#if plugin.ui.services.length}
                <p
                    class="counts"
                    title={plugin.ui.services.map((s) => `${s.label}: ${s.where}, ${s.paths.join(' ')}`).join('\n')}
                >
                    Calls {plugin.ui.services.map((s) => s.label).join(', ')} in the cluster · GET only
                </p>
            {/if}
        {/if}
        {#if objectExtras(plugin)}
            <p class="counts" title={(plugin.actions ?? []).map((a) => `${a.label} on ${a.kind}`).join('\n')}>
                {objectExtras(plugin)}
            </p>
        {/if}
        {@render links(plugin.links ?? [], plugin.docs)}
        {#if plugin.origin !== 'builtin'}
            <p class="from" title={plugin.origin}>
                {#if plugin.pack}{plugin.pack} · {/if}{watchedFolder(plugin) || fileOf(plugin.origin)}
            </p>
        {/if}
        <!-- What changes the installed plugins, which in the web version
             every signed-in user shares -- so only an administrator's. -->
        {#if !session.admin}
            <!-- Nothing: the card says what the plugin is, and that is all. -->
        {:else if watchedFolder(plugin)}
            <!-- The user's own checkout: the app neither pulls into it nor
                 deletes it. It can only stop reading it. -->
            <div class="card-actions">
                <button
                    class="suggest unwatch"
                    title="Stop reading plugins from {watchedFolder(plugin)} -- nothing on disk is touched"
                    onclick={() => void workspace.removePluginFolder(watchedFolder(plugin))}
                >
                    Stop watching this folder
                </button>
            </div>
        {:else if plugin.repo || inPluginsDir(plugin)}
            <div class="card-actions">
                {#if plugin.repo && inPluginsDir(plugin)}
                    <button
                        class="update"
                        disabled={updating === plugin.id}
                        title="git pull in {plugin.repo}"
                        onclick={() => void update(plugin.id)}
                    >
                        <Icon name="refresh" size={11} />
                        {updating === plugin.id ? 'Updating…' : 'Update from repository'}
                    </button>
                {/if}
                <!-- Only what was installed into the plugins folder: the app
                     asks what that deletes, and says it, before deleting. -->
                {#if inPluginsDir(plugin)}
                    <button
                        class="uninstall"
                        disabled={uninstalling !== null || asking?.id === plugin.id}
                        title="Delete {plugin.name} from the plugins folder"
                        onclick={() => void askUninstall(plugin.id)}
                    >
                        <Icon name="trash" size={11} />
                        {uninstalling === plugin.id ? 'Uninstalling…' : 'Uninstall'}
                    </button>
                {/if}
            </div>
        {/if}
        {#if asking?.id === plugin.id}
            {@const others = asking.plugins.filter((other) => other !== plugin.id)}
            <!-- Asked here, in the page: the macOS webview answers
                 window.confirm with a silent "no" and shows nothing. -->
            <div class="uninstall-ask" role="alertdialog" aria-label="Uninstall {plugin.name}?">
                <p class="question">
                    Delete <span class="path selectable">{asking.path}</span>{#if others.length > 0}, and with it
                        {others.join(', ')}{/if}? Tabs open on its views will say it is gone.
                </p>
                <div class="answers">
                    <button bind:this={cancelEl} class="uninstall-cancel" onclick={() => (asking = null)}>Cancel</button>
                    <button
                        class="uninstall-confirm"
                        disabled={uninstalling !== null}
                        onclick={() => void uninstall(plugin.id)}
                    >
                        <Icon name="trash" size={11} />
                        {uninstalling === plugin.id ? 'Uninstalling…' : 'Uninstall'}
                    </button>
                </div>
            </div>
        {/if}
    </article>
{/snippet}

{#snippet categoryTag(id: string | undefined)}
    <!-- Also the filter: the quickest way to "more like this one". -->
    {@const category = categoryOf(id)}
    <button
        class="tag"
        class:on={query.category === category.id}
        title="{category.note} — show only these"
        onclick={() => pickCategory(category.id)}
    >
        <Icon name={category.icon} size={11} />
        {category.label}
    </button>
{/snippet}

{#snippet links(list: PluginLink[], docs = '')}
    <!-- What the plugin is about, one click each; the docs link is shown only
         when the links do not already carry it. -->
    {@const all = docs && !list.some((l) => l.url === docs) ? [...list, { label: 'Docs', url: docs }] : list}
    {#if all.length > 0}
        <p class="links">
            {#each all as link (link.url)}
                <a href={link.url} target="_blank" rel="noreferrer noopener" title={link.url} onclick={onExternalClick(link.url)}
                    >{link.label}</a
                >
            {/each}
        </p>
    {/if}
{/snippet}

<!--
  What an install that did not work says, where it was asked for.

  The status bar carries the first line and is gone a moment later, and the
  whole reason an install fails is usually something to go and do -- check an
  address, make a repository public, add a key. So the card that was pressed
  keeps the full message, and stays marked, until it is dismissed or the
  install is tried again. Not a dialog: this webview answers window.confirm
  with a silent no, and the card is where the reader already is.
-->
{#snippet installFailure(id: string)}
    {@const failure = workspace.pluginInstallFailure}
    {#if failure && failure.id === id}
        <div class="failure" role="alert">
            <Icon name="alert" size={13} />
            <p class="why selectable">{failure.message}</p>
            <button class="dismiss" title="Dismiss" onclick={() => workspace.clearPluginInstallFailure()}>
                <Icon name="close" size={11} />
            </button>
        </div>
    {/if}
{/snippet}

{#snippet knownCard(offer: KnownPlugin)}
    {@const running = workspace.clustersRunning(offer)}
    {@const watchedCopy = watched.find((p) => p.id === offer.id)}
    {@const failed = workspace.pluginInstallFailure?.id === offer.id}
    <article class="plugin known" class:failed>
        <header>
            <PluginMark id={offer.id} icon={offer.icon} size={18} />
            <div class="naming">
                <p class="name">{offer.name}</p>
                <p class="tagline">{offer.tagline}</p>
            </div>
            {#if session.admin}
                <button
                    class="install"
                    disabled={installing !== null}
                    title="git clone {offer.repo}"
                    onclick={() => void installKnown(offer)}
                >
                    <Icon name="download" size={12} />
                    {installing === offer.id ? 'Installing…' : 'Install'}
                </button>
            {/if}
        </header>
        <p class="credit-line">
            <PluginCredit author={offer.author ?? ''} authorUrl={offer.authorUrl} standing={knownStanding(offer)} />
        </p>
        <p class="tags">{@render categoryTag(offer.category)}</p>
        <p class="description">{offer.description}</p>
        {#if watchedCopy}
            <!-- Installing is still offered: it is how to get the published
                 copy back. Said here, because it changes which copy loads. -->
            <p class="counts" title={watchedCopy.origin}>
                A copy is read from {watchedFolder(watchedCopy)} now. Installing puts one in the plugins folder, which
                then takes its place.
            </p>
        {/if}
        {#if running.length > 0}
            <p class="running" title="Seen in the definitions of {running.join(', ')}">
                <span class="dot"></span>
                Running in {running.slice(0, 3).join(', ')}{running.length > 3 ? ` and ${running.length - 3} more` : ''}
            </p>
        {/if}
        {@render links(offer.links)}
        {@render installFailure(offer.id)}
        {#if hiddenSuggestion(offer.id)}
            <button class="suggest" onclick={() => void workspace.hidePluginSuggestion(offer.id, false)}>
                Not suggested in the sidebar · suggest it again
            </button>
        {/if}
    </article>
{/snippet}

<style>
    /* A card whose install failed is marked, so the eye lands on it from
       anywhere in a list of a dozen. */
    .plugin.failed {
        border-color: var(--error);
        box-shadow: 0 0 0 1px var(--error) inset;
    }

    .failure {
        display: flex;
        align-items: flex-start;
        gap: 7px;
        margin-top: 8px;
        padding: 7px 8px;
        border-radius: var(--radius-sm);
        border: 1px solid var(--error);
        background: color-mix(in srgb, var(--error) 10%, transparent);
        color: var(--error);
    }

    .failure :global(svg) {
        flex: none;
        margin-top: 1px;
    }

    /* The message is git's, newlines and all: the first line says what to do
       and the rest is what the tool actually printed, which is the part
       someone pastes into a search or an issue. */
    .why {
        margin: 0;
        flex: 1;
        min-width: 0;
        font-size: 11px;
        line-height: 1.45;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
        color: var(--text);
    }

    .dismiss {
        flex: none;
        padding: 2px;
        border: 0;
        background: transparent;
        color: inherit;
        cursor: pointer;
        opacity: 0.7;
    }

    .dismiss:hover {
        opacity: 1;
    }

    /* A switched-off card is dimmed rather than hidden: this is the one place
       it still appears, because this is where it gets switched back on. */
    .plugin.off .naming,
    .plugin.off .counts,
    .plugin.off .from {
        opacity: 0.45;
    }

    .switch {
        margin-left: auto;
        display: flex;
        align-items: center;
        cursor: pointer;
        flex: none;
    }

    .switch input {
        position: absolute;
        opacity: 0;
        width: 0;
        height: 0;
    }

    .track {
        display: block;
        width: 30px;
        height: 17px;
        padding: 2px;
        border-radius: 999px;
        background: var(--bg-raised);
        border: 1px solid var(--border);
        transition:
            background 120ms ease,
            border-color 120ms ease;
    }

    .knob {
        display: block;
        width: 13px;
        height: 13px;
        border-radius: 50%;
        background: var(--text-faint);
        transition:
            transform 120ms ease,
            background 120ms ease;
    }

    .switch input:checked + .track {
        background: var(--accent);
        border-color: var(--accent);
    }

    .switch input:checked + .track .knob {
        background: var(--accent-text, #fff);
        transform: translateX(13px);
    }

    .switch input:focus-visible + .track {
        outline: 2px solid var(--accent);
        outline-offset: 2px;
    }

    .sr-only {
        position: absolute;
        width: 1px;
        height: 1px;
        overflow: hidden;
        clip-path: inset(50%);
        white-space: nowrap;
    }

    /* ----- finding one ------------------------------------------------ */

    .finder {
        display: flex;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
        margin: 4px 0 8px;
    }

    .search {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 1 1 240px;
        min-width: 200px;
        padding: 0 9px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        color: var(--text-faint);
    }

    .search:focus-within {
        border-color: var(--accent);
        color: var(--text-dim);
    }

    .search input {
        flex: 1;
        min-width: 0;
        border: none;
        background: none;
        color: var(--text);
        font: inherit;
        font-size: 12px;
        padding: 5px 0;
        outline: none;
    }

    .sort {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .sort select {
        font: inherit;
        font-size: 11.5px;
        color: var(--text);
        background: var(--bg-raised);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        padding: 4px 6px;
    }

    .chips {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-bottom: 4px;
    }

    .chip {
        display: flex;
        align-items: center;
        gap: 5px;
        padding: 3px 9px;
        border: 1px solid var(--border);
        border-radius: 999px;
        background: var(--bg-raised);
        color: var(--text-dim);
        font-size: 11.5px;
    }

    .chip:hover {
        color: var(--text);
        border-color: var(--text-faint);
    }

    .chip.on {
        background: color-mix(in srgb, var(--accent) 16%, transparent);
        border-color: var(--accent);
        color: var(--text);
    }

    .chip .n {
        font-variant-numeric: tabular-nums;
        color: var(--text-faint);
    }

    .tally {
        font-size: 10.5px;
        color: var(--text-faint);
        font-variant-numeric: tabular-nums;
        letter-spacing: 0;
    }

    .nothing {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .tags {
        display: flex;
        gap: 5px;
        margin: 0;
    }

    .tag {
        display: flex;
        align-items: center;
        gap: 4px;
        padding: 1px 7px;
        border-radius: 999px;
        border: 1px solid var(--border);
        background: transparent;
        color: var(--text-faint);
        font-size: 10.5px;
    }

    .tag:hover {
        color: var(--text);
        border-color: var(--text-faint);
    }

    .tag.on {
        border-color: var(--accent);
        color: var(--text-dim);
    }

    h3 {
        margin: 22px 0 8px;
        font-size: 11px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .note {
        margin: 0 0 12px;
        max-width: 70ch;
        font-size: 12px;
        line-height: 1.7;
        color: var(--text-faint);
    }

    .gallery {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
        gap: 10px;
    }

    .gallery.wide {
        grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    }

    .known {
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 12px 14px;
    }

    .known header :global(svg) {
        color: var(--accent);
    }

    .description {
        margin: 0;
        font-size: 11.5px;
        line-height: 1.55;
        color: var(--text-dim);
        display: -webkit-box;
        -webkit-line-clamp: 3;
        line-clamp: 3;
        -webkit-box-orient: vertical;
        overflow: hidden;
    }

    .install {
        margin-left: auto;
        display: flex;
        align-items: center;
        gap: 5px;
        flex: 0 0 auto;
        padding: 4px 10px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        background: var(--accent);
        color: var(--accent-text);
    }

    .install:hover:not(:disabled) {
        filter: brightness(1.08);
    }

    .install:disabled {
        opacity: 0.55;
    }

    .running {
        display: flex;
        align-items: center;
        gap: 6px;
        margin: 0;
        font-size: 11px;
        color: var(--text);
    }

    .running .dot {
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--ok);
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
        flex: none;
    }

    .links {
        display: flex;
        flex-wrap: wrap;
        gap: 2px 12px;
        margin: 7px 0 0;
        font-size: 11px;
    }

    .known .links {
        margin-top: auto;
        padding-top: 2px;
    }

    .links a {
        color: var(--accent);
        text-decoration: none;
    }

    .links a:hover {
        text-decoration: underline;
    }

    .version {
        font-family: var(--mono);
        font-size: 10.5px;
    }

    .credit-line {
        display: flex;
        min-width: 0;
        margin: 6px 0 0;
    }

    .suggest {
        align-self: flex-start;
        font-size: 11px;
        color: var(--text-faint);
    }

    .suggest:hover {
        color: var(--text);
        text-decoration: underline;
    }

    .plugin {
        padding: 10px 12px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        min-width: 0;
    }

    .plugin header {
        display: flex;
        align-items: center;
        gap: 9px;
        min-width: 0;
    }

    .plugin header :global(svg) {
        flex: 0 0 auto;
        color: var(--text-dim);
    }

    .naming {
        min-width: 0;
    }

    .name {
        margin: 0;
        font-size: 12.5px;
        font-weight: 500;
        color: var(--text);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .tagline,
    .counts,
    .from {
        margin: 2px 0 0;
        font-size: 11px;
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .counts {
        margin-top: 7px;
    }

    .from {
        font-family: var(--mono);
        font-size: 10px;
    }

    .path-row {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 7px 10px;
        margin-bottom: 12px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .path {
        flex: 1 1 auto;
        min-width: 0;
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .path-row button {
        flex: 0 0 auto;
        padding: 3px 9px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .path-row button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 12px;
    }

    .actions button {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .actions button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .actions .primary {
        background: var(--accent);
        color: var(--accent-text);
        box-shadow: none;
    }

    .actions .primary:hover {
        filter: brightness(1.08);
        background: var(--accent);
        color: var(--accent-text);
    }

    .repo-row {
        display: flex;
        gap: 8px;
        margin-bottom: 12px;
    }

    .repo-row input {
        flex: 1 1 auto;
        min-width: 0;
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font-family: var(--mono);
        font-size: 11.5px;
    }

    .repo-row input:focus {
        outline: none;
        box-shadow: inset 0 0 0 1px var(--accent);
    }

    .repo-row button,
    .update {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 0 0 auto;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .repo-row button:hover:not(:disabled),
    .update:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .repo-row button:disabled,
    .update:disabled {
        opacity: 0.5;
    }

    .update {
        margin-top: 8px;
        padding: 3px 8px;
        font-size: 11px;
    }

    .card-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 8px;
    }

    .card-actions .update {
        margin-top: 0;
    }

    /* Plain until it is pointed at: it deletes, but it also asks first. */
    .uninstall {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 0 0 auto;
        padding: 3px 8px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .uninstall:hover:not(:disabled) {
        color: var(--error);
        background: color-mix(in srgb, var(--error) 12%, transparent);
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--error) 50%, transparent);
    }

    .uninstall:disabled {
        opacity: 0.5;
    }

    /* The question an uninstall waits on, in the card it is about: what goes,
       then the safe answer first and the one that deletes coloured apart. */
    .uninstall-ask {
        display: flex;
        flex-direction: column;
        gap: 8px;
        margin-top: 8px;
        padding: 8px 10px;
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--error) 8%, var(--bg-panel));
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--error) 40%, transparent);
    }

    .uninstall-ask .question {
        margin: 0;
        font-size: 12px;
        color: var(--text);
        overflow-wrap: anywhere;
    }

    .uninstall-ask .path {
        font-family: var(--mono);
        font-size: 11px;
    }

    .uninstall-ask .answers {
        display: flex;
        justify-content: flex-end;
        gap: 6px;
    }

    .uninstall-cancel {
        padding: 3px 10px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .uninstall-cancel:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .uninstall-confirm {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 3px 10px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--error);
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--error) 55%, transparent);
    }

    .uninstall-confirm:hover:not(:disabled) {
        background: color-mix(in srgb, var(--error) 16%, transparent);
    }

    .uninstall-confirm:disabled {
        opacity: 0.5;
    }

    .paths,
    .problems {
        list-style: none;
        margin: 0 0 12px;
        padding: 0;
    }

    .paths li,
    .problems li {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 6px 2px;
        border-bottom: 1px solid var(--border-soft);
    }

    .problems li {
        align-items: flex-start;
    }

    .problems :global(svg) {
        flex: 0 0 auto;
        margin-top: 2px;
        color: var(--error);
    }

    .problems div {
        min-width: 0;
    }

    .problems .file {
        display: block;
        font-size: 12px;
        font-weight: 500;
        color: var(--text);
    }

    .problems .path {
        display: block;
        font-size: 10px;
        color: var(--text-faint);
    }

    .reasons {
        list-style: none;
        margin: 6px 0 2px;
        padding: 0;
    }

    .reasons li {
        display: block;
        position: relative;
        padding: 2px 0 2px 12px;
        border: 0;
        font-size: 11.5px;
        line-height: 1.5;
        color: var(--text-dim);
        overflow-wrap: anywhere;
    }

    .reasons li::before {
        content: '';
        position: absolute;
        left: 2px;
        top: 9px;
        width: 4px;
        height: 4px;
        border-radius: 50%;
        background: var(--error);
    }

    .drop {
        display: grid;
        place-items: center;
        width: 20px;
        height: 20px;
        flex: 0 0 auto;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .drop:hover {
        background: var(--bg-hover);
        color: var(--error);
    }

    .disclose {
        display: flex;
        align-items: center;
        gap: 6px;
        margin-top: 6px;
        font-size: 12px;
        color: var(--text-dim);
    }

    .disclose:hover {
        color: var(--text);
    }

    .example {
        margin: 10px 0 0;
        padding: 12px 14px;
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-family: var(--mono);
        font-size: 11px;
        line-height: 1.7;
        color: var(--text-dim);
        overflow-x: auto;
    }

    code {
        font-family: var(--mono);
        font-size: 11px;
        background: var(--bg-raised);
        border-radius: 3px;
        padding: 1px 5px;
    }
</style>
