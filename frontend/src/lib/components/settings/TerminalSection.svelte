<!--
  How a shell opens.

  Two honest answers, so both are here. The terminal in the dock needs nothing
  installed and keeps the shell beside the cluster it belongs to; the terminal
  you already have is yours -- your font, your colours, your scrollback, your
  tmux -- and this app has no business reimplementing any of that. The choice
  below is which one the Shell button uses, and either way the other is still a
  click away in the terminal's own bar.

  The rest of the section is the two things a shell cannot work out for itself:
  which shell a container actually has, and what a node shell is made of --
  and, for the local terminal (a shell on this machine, in the dock), which of
  this machine's shells it runs.

  The web version has only the first answer. "The terminal you already have" is
  one on the user's machine, and the server running this cannot open a window
  there -- so the choice is not offered, and every shell opens in the dock
  whatever an older preference says. See workspace.openShell.
-->
<script lang="ts">
    import { TerminalService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type {
        ExternalTerminals,
        LocalShells,
    } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/models.js';
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SegmentedControl from './SegmentedControl.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import { notices } from '../../state/notices.svelte';

    const MODES = [
        { value: 'app', label: 'In this window', icon: 'terminal' },
        { value: 'external', label: 'In my terminal', icon: 'display' },
    ];

    let terminal = $derived(workspace.terminal);

    /**
     * What is installed on this machine, read once when the section opens.
     *
     * It is a question about the machine rather than about the settings, so it
     * is asked here rather than held in the store: nothing else in the app needs
     * to know which terminal emulators exist, and a list read at launch would
     * miss one installed since.
     */
    let externals = $state<ExternalTerminals | null>(null);
    let looking = $state(true);

    $effect(() => {
        // Nothing to look for in the web version: the machine it would list
        // the emulators of is the server.
        if (session.server) {
            looking = false;
            return;
        }
        void (async () => {
            try {
                externals = await TerminalService.Externals();
            } finally {
                looking = false;
            }
        })();
    });

    /**
     * The shells, edited as the text the user reads them as.
     *
     * Held apart from the setting while it is being typed: parsing on every
     * keystroke would turn "bash, z" into a list containing "z" and a comma the
     * user is halfway through typing into a separator.
     */
    let shellText = $state('');
    let editingShells = $state(false);

    let shownShells = $derived(editingShells ? shellText : terminal.shells.join(', '));

    function startEditing(): void {
        shellText = terminal.shells.join(', ');
        editingShells = true;
    }

    function commitShells(): void {
        editingShells = false;
        const shells = shellText
            .split(',')
            .map((shell) => shell.trim())
            .filter((shell) => shell !== '');
        // An empty list is a shell that can never open. The store would repair
        // it, but saying so here is better than the field silently refilling.
        if (shells.length === 0) {
            notices.fail('A terminal needs at least one shell to try');
            return;
        }
        workspace.setTerminal({ shells });
    }

    /**
     * The shells on this machine for the local terminal, read once when the
     * section opens, for the reason the emulators are.
     */
    let locals = $state<LocalShells | null>(null);

    $effect(() => {
        if (session.server) return;
        void (async () => {
            try {
                locals = await TerminalService.LocalShells();
            } catch {
                locals = { shells: [], supported: false, reason: 'The shells on this machine could not be listed.' };
            }
        })();
    });

    const CUSTOM = '__custom__';

    /** Whether the setting names a shell that was not detected: one typed in by hand. */
    let customShell = $derived(
        terminal.localShell !== '' && !(locals?.shells ?? []).some((s) => s.path === terminal.localShell),
    );
    /** Shown while "Custom…" is picked and nothing has been typed yet. */
    let choosingCustom = $state(false);
    let localChoice = $derived(customShell || choosingCustom ? CUSTOM : terminal.localShell);

    function chooseLocal(value: string): void {
        if (value === CUSTOM) {
            choosingCustom = true;
            return;
        }
        choosingCustom = false;
        workspace.setTerminal({ localShell: value });
    }

    function localDefaultName(): string {
        const found = locals?.shells?.find((s) => s.default);
        return found ? found.name : 'none found';
    }

    /** The terminal chosen for external mode, including "whatever is default". */
    let chosenExternal = $derived(terminal.external);

    function defaultName(): string {
        const found = externals?.terminals?.find((t) => t.default);
        return found ? found.name : 'none found';
    }
</script>

<SettingsSection
    title="Terminal"
    lede={session.server
        ? 'Which shell the Shell button tries, and what a shell on a node is made of.'
        : 'Where the Shell button opens a shell, which shell it tries, and what a shell on a node is made of.'}
>
    {#if !session.server}
    <SettingsRow
        label="Open shells"
        hint="In this window, a terminal opens in the dock beside the logs and the editor — nothing needs to be installed. In your own terminal, the app runs kubectl in the emulator you use, and everything you have set up there applies. The same choice decides where a plugin's full-screen commands run."
    >
        <SegmentedControl
            options={MODES}
            value={terminal.mode}
            label="Where shells open"
            onchange={(mode) => workspace.setTerminal({ mode: mode as 'app' | 'external' })}
        />
    </SettingsRow>

    {#if terminal.mode === 'external'}
        <SettingsRow
            label="Which terminal"
            hint="Only the emulators found on this machine are listed. Leaving it on the default means this setting still says “the usual one” on another machine, where the usual one may be something else."
        >
            <div class="terminals">
                {#if looking}
                    <span class="note">Looking…</span>
                {:else if (externals?.terminals ?? []).length === 0}
                    <span class="note warn">No terminal emulator was found on this machine.</span>
                {:else}
                    <select
                        aria-label="Terminal emulator"
                        value={chosenExternal}
                        onchange={(event) =>
                            workspace.setTerminal({ external: (event.currentTarget as HTMLSelectElement).value })}
                    >
                        <option value="">Default ({defaultName()})</option>
                        {#each externals?.terminals ?? [] as candidate (candidate.id)}
                            <option value={candidate.id}>{candidate.name}</option>
                        {/each}
                    </select>
                {/if}
            </div>
        </SettingsRow>

        {#if !looking && externals && !externals.kubectl}
            <p class="warning">
                <Icon name="alert" size={13} />
                {externals.reason}
            </p>
        {/if}
    {/if}
    {/if}

    {#if !session.server}
        <SettingsRow
            label="Local shell"
            hint="The shell the local terminal runs on this machine — opened from a cluster's terminal button, the File menu, or Ctrl+`. Opened for a cluster, its KUBECONFIG holds that cluster's context alone, so kubectl and helm there cannot reach another one. The default is your login shell; on Windows, PowerShell 7, then Windows PowerShell, then Command Prompt."
        >
            <div class="terminals">
                {#if !locals}
                    <span class="note">Looking…</span>
                {:else if !locals.supported}
                    <span class="note warn">{locals.reason}</span>
                {:else}
                    <select
                        aria-label="Local shell"
                        value={localChoice}
                        onchange={(event) => chooseLocal((event.currentTarget as HTMLSelectElement).value)}
                    >
                        <option value="">Default ({localDefaultName()})</option>
                        {#each locals.shells ?? [] as shell (shell.path)}
                            <option value={shell.path}>{shell.name}</option>
                        {/each}
                        <option value={CUSTOM}>Custom…</option>
                    </select>
                {/if}
            </div>
        </SettingsRow>

        {#if locals?.supported && localChoice === CUSTOM}
            <SettingsRow
                label="Custom shell"
                hint="The full path of the program, or a name found on your PATH. Started as a login shell when it is one known to take -l."
            >
                <input
                    class="text"
                    aria-label="Custom shell path"
                    placeholder={navigator.platform.startsWith('Win') ? 'C:\\path\\to\\shell.exe' : '/path/to/shell'}
                    value={customShell ? terminal.localShell : ''}
                    onchange={(event) => {
                        const value = (event.currentTarget as HTMLInputElement).value.trim();
                        choosingCustom = value === '';
                        workspace.setTerminal({ localShell: value });
                    }}
                />
            </SettingsRow>
        {/if}
    {/if}

    <SettingsRow
        label="Shells to try"
        hint="In a container, in order, until one of them runs. A container image is free to have bash, only sh, or neither, and there is no way to know which without asking it."
    >
        <input
            class="text"
            aria-label="Shells to try"
            value={shownShells}
            oninput={(event) => {
                if (!editingShells) startEditing();
                shellText = (event.currentTarget as HTMLInputElement).value;
            }}
            onfocus={() => startEditing()}
            onblur={() => commitShells()}
            onkeydown={(event) => event.key === 'Enter' && (event.currentTarget as HTMLInputElement).blur()}
        />
    </SettingsRow>

    <SettingsRow
        label="Node shell image"
        hint="A shell on a node is a privileged pod created on it, chrooted into the machine's filesystem — the same thing kubectl debug does. This is the image that pod runs; change it for a cluster that mirrors its own."
    >
        <input
            class="text"
            aria-label="Node shell image"
            value={terminal.nodeImage}
            onchange={(event) =>
                workspace.setTerminal({ nodeImage: (event.currentTarget as HTMLInputElement).value.trim() })}
        />
    </SettingsRow>

    <SettingsRow
        label="Node shell namespace"
        hint="Where that pod is created. Worth changing when the default namespace enforces the restricted pod security standard, which will refuse a privileged pod outright."
    >
        <input
            class="text narrow"
            aria-label="Node shell namespace"
            value={terminal.nodeNamespace}
            onchange={(event) =>
                workspace.setTerminal({ nodeNamespace: (event.currentTarget as HTMLInputElement).value.trim() })}
        />
    </SettingsRow>

    <SettingsRow label="Type size" hint="The size of the text in the terminal in the dock, in pixels.">
        <input
            type="number"
            class="text narrow"
            min="8"
            max="32"
            aria-label="Terminal type size"
            value={terminal.fontSize}
            onchange={(event) =>
                workspace.setTerminal({ fontSize: Number((event.currentTarget as HTMLInputElement).value) })}
        />
    </SettingsRow>

    <SettingsRow
        label="Scrollback"
        hint="How many lines a terminal keeps. Every line is held in memory, so a very large number is paid for by the window rather than by the cluster."
    >
        <input
            type="number"
            class="text narrow"
            min="200"
            max="200000"
            step="500"
            aria-label="Scrollback lines"
            value={terminal.scrollback}
            onchange={(event) =>
                workspace.setTerminal({ scrollback: Number((event.currentTarget as HTMLInputElement).value) })}
        />
    </SettingsRow>
</SettingsSection>

<style>
    .text {
        height: 26px;
        width: 220px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font: inherit;
        font-size: 12px;
    }

    .narrow {
        width: 120px;
    }

    .terminals select {
        height: 26px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font: inherit;
        font-size: 12px;
    }

    .note {
        font-size: 12px;
        color: var(--text-faint);
    }

    .note.warn {
        color: var(--warn);
    }

    .warning {
        display: flex;
        align-items: flex-start;
        gap: 8px;
        margin: 0 0 14px;
        padding: 10px 12px;
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--warn) 12%, transparent);
        color: var(--warn);
        font-size: 11.5px;
        line-height: 1.6;
    }
</style>
