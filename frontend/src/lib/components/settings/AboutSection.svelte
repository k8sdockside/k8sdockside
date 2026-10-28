<!--
  What this build is, whether there is a newer one, and where it keeps things.

  The settings file is the useful part: it is plain JSON, it is the only thing
  the app writes, and deleting it is how you start over. So it is shown in full
  with a way to open it, rather than being a path in a tooltip.

  The update check is here as well as on the bell, because this is where
  somebody who has switched the automatic check off comes to ask by hand.

  The web version shows neither. Its settings file is on the server, where
  nobody in a browser can open it, and it is upgraded by whoever runs it rather
  than from here -- so what is left is what this build is.
-->
<script lang="ts">
    import { formatTime } from '../../datetime.svelte';
    import { SettingsService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type { About } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/models.js';
    import { session } from '../../state/session.svelte';
    import { updates } from '../../state/updates.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import { notices } from '../../state/notices.svelte';
    import { PROFILE_NAME, profileLabel, profiles } from '../../state/profiles.svelte';

    let about = $state<About | null>(null);
    let newProfile = $state('');
    let newProfileValid = $derived(PROFILE_NAME.test(newProfile.trim()) && !profiles.all.includes(newProfile.trim()));

    $effect(() => {
        void profiles.load();
    });

    async function createProfile(): Promise<void> {
        const name = newProfile.trim();
        await profiles.openAlongside(name);
        newProfile = '';
    }
    let copied = $state(false);

    $effect(() => {
        let live = true;
        SettingsService.About()
            .then((info) => {
                if (live) about = info;
            })
            .catch(() => {
                // The About block is decoration; failing to read it should not
                // put an error in the status bar over whatever is really going on.
            });
        return () => {
            live = false;
        };
    });

    async function copyPath(): Promise<void> {
        try {
            await navigator.clipboard.writeText(workspace.configPath);
            copied = true;
            setTimeout(() => (copied = false), 2000);
        } catch {
            notices.inform('Could not copy the path to the clipboard');
        }
    }

    async function reveal(): Promise<void> {
        try {
            await SettingsService.RevealConfig();
        } catch {
            notices.inform('Could not open the settings file');
        }
    }

    async function openRelease(): Promise<void> {
        try {
            await updates.openRelease();
        } catch {
            notices.fail('Could not open the release page');
        }
    }

    async function openDownload(): Promise<void> {
        try {
            await updates.openDownload();
        } catch {
            notices.fail('Could not open the download');
        }
    }

    /** One sentence on where this build stands. */
    const standing = $derived.by(() => {
        const s = updates.status;
        if (updates.checking) return 'Checking…';
        if (s.newer && s.latest) return `${s.latest.version} is available. You have ${s.current}.`;
        if (s.latest && s.latest.version === s.current) return `You have the latest release, ${s.latest.version}.`;
        if (s.latest) return `The latest release is ${s.latest.version}. This build is ${s.current}.`;
        if (s.error) return 'Not checked yet.';
        return workspace.checkForUpdates
            ? 'Not checked yet. The app checks on its own shortly after launch.'
            : 'Not checked yet. Automatic checks are off under Notifications.';
    });

    function timeOf(iso: string): string {
        return formatTime(iso);
    }
</script>

<SettingsSection title="About">
    <div class="identity">
        <img src="/icon-ship.svg" alt="" width="40" height="40" />
        <div>
            <h3>K8s Dockside</h3>
            {#if session.server}
                <p>A workspace for the Kubernetes clusters your administrator has added, in the browser.</p>
            {:else}
                <p>A desktop workspace for the Kubernetes clusters in your local kubeconfig files.</p>
            {/if}
        </div>
    </div>

    <dl class="facts">
        <div><dt>Version</dt><dd class="selectable">{about?.version ?? '…'}</dd></div>
        {#if !session.server}
            <div><dt>Latest release</dt><dd class="selectable">{updates.latest?.version ?? '—'}</dd></div>
        {/if}
        <div><dt>Wails</dt><dd class="selectable">{about?.wails || '—'}</dd></div>
        <div><dt>Go</dt><dd class="selectable">{about?.go ?? '…'}</dd></div>
        <div><dt>Platform</dt><dd class="selectable">{about?.platform ?? '…'}</dd></div>
        {#if !session.server}
            <div>
                <dt>Profile</dt>
                <dd class="selectable">{about ? about.profile || 'default' : '…'}</dd>
            </div>
        {/if}
        {#if updates.status.install}
            <div><dt>Installed as</dt><dd class="selectable">{updates.status.install}</dd></div>
        {/if}
        <div>
            <dt>Contexts</dt>
            <dd>{workspace.contexts.length} in {workspace.files.length} kubeconfig files</dd>
        </div>
    </dl>

    {#if !session.server}
    <h3 class="heading">Updates</h3>
    <p class="note" class:news={updates.available}>{standing}</p>
    {#if updates.status.error}
        <p class="note problem">Could not check for updates: {updates.status.error}</p>
    {:else if updates.status.checkedAt}
        <p class="note">Checked at {timeOf(updates.status.checkedAt)}.</p>
    {/if}
    <div class="file updates">
        <button onclick={() => void updates.check()} disabled={updates.checking}>
            <Icon name="refresh" size={13} />
            {updates.checking ? 'Checking…' : 'Check for updates'}
        </button>
        {#if updates.available && updates.download}
            <button onclick={openDownload} title={updates.downloadName}>
                <Icon name="download" size={13} />
                Download {updates.latest?.version} for {updates.status.install}
            </button>
        {/if}
        <button onclick={openRelease}>
            <Icon name="link" size={13} />
            {updates.latest ? `Open ${updates.latest.version} on GitHub` : 'Open the releases page'}
        </button>
    </div>

    <h3 class="heading">Settings file</h3>
    <p class="note">
        Everything on these pages lives in one JSON file: your context names and colours, the tab order, the window
        layout and these preferences. Your kubeconfig files are never written to. Deleting this file resets the app.
    </p>
    <div class="file">
        <!-- Right-to-left so a long path loses its start rather than the file
             name -- with the path itself isolated as left-to-right, or the
             leading "/" is moved to the far end. -->
        <code class="selectable"><span dir="ltr">{workspace.configPath || '…'}</span></code>
        <button onclick={copyPath} disabled={!workspace.configPath}>
            <Icon name={copied ? 'check' : 'file'} size={13} />
            {copied ? 'Copied' : 'Copy path'}
        </button>
        <button onclick={reveal} disabled={!workspace.configPath}>
            <Icon name="folder" size={13} /> Show in file manager
        </button>
    </div>

    <h3 class="heading">Profiles</h3>
    <p class="note">
        A profile is a separate set of everything above — settings, plugins and themes — say one for a development
        build that loads plugins from your working folders. This window runs on
        <strong>{profileLabel(profiles.current)}</strong>. Switching restarts the app on the other profile; Open
        alongside runs both at once. A new profile starts from a copy of the default settings.
    </p>
    <ul class="profiles">
        {#each profiles.all as name (name)}
            <li>
                <Icon name="folder" size={13} />
                <span class="name">{profileLabel(name)}</span>
                {#if name === profiles.current}
                    <span class="here">this window</span>
                {:else}
                    <button disabled={profiles.starting !== null} onclick={() => void profiles.switchTo(name)}>
                        Switch
                    </button>
                {/if}
                <button
                    disabled={profiles.starting !== null}
                    onclick={() => void profiles.openAlongside(name)}
                    title="Start another K8s Dockside on this profile, beside this one"
                >
                    Open alongside
                </button>
            </li>
        {/each}
    </ul>
    <form
        class="file new-profile"
        onsubmit={(e) => {
            e.preventDefault();
            void createProfile();
        }}
    >
        <input
            type="text"
            placeholder="new-profile"
            spellcheck="false"
            autocomplete="off"
            aria-label="New profile name"
            bind:value={newProfile}
        />
        <button type="submit" disabled={!newProfileValid || profiles.starting !== null}>
            <Icon name="plus" size={13} /> Create and open alongside
        </button>
    </form>
    {#if newProfile && !newProfileValid}
        <p class="note problem">Lower-case letters, digits, “-” and “_”, starting with a letter or digit.</p>
    {/if}
    <p class="note">
        From a terminal: <code class="inline">--profile &lt;name&gt;</code> or
        <code class="inline">K8SDOCKSIDE_PROFILE=&lt;name&gt;</code>; <code class="inline">make dev</code> uses
        <strong>dev</strong>.
    </p>
    {/if}
</SettingsSection>

<style>
    .identity {
        display: flex;
        align-items: center;
        gap: 14px;
        margin: 16px 0 22px;
    }

    .identity h3 {
        margin: 0 0 3px;
        font-size: 15px;
        font-weight: 600;
    }

    .identity p {
        margin: 0;
        font-size: 12px;
        line-height: 1.6;
        color: var(--text-dim);
        max-width: 52ch;
    }

    .facts {
        display: flex;
        flex-direction: column;
        gap: 1px;
        margin: 0 0 28px;
    }

    .facts > div {
        display: grid;
        grid-template-columns: 110px 1fr;
        gap: 12px;
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-size: 12px;
    }

    dt {
        color: var(--text-faint);
    }

    dd {
        margin: 0;
        color: var(--text-dim);
        font-family: var(--mono);
        font-size: 11px;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .heading {
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

    .file {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px;
    }

    .updates {
        margin-bottom: 28px;
    }

    .note.news {
        color: var(--text);
    }

    .note.problem {
        color: var(--error);
    }

    .file code {
        flex: 1 1 260px;
        min-width: 0;
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        direction: rtl;
        text-align: left;
    }

    .profiles {
        list-style: none;
        margin: 0 0 10px;
        padding: 0;
        display: grid;
        gap: 4px;
    }

    .profiles li {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 5px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-size: 12px;
    }

    .profiles .name {
        flex: 1 1 auto;
        font-family: var(--mono);
    }

    .profiles .here {
        font-size: 11px;
        color: var(--accent);
    }

    .profiles button {
        padding: 3px 9px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11.5px;
    }

    .new-profile input {
        flex: 1 1 200px;
        min-width: 0;
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        border: 1px solid var(--border);
        background: var(--bg-panel);
        font-family: var(--mono);
        font-size: 11.5px;
        color: var(--text);
    }

    code.inline {
        font-family: var(--mono);
        font-size: 11px;
        padding: 1px 4px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
    }

    .file button {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 0 0 auto;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .file button:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .file button:disabled {
        opacity: 0.5;
        cursor: default;
    }
</style>
