<!--
  What the detail panel shows for a Helm release, in place of a describe report.

  A release has no Kubernetes kind, so there is nothing for the describe path to
  read: what stands in for it is the release's own record, which Helm keeps in a
  Secret and already holds everything worth showing. See internal/kube/helmdetail.go.

  The order is the order the questions get asked. What is this and is it healthy,
  first, because that is why the drawer was opened. Then the values, which are
  the thing anyone actually came to check. Then the notes, the objects it owns
  and where it has been -- each behind a fold, because the panel is narrow and
  all four at once would be a scroll rather than a view.
-->
<script lang="ts">
    import { formatDateTime } from '../datetime.svelte';
    import { openSearchPanel } from '@codemirror/search';
    import { EditorView, lineNumbers } from '@codemirror/view';
    import { untrack } from 'svelte';
    import { numbering, viewerExtensions } from '../editor/setup';
    import { helm, type ReleaseRef } from '../state/helm.svelte';
    import ErrorState from './ErrorState.svelte';
    import Icon from './Icon.svelte';

    interface Props {
        release: ReleaseRef;
        /**
         * Whether the values carry line numbers: the editor's preference,
         * handed down by the panel so this view need not know the store.
         */
        numbers?: boolean;
    }

    let { release, numbers = true }: Props = $props();

    let record = $derived(helm.stateOf(release));
    let detail = $derived(record.detail);

    /**
     * Whether the values shown are the overrides alone.
     *
     * Off by default, matching `helm get values --all`: the merged document is
     * the one that answers "what is this release doing", and the overrides on
     * their own answer the narrower "what did we change", which is the second
     * question rather than the first.
     */
    let userValuesOnly = $state(false);

    // A new release is a new drawer: read it, and drop the toggle back to where
    // it started rather than carrying one release's answer onto the next.
    //
    // The read is untracked because it writes what this component then displays.
    // Naming the store as a dependency would make the effect re-run on its own
    // result, and re-read forever. What it depends on is which release it is
    // pointed at, which is the three fields named here.
    $effect(() => {
        const ref = {
            contextId: release.contextId,
            namespace: release.namespace,
            name: release.name,
        };
        untrack(() => {
            userValuesOnly = false;
            void helm.load(ref);
        });
    });

    let values = $derived(userValuesOnly ? (detail?.userValues ?? '') : (detail?.values ?? ''));
    let resources = $derived(detail?.resources ?? []);
    let revisions = $derived(detail?.revisions ?? []);

    /**
     * The values are shown in the same editor the YAML tab uses, with changes
     * refused. A values file is mostly nesting you are not reading, and what
     * you do with one is find a key in it and fold the rest away; a <pre> can
     * do neither. See viewerExtensions.
     */
    let host = $state<HTMLElement | null>(null);
    let view: EditorView | null = null;
    // The name alone, so that the panel handing down an equal ref as a new
    // object -- which it does on every repaint -- does not rebuild the viewer.
    let name = $derived(release.name);

    // Builds the viewer on the element it lives in, and again for another
    // release: its folds and its search belong to the document they were made
    // on. The text is read untracked here; the effect below keeps it current.
    $effect(() => {
        const parent = host;
        const label = `Values of ${name}`;
        if (!parent) return;

        const built = new EditorView({
            parent,
            doc: untrack(() => values),
            extensions: viewerExtensions({ numbers: untrack(() => numbers), label }),
        });
        view = built;

        return () => {
            built.destroy();
            if (view === built) view = null;
        };
    });

    // The toggle swaps the document in place rather than rebuilding the
    // viewer, so an open search and the folds survive it.
    $effect(() => {
        const text = values;
        const current = view;
        if (!current || current.state.doc.toString() === text) return;
        current.dispatch({ changes: { from: 0, to: current.state.doc.length, insert: text } });
    });

    // Line numbers follow the preference as it changes, without a rebuild.
    $effect(() => {
        const on = numbers;
        view?.dispatch({ effects: numbering.reconfigure(on ? lineNumbers() : []) });
    });

    /** Opens the find panel: what ⌘F does once the viewer has focus. */
    function find(): void {
        if (!view) return;
        view.focus();
        openSearchPanel(view);
    }

    /**
     * The same four tones the resource tables use, so a status reads the same
     * colour here as it does in the row this drawer was opened from.
     */
    function toneOf(status: string): string {
        switch (status) {
            case 'deployed':
            case 'superseded':
                return status === 'deployed' ? 'ok' : 'info';
            case 'pending-install':
            case 'pending-upgrade':
            case 'pending-rollback':
            case 'uninstalling':
                return 'warn';
            case 'failed':
                return 'error';
            default:
                return '';
        }
    }

    /** An RFC3339 stamp as something readable, or a dash where there is none. */
    function when(stamp: string): string {
        if (!stamp) return '—';
        const at = new Date(stamp);
        if (Number.isNaN(at.getTime())) return stamp;
        return formatDateTime(at);
    }
</script>

{#if record.loading && !detail}
    <p class="status">Reading {release.name}…</p>
{:else if record.error}
    <ErrorState message={record.error} compact />
{:else if detail}
    <div class="release">
        <dl class="facts">
            <div><dt>Chart</dt><dd class="selectable">{detail.chart}</dd></div>
            <div><dt>Status</dt><dd class={toneOf(detail.status)}>{detail.status}</dd></div>
            {#if detail.appVersion}
                <div><dt>App version</dt><dd class="selectable">{detail.appVersion}</dd></div>
            {/if}
            <div><dt>Revision</dt><dd>{detail.revision}</dd></div>
            <div><dt>Namespace</dt><dd class="selectable">{detail.namespace}</dd></div>
            <div><dt>Updated</dt><dd>{when(detail.updated)}</dd></div>
            <div><dt>First deployed</dt><dd>{when(detail.firstDeployed)}</dd></div>
            {#if detail.description}
                <!-- Helm's own log entry for the revision: "Upgrade complete",
                     or the reason it is not. On a failed release this line is
                     the whole answer. -->
                <div><dt>Last action</dt><dd class="selectable">{detail.description}</dd></div>
            {/if}
        </dl>

        <details class="fold" open>
            <summary>
                <span>Values</span>
                <span class="count">{userValuesOnly ? 'user-supplied' : 'merged'}</span>
            </summary>

            <div class="tools">
                <label class="toggle">
                    <input type="checkbox" bind:checked={userValuesOnly} />
                    User-supplied values only
                </label>
                {#if values}
                    <button class="find" onclick={find} title="Find in the values. ⌘F does the same once the text has focus.">
                        <Icon name="search" size={12} />
                        Find
                    </button>
                {/if}
            </div>

            {#if values}
                <!-- CodeMirror mounts itself in here, read-only: ⌘F searches
                     and the gutter folds, as in the editor. -->
                <div class="doc selectable" bind:this={host}></div>
            {:else if userValuesOnly}
                <!-- Worth saying rather than showing an empty box: a release
                     installed with no overrides is running the chart exactly as
                     it ships, which is a fact about it. -->
                <p class="empty">Installed with no overrides — the chart's own defaults, unchanged.</p>
            {:else}
                <p class="empty">This chart declares no values.</p>
            {/if}
        </details>

        {#if detail.notes}
            <details class="fold">
                <summary><span>Notes</span></summary>
                <pre class="selectable notes">{detail.notes}</pre>
            </details>
        {/if}

        <details class="fold" open>
            <summary>
                <span>Resources</span>
                <span class="count">{resources.length}</span>
            </summary>

            {#if resources.length > 0}
                <table>
                    <tbody>
                        {#each resources as resource (resource.apiVersion + resource.kind + resource.namespace + resource.name)}
                            <tr>
                                <td class="kind">{resource.kind}</td>
                                <td class="selectable">{resource.name}</td>
                                <td class="ns">{resource.namespace || '—'}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            {:else}
                <p class="empty">This release rendered no objects.</p>
            {/if}
        </details>

        <details class="fold">
            <summary>
                <span>History</span>
                <span class="count">{revisions.length}</span>
            </summary>

            <table>
                <tbody>
                    {#each revisions as revision (revision.revision)}
                        <tr class:current={revision.current}>
                            <td class="rev">{revision.revision}</td>
                            <td class={toneOf(revision.status)}>{revision.status}</td>
                            <td class="ns">{revision.chart}</td>
                            <td class="selectable">{revision.description}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </details>
    </div>
{/if}

<style>
    .release {
        padding: 12px 14px 20px;
        display: flex;
        flex-direction: column;
        gap: 14px;
        min-width: 0;
    }

    .status {
        padding: 18px 16px;
        color: var(--text-dim);
    }

    /* The facts, as label-over-value pairs that wrap rather than a two-column
       grid: the panel is resizable down to something narrow, and a fixed
       column would truncate a chart name long before it had to. */
    .facts {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
        gap: 10px 16px;
        margin: 0;
    }

    .facts div {
        min-width: 0;
    }

    dt {
        font-size: 10px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    dd {
        margin: 2px 0 0;
        font-size: 12px;
        overflow-wrap: anywhere;
    }

    /* The four tones the tables use, so a status is the same colour wherever
       it is met. */
    dd.ok,
    td.ok {
        color: var(--ok);
    }

    dd.warn,
    td.warn {
        color: var(--warn);
    }

    dd.error,
    td.error {
        color: var(--error);
    }

    dd.info,
    td.info {
        color: var(--text-dim);
    }

    .fold {
        border-top: 1px solid var(--border);
        padding-top: 10px;
        min-width: 0;
    }

    summary {
        display: flex;
        align-items: baseline;
        gap: 8px;
        cursor: pointer;
        font-size: 11px;
        font-weight: 600;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-dim);
        list-style-position: outside;
    }

    summary:hover {
        color: var(--text);
    }

    .count {
        font-weight: 400;
        letter-spacing: 0;
        text-transform: none;
        color: var(--text-faint);
    }

    .tools {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        margin: 10px 0 6px;
    }

    .toggle {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 11px;
        color: var(--text-dim);
        cursor: pointer;
    }

    .find {
        display: flex;
        align-items: center;
        gap: 5px;
        height: 22px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
    }

    .find:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* The values in the editor's own frame, capped so a long file scrolls
       inside the fold rather than growing the panel. */
    .doc {
        margin: 8px 0 0;
        border: 1px solid var(--border-soft);
        border-radius: var(--radius-sm);
        overflow: hidden;
    }

    .doc :global(.cm-editor) {
        max-height: 420px;
        font-size: 11px;
    }

    /* Values and notes are documents rather than fields: they keep their own
       whitespace and scroll sideways inside the fold rather than forcing the
       panel to. */
    pre {
        margin: 8px 0 0;
        padding: 10px 12px;
        background: var(--bg);
        border: 1px solid var(--border-soft);
        border-radius: var(--radius-sm);
        font-family: var(--mono);
        font-size: 11px;
        line-height: 1.6;
        color: var(--text-dim);
        white-space: pre;
        overflow-x: auto;
        max-height: 420px;
    }

    pre.notes {
        white-space: pre-wrap;
        overflow-wrap: anywhere;
    }

    .empty {
        margin: 8px 0 0;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    table {
        width: 100%;
        margin-top: 8px;
        border-collapse: collapse;
        font-size: 11.5px;
    }

    td {
        padding: 3px 8px 3px 0;
        vertical-align: top;
        overflow-wrap: anywhere;
    }

    .kind,
    .rev {
        color: var(--text-faint);
        white-space: nowrap;
    }

    .ns {
        color: var(--text-dim);
    }

    /* The revision the release is actually on, which is the one the rest of the
       drawer is describing. */
    tr.current td {
        color: var(--text);
        font-weight: 600;
    }
</style>
