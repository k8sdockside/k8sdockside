<!--
  What the app tells you about, and how: the cluster alerts -- on the bell and
  as the system's notifications, on the bell only, or not at all -- a snooze of
  them, whether the system will show them, and whether the app looks for new
  releases to put on the bell.
-->
<script lang="ts">
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import Toggle from './Toggle.svelte';
    import SegmentedControl from './SegmentedControl.svelte';
    import type { AlertMode } from '../../state/adopt';
    import { isSnoozed, snoozeChoices } from '../../fleet/snooze';
    import { formatDateTime } from '../../datetime.svelte';
    import * as NotifyService from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/notifyservice.js';

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

    /** The choices of where alerts go. The web version has no system to post to. */
    let alertModes = $derived(
        session.server
            ? [
                  { value: 'bell', label: 'The bell' },
                  { value: 'off', label: 'Off' },
              ]
            : [
                  { value: 'system', label: 'Bell and system' },
                  { value: 'bell', label: 'Only the bell' },
                  { value: 'off', label: 'Off' },
              ],
    );
    let alertMode = $derived(session.server && workspace.alertMode === 'system' ? 'bell' : workspace.alertMode);

    // Ticks now and then, so a snooze that has run out stops being shown.
    let now = $state(Date.now());
    $effect(() => {
        const timer = setInterval(() => (now = Date.now()), 30_000);
        return () => clearInterval(timer);
    });
    let snoozed = $derived(isSnoozed(workspace.alertsSnoozedUntil, now));

    async function setAlerts(mode: AlertMode): Promise<void> {
        workspace.setAlertMode(mode);
        if (mode !== 'system' || !notifyStatus?.available || notifyStatus.authorized) return;
        // Asked as it is turned on, when the answer is wanted.
        try {
            const authorized = await NotifyService.RequestPermission();
            notifyStatus = { ...notifyStatus, authorized };
        } catch {
            // The status line says what the system thinks.
        }
    }

    const notifyHint =
        'When a connected cluster gets worse — a node not ready, pods crashing or evicted, credentials about to expire — say so on the bell in the title bar, and as a system notification if you like. Off, nothing is raised; the marks in the sidebar and Fleet health still show what is wrong. Only clusters connected in this window are watched.';

    /** What the system says about the app's notifications, in a sentence. */
    let systemHint = $derived.by(() => {
        if (!notifyStatus) return 'Asking the system whether it will show them…';
        if (!notifyStatus.available) return `This build cannot post them: ${notifyStatus.reason}. Alerts still reach the bell.`;
        if (!notifyStatus.authorized) return 'The system has not allowed them yet. Until it does, alerts reach the bell only.';
        if (tested) return 'Allowed. A test was sent — it should be on screen now, or in the notification centre.';
        return 'Allowed. Send a test to see one.';
    });

    let tested = $state(false);

    async function askPermission(): Promise<void> {
        if (!notifyStatus) return;
        try {
            const authorized = await NotifyService.RequestPermission();
            notifyStatus = { ...notifyStatus, authorized };
        } catch {
            // The line above says what the system thinks.
        }
    }

    /** Posts one notification, to see where and how they appear. */
    async function sendTest(): Promise<void> {
        try {
            if (notifyStatus?.available && !notifyStatus.authorized) await askPermission();
            await NotifyService.Send('k8sdockside-test', 'K8s Dockside', 'A test notification', 'Cluster alerts will look like this.', '', '');
            tested = true;
        } catch (err) {
            notifyStatus = { available: false, authorized: false, reason: err instanceof Error ? err.message : String(err) };
        }
    }
</script>

<SettingsSection title="Notifications">
    <SettingsRow label="Cluster alerts" hint={notifyHint}>
        <SegmentedControl
            label="Cluster alerts"
            options={alertModes}
            value={alertMode}
            onchange={(v) => void setAlerts(v as AlertMode)}
        />
    </SettingsRow>

    <!-- Whether the system will actually show them: a build it will not take,
         or a permission not given yet, is the usual reason nothing appears. -->
    {#if alertMode === 'system' && !session.server}
        <SettingsRow label="System notifications" hint={systemHint}>
            <div class="snooze">
                {#if notifyStatus?.available && !notifyStatus.authorized}
                    <button class="snooze-button" onclick={() => void askPermission()}>Allow notifications</button>
                {/if}
                <button class="snooze-button" onclick={() => void sendTest()} disabled={!notifyStatus?.available}>
                    Send a test
                </button>
            </div>
        </SettingsRow>
    {/if}

    {#if alertMode !== 'off'}
        <SettingsRow
            label="Snooze"
            hint={snoozed
                ? `Snoozed until ${formatDateTime(workspace.alertsSnoozedUntil)}. Alerts are still kept on the bell, quietly: no notification, no unread dot.`
                : 'A while without being told: alerts are still kept on the bell, but raise no notification and no unread dot until the snooze ends. Also on the bell itself.'}
        >
            <div class="snooze">
                {#if snoozed}
                    <button class="snooze-button" onclick={() => workspace.snoozeAlerts(null)}>Resume now</button>
                {:else}
                    {#each snoozeChoices() as choice (choice.label)}
                        <button class="snooze-button" onclick={() => workspace.snoozeAlerts(choice.until)}>{choice.label}</button>
                    {/each}
                {/if}
            </div>
        </SettingsRow>
    {/if}

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
    {/if}
</SettingsSection>

<style>
    .snooze {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        justify-content: flex-end;
    }

    .snooze-button {
        padding: 4px 10px;
        font-size: 12px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text-dim);
        white-space: nowrap;
    }

    .snooze-button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }
</style>
