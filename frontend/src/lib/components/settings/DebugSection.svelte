<!--
  What `kubectl debug` from the app is made of: the image a debug container
  runs, the images offered beside the presets, and the profile it is given.

  These are defaults. The Debug button's form starts on them and every one can
  be changed for a single launch there; this is where they are changed for
  good. The presets are the app's and cannot be removed -- only the images the
  user added can.
-->
<script lang="ts">
    import { debugPresets, DEBUG_PROFILES } from '../../state/debug.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';

    let debug = $derived(workspace.debug);
    let adding = $state('');

    $effect(() => {
        void debugPresets.load();
    });

    function add(): void {
        const image = adding.trim();
        if (!image) return;
        workspace.addDebugImage(image, debugPresets.images());
        adding = '';
    }
</script>

<SettingsSection
    title="Debug"
    lede="What the Debug button on a pod or a node starts with: the same choices kubectl debug offers. Every one of them can be changed for a single launch in the form it opens."
>
    <SettingsRow
        label="Default image"
        hint="The image a debug container runs. Pick one of the presets or one you have added below."
    >
        <div class="images" role="radiogroup" aria-label="Default debug image">
            {#each debugPresets.list as preset (preset.image)}
                <label class="image" title={preset.hint}>
                    <input
                        type="radio"
                        name="debug-image"
                        value={preset.image}
                        checked={debug.image === preset.image}
                        onchange={() => workspace.setDebug({ image: preset.image })}
                    />
                    <span class="name">{preset.label}</span>
                    <span class="ref">{preset.image}</span>
                </label>
            {/each}
            {#each debug.images as image (image)}
                <div class="image custom">
                    <label>
                        <input
                            type="radio"
                            name="debug-image"
                            value={image}
                            checked={debug.image === image}
                            onchange={() => workspace.setDebug({ image })}
                        />
                        <span class="ref">{image}</span>
                    </label>
                    <button
                        class="remove"
                        aria-label="Remove {image}"
                        title="Remove {image}"
                        onclick={() => workspace.removeDebugImage(image)}
                    >
                        <Icon name="close" size={12} />
                    </button>
                </div>
            {/each}
            {#if debug.image && !debugPresets.images().includes(debug.image) && !debug.images.includes(debug.image)}
                <!-- A default that is in neither list: set from a file, or a
                     preset the app no longer offers. Shown so it is not a
                     choice nobody can see. -->
                <label class="image">
                    <input type="radio" name="debug-image" checked disabled />
                    <span class="ref">{debug.image}</span>
                </label>
            {/if}
        </div>
    </SettingsRow>

    <SettingsRow
        label="Add an image"
        hint="For a cluster that mirrors its own images, or a toolbox of your own. It is offered beside the presets from then on."
    >
        <form
            class="add"
            onsubmit={(event) => {
                event.preventDefault();
                add();
            }}
        >
            <input
                class="text"
                aria-label="Debug image to add"
                placeholder="registry/image:tag"
                spellcheck="false"
                autocomplete="off"
                bind:value={adding}
            />
            <button type="submit" disabled={adding.trim() === ''}>
                <Icon name="plus" size={12} />
                Add
            </button>
        </form>
    </SettingsRow>

    <SettingsRow
        label="Default profile"
        hint="The security profile a debug container is given. general adds what a debugger needs; restricted fits a namespace that enforces the restricted standard; sysadmin is privileged. legacy is deprecated in kubectl."
    >
        <select
            class="text narrow"
            aria-label="Default debug profile"
            value={debug.profile}
            onchange={(event) => workspace.setDebug({ profile: (event.currentTarget as HTMLSelectElement).value })}
        >
            {#each DEBUG_PROFILES as option (option.value)}
                <option value={option.value}>{option.label}</option>
            {/each}
        </select>
    </SettingsRow>
</SettingsSection>

<style>
    .images {
        display: flex;
        flex-direction: column;
        gap: 4px;
        min-width: 260px;
    }

    .image,
    .image.custom label {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 12px;
        color: var(--text);
    }

    .image.custom {
        justify-content: space-between;
    }

    .name {
        font-weight: 600;
    }

    .ref {
        font-family: var(--mono);
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .remove {
        display: flex;
        align-items: center;
        padding: 2px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .remove:hover {
        color: var(--error);
        background: var(--bg-hover);
    }

    .add {
        display: flex;
        gap: 6px;
    }

    .add button {
        display: flex;
        align-items: center;
        gap: 4px;
        height: 26px;
        padding: 0 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        color: var(--text);
    }

    .add button:disabled {
        opacity: 0.5;
    }

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
        width: 160px;
    }
</style>
