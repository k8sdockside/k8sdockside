<!--
  What the app does on its own: what it reopens at launch, which parts of the
  sidebar tree start folded, whether it asks before dropping a kubeconfig
  source, whether it looks for new releases, and whether cluster alerts become
  system notifications.

  Where a panel appears is not here any more. Every view is a tab now, including
  the describe panel, and a tab's place is where it was dragged to -- a
  preference for it would be a second answer to a question the window already
  asks directly.
-->
<script lang="ts">
    import { NAV_GROUPS } from '../../catalogue';
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import Toggle from './Toggle.svelte';
    import * as NotifyService from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/notifyservice.js';

    let overrides = $derived(workspace.foldingOverrideCount);

    /**
     * Whether the system will take the app's notifications, asked when this
     * section is shown. Null until it has answered, or when it could not be
     * asked at all.
     */
    let notifyStatus = $state<{ available: boolean; authorized: boolean; reason: string } | null>(null);
    $effect(() => {
        if (session.server) return;
        let live = true;
        (async () => {
            try {
                const status = await NotifyService.Status();
                if (live) notifyStatus = status;
            } catch {
                // Said as nothing: the toggle still works for the bell.
            }
        })();
        return () => {
            live = false;
        };
    });

    async function setNotifications(on: boolean): Promise<void> {
        workspace.setDesktopNotifications(on);
        if (!on || !notifyStatus?.available || notifyStatus.authorized) return;
        // Asked as it is turned on, when the answer is wanted.
        try {
            const authorized = await NotifyService.RequestPermission();
            notifyStatus = { ...notifyStatus, authorized };
        } catch {
            // The status line says what the system thinks.
        }
    }

    let notifyHint = $derived.by(() => {
        const base =
            'When a connected cluster gets worse — a node not ready, pods crashing or evicted, credentials about to expire — say so as a system notification, as well as on the bell. Only clusters connected in this window are watched.';
        if (!notifyStatus) return base;
        if (!notifyStatus.available) return `${base} This build cannot post them: ${notifyStatus.reason}`;
        if (!notifyStatus.authorized) return `${base} The system has not allowed them yet; turning this on asks.`;
        return base;
    });
</script>

<SettingsSection title="Behaviour">
    <SettingsRow
        label="Restore tabs at launch"
        hint="Reopens the tabs you left open. Turned off, k8sdockside starts empty — but the order is kept, so turning it back on restores the session you switched it off during."
    >
        <Toggle
            checked={workspace.restoreTabsOnLaunch}
            label="Restore tabs at launch"
            onchange={(v) => workspace.setRestoreTabs(v)}
        />
    </SettingsRow>

    <SettingsRow
        label="Ask before removing a source"
        hint="Confirms before hiding a kubeconfig or dropping a watched folder. Both are already undoable from the lists under Kubeconfig sources, so this is off unless you would rather not have to undo."
    >
        <Toggle
            checked={workspace.confirmSourceRemoval}
            label="Ask before removing a kubeconfig source"
            onchange={(v) => workspace.setConfirmSourceRemoval(v)}
        />
    </SettingsRow>

    <!-- Not in the web version, which is upgraded by whoever runs the server
         and never asks GitHub anything on a user's behalf. -->
    {#if !session.server}
        <SettingsRow
            label="Check for new versions"
            hint="Asks GitHub shortly after launch, and every six hours after, whether a newer release is out, and says so on the bell in the title bar. The request carries nothing but the app's name and version. Off, the About page can still check when you ask it to."
        >
            <Toggle
                checked={workspace.checkForUpdates}
                label="Check for new versions"
                onchange={(v) => workspace.setCheckForUpdates(v)}
            />
        </SettingsRow>

        <SettingsRow label="Cluster alerts as notifications" hint={notifyHint}>
            <Toggle
                checked={workspace.desktopNotifications}
                label="Cluster alerts as notifications"
                onchange={(v) => void setNotifications(v)}
            />
        </SettingsRow>
    {/if}
</SettingsSection>

<div class="folding">
    <h3>Sidebar groups folded by default</h3>
    <p class="note">
        Which headings start closed under a context. A cluster you fold in the sidebar keeps its own answer from then
        on; this is the one every other cluster follows.
    </p>

    <ul class="groups">
        {#each NAV_GROUPS as group (group.label)}
            {@const folded = workspace.collapsedGroups.includes(group.label)}
            <li>
                <span class="group-label">{group.label}</span>
                <span class="state">{folded ? 'Folded' : 'Open'}</span>
                <Toggle
                    checked={folded}
                    label="Fold {group.label} by default"
                    onchange={() => workspace.toggleDefaultGroup(group.label)}
                />
            </li>
        {/each}
    </ul>

    <div class="bulk">
        <button onclick={() => workspace.setDefaultCollapsedGroups(NAV_GROUPS.map((g) => g.label))}>Fold all</button>
        <button onclick={() => workspace.setDefaultCollapsedGroups([])}>Open all</button>
        {#if overrides > 0}
            <button class="clear" onclick={() => workspace.clearAllFoldingOverrides()}>
                Clear {overrides} context override{overrides === 1 ? '' : 's'}
            </button>
        {/if}
    </div>
    {#if overrides > 0}
        <p class="note">
            {overrides}
            {overrides === 1 ? 'context has' : 'contexts have'} folded their groups differently and ignore the default
            above. Clearing returns {overrides === 1 ? 'it' : 'them'} to following it.
        </p>
    {/if}
</div>

<style>
    .folding {
        max-width: 760px;
        margin-top: 32px;
        padding-top: 20px;
        border-top: 1px solid var(--border);
    }

    h3 {
        margin: 0 0 4px;
        font-size: 13px;
        font-weight: 600;
    }

    .note {
        margin: 0 0 12px;
        font-size: 11.5px;
        line-height: 1.6;
        color: var(--text-faint);
        max-width: 62ch;
    }

    .groups {
        list-style: none;
        margin: 0 0 14px;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 1px;
    }

    .groups li {
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 7px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
    }

    .groups li:hover {
        background: var(--bg-raised);
    }

    .group-label {
        flex: 1 1 auto;
        font-size: 12.5px;
        color: var(--text);
    }

    .state {
        flex: 0 0 auto;
        font-size: 11px;
        color: var(--text-faint);
        min-width: 46px;
        text-align: right;
    }

    .bulk {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 12px;
    }

    .bulk button {
        padding: 5px 11px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .bulk button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .bulk .clear:hover {
        color: var(--warn);
    }
</style>
