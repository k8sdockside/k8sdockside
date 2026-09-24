<!--
  The bell in the title bar.

  It is there all the time so that news has a fixed place to arrive. A banner
  across the window would be the app interrupting, and a line in the status bar
  is gone the moment the next notice replaces it. A dot on the bell says there
  is something unread; opening it shows what; "Mark as read" puts the dot away
  for that release, and it stays away across restarts until a newer one is out.

  It carries two kinds of news: what has got worse on a connected cluster --
  the alerts the fleet store raises, see fleet.svelte.ts -- and a new release.
  The cluster alerts come first, because they are about something happening
  now. Drawn by the app rather than the platform for the reason the menus are
  -- see MenuBar.svelte.

  The web version is upgraded by whoever runs it, with Helm, not from a
  browser. So there the bell never asks GitHub on its own: it says which
  version the server runs, checks when somebody presses Check now -- unless
  the operator switched that off -- and a newer release is shown as work for
  whoever runs the server, with no download.
-->
<script lang="ts">
    import { formatDate, formatDateTime, formatTime } from '../datetime.svelte';
    import { detail } from '../state/detail.svelte';
    import type { AlertItem } from '../fleet/alerts';
    import { isSnoozed, snoozeChoices } from '../fleet/snooze';
    import { rememberSection } from './settings/section.svelte';
    import { onMount, untrack } from 'svelte';
    import { session } from '../state/session.svelte';
    import { updates } from '../state/updates.svelte';
    import { fleet, type ClusterAlert } from '../state/fleet.svelte';
    import { DASHBOARD } from '../catalogue';
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
    const unread = $derived(fleet.unread + (updates.unread ? 1 : 0));
    const label = $derived(unread > 0 ? `Notifications, ${unread} unread` : 'Notifications');
    /** The unread dot takes the colour of the worst unread news. */
    const dot = $derived(
        fleet.alerts.some((a) => !a.read && a.tone === 'error')
            ? 'error'
            : fleet.unread > 0
              ? 'warn'
              : updates.unread
                ? 'news'
                : null,
    );

    function clusterName(contextId: string): string {
        const context = workspace.contexts.find((c) => c.id === contextId);
        return context ? workspace.displayName(context) : contextId;
    }

    /** Whether the snooze choices are showing. */
    let choosingSnooze = $state(false);

    // Read as the panel opens, so a snooze that has run out since stops showing.
    let snoozed = $derived(open && isSnoozed(workspace.alertsSnoozedUntil));

    function snooze(until: Date | null): void {
        workspace.snoozeAlerts(until);
        choosingSnooze = false;
    }

    /** The alert whose details are open, if any. One at a time. */
    let expanded = $state<string | null>(null);

    /** How many of an alert's objects are listed before "and N more". */
    const ITEMS_SHOWN = 12;

    function toggleAlert(alert: ClusterAlert): void {
        expanded = expanded === alert.id ? null : alert.id;
        if (expanded) fleet.markRead(alert.id);
    }

    /** Opens one object an alert is about, in the detail panel. */
    function openItem(alert: ClusterAlert, item: AlertItem): void {
        if (!item.ref) return;
        close();
        void detail.open({ contextId: alert.contextId, ...item.ref });
    }

    /** Goes where the alert's details point: its list, or the dashboard. */
    function follow(alert: ClusterAlert): void {
        close();
        const action = alert.action;
        if (action.kind === 'list') {
            if (action.list === 'pods') workspace.showPodsMatching(alert.contextId, action.query ?? '');
            else workspace.openTab(alert.contextId, action.list);
        } else {
            workspace.openTab(alert.contextId, DASHBOARD);
        }
    }

    function openDashboard(alert: ClusterAlert): void {
        close();
        workspace.openTab(alert.contextId, DASHBOARD);
    }

    // A clicked system notification opens the bell on that alert, with its
    // details showing and in view.
    // Only a request made after the bell was drawn: one from before -- a
    // notification clicked an hour ago -- must not open it again.
    let revealSeen = untrack(() => fleet.revealed?.nonce ?? 0);
    $effect(() => {
        const request = fleet.revealed;
        if (!request || request.nonce === revealSeen) return;
        revealSeen = request.nonce;
        open = true;
        expanded = request.id;
        requestAnimationFrame(() => {
            panelEl?.querySelector(`[data-alert="${CSS.escape(request.id)}"]`)?.scrollIntoView({ block: 'nearest' });
        });
    });

    function openFleet(): void {
        close();
        fleet.markAllRead();
        workspace.openFleet();
    }

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
        return formatDate(iso);
    }

    /** A time of day, for "checked at". */
    function timeOf(iso: string): string {
        return formatTime(iso);
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
        {#if dot}<span class="badge {dot}"></span>{/if}
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
            <div class="heading-row">
                <p class="heading">Clusters</p>
                {#if workspace.alertMode !== 'off'}
                    {#if snoozed}
                        <button class="snooze-toggle on" onclick={() => snooze(null)} title="Alerts are snoozed; resume them now">
                            <Icon name="pause" size={11} /> Snoozed · Resume
                        </button>
                    {:else}
                        <button
                            class="snooze-toggle"
                            onclick={() => (choosingSnooze = !choosingSnooze)}
                            aria-expanded={choosingSnooze}
                            title="A while without being told about cluster alerts"
                        >
                            <Icon name="pause" size={11} /> Snooze
                        </button>
                    {/if}
                {/if}
            </div>
            {#if choosingSnooze && !snoozed}
                <div class="snooze-choices" role="group" aria-label="Snooze cluster alerts">
                    {#each snoozeChoices() as choice (choice.label)}
                        <button onclick={() => snooze(choice.until)}>{choice.label}</button>
                    {/each}
                </div>
            {/if}
            {#if snoozed}
                <p class="snooze-note">
                    Snoozed until {formatDateTime(workspace.alertsSnoozedUntil)}. Alerts are still kept here, but raise no
                    notification and no dot.
                </p>
            {/if}

            {#if fleet.alerts.length > 0}
                <ul class="alerts">
                    {#each fleet.alerts as alert (alert.id)}
                        {@const isOpen = expanded === alert.id}
                        <li class="alert {alert.tone}" class:unread={!alert.read} class:expanded={isOpen} data-alert={alert.id}>
                            <div class="alert-head">
                                <button
                                    class="open"
                                    onclick={() => toggleAlert(alert)}
                                    aria-expanded={isOpen}
                                    title={isOpen ? 'Hide the details' : 'Show the details'}
                                >
                                    <span class="mark" aria-hidden="true"></span>
                                    <span class="text">
                                        <span class="title">{alert.title}</span>
                                        <span class="detail">
                                            {clusterName(alert.contextId)} · {timeOf(new Date(alert.at).toISOString())}
                                        </span>
                                        {#if alert.body && !isOpen}<span class="detail body">{alert.body}</span>{/if}
                                    </span>
                                    <Icon name={isOpen ? 'chevron-up' : 'chevron-down'} size={12} />
                                </button>
                                <button class="dismiss" onclick={() => fleet.dismiss(alert.id)} aria-label="Dismiss" title="Dismiss">
                                    <Icon name="close" size={11} />
                                </button>
                            </div>
                            {#if isOpen}
                                <!-- The details: the whole of what it said, when
                                     and where, every object it is about -- the
                                     notification could only name three -- and
                                     where to go next. -->
                                <div class="alert-details">
                                    {#if alert.body}<p class="full">{alert.body}</p>{/if}
                                    <dl>
                                        <div><dt>Cluster</dt><dd>{clusterName(alert.contextId)}</dd></div>
                                        <div><dt>When</dt><dd>{formatDateTime(alert.at, { seconds: true })}</dd></div>
                                    </dl>
                                    {#if alert.items.length > 0}
                                        <ul class="items">
                                            {#each alert.items.slice(0, ITEMS_SHOWN) as item, i (i)}
                                                <li>
                                                    {#if item.ref}
                                                        <button class="item-link" onclick={() => openItem(alert, item)} title="Open {item.label}">
                                                            <span class="item-label">{item.label}</span>
                                                            {#if item.detail}<span class="item-detail">{item.detail}</span>{/if}
                                                        </button>
                                                    {:else}
                                                        <span class="item-label">{item.label}</span>
                                                        {#if item.detail}<span class="item-detail">{item.detail}</span>{/if}
                                                    {/if}
                                                </li>
                                            {/each}
                                        </ul>
                                        {#if alert.items.length > ITEMS_SHOWN}
                                            <p class="more">And {alert.items.length - ITEMS_SHOWN} more.</p>
                                        {/if}
                                    {/if}
                                    <div class="actions">
                                        <button onclick={() => follow(alert)}><Icon name="link" size={12} /> {alert.action.label}</button>
                                        {#if alert.action.kind !== 'dashboard'}
                                            <button onclick={() => openDashboard(alert)}><Icon name="dashboard" size={12} /> Dashboard</button>
                                        {/if}
                                    </div>
                                </div>
                            {/if}
                        </li>
                    {/each}
                </ul>
                <div class="row-actions">
                    <button onclick={openFleet}><Icon name="gauge" size={12} /> Fleet health</button>
                    {#if fleet.unread > 0}
                        <button onclick={() => fleet.markAllRead()}><Icon name="check" size={12} /> Mark all read</button>
                    {/if}
                    <button onclick={() => fleet.clear()}><Icon name="trash" size={12} /> Clear</button>
                </div>
            {:else if workspace.alertMode === 'off'}
                <p class="empty">
                    Cluster alerts are off. The marks in the sidebar still show what is wrong.
                    <button class="inline" onclick={() => { close(); rememberSection('notifications'); workspace.openSettings(); }}>Settings</button>
                </p>
            {:else}
                <p class="empty">
                    Nothing has changed for the worse on a connected cluster.
                    <button class="inline" onclick={openFleet}>Fleet health</button>
                </p>
            {/if}

            <p class="heading releases">Releases</p>

            {#if updates.available && latest}
                <article class="item" class:unread={updates.unread}>
                    <span class="mark" aria-hidden="true"></span>
                    <div class="text">
                        <p class="title">K8s Dockside {latest.version} is available</p>
                        <p class="detail">
                            {session.server ? 'This server runs' : 'You have'} {updates.status.current}{#if dayOf(latest.publishedAt)}
                                · released {dayOf(latest.publishedAt)}{/if}
                        </p>
                        <!-- Upgraded by whoever runs it, not from here: say how. -->
                        {#if session.server}
                            <p class="detail">
                                Whoever runs this server upgrades it, usually with
                                <code>helm upgrade</code> to the new chart version.
                            </p>
                        {/if}
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
                    {#if updates.checking}
                        Checking for updates…
                    {:else if updates.status.error}
                        Could not check for updates.
                    {:else if latest}
                        You're up to date. {latest.version} is the latest release.
                    {:else if session.server}
                        <!-- The web version never asks on its own: it says what it
                             runs, and asks when somebody presses the button. -->
                        This server runs K8s Dockside {updates.status.current || 'of an unknown version'}.
                        {#if updates.canCheck}Check now to see whether a newer release is out.{/if}
                    {:else if workspace.checkForUpdates}
                        Nothing yet.
                    {:else}
                        Update checks are off. Turn them on under Settings › Notifications, or check now.
                    {/if}
                </p>
            {/if}

            <footer>
                {#if updates.status.error}
                    <span class="problem" title={updates.status.error}>{updates.status.error}</span>
                {:else if updates.status.checkedAt}
                    <span class="checked">Checked at {timeOf(updates.status.checkedAt)}</span>
                {:else}
                    <span class="checked"></span>
                {/if}
                {#if updates.canCheck}
                    <button class="check" disabled={updates.checking} onclick={() => void updates.check()}>
                        <Icon name="refresh" size={12} />
                        {updates.checking ? 'Checking…' : 'Check now'}
                    </button>
                {:else}
                    <!-- The operator keeps this server off the internet. -->
                    <span class="checked">Version {updates.status.current}</span>
                {/if}
            </footer>
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

    /* A cluster alert takes a status colour: unlike a release, it is a fault. */
    .badge.warn {
        background: var(--warn);
    }

    .badge.error {
        background: var(--error);
    }

    .panel {
        position: absolute;
        outline: none;
        top: calc(100% + 4px);
        /* Anchored to the trigger's right edge: this sits at the right end of
           the title bar, and a panel growing rightwards would leave the window. */
        right: 0;
        width: 360px;
        max-height: min(560px, 80vh);
        overflow: auto;
        padding: 6px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    }

    .heading-row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
    }

    .snooze-toggle {
        display: inline-flex;
        align-items: center;
        gap: 4px;
        margin: 0 4px 4px 0;
        padding: 2px 8px;
        font-size: 11px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .snooze-toggle:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .snooze-toggle.on {
        color: var(--warn);
        background: color-mix(in srgb, var(--warn) 12%, transparent);
    }

    .snooze-choices {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin: 0 0 8px;
    }

    .snooze-choices button {
        padding: 4px 9px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11px;
        color: var(--text-dim);
    }

    .snooze-choices button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .snooze-note {
        margin: 0 6px 8px;
        font-size: 11px;
        line-height: 1.45;
        color: var(--text-faint);
    }

    .heading.releases {
        margin-top: 12px;
    }

    .alerts {
        list-style: none;
        margin: 0;
        padding: 0;
        display: grid;
        gap: 2px;
    }

    .alert {
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
    }

    .alert.expanded {
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .alert-head {
        display: flex;
        align-items: flex-start;
    }

    .alert-details {
        padding: 0 10px 10px 22px;
        display: flex;
        flex-direction: column;
        gap: 8px;
        font-size: 11.5px;
    }

    .alert-details .full {
        margin: 0;
        color: var(--text);
        line-height: 1.45;
        overflow-wrap: anywhere;
    }

    .alert-details dl {
        margin: 0;
        display: flex;
        flex-wrap: wrap;
        gap: 2px 14px;
    }

    .alert-details dl div {
        display: flex;
        gap: 6px;
    }

    .alert-details dt {
        color: var(--text-faint);
    }

    .alert-details dd {
        margin: 0;
        color: var(--text-dim);
    }

    .items {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 2px;
    }

    .items li > .item-label,
    .items li > .item-detail {
        display: block;
        padding: 0 6px;
    }

    .item-link {
        display: flex;
        flex-direction: column;
        align-items: flex-start;
        width: 100%;
        padding: 4px 6px;
        text-align: left;
        border-radius: var(--radius-sm);
        font: inherit;
        color: inherit;
    }

    .item-link:hover {
        background: var(--bg-hover);
    }

    .item-label {
        font-family: var(--mono);
        font-size: 11.5px;
        color: var(--text);
    }

    .item-link:hover .item-label {
        color: var(--accent);
    }

    .item-detail {
        font-size: 11px;
        color: var(--text-faint);
        overflow-wrap: anywhere;
    }

    .more {
        margin: 0;
        color: var(--text-faint);
    }

    .alert .open {
        flex: 1;
        min-width: 0;
        display: grid;
        grid-template-columns: 10px minmax(0, 1fr) auto;
        align-items: start;
        gap: 6px;
        padding: 7px 4px 7px 6px;
        text-align: left;
        font: inherit;
        color: inherit;
        background: none;
        border: 0;
        cursor: pointer;
        border-radius: var(--radius-sm);
    }

    .alert .open:hover {
        background: var(--bg-hover);
    }

    .alert .open > :global(svg) {
        margin-top: 3px;
        color: var(--text-faint);
    }

    .alert .text {
        display: flex;
        flex-direction: column;
    }

    .alert.unread .mark {
        background: var(--warn);
    }

    .alert.error.unread .mark {
        background: var(--error);
    }

    .alert.error .title {
        color: var(--error);
    }

    .alert .body {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .dismiss {
        flex: none;
        display: grid;
        place-items: center;
        width: 22px;
        height: 22px;
        margin: 4px 4px 0 0;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .dismiss:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .row-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 6px;
    }

    .row-actions button {
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

    .row-actions button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .inline {
        font: inherit;
        color: var(--accent);
        text-decoration: underline;
        text-underline-offset: 2px;
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

    .item.unread .title,
    .alert.unread .title {
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
