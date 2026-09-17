<!--
  One of a plugin's own views: a page from the plugin's folder, in a sandboxed
  frame, and the one narrow door through which it reaches the cluster.

  The frame is sandboxed without allow-same-origin, so the page has an opaque
  origin: it cannot touch this document, the app's storage, or -- because the Go
  side refuses the runtime to opaque origins -- the services. What it can do is
  post messages here, and everything it asks for is answered by `handle` below,
  which only speaks for the kinds the plugin declares and never writes without
  the user saying yes in a dialog the page cannot reach.

  The page talks to this through the SDK the Go side serves at
  /plugin-ui/_sdk/k8sdockside.js; see internal/plugins/sdk.
-->
<script lang="ts">
    import { onDestroy, untrack } from 'svelte';
    import {
        MetricsService,
        PluginService,
        ResourceService,
    } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
    import { adoptPanel } from '../charts/adopt';
    import { isPluginOverview, PLUGIN_OVERVIEW, pluginKindFor } from '../catalogue';
    import { openExternal } from '../links';
    import { adoptPluginSummary } from '../plugins/adopt';
    import { pluginState, setPluginState } from '../plugins/storage';
    import { detail, type DetailTarget } from '../state/detail.svelte';
    import { resourceTabId, workspace } from '../state/workspace.svelte';
    import { pluginFocus } from '../plugins/focus.svelte';
    import Icon from './Icon.svelte';

    interface Props {
        contextId: string;
        /** The `plugin:<id>/<view>` kind a tab was opened with, for a page that is a whole tab. */
        kind?: string;
        /**
         * For a panel in an object's detail view instead: which plugin's, which
         * of its sections, and the object it is drawn for.
         */
        section?: { pluginId: string; sectionId: string; object: DetailTarget };
    }

    let { contextId, kind = '', section }: Props = $props();

    /** Matches PROTOCOL in the SDK. Bumped only for a change old pages would misread. */
    const PROTOCOL = 'k8sdockside/plugin@1';

    let plugin = $derived(
        section ? (workspace.plugins.find((p) => p.id === section.pluginId) ?? null) : workspace.pluginFor(kind),
    );
    let view = $derived(section ? null : workspace.pluginViewFor(kind));
    let sectionSpec = $derived(section ? (plugin?.sections ?? []).find((s) => s.id === section.sectionId) ?? null : null);
    /** The object a section is drawn for; null on a tab. */
    let object = $derived(section?.object ?? null);
    let context = $derived(workspace.contexts.find((c) => c.id === contextId) ?? null);
    let contextName = $derived(context ? workspace.displayName(context) : contextId);

    let frame = $state<HTMLIFrameElement | null>(null);

    /**
     * The object a tab's page was asked to open on -- from a search hit, for a
     * view that declares a focus. See plugins/focus.svelte.ts.
     *
     * It rides in the address after the #, and each request reloads the frame:
     * a page reads its address as it loads, and a fragment changed under a page
     * already loaded would not load anything. The request is taken as it is
     * used, so bringing the tab forward again later opens the page as the
     * reader left it rather than back on the object searched for.
     */
    const tabId = untrack(() => (section ? '' : resourceTabId(contextId, kind)));
    let focusHash = $state('');
    let focusNonce = $state(0);
    $effect(() => {
        if (!tabId || !pluginFocus.peek(tabId)) return;
        untrack(() => {
            const asked = pluginFocus.take(tabId);
            if (!asked) return;
            focusHash = asked.hash;
            focusNonce = asked.nonce;
        });
    });

    /**
     * What the page is: a tab's view, the plugin's own overview, or a section.
     * Null when none of them is installed any more.
     */
    let page = $derived.by((): { id: string; entry: string; label: string } | null => {
        if (sectionSpec) return { id: sectionSpec.id, entry: sectionSpec.entry, label: sectionSpec.label };
        if (view && view.type === 'custom') return { id: view.id, entry: view.entry || 'index.html', label: view.label };
        if (!section && plugin?.overview && isPluginOverview(kind)) {
            return { id: PLUGIN_OVERVIEW, entry: plugin.overview.entry, label: plugin.name };
        }
        return null;
    });

    /**
     * Light or dark, as the page should start before the bridge has said a
     * word. It rides in the address so the SDK can set the page's colour
     * scheme before anything is drawn: WebKit keeps the scrollbars it drew
     * first, and a page that turned dark a moment after loading kept white
     * ones. Read once -- a later change of theme reaches the page through the
     * bridge, and changing the address would reload it.
     */
    const scheme = document.documentElement.dataset.themeBase === 'light' ? 'light' : 'dark';

    /** The page's address. Each segment is encoded; the Go side decodes and re-checks it. */
    let src = $derived.by(() => {
        if (!plugin || !page) return '';
        const entry = page.entry.split('/').map(encodeURIComponent).join('/');
        const address = `/plugin-ui/${encodeURIComponent(plugin.id)}/${entry}?scheme=${scheme}`;
        return focusHash ? `${address}#${focusHash}` : address;
    });

    /**
     * The app's zoom, which the page follows without ever seeing it.
     *
     * The app zooms with CSS zoom on its root, and WebKit gets a frame under a
     * zoomed element wrong: the page inside is laid out at the zoomed size and
     * then drawn zoomed again, so at any zoom but 1 it overflows its frame and
     * scrolls both ways whatever its size. So the frame is taken out of the
     * zoom -- the box around it zooms by 1 / zoom -- and scaled back up with a
     * transform. The page gets an ordinary viewport of exactly the room it is
     * shown in, in the app's own CSS pixels, and no zoom at all, which also
     * keeps its mouse positions and its element positions in the same units.
     */
    let zoom = $derived(workspace.zoom || 1);

    /**
     * A section's height: where the manifest says to start, then whatever the
     * page measures itself at. A tab fills its pane and ignores this.
     */
    let height = $state(0);
    $effect(() => {
        height = sectionSpec?.height ?? 240;
    });

    /** Reloads a section's page when the panel moves to another object. */
    let objectKey = $derived(object ? `${object.contextId}/${object.kind}/${object.namespace}/${object.name}` : '');
    /** What the frame is rebuilt on: another object for a section, another focus for a tab. */
    let frameKey = $derived(`${objectKey}|${focusNonce}`);

    // ----- the confirmation a write waits on -------------------------------

    interface Confirmation {
        title: string;
        target: DetailTarget;
        /** Shown in a code block: the patch, or what the action is. */
        detail: string;
        apply: string;
        answer: (yes: boolean) => void;
    }

    let confirming = $state<Confirmation | null>(null);

    function ask(request: Omit<Confirmation, 'answer'>): Promise<boolean> {
        // One at a time: a page that queues twenty would otherwise stack
        // twenty dialogs, and the user would be approving the pile, not a change.
        if (confirming) return Promise.reject(new Error('another change is already waiting for an answer'));
        return new Promise((resolve) => {
            confirming = {
                ...request,
                answer: (yes) => {
                    confirming = null;
                    resolve(yes);
                },
            };
        });
    }

    onDestroy(() => confirming?.answer(false));

    // ----- the bridge --------------------------------------------------------

    function text(value: unknown): string {
        return typeof value === 'string' ? value : '';
    }

    /** Refuses a kind the plugin did not declare. The Go side checks again for reads and writes. */
    function readable(value: unknown): string {
        const wanted = text(value);
        if (!plugin?.ui?.readable.includes(wanted)) {
            throw new Error(
                `${plugin?.name ?? 'This plugin'} does not declare "${wanted}", so its views cannot use it. Add it to "ui": { "kinds": [...] }.`,
            );
        }
        return wanted;
    }

    /**
     * A service call's query as the Go side takes it: each parameter a list of
     * strings. A page may give a single value, a number or a boolean.
     */
    function queryOf(value: unknown): Record<string, string[]> {
        const out: Record<string, string[]> = {};
        if (typeof value !== 'object' || value === null || Array.isArray(value)) return out;
        const scalar = (v: unknown): string | null =>
            typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : null;
        for (const [key, raw] of Object.entries(value as Record<string, unknown>)) {
            const values = (Array.isArray(raw) ? raw : [raw]).map(scalar).filter((v): v is string => v !== null);
            if (values.length) out[key] = values;
        }
        return out;
    }

    function targetOf(params: Record<string, unknown>): DetailTarget {
        return {
            contextId,
            kind: readable(params.kind),
            namespace: text(params.namespace),
            name: text(params.name),
        };
    }

    /**
     * The theme as the page should wear it: every token the app has written on
     * its root, which is where applyTheme puts them.
     */
    function currentTheme(): { id: string; base: string; tokens: Record<string, string> } {
        const root = document.documentElement;
        const tokens: Record<string, string> = {};
        for (let i = 0; i < root.style.length; i++) {
            const name = root.style[i];
            if (name.startsWith('--')) tokens[name.slice(2)] = root.style.getPropertyValue(name).trim();
        }
        return { id: root.dataset.theme ?? '', base: root.dataset.themeBase ?? 'dark', tokens };
    }

    /**
     * The object an action is run on: the one named, which must be of the
     * action's kind, or else the one the section is drawn for.
     */
    function actionTarget(actionKind: string, params: Record<string, unknown>): DetailTarget {
        const name = text(params.name);
        if (name) return { contextId, kind: actionKind, namespace: text(params.namespace), name };
        if (object && object.kind === actionKind) return { ...object };
        throw new Error('name the object to run this on: { namespace, name }');
    }

    async function handle(method: string, params: Record<string, unknown>): Promise<unknown> {
        const p = plugin;
        const v = page;
        if (!p || !v || p.disabled) throw new Error('this plugin is no longer installed or is switched off');

        switch (method) {
            case 'hello':
                return {
                    pluginId: p.id,
                    viewId: section ? '' : v.id,
                    sectionId: section ? v.id : '',
                    object: object ? { kind: object.kind, namespace: object.namespace, name: object.name } : null,
                    contextId,
                    contextName,
                    readable: [...(p.ui?.readable ?? [])],
                    write: p.ui?.write ?? false,
                    registries: p.ui?.registries ?? false,
                    services: (p.ui?.services ?? []).map((svc) => ({ id: svc.id, label: svc.label, paths: [...svc.paths] })),
                    actions: (p.actions ?? []).map((a) => ({ id: a.id, label: a.label, kind: a.kind })),
                    // What the plugin says about itself, so a page that is its
                    // own overview can link to what it is about the way the
                    // generated one does.
                    plugin: {
                        id: p.id,
                        name: p.name,
                        version: p.version ?? '',
                        docs: p.docs,
                        links: (p.links ?? []).map((l) => ({ label: l.label, url: l.url })),
                        author: p.author,
                        authorUrl: p.authorUrl ?? '',
                    },
                    theme: currentTheme(),
                };
            case 'actions': {
                // What this plugin offers on an object right now -- the section's
                // own, unless another is named -- so a page can draw its own
                // button group with only the buttons that apply.
                const target = text(params.name) ? targetOf(params) : object;
                if (!target) throw new Error('name the object: { kind, namespace, name }');
                const all =
                    (await PluginService.ObjectActions(contextId, target.kind, target.namespace, target.name)) ?? [];
                return all.filter((a) => a.pluginId === p.id);
            }
            case 'action': {
                const id = text(params.id);
                const spec = (p.actions ?? []).find((a) => a.id === id);
                if (!spec) throw new Error(`${p.name} declares no action called "${id}"`);
                const target = actionTarget(spec.kind, params);
                // Always asked, whatever the manifest says about confirming:
                // the click that got here was inside the plugin's own page, and
                // nothing the page draws can be taken as the user saying yes.
                const yes = await ask({
                    title: `${p.name} wants to run ${spec.label}`,
                    target,
                    detail: `${spec.label} — ${target.namespace ? `${target.namespace}/` : ''}${target.name}`,
                    apply: spec.label,
                });
                if (!yes) throw new Error('the action was declined');
                const created = await PluginService.RunAction(
                    contextId,
                    p.id,
                    spec.id,
                    target.namespace,
                    target.name,
                );
                return { created };
            }
            case 'resize': {
                // Sections only: a tab fills its pane whatever the page says.
                const wanted = Number(params.height);
                if (section && Number.isFinite(wanted)) height = Math.min(4000, Math.max(60, Math.ceil(wanted)));
                return null;
            }
            case 'list':
                return (
                    (await PluginService.Objects(
                        contextId,
                        p.id,
                        readable(params.kind),
                        text(params.namespace),
                        text(params.selector),
                    )) ?? []
                );
            case 'get': {
                const target = targetOf(params);
                return PluginService.Object(contextId, p.id, target.kind, target.namespace, target.name);
            }
            case 'namespaces':
                return (await ResourceService.Namespaces(contextId)) ?? [];
            case 'summary':
                // What the generated overview is made of -- which of the kinds
                // it needs this cluster serves, and the manifest's card counts
                // -- so a page of the plugin's own can still answer "is this
                // even installed here?" first. The plugin's own, never another's.
                return adoptPluginSummary(await PluginService.Summary(contextId, p.id));
            case 'charts': {
                // The plugin's own overview charts, drawn by the page however
                // it likes -- the generated panel they would sit in is not on
                // screen when the plugin has an overview of its own. Only its
                // own overview surface: never another plugin's, never an object's.
                const wanted = Number(params.minutes);
                const minutes = Number.isFinite(wanted) ? Math.min(10080, Math.max(5, Math.round(wanted))) : 60;
                return adoptPanel(
                    await MetricsService.Charts(contextId, pluginKindFor(p.id, PLUGIN_OVERVIEW), '', '', minutes),
                );
            }
            case 'registry.lookup': {
                // Asked by the app, since the page has no network; Go checks
                // the declaration again, and that a pod runs the image.
                if (!p.ui?.registries) throw new Error(`${p.name} does not declare "ui": { "registries": true }`);
                const found = await PluginService.RegistryLookup(
                    contextId,
                    p.id,
                    text(params.image),
                    params.refresh === true,
                );
                return { ...found, tags: found?.tags ?? [] };
            }
            case 'services.get': {
                // A GET to one of the plugin's declared in-cluster services,
                // made by the app through the API server. Go checks the
                // declaration again, and that the path is under its prefixes.
                const id = text(params.service);
                const svc = (p.ui?.services ?? []).find((s) => s.id === id);
                if (!svc) throw new Error(`${p.name} does not declare a service "${id}" in "ui": { "services": [...] }`);
                return PluginService.ServiceGet(contextId, p.id, svc.id, text(params.path), queryOf(params.query));
            }
            case 'patch': {
                const target = targetOf(params);
                if (!p.ui?.write) throw new Error(`${p.name} does not declare "ui": { "write": true }`);
                const patch = text(params.patch);
                const yes = await ask({
                    title: `${p.name} wants to change ${target.name}`,
                    target,
                    detail: patch,
                    apply: 'Apply change',
                });
                if (!yes) throw new Error('the change was declined');
                await PluginService.Patch(contextId, p.id, target.kind, target.namespace, target.name, patch);
                return null;
            }
            case 'create': {
                const kind = readable(params.kind);
                if (!p.ui?.write) throw new Error(`${p.name} does not declare "ui": { "write": true }`);
                const body = text(params.object);
                let name = '';
                try {
                    const parsed = JSON.parse(body) as { metadata?: { name?: string; generateName?: string } };
                    name = parsed.metadata?.name || parsed.metadata?.generateName || '';
                } catch {
                    throw new Error('the object to create is not JSON');
                }
                const target: DetailTarget = { contextId, kind, namespace: text(params.namespace), name };
                const yes = await ask({
                    title: `${p.name} wants to create ${name || 'an object'}`,
                    target,
                    detail: body,
                    apply: 'Create',
                });
                if (!yes) throw new Error('the creation was declined');
                return { name: await PluginService.Create(contextId, p.id, kind, target.namespace, body) };
            }
            case 'open': {
                const target = targetOf(params);
                if (target.name) void detail.open(target);
                else workspace.openTab(contextId, target.kind);
                return null;
            }
            case 'openView': {
                const id = text(params.viewId);
                if (id !== PLUGIN_OVERVIEW && !p.views.some((other) => other.id === id)) {
                    throw new Error(`${p.name} has no view called "${id}"`);
                }
                workspace.openTab(contextId, pluginKindFor(p.id, id));
                return null;
            }
            case 'edit':
                workspace.openEditor(targetOf(params));
                return null;
            case 'logs':
                workspace.openLogs(targetOf(params));
                return null;
            case 'openUrl':
                await openExternal(text(params.url));
                return null;
            // What the page keeps between sessions: this plugin's, on this
            // tab's cluster, and nobody else's. The SDK encodes values as JSON.
            case 'storage.get': {
                const value = pluginState(p.id, contextId)[text(params.key)];
                return value === undefined ? null : value;
            }
            case 'storage.keys':
                return Object.keys(pluginState(p.id, contextId)).sort();
            case 'storage.set':
                await setPluginState(p.id, contextId, text(params.key), text(params.value));
                return null;
            default:
                throw new Error(`unknown request "${method}"`);
        }
    }

    /**
     * Posts to the page. The target origin has to be '*': a sandboxed page's
     * origin is opaque and matches nothing else. What makes that safe is that
     * the window posted to is the frame's own.
     */
    function post(message: Record<string, unknown>): void {
        // Through JSON so what is posted is plain data: a $state proxy or a
        // binding's class instance would fail the structured clone.
        const plain = JSON.parse(JSON.stringify({ protocol: PROTOCOL, ...message })) as unknown;
        frame?.contentWindow?.postMessage(plain, '*');
    }

    function onMessage(event: MessageEvent): void {
        if (!frame || event.source !== frame.contentWindow) return;
        const msg = event.data as { protocol?: unknown; id?: unknown; method?: unknown; params?: unknown };
        if (!msg || msg.protocol !== PROTOCOL || typeof msg.id !== 'number' || typeof msg.method !== 'string') return;

        const id = msg.id;
        const params = (typeof msg.params === 'object' && msg.params !== null ? msg.params : {}) as Record<
            string,
            unknown
        >;
        handle(msg.method, params).then(
            (result) => post({ id, result: result ?? null }),
            (err: unknown) => post({ id, error: err instanceof Error ? err.message : String(err) }),
        );
    }

    // A theme change repaints the page too: the app's root is watched, and the
    // tokens re-posted whenever applyTheme writes it.
    $effect(() => {
        const observer = new MutationObserver(() => post({ event: 'theme', data: currentTheme() }));
        observer.observe(document.documentElement, {
            attributes: true,
            attributeFilter: ['style', 'data-theme', 'data-theme-base'],
        });
        return () => observer.disconnect();
    });
</script>

<svelte:window onmessage={onMessage} />

<div class="host" class:section={!!section} style:height={section ? `${height}px` : null}>
    {#if section && (!plugin || !page || plugin.disabled)}
        <!-- A section whose plugin has gone draws nothing: the panel is about
             the object, and a notice about a missing plugin is noise there. -->
    {:else if !plugin || !page}
        <div class="gone">
            <Icon name="alert" size={18} />
            <div>
                <h1>This view is not installed</h1>
                <p>
                    The tab was opened on <code>{kind}</code>, and no installed plugin offers it. Add the folder it
                    came from under Settings → Plugins, or close this tab.
                </p>
            </div>
        </div>
    {:else if plugin.disabled}
        <div class="gone">
            <Icon name="alert" size={18} />
            <div>
                <h1>{plugin.name} is switched off</h1>
                <p>Switch it back on under Settings → Plugins to use this view.</p>
            </div>
        </div>
    {:else}
        <!-- allow-scripts and nothing else: no same-origin, no forms, no
             popups, no top-level navigation. The Go side repeats the sandbox in
             the page's own Content-Security-Policy. -->
        <div class="viewport" style:zoom={zoom === 1 ? null : 1 / zoom}>
            {#key frameKey}
                <iframe
                    bind:this={frame}
                    {src}
                    title="{plugin.name}: {page.label}"
                    sandbox="allow-scripts"
                    referrerpolicy="no-referrer"
                    style:width={zoom === 1 ? null : `${100 / zoom}%`}
                    style:height={zoom === 1 ? null : `${100 / zoom}%`}
                    style:transform={zoom === 1 ? null : `scale(${zoom})`}
                ></iframe>
            {/key}
        </div>

        {#if confirming}
            {@const c = confirming}
            <div class="scrim">
                <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="plugin-confirm-title">
                    <h2 id="plugin-confirm-title">{c.title}</h2>
                    <p>
                        {c.target.kind}{c.target.namespace ? ` in ${c.target.namespace}` : ''} on
                        <strong>{contextName}</strong>:
                    </p>
                    <pre>{c.detail}</pre>
                    <div class="buttons">
                        <button class="cancel" onclick={() => c.answer(false)}>Cancel</button>
                        <button class="apply" onclick={() => c.answer(true)}>{c.apply}</button>
                    </div>
                </div>
            </div>
        {/if}
    {/if}
</div>

<style>
    .host {
        position: relative;
        display: flex;
        flex: 1 1 auto;
        min-height: 0;
        height: 100%;
    }

    /* Fills the host, and is where the app's zoom is undone; the frame in it
       is scaled back up from its top-left corner. See `zoom` above. */
    .viewport {
        position: absolute;
        inset: 0;
        overflow: hidden;
    }

    iframe {
        position: absolute;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
        border: 0;
        background: var(--bg);
        transform-origin: 0 0;
    }

    /* In a detail view: as tall as the page says, framed like the panels
       around it. Its confirmation covers the window rather than the frame,
       which may be too short to hold it. */
    .host.section {
        flex: 0 0 auto;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        overflow: hidden;
    }

    .host.section iframe {
        background: var(--bg-raised);
    }

    .host.section .scrim {
        position: fixed;
        z-index: 50;
    }

    .gone {
        display: flex;
        align-items: flex-start;
        gap: 12px;
        max-width: 60ch;
        margin: 40px auto;
    }

    .gone :global(svg) {
        flex: 0 0 auto;
        margin-top: 4px;
        color: var(--warn);
    }

    .gone h1 {
        margin: 0 0 8px;
        font-size: 17px;
        font-weight: 600;
    }

    .gone p {
        margin: 0;
        color: var(--text-dim);
        line-height: 1.7;
    }

    .scrim {
        position: absolute;
        inset: 0;
        z-index: 5;
        display: grid;
        place-items: center;
        background: color-mix(in srgb, var(--bg) 60%, transparent);
    }

    .dialog {
        width: min(560px, calc(100% - 48px));
        padding: 18px 20px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        border: 1px solid var(--border);
        box-shadow: 0 12px 40px rgb(0 0 0 / 35%);
    }

    .dialog h2 {
        margin: 0 0 8px;
        font-size: 15px;
        font-weight: 600;
    }

    .dialog p {
        margin: 0 0 10px;
        color: var(--text-dim);
        font-size: 12.5px;
    }

    .dialog pre {
        max-height: 40vh;
        overflow: auto;
        margin: 0 0 14px;
        padding: 10px 12px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        border: 1px solid var(--border-soft);
        font-size: 12px;
    }

    .buttons {
        display: flex;
        justify-content: flex-end;
        gap: 8px;
    }

    .buttons button {
        padding: 6px 14px;
        border-radius: var(--radius-sm);
        font-size: 12.5px;
    }

    .cancel {
        color: var(--text-dim);
        background: var(--bg-raised);
    }

    .cancel:hover {
        color: var(--text);
    }

    .apply {
        color: var(--accent-text);
        background: var(--accent);
    }
</style>
