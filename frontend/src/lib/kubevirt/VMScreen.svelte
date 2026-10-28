<!--
  A virtual machine's display: what its graphical console shows, read from
  virt-api's VNC screenshot about once a second while the tab is on screen.

  What is on screen is a picture, not a VNC session: drawing every frame across
  the bridge would be a VNC viewer's worth of work for what the screenshots
  already show. Typing is the one thing a picture cannot do, so for that the
  backend holds a VNC connection that only carries keys (kube/kubevirtvnc.go):
  click the picture and the keyboard goes to the guest -- arrows and F-keys
  for a Talos dashboard -- and the picture is read again straight after. The
  strip of buttons sends what this machine's own keyboard or the app keeps for
  itself: F-keys a laptop turns into volume, Ctrl+Alt+Delete.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import { ResourceService, TerminalService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import { CHORDS, chordKeys, keysymFor, type Chord } from './keysym';
    import { alpha } from '../colors';
    import { formatTime } from '../datetime.svelte';
    import { workspace, type DockTab } from '../state/workspace.svelte';
    import Icon from '../components/Icon.svelte';

    interface Props {
        tab: DockTab;
    }

    let { tab }: Props = $props();

    /** How often the picture is read while it is being watched, and while it is failing. */
    const EVERY = 1000;
    const AFTER_FAILURE = 5000;

    let color = $derived(workspace.colorOf(tab.contextId));
    let src = $state('');
    let error = $state('');
    let paused = $state(false);
    let updated = $state<Date | null>(null);
    let visible = $state(typeof document === 'undefined' || document.visibilityState === 'visible');

    // ---- the keyboard -----------------------------------------------------

    /** The backend's keyboard on this display, opened on the first key. */
    let keyboard: Promise<string> | null = null;
    /** Keys go one call after another, so they arrive in the order pressed. */
    let sending: Promise<unknown> = Promise.resolve();
    let typing = $state(false);
    let keyError = $state('');

    function connect(): Promise<string> {
        keyboard ??= TerminalService.OpenVMKeyboard(tab.contextId, tab.namespace, tab.name).catch((err: unknown) => {
            keyboard = null;
            throw err;
        });
        return keyboard;
    }

    function send(keys: { keysym: number; down: boolean }[]): void {
        sending = sending.then(async () => {
            try {
                try {
                    await TerminalService.SendVMKeys(await connect(), keys);
                } catch {
                    // The connection went -- the guest restarted, virt-api
                    // was redeployed. Once more on a new one.
                    keyboard = null;
                    await TerminalService.SendVMKeys(await connect(), keys);
                }
                keyError = '';
                soon();
            } catch (err: unknown) {
                keyError = err instanceof Error ? err.message : String(err);
            }
        });
    }

    function onKey(event: KeyboardEvent, down: boolean): void {
        // Cmd stays the app's -- zoom, settings, search -- on a Mac, where it
        // means nothing to a guest anyway.
        if (event.metaKey) return;
        const keysym = keysymFor(event.key);
        if (keysym === null) return;
        event.preventDefault();
        event.stopPropagation();
        send([{ keysym, down }]);
    }

    function press(chord: Chord): void {
        send(chordKeys(chord));
        body?.focus();
    }

    let body = $state<HTMLElement | null>(null);

    /** Reads the picture again shortly after a key, rather than on the next tick. */
    let nudge: ReturnType<typeof setTimeout> | undefined;
    function soon(): void {
        clearTimeout(nudge);
        nudge = setTimeout(() => void read(), 150);
    }

    onMount(() => () => {
        clearTimeout(nudge);
        const open = keyboard;
        keyboard = null;
        void open?.then((id) => TerminalService.CloseVMKeyboard(id)).catch(() => {});
    });

    async function read(): Promise<void> {
        try {
            src = await ResourceService.VMScreenshot(tab.contextId, tab.namespace, tab.name);
            error = '';
            updated = new Date();
        } catch (err: unknown) {
            error = err instanceof Error ? err.message : String(err);
        }
    }

    // One read at a time, the next one a beat after the last came back: a slow
    // virt-api is asked less often rather than queued up behind.
    $effect(() => {
        if (paused || !visible) return;
        let stopped = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        const loop = async (): Promise<void> => {
            await read();
            if (!stopped) timer = setTimeout(() => void loop(), error ? AFTER_FAILURE : EVERY);
        };
        void loop();
        return () => {
            stopped = true;
            clearTimeout(timer);
        };
    });

    onMount(() => {
        const seen = (): void => {
            visible = document.visibilityState === 'visible';
        };
        document.addEventListener('visibilitychange', seen);
        return () => document.removeEventListener('visibilitychange', seen);
    });
</script>

<div class="screen" style:--ctx-color={color} style:--ctx-tint={alpha(color, 0.12)}>
    <div class="bar">
        <span class="tag" title="The virtual machine's display, as virt-api's VNC screenshot">console</span>
        <span class="what" title={`${tab.namespace}/${tab.name}`}>{tab.name}</span>
        <span class="where" class:on={typing}>
            {typing ? 'keys go to the VM — click outside to stop' : 'click the screen to type'}
        </span>

        <span class="state" class:error={!!error} class:live={!error && !paused}>
            {#if error}
                Failed
            {:else if paused}
                Paused{updated ? ` · ${formatTime(updated, { seconds: true })}` : ''}
            {:else if updated}
                Live
            {:else}
                Opening…
            {/if}
        </span>

        <button
            class="toggle"
            title={paused ? 'Keep the picture up to date again' : 'Hold the picture as it is'}
            onclick={() => (paused = !paused)}
        >
            <Icon name={paused ? 'play' : 'pause'} size={13} />
            {paused ? 'Resume' : 'Pause'}
        </button>
        <button
            class="toggle"
            title="Open the serial console, which takes input"
            onclick={() => workspace.openVMConsole(tab)}
        >
            <Icon name="terminal" size={13} />
            Serial console
        </button>
    </div>

    <div class="keys" role="toolbar" aria-label="Keys to send">
        {#each CHORDS as chord (chord.label)}
            <button title="Send {chord.title}" onclick={() => press(chord)}>{chord.label}</button>
        {/each}
        {#if keyError}<span class="key-error" title={keyError}>{keyError}</span>{/if}
    </div>

    <!-- A focusable surface that takes the keyboard, as a terminal is. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
    <div
        class="body"
        class:typing
        bind:this={body}
        tabindex="0"
        role="application"
        aria-label="The display of {tab.name}; keys typed here go to the virtual machine"
        onkeydown={(e) => onKey(e, true)}
        onkeyup={(e) => onKey(e, false)}
        onfocus={() => (typing = true)}
        onblur={() => (typing = false)}
    >
        {#if src}
            <img {src} alt="The display of {tab.name}" class:stale={!!error} />
        {/if}
        {#if error}
            <p class="message">{error}</p>
        {:else if !src}
            <p class="message">Reading the display of {tab.name}…</p>
        {/if}
    </div>
</div>

<style>
    .screen {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
    }

    .bar {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 6px 10px;
        border-bottom: 1px solid var(--border);
        background: var(--ctx-tint);
        border-left: 3px solid var(--ctx-color);
        flex: 0 0 auto;
    }

    .tag {
        font-size: 10.5px;
        padding: 1px 6px;
        border-radius: var(--radius-sm);
        color: var(--accent);
        box-shadow: inset 0 0 0 1px var(--accent);
        text-transform: uppercase;
        letter-spacing: 0.04em;
    }

    .what {
        font-size: 12px;
        font-weight: 600;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        max-width: 260px;
    }

    .where {
        font-size: 11px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .where.on {
        color: var(--accent);
    }

    .keys {
        display: flex;
        align-items: center;
        flex-wrap: wrap;
        gap: 3px;
        padding: 4px 10px;
        border-bottom: 1px solid var(--border);
        flex: 0 0 auto;
    }

    .keys button {
        height: 20px;
        padding: 0 7px;
        border-radius: var(--radius-sm);
        font-family: var(--mono);
        font-size: 10.5px;
        color: var(--text-dim);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .keys button:hover {
        color: var(--text);
        background: var(--bg-hover);
    }

    .key-error {
        margin-left: 8px;
        font-size: 11px;
        color: var(--error);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        min-width: 0;
        flex: 1 1 0;
    }

    .body.typing {
        box-shadow: inset 0 0 0 2px var(--accent);
    }

    .body:focus {
        outline: none;
    }

    .state {
        margin-left: auto;
        font-size: 11px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .state.live {
        color: var(--ok);
    }

    .state.error {
        color: var(--error);
    }

    .toggle {
        display: flex;
        align-items: center;
        gap: 5px;
        height: 22px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--text-dim);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        white-space: nowrap;
    }

    .toggle:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* The guest's screen, as large as the dock allows and never stretched out
       of its own proportions. */
    .body {
        position: relative;
        flex: 1 1 auto;
        min-height: 0;
        display: flex;
        align-items: center;
        justify-content: center;
        background: #000;
        overflow: hidden;
    }

    img {
        max-width: 100%;
        max-height: 100%;
        object-fit: contain;
    }

    img.stale {
        opacity: 0.35;
    }

    .message {
        position: absolute;
        margin: 0;
        padding: 8px 12px;
        max-width: 80%;
        font-size: 12px;
        color: #ddd;
        background: rgb(0 0 0 / 0.7);
        border-radius: var(--radius-sm);
        text-align: center;
    }
</style>
