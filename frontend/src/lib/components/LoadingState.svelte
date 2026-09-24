<!--
  What a list shows while it waits for the cluster.

  "Loading…" in a line of grey text is fine for the half second a list usually
  takes, and useless for the half minute it can take over a slow VPN, through
  a login helper, or from a cluster with thousands of objects: it looks the
  same as a list that has hung. So this says what it is waiting for --
  connecting, then reading -- and, as the wait grows, how long it has been,
  that the list will appear as soon as it arrives, and finally what to check
  and a way to try again.

  It never gives up on its own: a slow answer is still an answer, and a real
  failure arrives as one and replaces this with the error.
-->
<script lang="ts">
    import Icon from './Icon.svelte';

    interface Props {
        /** What is being loaded, as the reader calls it: "pods", "Helm releases". */
        what: string;
        /** The cluster it is loaded from, by the name the sidebar shows. */
        cluster: string;
        /** Connecting to the cluster, or reading the list from it. */
        phase: 'connecting' | 'reading';
        /** Starts again from nothing. Offered once the wait is long. */
        onRetry?: () => void;
        /**
         * Whether to show the two steps. Off for a read that is one call and
         * so cannot say which of the two it is on.
         */
        steps?: boolean;
    }

    let { what, cluster, phase, onRetry, steps = true }: Props = $props();

    /** Past this, the time waited is shown. */
    const SHOW_TIME_AFTER = 3;
    /** Past this, the wait is said to be long, and why that can be. */
    const SLOW_AFTER = 10;
    /** Past this, what to check, and a way to try again. */
    const STUCK_AFTER = 30;

    const started = Date.now();
    let seconds = $state(0);
    $effect(() => {
        const timer = setInterval(() => (seconds = Math.floor((Date.now() - started) / 1000)), 1000);
        return () => clearInterval(timer);
    });

    let headline = $derived(phase === 'connecting' ? `Connecting to ${cluster}…` : `Reading ${what} from ${cluster}…`);
</script>

<div class="loading" role="status" aria-live="polite">
    <div class="line">
        <span class="spinner" aria-hidden="true"></span>
        <span class="headline">{headline}</span>
        {#if seconds >= SHOW_TIME_AFTER}
            <span class="time">{seconds} s</span>
        {/if}
    </div>

    <!-- Two steps, so the reader can see which one it is stuck on. -->
    {#if steps}
    <ol class="steps" aria-label="Progress">
        <li class:done={phase === 'reading'} class:now={phase === 'connecting'}>
            {#if phase === 'reading'}<Icon name="check" size={11} />{:else}<span class="dot"></span>{/if}
            Connect
        </li>
        <li class:now={phase === 'reading'}>
            <span class="dot"></span>
            Read {what}
        </li>
    </ol>
    {/if}

    {#if seconds >= STUCK_AFTER}
        <div class="note stuck">
            <p>
                Still no answer from {cluster}. If you reach it through a VPN or a bastion, check that it is up; a
                login helper may also be waiting for you in a browser window.
            </p>
            {#if onRetry}
                <button onclick={onRetry}><Icon name="refresh" size={12} /> Try again</button>
            {/if}
        </div>
    {:else if seconds >= SLOW_AFTER}
        <p class="note">
            {phase === 'connecting'
                ? 'This is taking longer than usual. A slow connection, or a login helper, can hold it up.'
                : `This is taking longer than usual. A slow connection, or a cluster with many ${what}, can take a while — the list appears as soon as it has arrived.`}
        </p>
    {/if}
</div>

<style>
    .loading {
        display: flex;
        flex-direction: column;
        gap: 10px;
        padding: 22px 16px;
        max-width: 60ch;
    }

    .line {
        display: flex;
        align-items: center;
        gap: 10px;
    }

    .headline {
        color: var(--text);
    }

    .time {
        font-size: 12px;
        font-variant-numeric: tabular-nums;
        color: var(--text-faint);
    }

    .spinner {
        width: 14px;
        height: 14px;
        flex: none;
        border-radius: 50%;
        border: 2px solid color-mix(in srgb, var(--accent) 25%, transparent);
        border-top-color: var(--accent);
        animation: spin 0.8s linear infinite;
    }

    @media (prefers-reduced-motion: reduce) {
        .spinner {
            animation-duration: 2.4s;
        }
    }

    @keyframes spin {
        to {
            transform: rotate(360deg);
        }
    }

    .steps {
        display: flex;
        gap: 16px;
        margin: 0;
        padding: 0 0 0 24px;
        list-style: none;
        font-size: 12px;
        color: var(--text-faint);
    }

    .steps li {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    .steps li.now {
        color: var(--text-dim);
    }

    .steps li.done {
        color: var(--ok);
    }

    .steps .dot {
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: currentColor;
        opacity: 0.6;
    }

    .steps li.now .dot {
        background: var(--accent);
        opacity: 1;
        animation: pulse 1.2s ease-in-out infinite;
    }

    @keyframes pulse {
        50% {
            opacity: 0.3;
        }
    }

    .note {
        margin: 0 0 0 24px;
        font-size: 12px;
        line-height: 1.5;
        color: var(--text-dim);
    }

    .note.stuck {
        display: flex;
        flex-direction: column;
        align-items: flex-start;
        gap: 8px;
        padding: 10px 12px;
        border-radius: var(--radius);
        background: color-mix(in srgb, var(--warn) 10%, transparent);
        color: var(--text);
    }

    .note.stuck p {
        margin: 0;
    }

    .note button {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        padding: 4px 10px;
        font-size: 12px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
    }

    .note button:hover {
        background: var(--bg-hover);
    }
</style>
