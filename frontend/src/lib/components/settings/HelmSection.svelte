<!--
  Where helm is, and how it is run.

  This section exists because of one specific thing, and the rest follows from
  it: reading a Helm release needs nothing installed -- a release is a Secret,
  and the app decodes it -- while changing one is Helm's own operation and is
  done by running helm. So the drawer always works and four buttons depend on a
  binary that may not be there.

  On macOS it usually is there and the app still cannot see it: an app launched
  from Finder inherits a PATH of four system directories, and Homebrew is on
  none of them. That is not something anything can repair on the user's behalf,
  so it is asked -- with the answer to `which helm` shown as soon as one is
  found, so it is clear which helm is being used.
-->
<script lang="ts">
    import type * as helmcli from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/helmcli/models.js';
    import { helm } from '../../state/helm.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import Toggle from './Toggle.svelte';

    let settings = $derived(workspace.helm);
    let tool = $derived<helmcli.Tool>(helm.tool);

    /**
     * The path as it is being typed, held apart from the setting.
     *
     * Saving on every keystroke would re-probe for a helm at "/opt/h", "/opt/ho"
     * and so on, and report each of them missing under the field while the user
     * is still typing.
     */
    let typing = $state(false);
    let draft = $state('');
    let shown = $derived(typing ? draft : settings.path);

    let checking = $state(false);

    // Ask where helm is when the section opens, so the answer is on screen
    // before anyone reaches for the field.
    $effect(() => {
        if (!helm.probed) void recheck();
    });

    async function recheck(): Promise<void> {
        checking = true;
        try {
            await helm.probe();
        } finally {
            checking = false;
        }
    }

    /** Saves the typed path and asks again what is there. */
    async function commitPath(): Promise<void> {
        typing = false;
        const path = draft.trim();
        if (path === settings.path) return;
        workspace.setHelm({ path });
        await recheck();
    }

    /**
     * The timeout as minutes, which is how anybody thinks about waiting for a
     * rollout. Stored in seconds because that is what helm takes.
     */
    let minutes = $derived(Math.round(settings.timeoutSeconds / 60));

    function setMinutes(value: number): void {
        const clamped = Math.max(1, Math.min(60, Math.round(value) || 5));
        workspace.setHelm({ timeoutSeconds: clamped * 60 });
    }
</script>

<SettingsSection
    title="Helm"
    lede="Reading a release needs nothing installed — it is a Secret this app decodes. Upgrading, rolling back and uninstalling are Helm's own operations, and are run by running helm."
>
    <SettingsRow
        label="Path to helm"
        hint="Leave it empty to look for helm on PATH and in the usual install locations. Set it when the app cannot find a helm you know is there — on macOS an app started from Finder does not see your shell's PATH, so run `which helm` in a terminal and paste the answer."
    >
        <input
            class="text"
            aria-label="Path to helm"
            placeholder="find it automatically"
            spellcheck="false"
            value={shown}
            oninput={(event) => {
                typing = true;
                draft = (event.currentTarget as HTMLInputElement).value;
            }}
            onfocus={() => {
                typing = true;
                draft = settings.path;
            }}
            onblur={() => void commitPath()}
            onkeydown={(event) => event.key === 'Enter' && (event.currentTarget as HTMLInputElement).blur()}
        />
    </SettingsRow>

    <!-- What was actually found, which is the whole point of the field above:
         a path that is right and a path that is wrong look identical until
         something says so. -->
    <p class="found" class:missing={!tool.found}>
        {#if checking}
            <Icon name="refresh" size={13} />
            <span>Looking for helm…</span>
        {:else if tool.found}
            <Icon name="check" size={13} />
            <span class="selectable">
                {tool.version || 'helm'} at {tool.path}{tool.configured ? '' : ' — found automatically'}
            </span>
        {:else}
            <Icon name="alert" size={13} />
            <span>{tool.reason}</span>
        {/if}

        <button class="recheck" onclick={() => void recheck()} disabled={checking}>Check again</button>
    </p>

    <SettingsRow
        label="Wait for changes to finish"
        hint="Holds an upgrade, rollback or uninstall open until the objects it wrote report ready, rather than returning as soon as the API server has taken them. Off is helm's own default: a slow rollout means a button that stays busy for minutes."
    >
        <Toggle
            checked={settings.wait}
            label="Wait for changes to finish"
            onchange={(wait) => workspace.setHelm({ wait })}
        />
    </SettingsRow>

    <SettingsRow
        label="Roll back a failed upgrade"
        hint="If an upgrade does not come up, put the release back where it was. It waits whether or not waiting is on above — there is no knowing an upgrade failed without waiting for it — so turning this on turns that on too."
    >
        <Toggle
            checked={settings.atomic}
            label="Roll back a failed upgrade"
            onchange={(atomic) => workspace.setHelm({ atomic })}
        />
    </SettingsRow>

    <SettingsRow
        label="Give up after"
        hint="How long a change is allowed to take before it is called off. Only bites when waiting is on; otherwise a command returns long before this."
    >
        <label class="minutes">
            <input
                type="number"
                min="1"
                max="60"
                aria-label="Give up after, in minutes"
                value={minutes}
                onchange={(event) => setMinutes(Number((event.currentTarget as HTMLInputElement).value))}
            />
            minutes
        </label>
    </SettingsRow>
</SettingsSection>

<style>
    .text {
        width: min(360px, 100%);
        padding: 5px 8px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg);
        color: var(--text);
        font-family: var(--mono);
        font-size: 12px;
    }

    .text:focus-visible {
        outline: none;
        border-color: var(--accent);
    }

    .found {
        display: flex;
        align-items: center;
        gap: 8px;
        margin: 0 0 4px;
        padding: 8px 10px;
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--ok) 12%, transparent);
        color: var(--ok);
        font-size: 11.5px;
        line-height: 1.6;
    }

    /* Not an error state: a machine without helm is a machine where everything
       except four buttons still works. */
    .found.missing {
        background: color-mix(in srgb, var(--warn) 12%, transparent);
        color: var(--warn);
    }

    .found span {
        flex: 1 1 auto;
        min-width: 0;
        overflow-wrap: anywhere;
    }

    .recheck {
        flex: 0 0 auto;
        padding: 3px 9px;
        border: 1px solid currentColor;
        border-radius: var(--radius-sm);
        color: inherit;
        font-size: 11px;
    }

    .recheck:hover:not(:disabled) {
        background: color-mix(in srgb, currentColor 14%, transparent);
    }

    .recheck:disabled {
        opacity: 0.55;
    }

    .minutes {
        display: flex;
        align-items: center;
        gap: 7px;
        font-size: 12px;
        color: var(--text-dim);
    }

    .minutes input {
        width: 68px;
        padding: 5px 8px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg);
        color: var(--text);
        font-size: 12px;
    }

    .minutes input:focus-visible {
        outline: none;
        border-color: var(--accent);
    }
</style>
