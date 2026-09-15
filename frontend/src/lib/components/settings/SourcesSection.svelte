<!--
  Where kubeconfigs come from: the files discovery found, the ones the user
  added, the folders being watched, and the ones they have hidden.

  The sidebar shows the same four lists and its own copies of these buttons.
  That is deliberate rather than an oversight -- adding a kubeconfig is a
  frequent thing and should stay one click away in the sidebar, while a full
  path only fits here. Both render from workspace.settings, so there is one
  piece of state under the two views and they cannot disagree.

  In the web version the sources are the server's, and they are shared by
  everyone signed in to it. A cluster is added there by uploading a kubeconfig
  on the administration page, not by picking a file off a disk the browser
  cannot see -- so the two pickers become a link to that page, offered to
  administrators, and everything that changes the lists is theirs alone.
-->
<script lang="ts">
    import { splitContextId } from '../../state/adopt';
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SettingsSection from './SettingsSection.svelte';

    // Discovery and the user's own additions are worth telling apart here, in a
    // way the sidebar has no room to: removing one is forgetting it, and
    // removing the other only hides it.
    let added = $derived(workspace.files.filter((f) => f.source === 'manual' && f.error === ''));
    let discovered = $derived(workspace.files.filter((f) => f.source !== 'manual' && f.error === ''));

    function contextCount(count: number): string {
        return `${count} context${count === 1 ? '' : 's'}`;
    }
</script>

<SettingsSection
    title="Kubeconfig sources"
    lede={session.server
        ? 'The clusters on this server, shared by everyone who signs in to it. An administrator adds them under Administration → Clusters.'
        : 'k8sdockside reads ~/.kube/config, every path in $KUBECONFIG, anything else in ~/.kube that parses as a kubeconfig, and whatever you add here. Your kubeconfig files are never modified.'}
>
    <div class="actions">
        {#if session.server}
            {#if session.admin && session.clustersUrl}
                <a class="primary" href={session.clustersUrl}>
                    <Icon name="server" size={14} /> Manage clusters
                </a>
            {/if}
        {:else}
            <button class="primary" onclick={() => workspace.addFile()}>
                <Icon name="plus" size={14} /> Add kubeconfigs
            </button>
            <button onclick={() => workspace.addFolder()}>
                <Icon name="folder-plus" size={14} /> Watch a folder
            </button>
        {/if}
        <button onclick={() => workspace.sync()} disabled={workspace.syncing}>
            <Icon name="refresh" size={14} />
            {workspace.syncing ? 'Syncing…' : 'Sync now'}
        </button>
    </div>

    {#if added.length > 0}
        <h3>Added by you</h3>
        <ul class="paths">
            {#each added as file (file.path)}
                <li>
                    <Icon name="file" size={13} />
                    <span class="path selectable">{file.path}</span>
                    <span class="count">{contextCount(file.contexts.length)}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removeFile(file.path)}
                            title="Stop tracking {file.path}"
                            aria-label="Stop tracking {file.path}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.folders.length > 0}
        <h3>Watched folders</h3>
        <p class="note">
            Scanned one level deep on every sync. The file names are ignored entirely — anything in there that parses
            as a kubeconfig is taken, so a config dropped in later appears without being added by hand.
        </p>
        <ul class="paths">
            {#each workspace.folders as folder (folder)}
                <li>
                    <Icon name="folder" size={13} />
                    <span class="path selectable">{folder}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removeFolder(folder)}
                            title="Stop watching {folder}"
                            aria-label="Stop watching {folder}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if discovered.length > 0}
        <h3>Found on this machine</h3>
        <ul class="paths">
            {#each discovered as file (file.path)}
                <li>
                    <Icon name="file" size={13} />
                    <span class="path selectable">{file.path}</span>
                    <span class="count">{contextCount(file.contexts.length)}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removeFile(file.path)}
                            title="Hide {file.path}"
                            aria-label="Hide {file.path}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.excluded.length > 0 || workspace.removedContexts.length > 0}
        <h3>Hidden</h3>
        <p class="note">
            Discovery would find these again on the next sync, so refusing one has to be remembered rather than
            forgotten — a rescan will not bring them back. Restore any of them here.
        </p>
        <ul class="paths">
            {#each workspace.excluded as path (path)}
                <li>
                    <Icon name="file" size={13} />
                    <span class="path selectable dim">{path}</span>
                    {#if session.admin}
                        <button
                            class="drop restore"
                            onclick={() => workspace.restoreFile(path)}
                            title="Show {path} again"
                            aria-label="Show {path} again"
                        >
                            <Icon name="undo" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
            <!-- A removed context belongs under the same heading: it is the
                 same act on a smaller thing, and it was the one with nowhere
                 to undo it. -->
            {#each workspace.removedContexts as id (id)}
                {@const context = splitContextId(id)}
                <li>
                    <Icon name="server" size={13} />
                    <span class="path selectable dim">{context.name}</span>
                    <span class="reason">in {context.file}</span>
                    {#if session.admin}
                        <button
                            class="drop restore"
                            onclick={() => workspace.restoreContext(id)}
                            title="Show {context.name} again"
                            aria-label="Show context {context.name} again"
                        >
                            <Icon name="undo" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.brokenFiles.length > 0}
        <h3>Could not be read</h3>
        <ul class="paths">
            {#each workspace.brokenFiles as file (file.path)}
                <li class="broken">
                    <Icon name="alert" size={13} />
                    <span class="path selectable">{file.path}</span>
                    <span class="reason">{file.error}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removeFile(file.path)}
                            title="Hide {file.path}"
                            aria-label="Hide {file.path}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.loaded && workspace.files.length === 0 && workspace.excluded.length === 0 && workspace.removedContexts.length === 0}
        <p class="empty">
            {#if session.server}
                No clusters yet. An administrator adds them under Administration → Clusters.
            {:else}
                No kubeconfig found. Add files above, or point k8sdockside at a folder and it will take every one in
                there — whatever they are named.
            {/if}
        </p>
    {/if}
</SettingsSection>

<style>
    .actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin: 14px 0 4px;
    }

    .actions button,
    .actions a {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 6px 12px;
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        color: var(--text);
        text-decoration: none;
    }

    .actions button:hover:not(:disabled) {
        background: var(--bg-hover);
    }

    .actions button:disabled {
        opacity: 0.5;
        cursor: default;
    }

    .actions .primary {
        background: var(--accent);
        color: var(--accent-text);
        box-shadow: none;
    }

    .actions .primary:hover:not(:disabled) {
        filter: brightness(1.08);
        background: var(--accent);
    }

    h3 {
        margin: 24px 0 6px;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .note {
        margin: 0 0 8px;
        font-size: 11.5px;
        line-height: 1.6;
        color: var(--text-faint);
        max-width: 62ch;
    }

    .paths {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 1px;
    }

    .paths li {
        display: flex;
        align-items: center;
        gap: 9px;
        padding: 7px 9px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-size: 12px;
        min-width: 0;
    }

    .paths li:hover {
        background: var(--bg-raised);
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
        /* Read right-to-left so a long path keeps its file name rather than
           its leading /Users/… */
        direction: rtl;
        text-align: left;
    }

    .path.dim {
        color: var(--text-faint);
    }

    .count,
    .reason {
        flex: 0 0 auto;
        font-size: 11px;
        color: var(--text-faint);
    }

    .broken .reason {
        color: var(--error);
        max-width: 30ch;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .broken > :global(svg) {
        color: var(--error);
    }

    .drop {
        display: grid;
        place-items: center;
        flex: 0 0 auto;
        width: 20px;
        height: 20px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .drop:hover {
        background: var(--bg-hover);
        color: var(--error);
    }

    .drop.restore:hover {
        color: var(--ok);
    }

    .empty {
        margin: 18px 0 0;
        font-size: 12px;
        line-height: 1.7;
        color: var(--text-faint);
        max-width: 58ch;
    }
</style>
