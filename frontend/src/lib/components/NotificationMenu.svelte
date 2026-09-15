<!--
  The bell in the title bar.

  It is there all the time so that news has a fixed place to arrive. A banner
  across the window would be the app interrupting, and a line in the status bar
  is gone the moment the next notice replaces it. A dot on the bell says there
  is something unread; opening it shows what; "Mark as read" puts the dot away
  for that release, and it stays away across restarts until a newer one is out.

  The only news it carries today is a new release. Drawn by the app rather than
  the platform for the reason the View menu is -- see ViewMenu.svelte.

  The web version keeps the bell, as the fixed place news would arrive, but has
  no release news to put in it: the server is upgraded by whoever runs it, not
  from a browser. So there it is quiet, and offers no check.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import { session } from '../state/session.svelte';
    import { updates } from '../state/updates.svelte';
    import { workspace } from '../state/workspace.svelte';
    import Icon from './Icon.svelte';
    import { notices } from '../state/notices.svelte';

    let open = $state(false);
    let panelEl = $state<HTMLElement | null>(null);
    let buttonEl = $state<HTMLButtonElement | null>(null);

    // The backend is asked what it knows as the bell mounts, which is as the
    // window opens. The first automatic check follows a few seconds later and
    // arrives as a push, so this only recovers what an earlier one found.
    onMount(() => {
        void updates.load();
    });

    const latest = $derived(updates.latest);
    const label = $derived(updates.unread ? 'Notifications, 1 unread' : 'Notifications');

    function close(): void {
        open = false;
    }

    function onKeyDown(event: KeyboardEvent): void {
        if (event.key !== 'Escape') return;
        event.stopPropagation();
        close();
        buttonEl?.focus();
    }

    async function markRead(): Promise<void> {
        try {
            await updates.markRead();
        } catch (err) {
            notices.fail(`Could not mark the notification as read: ${err instanceof Error ? err.message : String(err)}`);
        }
    }

    async function openRelease(): Promise<void> {
        close();
        try {
            await updates.openRelease();
        } catch {
            notices.fail('Could not open the release page');
        }
    }

    async function openDownload(): Promise<void> {
        close();
        try {
            await updates.openDownload();
        } catch {
            notices.fail('Could not open the download');
        }
    }

    /** A date as the user would write it, or nothing for one that is not one. */
    function dayOf(iso: string): string {
        const date = new Date(iso);
        if (Number.isNaN(date.getTime())) return '';
        return date.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
    }

    /** A time of day, for "checked at". */
    function timeOf(iso: string): string {
        const date = new Date(iso);
        if (Number.isNaN(date.getTime())) return '';
        return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    }

    // Focus the first control, so the panel is usable without the mouse that
    // opened it.
    $effect(() => {
        if (open && panelEl) panelEl.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
    });
</script>

<svelte:window onclick={close} onresize={close} />

<!-- The click that opens the panel must not reach the window handler that
     closes it again, and neither must a click inside it. Everything in here
     is a button, so there is nothing for a keyboard to need a handler for. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="host" onclick={(e) => e.stopPropagation()}>
    <button
        class="trigger"
        bind:this={buttonEl}
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        onclick={() => (open = !open)}
    >
        <Icon name="bell" size={15} />
        {#if updates.unread}<span class="badge"></span>{/if}
    </button>

    {#if open}
        <!-- Focusable so that focus has somewhere to land inside it if every
             button is disabled; the effect above prefers a button. -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
            class="panel"
            role="dialog"
            aria-label="Notifications"
            tabindex="-1"
            bind:this={panelEl}
            onkeydown={onKeyDown}
        >
            <p class="heading">Notifications</p>

            {#if updates.available && latest}
                <article class="item" class:unread={updates.unread}>
                    <span class="mark" aria-hidden="true"></span>
                    <div class="text">
                        <p class="title">K8s Dockside {latest.version} is available</p>
                        <p class="detail">
                            You have {updates.status.current}{#if dayOf(latest.publishedAt)}
                                · released {dayOf(latest.publishedAt)}{/if}
                        </p>
                    </div>
                    <div class="actions">
                        <!-- The file for this install, when the release has
                             one. The backend chose it from what this build
                             is, which the tooltip says. -->
                        {#if updates.download}
                            <button onclick={openDownload} title="{updates.downloadName} — for this install: {updates.status.install}">
                                <Icon name="download" size={12} /> Download for {updates.status.install}
                            </button>
                        {/if}
                        <button onclick={openRelease}><Icon name="link" size={12} /> View release</button>
                        {#if updates.unread}
                            <button onclick={markRead}><Icon name="check" size={12} /> Mark as read</button>
                        {/if}
                    </div>
                </article>
            {:else}
                <p class="empty">
                    {#if session.server}
                        Nothing yet.
                    {:else if updates.checking}
                        Checking for updates…
                    {:else if updates.status.error}
                        Could not check for updates.
                    {:else if latest}
                        You're up to date. {latest.version} is the latest release.
                    {:else if workspace.checkForUpdates}
                        Nothing yet.
                    {:else}
                        Update checks are off. Turn them on under Settings › Behaviour, or check now.
                    {/if}
                </p>
            {/if}

            {#if !session.server}
            <footer>
                {#if updates.status.error}
                    <span class="problem" title={updates.status.error}>{updates.status.error}</span>
                {:else if updates.status.checkedAt}
                    <span class="checked">Checked at {timeOf(updates.status.checkedAt)}</span>
                {:else}
                    <span class="checked"></span>
                {/if}
                <button class="check" disabled={updates.checking} onclick={() => void updates.check()}>
                    <Icon name="refresh" size={12} />
                    {updates.checking ? 'Checking…' : 'Check now'}
                </button>
            </footer>
            {/if}
        </div>
    {/if}
</div>

<style>
    .host {
        position: relative;
        display: flex;
        align-items: center;
        /* The title bar drags the window; a control in it must not. */
        --wails-draggable: no-drag;
        z-index: 5;
    }

    .trigger {
        position: relative;
        display: grid;
        place-items: center;
        width: 26px;
        height: 24px;
        border-radius: var(--radius-sm);
        color: var(--text-dim);
    }

    .trigger:hover,
    .trigger[aria-expanded='true'] {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* The unread dot. Accent rather than a status colour: a release is news,
       not a fault. */
    .badge {
        position: absolute;
        top: 4px;
        right: 5px;
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--accent);
        box-shadow: 0 0 0 2px var(--bg-sidebar);
    }

    .panel {
        position: absolute;
        outline: none;
        top: calc(100% + 4px);
        /* Anchored to the trigger's right edge for the reason the View menu
           is: this sits at the right end of the title bar. */
        right: 0;
        width: 320px;
        padding: 6px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    }

    .heading {
        margin: 2px 6px 6px;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .item {
        display: grid;
        grid-template-columns: 10px 1fr;
        gap: 4px 6px;
        padding: 8px 8px 8px 6px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
    }

    /* A fixed column for the dot, so the text does not shift when it goes. */
    .mark {
        width: 6px;
        height: 6px;
        margin-top: 5px;
        border-radius: 50%;
        justify-self: center;
    }

    .item.unread .mark {
        background: var(--accent);
    }

    .text {
        min-width: 0;
    }

    .title {
        margin: 0;
        font-size: 12.5px;
        color: var(--text);
    }

    .item.unread .title {
        font-weight: 600;
    }

    .detail {
        margin: 2px 0 0;
        font-size: 11px;
        color: var(--text-dim);
    }

    .actions {
        grid-column: 2;
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 4px;
    }

    .actions button,
    .check {
        display: flex;
        align-items: center;
        gap: 5px;
        padding: 4px 9px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11px;
        color: var(--text-dim);
    }

    .actions button:hover,
    .check:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .check:disabled {
        opacity: 0.5;
        cursor: default;
    }

    .empty {
        margin: 0;
        padding: 10px 8px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-size: 12px;
        line-height: 1.5;
        color: var(--text-dim);
    }

    footer {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        margin-top: 6px;
        padding: 2px 2px 2px 6px;
    }

    .checked,
    .problem {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-size: 11px;
        color: var(--text-faint);
    }

    .problem {
        color: var(--error);
    }

    .check {
        flex: 0 0 auto;
    }
</style>
