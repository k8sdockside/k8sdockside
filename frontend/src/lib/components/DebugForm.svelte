<!--
  The question `kubectl debug` asks before it makes anything: how, with which
  image and profile, and against which container.

  It replaces the action bar the way the drain question does, because every
  answer to it creates something in the cluster -- a container that cannot be
  removed again, a copy of a pod, a pod on a node -- and that is said here, in
  the question, before the button that does it. Replacing the original pod is
  the one answer that also deletes something, and the question and the button
  both turn to say so.

  The common options are on the bar; the rest of kubectl debug's flags are under
  Advanced, folded, with kubectl's own defaults. The image and profile start on
  the settings' defaults and are this launch's to change.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import { TerminalService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import {
        blankDebugRequest,
        debugImageChoices,
        debugPresets,
        DEBUG_PROFILES,
        PULL_POLICIES,
        splitCommand,
        splitList,
        type DebugMode,
    } from '../state/debug.svelte';
    import { workspace, type DetailTarget } from '../state/workspace.svelte';

    interface Props {
        object: DetailTarget;
        /** Called when the question is put away, whether it was answered or not. */
        onclose: () => void;
    }

    let { object, onclose }: Props = $props();

    /** The picker's answer for "Other image…". */
    const OTHER = '\u0000other';
    /** The picker's answer for "keep the container's own image", in a copy. */
    const KEEP_IMAGE = '';

    let onNode = $derived(object.kind === 'nodes');
    let mode = $state<DebugMode>('ephemeral');
    let image = $state('');
    let otherImage = $state('');
    let profile = $state('');
    let target = $state('');
    let command = $state('');
    let argumentsOnly = $state(false);
    let copyTo = $state('');
    /** In a copy: '' for a new debug container, or one of the pod's own to change. */
    let existing = $state('');
    let setImages = $state('');
    let shareProcesses = $state(true);
    let sameNode = $state(false);
    let replace = $state(false);
    let keep = $state(false);
    let namespace = $state('');

    // Advanced, with kubectl's defaults.
    let containerName = $state('');
    let pullPolicy = $state('');
    let env = $state('');
    let custom = $state('');
    let interactive = $state(true);
    let keepLabels = $state(false);
    let keepAnnotations = $state(false);
    let keepLiveness = $state(false);
    let keepReadiness = $state(false);
    let keepStartup = $state(false);
    let keepInit = $state(true);

    /** The pod's containers, and whether it runs as non-root. */
    let containers = $state<string[]>([]);
    let nonRoot = $state(false);
    let cancelEl = $state<HTMLButtonElement | null>(null);

    let changingExisting = $derived(mode === 'copy' && existing !== '');
    let choices = $derived(debugImageChoices(debugPresets.list, workspace.debug.images, workspace.debug.image));
    let chosenImage = $derived(image === OTHER ? otherImage.trim() : image);
    let profileHint = $derived(DEBUG_PROFILES.find((p) => p.value === profile)?.hint ?? '');
    /** Whether the profile asks for something a non-root pod may not grant. */
    let profileNeedsRoot = $derived(['general', 'netadmin', 'sysadmin'].includes(profile));

    onMount(() => {
        mode = onNode ? 'node' : 'ephemeral';
        image = workspace.debug.image;
        profile = workspace.debug.profile;
        copyTo = `${object.name}-debug`;
        namespace = workspace.terminal.nodeNamespace;
        void debugPresets.load();
        if (!onNode) void loadInfo();
        cancelEl?.focus();
    });

    async function loadInfo(): Promise<void> {
        try {
            const info = await TerminalService.DebugInfo(object.contextId, object.namespace, object.name);
            containers = info.containers ?? [];
            nonRoot = info.nonRoot;
            // kubectl targets nothing unless told; the first container is
            // what almost everyone means, and "none" is still there.
            if (!target) target = containers[0] ?? '';
        } catch {
            // Without it the pickers offer "none" and "new", which still work.
        }
    }

    // Changing an existing container defaults to keeping its own image, which
    // is what "change its command" means; a new one needs an image to run.
    $effect(() => {
        if (changingExisting && image !== KEEP_IMAGE && image === workspace.debug.image) image = KEEP_IMAGE;
        if (!changingExisting && image === KEEP_IMAGE) image = workspace.debug.image;
    });

    /** What is about to happen, in the words the button cannot fit. */
    let question = $derived.by(() => {
        const fate = !interactive || keep ? 'It is kept afterwards.' : 'It is deleted when the terminal closes.';
        if (mode === 'node') {
            return `Create a debug pod on node ${object.name}, in namespace ${namespace || 'default'}? ${fate}`;
        }
        if (mode === 'copy') {
            const what = changingExisting
                ? `with container ${existing} changed`
                : 'with a debug container';
            const stripped = keepLabels ? '' : ' Its labels are left behind, so nothing routes to it.';
            const replaced = replace ? ` The original pod ${object.name} is then DELETED.` : '';
            return `Create pod ${copyTo || '…'}, a copy of ${object.name} ${what}?${stripped} ${fate}${replaced}`;
        }
        return `Add an ephemeral debug container to pod ${object.name}? ` +
            'It stays in the pod until the pod is deleted — the API cannot remove one.';
    });

    let canStart = $derived(
        (chosenImage !== '' || changingExisting) && (mode !== 'copy' || copyTo.trim() !== ''),
    );

    function start(): void {
        if (!canStart) return;
        const request = blankDebugRequest(mode, mode === 'node' ? namespace.trim() : object.namespace, object.name);
        request.image = chosenImage;
        request.profile = profile;
        request.command = splitCommand(command);
        request.argumentsOnly = argumentsOnly;
        request.imagePullPolicy = pullPolicy;
        request.env = splitList(env);
        request.custom = custom;
        request.interactive = interactive;
        request.container = changingExisting ? existing : containerName.trim();
        if (mode === 'ephemeral') request.target = target;
        if (mode === 'copy') {
            request.copyTo = copyTo.trim();
            request.setImages = splitList(setImages);
            request.shareProcesses = shareProcesses;
            request.sameNode = sameNode;
            request.replace = replace;
            request.keepLabels = keepLabels;
            request.keepAnnotations = keepAnnotations;
            request.keepLiveness = keepLiveness;
            request.keepReadiness = keepReadiness;
            request.keepStartup = keepStartup;
            request.keepInitContainers = keepInit;
        }
        if (mode !== 'ephemeral') request.keep = keep;
        workspace.openDebug(object, request);
        onclose();
    }
</script>

<p class="question" class:warn={replace && mode === 'copy'}>{question}</p>

{#if nonRoot && profileNeedsRoot && mode !== 'node'}
    <p class="hint">
        This pod runs as a non-root user, so the capabilities the {profile} profile adds may not take effect.
    </p>
{/if}

<div class="options">
    {#if !onNode}
        <div class="modes" role="radiogroup" aria-label="How to debug">
            <label class="check">
                <input type="radio" name="debug-mode" value="ephemeral" bind:group={mode} />
                Ephemeral container
            </label>
            <label class="check">
                <input type="radio" name="debug-mode" value="copy" bind:group={mode} />
                Copy of the pod
            </label>
        </div>
    {/if}

    {#if mode === 'copy'}
        <label class="field">
            Copy name
            <input type="text" aria-label="Copy name" spellcheck="false" autocomplete="off" bind:value={copyTo} />
        </label>
        <label class="field">
            Debug in
            <select aria-label="Container to debug in" bind:value={existing}>
                <option value="">a new debug container</option>
                {#each containers as name (name)}
                    <option value={name}>{name} (change its command)</option>
                {/each}
            </select>
        </label>
    {/if}

    <label class="field">
        Image
        <select aria-label="Debug image" bind:value={image}>
            {#if changingExisting}
                <option value={KEEP_IMAGE}>keep the container's image</option>
            {/if}
            {#each choices as choice (choice.image)}
                <option value={choice.image}>{choice.label}</option>
            {/each}
            <option value={OTHER}>Other image…</option>
        </select>
    </label>
    {#if image === OTHER}
        <label class="field">
            <input
                type="text"
                aria-label="Other debug image"
                placeholder="registry/image:tag"
                spellcheck="false"
                autocomplete="off"
                bind:value={otherImage}
            />
        </label>
    {/if}

    <label class="field" title={profileHint}>
        Profile
        <select aria-label="Debug profile" bind:value={profile}>
            {#each DEBUG_PROFILES as option (option.value)}
                <option value={option.value}>{option.label}</option>
            {/each}
        </select>
    </label>

    {#if mode === 'ephemeral'}
        <label class="field" title="Share this container's process namespace (--target)">
            Target
            <select aria-label="Target container" bind:value={target}>
                <option value="">none</option>
                {#each containers as name (name)}
                    <option value={name}>{name}</option>
                {/each}
            </select>
        </label>
    {/if}

    {#if mode === 'node'}
        <label class="field">
            Namespace
            <input type="text" aria-label="Debug pod namespace" spellcheck="false" autocomplete="off" bind:value={namespace} />
        </label>
    {/if}

    <label class="field">
        Command
        <input
            type="text"
            aria-label="Debug command"
            placeholder="the image's own"
            spellcheck="false"
            autocomplete="off"
            bind:value={command}
        />
    </label>
    <label class="check" title="Pass these to the image's entrypoint as arguments (--arguments-only)">
        <input type="checkbox" bind:checked={argumentsOnly} />
        Arguments only
    </label>

    {#if mode === 'copy'}
        <label class="check">
            <input type="checkbox" bind:checked={shareProcesses} />
            Share processes
        </label>
        <label class="check">
            <input type="checkbox" bind:checked={sameNode} />
            Same node
        </label>
        <label class="check danger-check">
            <input type="checkbox" bind:checked={replace} />
            Replace — delete the original
        </label>
    {/if}

    {#if mode !== 'ephemeral'}
        <label class="check">
            <input type="checkbox" bind:checked={keep} disabled={!interactive} />
            Keep the pod afterwards
        </label>
    {/if}
</div>

<details class="advanced">
    <summary>Advanced</summary>
    <div class="options">
        {#if !changingExisting}
            <label class="field">
                Container name
                <input
                    type="text"
                    aria-label="Debug container name"
                    placeholder="debugger-xxxxx"
                    spellcheck="false"
                    autocomplete="off"
                    bind:value={containerName}
                />
            </label>
        {/if}
        <label class="field">
            Pull policy
            <select aria-label="Image pull policy" bind:value={pullPolicy}>
                {#each PULL_POLICIES as policy (policy)}
                    <option value={policy}>{policy || 'cluster default'}</option>
                {/each}
            </select>
        </label>
        <label class="field">
            Env
            <input
                type="text"
                aria-label="Environment variables"
                placeholder="KEY=value, OTHER=value"
                spellcheck="false"
                autocomplete="off"
                bind:value={env}
            />
        </label>
        <label class="check">
            <input type="checkbox" bind:checked={interactive} />
            Interactive (attach a terminal)
        </label>

        {#if mode === 'copy'}
            <label class="field">
                Set images
                <input
                    type="text"
                    aria-label="Set images"
                    placeholder="app=image:tag, *=image"
                    spellcheck="false"
                    autocomplete="off"
                    bind:value={setImages}
                />
            </label>
            <label class="check"><input type="checkbox" bind:checked={keepLabels} /> Keep labels</label>
            <label class="check"><input type="checkbox" bind:checked={keepAnnotations} /> Keep annotations</label>
            <label class="check"><input type="checkbox" bind:checked={keepLiveness} /> Keep liveness probes</label>
            <label class="check"><input type="checkbox" bind:checked={keepReadiness} /> Keep readiness probes</label>
            <label class="check"><input type="checkbox" bind:checked={keepStartup} /> Keep startup probes</label>
            <label class="check"><input type="checkbox" bind:checked={keepInit} /> Keep init containers</label>
        {/if}

        <label class="field custom">
            Custom container spec
            <textarea
                aria-label="Custom container spec"
                rows="4"
                placeholder={'JSON or YAML, merged over the profile, e.g.\nsecurityContext:\n  runAsUser: 0'}
                spellcheck="false"
                bind:value={custom}
            ></textarea>
        </label>
    </div>
</details>

<div class="answers">
    <button bind:this={cancelEl} class="plain" onclick={onclose}>Cancel</button>
    <button
        class="go"
        class:danger={profile === 'sysadmin' || (mode === 'copy' && replace)}
        disabled={!canStart}
        onclick={start}
    >
        {mode === 'copy' && replace ? 'Debug and delete original' : 'Debug'}
    </button>
</div>

<style>
    .question {
        margin: 0;
        flex: 1 1 100%;
        font-size: 12px;
        color: var(--text);
        min-width: 0;
        overflow-wrap: anywhere;
    }

    .question.warn {
        color: var(--error);
    }

    .hint {
        margin: 0;
        flex: 1 1 100%;
        font-size: 11.5px;
        color: var(--warn);
    }

    .options {
        flex: 1 1 100%;
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px 16px;
    }

    .modes {
        display: flex;
        gap: 12px;
    }

    .field,
    .check {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 12px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .danger-check {
        color: var(--error);
    }

    .field select,
    .field input,
    .field textarea {
        height: 24px;
        padding: 0 6px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font: inherit;
        font-size: 12px;
        max-width: 220px;
    }

    .field.custom {
        flex: 1 1 100%;
        align-items: flex-start;
    }

    .field textarea {
        height: auto;
        flex: 1 1 auto;
        max-width: none;
        padding: 4px 6px;
        font-family: var(--mono);
        resize: vertical;
    }

    .advanced {
        flex: 1 1 100%;
        font-size: 12px;
        color: var(--text-dim);
    }

    .advanced summary {
        cursor: pointer;
        margin-bottom: 6px;
    }

    .answers {
        display: flex;
        gap: 6px;
        margin-left: auto;
        flex: 0 0 auto;
    }

    button {
        display: flex;
        align-items: center;
        gap: 6px;
        height: 24px;
        padding: 0 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        color: var(--text);
        white-space: nowrap;
    }

    button:hover:not(:disabled) {
        background: var(--bg-hover);
    }

    button:disabled {
        opacity: 0.5;
    }

    .plain {
        background: none;
        box-shadow: none;
        color: var(--text-dim);
    }

    .danger {
        color: var(--error);
    }
</style>
