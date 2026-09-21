<!--
  The start page's picture: where it comes from, how often it changes, and
  which one to keep.

  The built-in pictures are drawn by the app -- each in a colour scheme of its
  own, or all in the theme's, and by night or by day as the theme is dark or
  light -- so the thumbnails here are drawn the same way. A folder of the user's own images is the desktop app's alone: in the
  web version it would be a folder on the server.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import { backdrop, type Picture } from '../../backgrounds/backdrop.svelte';
    import type { BackgroundSource } from '../../state/adopt';
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SegmentedControl from './SegmentedControl.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';

    let settings = $derived(workspace.background);

    let sources = $derived([
        { value: 'builtin', label: 'Built-in' },
        ...(session.server ? [] : [{ value: 'folder', label: 'My folder' }]),
        { value: 'none', label: 'None' },
    ]);

    const PALETTES = [
        { value: 'varied', label: 'Varied' },
        { value: 'theme', label: 'Match theme' },
    ];

    const INTERVALS = [
        { value: '5', label: '5 min' },
        { value: '15', label: '15 min' },
        { value: '30', label: '30 min' },
        { value: '60', label: '1 hour' },
    ];

    let pictures = $derived(backdrop.picturesFor(settings.source));
    let onScreen = $derived(backdrop.current(settings));
    let folderEmpty = $derived(settings.source === 'folder' && backdrop.folderPictures.length === 0);
    let problem = $state('');

    onMount(() => {
        void backdrop.loadFolder();
    });

    function thumb(picture: Picture): string {
        return backdrop.urlFor(backdrop.styled(picture, settings), workspace.activeTheme);
    }

    async function choose(): Promise<void> {
        problem = await backdrop.browseForFolder();
        if (!problem && backdrop.folder.path) workspace.setBackground({ source: 'folder', pinned: '' });
    }

    async function reveal(): Promise<void> {
        problem = await backdrop.revealFolder();
    }
</script>

<SettingsSection title="Start page">
    <SettingsRow
        label="Picture"
        hint="What is behind the start page. The built-in pictures are drawn by the app, and each time one comes round it is drawn afresh. My folder shows your own images instead."
    >
        <SegmentedControl
            options={sources}
            value={session.server && settings.source === 'folder' ? 'builtin' : settings.source}
            label="Start page picture"
            onchange={(v) => workspace.setBackground({ source: v as BackgroundSource, pinned: '' })}
        />
    </SettingsRow>

    {#if settings.source === 'folder' && !session.server}
        <SettingsRow
            label="Folder"
            hint="PNG, JPEG, WebP, GIF and AVIF files directly inside it are shown, in turn or one of them kept. Folders inside it are not read."
        >
            <div class="folder">
                {#if backdrop.folder.path}
                    <code class="path" title={backdrop.folder.path}>{backdrop.folder.path}</code>
                {/if}
                <div class="buttons">
                    <button class="jump" onclick={() => void choose()}>
                        <Icon name="folder" size={13} />
                        {backdrop.folder.path ? 'Change…' : 'Choose a folder…'}
                    </button>
                    {#if backdrop.folder.path}
                        <button class="jump" onclick={() => void reveal()}>Show</button>
                        <button class="jump" onclick={() => void backdrop.loadFolder()} title="Read the folder again">
                            <Icon name="refresh" size={13} />
                        </button>
                        <button class="jump" onclick={() => void backdrop.clearFolder()}>Forget</button>
                    {/if}
                </div>
                {#if backdrop.folder.problem || problem}
                    <p class="note failed">{problem || backdrop.folder.problem}</p>
                {:else if folderEmpty}
                    <p class="note">
                        {backdrop.folder.path ? 'No images in this folder yet' : 'No folder chosen yet'} — the built-in pictures are shown until there are.
                    </p>
                {:else}
                    <p class="note">{backdrop.folderPictures.length} {backdrop.folderPictures.length === 1 ? 'image' : 'images'}</p>
                {/if}
            </div>
        </SettingsRow>
    {/if}

    {#if settings.source !== 'none'}
        {#if settings.source === 'builtin' || folderEmpty}
            <SettingsRow
                label="Colours"
                hint="Varied gives every picture a colour scheme of its own — cyan, violet, emerald and more — and a different one each time it changes. Match theme draws them all in your theme's colours. Either way a dark theme gets a picture by night and a light theme one by day."
            >
                <SegmentedControl
                    options={PALETTES}
                    value={settings.palette}
                    label="Picture colours"
                    onchange={(v) => workspace.setBackground({ palette: v as 'varied' | 'theme' })}
                />
            </SettingsRow>
        {/if}

        <SettingsRow
            label="Change every"
            hint={settings.pinned
                ? 'One picture is kept, so it does not change. Choose "Take turns" below to have them change again.'
                : 'How long one picture stays before the next. It changes when the start page is showing, and straight away if it has been longer than this since you last saw it.'}
        >
            <SegmentedControl
                options={INTERVALS}
                value={String(settings.minutes)}
                label="Change the picture every"
                onchange={(v) => workspace.setBackground({ minutes: Number(v) })}
            />
        </SettingsRow>

        <div class="gallery-head">
            <h3>Pictures</h3>
            <p>Choose one to keep it, or let them take turns.</p>
        </div>
        <div class="gallery" role="radiogroup" aria-label="Start page picture">
            <button
                class="tile turns"
                class:current={!settings.pinned}
                role="radio"
                aria-checked={!settings.pinned}
                onclick={() => workspace.setBackground({ pinned: '' })}
            >
                <span class="turns-art">
                    <Icon name="refresh" size={22} />
                </span>
                <span class="caption">Take turns</span>
            </button>
            {#each pictures as picture (picture.id)}
                {@const kept = settings.pinned === picture.id}
                <button
                    class="tile"
                    class:current={kept}
                    class:showing={!settings.pinned && onScreen?.id === picture.id}
                    role="radio"
                    aria-checked={kept}
                    title={picture.kind === 'scene' ? picture.blurb : picture.name}
                    onclick={() => workspace.setBackground({ pinned: picture.id })}
                >
                    <img src={thumb(picture)} alt="" loading="lazy" />
                    <span class="caption">
                        {picture.name}
                        {#if !settings.pinned && onScreen?.id === picture.id}<span class="now">now</span>{/if}
                    </span>
                </button>
            {/each}
        </div>
    {/if}
</SettingsSection>

<style>
    .folder {
        display: flex;
        flex-direction: column;
        align-items: flex-end;
        gap: 8px;
        max-width: 420px;
    }

    .path {
        max-width: 100%;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-dim);
        background: var(--bg-raised);
        border-radius: 3px;
        padding: 2px 6px;
    }

    .buttons {
        display: flex;
        flex-wrap: wrap;
        justify-content: flex-end;
        gap: 6px;
    }

    .jump {
        display: flex;
        align-items: center;
        gap: 7px;
        padding: 5px 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        color: var(--text);
    }

    .jump:hover {
        background: var(--bg-hover);
    }

    .note {
        margin: 0;
        font-size: 11.5px;
        color: var(--text-faint);
        text-align: right;
    }

    .note.failed {
        color: var(--error);
    }

    .gallery-head {
        padding-top: 14px;
    }

    .gallery-head h3 {
        margin: 0;
        font-size: 13px;
        font-weight: 600;
        color: var(--text);
    }

    .gallery-head p {
        margin: 2px 0 10px;
        font-size: 12px;
        color: var(--text-dim);
    }

    .gallery {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(168px, 1fr));
        gap: 10px;
        padding-bottom: 8px;
    }

    .tile {
        display: flex;
        flex-direction: column;
        gap: 6px;
        padding: 5px;
        border-radius: 8px;
        text-align: left;
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .tile:hover {
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .tile.current {
        box-shadow:
            inset 0 0 0 2px var(--accent),
            0 0 0 3px color-mix(in srgb, var(--accent) 22%, transparent);
    }

    .tile img,
    .turns-art {
        display: block;
        width: 100%;
        aspect-ratio: 16 / 10;
        object-fit: cover;
        border-radius: 5px;
        background: var(--bg);
    }

    .turns-art {
        display: grid;
        place-items: center;
        color: var(--accent);
        background:
            linear-gradient(135deg, color-mix(in srgb, var(--accent) 22%, transparent), transparent 60%),
            var(--bg);
    }

    .caption {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 0 3px 2px;
        font-size: 12px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .tile.current .caption {
        color: var(--text);
    }

    .now {
        margin-left: auto;
        font-size: 9.5px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--accent);
    }
</style>
