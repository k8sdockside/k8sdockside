<!--
  The theme gallery, and everything about installing themes of your own.

  It is its own section rather than a row in Appearance because it stopped being
  a setting the moment there was more than one of it: choosing a theme is
  picking from pictures, and installing one is a small piece of file management.
  Neither fits on a line next to a zoom slider.

  In the web version that file management is on the server: nothing there can
  open a folder on the user's machine or pick one from it, and the folders
  every user's themes come from are the administrators' to change. Choosing a
  theme is still everyone's.
-->
<script lang="ts">
    import { session } from '../../state/session.svelte';
    import { workspace } from '../../state/workspace.svelte';
    import Icon from '../Icon.svelte';
    import SettingsSection from './SettingsSection.svelte';
    import ThemePreview from './ThemePreview.svelte';

    // Which token list is open, if any. Deliberately not persisted: it is a
    // reference table you open to check a name and close again.
    let showTokens = $state(false);

    // The themes that ship with the app and the ones the user installed are
    // worth telling apart -- one set is ours to fix, the other is theirs.
    let builtin = $derived(workspace.themes.filter((t) => t.origin === 'builtin'));
    let installed = $derived(workspace.themes.filter((t) => t.origin !== 'builtin'));

    /** The folder a theme was read from, which is what the user can act on. */
    function folderOf(origin: string): string {
        const at = Math.max(origin.lastIndexOf('/'), origin.lastIndexOf('\\'));
        return at > 0 ? origin.slice(0, at) : origin;
    }

    function fileOf(origin: string): string {
        const at = Math.max(origin.lastIndexOf('/'), origin.lastIndexOf('\\'));
        return at >= 0 ? origin.slice(at + 1) : origin;
    }
</script>

<SettingsSection
    title="Themes"
    lede="A theme is a set of colours and nothing else — it cannot ship CSS or run code, which is what makes installing someone else's a reasonable thing to do. Everything the app draws follows the one you pick, the YAML editor included."
>
    {#if workspace.themeMissing}
        <p class="missing">
            <Icon name="alert" size={13} />
            <span>
                Your settings ask for <code>{workspace.theme}</code>, which is not installed. The app is wearing
                <strong>{workspace.activeTheme?.name}</strong> until it turns up — add the folder it lives in below, or
                pick another theme and the choice will be replaced.
            </span>
        </p>
    {/if}

    <h3>Built in</h3>
    <div class="gallery">
        {#each builtin as theme (theme.id)}
            {@render swatch(theme)}
        {/each}
    </div>

    {#if installed.length > 0}
        <h3>Installed</h3>
        <div class="gallery">
            {#each installed as theme (theme.id)}
                {@render swatch(theme)}
            {/each}
        </div>
    {/if}

    <h3>Your own themes</h3>
    <p class="note">
        Drop a <code>.json</code> file into the themes folder — or a whole folder of them, which is how a theme pack
        arrives. Files are read from the folder itself and one level into any subfolder, so a pack cloned or unzipped
        into a directory of its own is picked up as it is.
    </p>

    <div class="path-row">
        <Icon name="folder" size={13} />
        <span class="path selectable">{workspace.themeDir || '…'}</span>
        {#if !session.server}
            <button onclick={() => workspace.revealThemeDir()}>Open folder</button>
        {/if}
    </div>

    <div class="actions">
        {#if session.admin}
            <button class="primary" onclick={() => workspace.createExampleTheme()}>
                <Icon name="plus" size={14} /> Write a starter theme
            </button>
        {/if}
        {#if !session.server}
            <button onclick={() => workspace.addThemeFolder()}>
                <Icon name="folder-plus" size={14} /> Watch another folder
            </button>
        {/if}
        <button onclick={() => workspace.reloadThemes()}>
            <Icon name="refresh" size={14} /> Reload
        </button>
    </div>
    <p class="note">
        The starter theme is a complete file with every colour already filled in, so you can change one at a time and
        reload to see it. Themes are read at launch and whenever you press Reload — an editor open beside the app is
        the intended way to work.
    </p>

    {#if workspace.themeFolders.length > 0}
        <h3>Extra folders</h3>
        <ul class="paths">
            {#each workspace.themeFolders as folder (folder)}
                <li>
                    <Icon name="folder" size={13} />
                    <span class="path selectable">{folder}</span>
                    {#if session.admin}
                        <button
                            class="drop"
                            onclick={() => workspace.removeThemeFolder(folder)}
                            title="Stop reading themes from {folder}"
                            aria-label="Stop reading themes from {folder}"
                        >
                            <Icon name="close" size={12} />
                        </button>
                    {/if}
                </li>
            {/each}
        </ul>
    {/if}

    {#if workspace.themeProblems.length > 0}
        <h3>Would not load</h3>
        <p class="note">
            One bad file does not cost you the themes either side of it, but a theme that silently fails to appear is
            worse than one that appears with a reason.
        </p>
        <ul class="problems">
            {#each workspace.themeProblems as problem (problem.path + problem.message)}
                <li>
                    <Icon name="alert" size={13} />
                    <div>
                        <span class="path selectable">{problem.path}</span>
                        <p>{problem.message}</p>
                    </div>
                </li>
            {/each}
        </ul>
    {/if}

    <h3>Writing one</h3>
    <p class="note">
        A theme needs an <code>id</code>, a <code>name</code> and a <code>base</code> of <code>dark</code> or
        <code>light</code>. Every colour is optional: whatever you leave out is inherited from the built-in theme
        matching your base, so a three-colour theme is a real one and stays complete as the app grows colours you never
        saw. Give a file a <code>themes</code> array instead to ship several at once as a pack.
    </p>

    <pre class="example selectable">{`{
    "id": "acme-neon",
    "name": "Acme Neon",
    "tagline": "loud dark · neon pink",
    "base": "dark",
    "author": "you",
    "tokens": {
        "bg": "#0b0b12",
        "text": "#f2e9ff",
        "accent": "#ff3d9a",
        "accent-text": "#1a0010"
    }
}`}</pre>

    <button class="disclose" onclick={() => (showTokens = !showTokens)} aria-expanded={showTokens}>
        <Icon name={showTokens ? 'dot' : 'plus'} size={12} />
        {showTokens ? 'Hide' : 'Show'} all {workspace.themeTokens.length} colours a theme can set
    </button>

    {#if showTokens}
        <dl class="tokens">
            {#each workspace.themeTokens as token (token.name)}
                <dt>
                    <span class="chip" style:background={workspace.activeTheme?.resolved[token.name]}></span>
                    <code>{token.name}</code>
                </dt>
                <dd>{token.help}</dd>
            {/each}
        </dl>
    {/if}
</SettingsSection>

{#snippet swatch(theme: import('../../theme/apply').Theme)}
    {@const current = theme.id === workspace.theme}
    <div class="cell">
        <button
            class="card"
            class:current
            aria-pressed={current}
            onclick={() => workspace.setTheme(theme.id)}
            title={theme.origin === 'builtin' ? theme.name : theme.origin}
        >
            <ThemePreview {theme} size="tile" />
            <span class="label">
                <span class="name">
                    {theme.name}
                    {#if current}<Icon name="check" size={12} />{/if}
                </span>
                <span class="tagline">{theme.tagline || theme.base}</span>
            </span>
        </button>

        {#if theme.origin !== 'builtin'}
            <span class="from" title={theme.origin}>
                {#if theme.pack}{theme.pack} · {/if}{fileOf(theme.origin)}
                <span class="dim">in {folderOf(theme.origin)}</span>
            </span>
        {/if}
        {#if theme.warnings.length > 0}
            <span class="warn" title={theme.warnings.join('\n')}>
                <Icon name="alert" size={11} />
                {theme.warnings.length} readability warning{theme.warnings.length === 1 ? '' : 's'}
            </span>
        {/if}
    </div>
{/snippet}

<style>
    h3 {
        margin: 22px 0 8px;
        font-size: 11px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .note {
        margin: 0 0 12px;
        max-width: 68ch;
        font-size: 12px;
        line-height: 1.7;
        color: var(--text-faint);
    }

    /* Wide enough that a preview is still readable as an interface, and packed
       by auto-fill so the gallery reflows with the settings panel rather than
       fixing a column count that is wrong at some widths. */
    .gallery {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
        gap: 14px;
    }

    .cell {
        display: flex;
        flex-direction: column;
        gap: 4px;
        min-width: 0;
    }

    .card {
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 8px;
        border-radius: var(--radius);
        text-align: left;
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .card:hover {
        background: var(--bg-hover);
    }

    .card.current {
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 2px var(--accent);
    }

    .card:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 1px;
    }

    .label {
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 0;
    }

    .name {
        display: flex;
        align-items: center;
        gap: 5px;
        font-size: 12.5px;
        font-weight: 500;
        color: var(--text);
    }

    .tagline {
        font-size: 11px;
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .from,
    .warn {
        display: flex;
        align-items: center;
        gap: 4px;
        padding: 0 8px;
        font-size: 10px;
        font-family: var(--mono);
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .warn {
        color: var(--warn);
        font-family: var(--font);
    }

    .from .dim {
        opacity: 0.65;
    }

    .missing {
        display: flex;
        align-items: flex-start;
        gap: 8px;
        margin: 0 0 16px;
        padding: 10px 12px;
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        line-height: 1.7;
        color: var(--text-dim);
    }

    .missing :global(svg) {
        flex: 0 0 auto;
        margin-top: 3px;
        color: var(--warn);
    }

    .path-row {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 7px 10px;
        margin-bottom: 12px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .path {
        flex: 1 1 auto;
        min-width: 0;
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .path-row button {
        flex: 0 0 auto;
        padding: 3px 9px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .path-row button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 12px;
    }

    .actions button {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .actions button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .actions .primary {
        background: var(--accent);
        color: var(--accent-text);
        box-shadow: none;
    }

    .actions .primary:hover {
        filter: brightness(1.08);
        background: var(--accent);
        color: var(--accent-text);
    }

    .paths,
    .problems {
        list-style: none;
        margin: 0 0 12px;
        padding: 0;
    }

    .paths li,
    .problems li {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 6px 2px;
        border-bottom: 1px solid var(--border-soft);
    }

    .problems li {
        align-items: flex-start;
    }

    .problems :global(svg) {
        flex: 0 0 auto;
        margin-top: 2px;
        color: var(--error);
    }

    .problems div {
        min-width: 0;
    }

    .problems p {
        margin: 2px 0 0;
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .drop {
        display: grid;
        place-items: center;
        width: 20px;
        height: 20px;
        flex: 0 0 auto;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .drop:hover {
        background: var(--bg-hover);
        color: var(--error);
    }

    .example {
        margin: 0 0 14px;
        padding: 12px 14px;
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-family: var(--mono);
        font-size: 11px;
        line-height: 1.7;
        color: var(--text-dim);
        overflow-x: auto;
    }

    .disclose {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 12px;
        color: var(--text-dim);
    }

    .disclose:hover {
        color: var(--text);
    }

    .tokens {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 6px 16px;
        margin: 12px 0 0;
        align-items: baseline;
    }

    .tokens dt {
        display: flex;
        align-items: center;
        gap: 7px;
        white-space: nowrap;
    }

    .tokens code {
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text);
    }

    .tokens dd {
        margin: 0;
        font-size: 11.5px;
        line-height: 1.6;
        color: var(--text-faint);
    }

    /* The colour the current theme gives the token, next to its name: the
       fastest way to work out which token is the one you want to change. */
    .chip {
        width: 12px;
        height: 12px;
        flex: 0 0 auto;
        border-radius: 3px;
        box-shadow: inset 0 0 0 1px var(--border);
    }

    code {
        font-family: var(--mono);
        font-size: 11px;
        background: var(--bg-raised);
        border-radius: 3px;
        padding: 1px 5px;
    }
</style>
