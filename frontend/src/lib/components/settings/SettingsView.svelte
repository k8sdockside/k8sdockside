<!--
  The app-wide settings, opened as a tab like any other view.

  It is a nav rail and one section at a time rather than a single long scroll:
  the sections have nothing to do with each other, and a scroll would put the
  About text between the user and the sources list they came for.

  The section you are on survives leaving the tab and coming back. It has to be
  module state rather than component state: the shell wraps the active view in
  {#key activeTab.id}, so switching to a cluster tab destroys this component
  entirely and returning builds a new one, which would otherwise land back on
  the first section every time.

  It is deliberately not persisted to disk. Which page of settings you had open
  is where you were a moment ago, not a preference worth surviving a restart.
-->
<script lang="ts" module>
    const SECTIONS = [
        { id: 'appearance', label: 'Appearance', icon: 'sun' },
        { id: 'themes', label: 'Themes', icon: 'display' },
        { id: 'startpage', label: 'Start page', icon: 'image' },
        { id: 'datetime', label: 'Dates and times', icon: 'clock' },
        { id: 'plugins', label: 'Plugins', icon: 'puzzle' },
        { id: 'behaviour', label: 'Behaviour', icon: 'sliders' },
        { id: 'terminal', label: 'Terminal', icon: 'terminal' },
        { id: 'helm', label: 'Helm', icon: 'helm' },
        { id: 'sources', label: 'Kubeconfig sources', icon: 'folder' },
        { id: 'about', label: 'About', icon: 'info' },
    ] as const;

    type SectionId = (typeof SECTIONS)[number]['id'];

    /** The section to open on: the one chosen last, if it is still a section. */
    function startSection(): SectionId {
        const id = settingsSection.current;
        return SECTIONS.some((s) => s.id === id) ? (id as SectionId) : 'appearance';
    }
</script>

<script lang="ts">
    import Icon from '../Icon.svelte';
    import { rememberSection, settingsSection } from './section.svelte';
    import AboutSection from './AboutSection.svelte';
    import AppearanceSection from './AppearanceSection.svelte';
    import BehaviourSection from './BehaviourSection.svelte';
    import HelmSection from './HelmSection.svelte';
    import SourcesSection from './SourcesSection.svelte';
    import StartPageSection from './StartPageSection.svelte';
    import TerminalSection from './TerminalSection.svelte';
    import PluginsSection from './PluginsSection.svelte';
    import ThemesSection from './ThemesSection.svelte';
    import DateTimeSection from './DateTimeSection.svelte';

    let active = $state<SectionId>(startSection());

    function show(id: SectionId): void {
        active = id;
        rememberSection(id);
    }

    // Up and down move between sections, as in any list of tabs. Left and right
    // are deliberately not bound: they are how the tab strip is navigated, and
    // taking them here would trap the user inside the settings tab.
    function onRailKey(event: KeyboardEvent): void {
        const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
        if (step === 0) return;

        event.preventDefault();
        const at = SECTIONS.findIndex((s) => s.id === active);
        const next = SECTIONS[(at + step + SECTIONS.length) % SECTIONS.length];
        show(next.id);
        document.getElementById(`settings-nav-${next.id}`)?.focus();
    }
</script>

<div class="settings-view">
    <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
    <nav class="rail" role="tablist" aria-orientation="vertical" aria-label="Settings sections" onkeydown={onRailKey}>
        <p class="rail-heading">Settings</p>
        {#each SECTIONS as section (section.id)}
            <button
                id="settings-nav-{section.id}"
                role="tab"
                class="rail-item"
                class:active={active === section.id}
                aria-selected={active === section.id}
                aria-controls="settings-panel-{section.id}"
                tabindex={active === section.id ? 0 : -1}
                onclick={() => show(section.id)}
            >
                <Icon name={section.icon} size={15} />
                <span>{section.label}</span>
            </button>
        {/each}
    </nav>

    <div
        class="panel"
        id="settings-panel-{active}"
        role="tabpanel"
        aria-labelledby="settings-nav-{active}"
        tabindex="-1"
    >
        {#if active === 'appearance'}
            <AppearanceSection onshowthemes={() => show('themes')} />
        {:else if active === 'themes'}
            <ThemesSection />
        {:else if active === 'startpage'}
            <StartPageSection />
        {:else if active === 'datetime'}
            <DateTimeSection />
        {:else if active === 'plugins'}
            <PluginsSection />
        {:else if active === 'behaviour'}
            <BehaviourSection />
        {:else if active === 'terminal'}
            <TerminalSection />
        {:else if active === 'helm'}
            <HelmSection />
        {:else if active === 'sources'}
            <SourcesSection />
        {:else}
            <AboutSection />
        {/if}
    </div>
</div>

<style>
    .settings-view {
        display: flex;
        height: 100%;
        min-height: 0;
    }

    .rail {
        display: flex;
        flex-direction: column;
        gap: 2px;
        flex: 0 0 auto;
        width: 190px;
        padding: 16px 10px;
        border-right: 1px solid var(--border);
        background: var(--bg-sidebar);
        overflow-y: auto;
    }

    .rail-heading {
        margin: 0 0 10px 8px;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .rail-item {
        display: flex;
        align-items: center;
        gap: 9px;
        width: 100%;
        padding: 7px 9px;
        border-radius: var(--radius-sm);
        font-size: 12.5px;
        text-align: left;
        color: var(--text-dim);
    }

    .rail-item:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .rail-item.active {
        background: var(--bg-active);
        color: var(--text);
        font-weight: 500;
    }

    .rail-item:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: -2px;
    }

    .panel {
        flex: 1 1 auto;
        min-width: 0;
        overflow-y: auto;
        padding: 24px 28px 40px;
    }

    /* The panel is focused when a section is opened from the keyboard; it is a
       scroll container rather than a control, so it should not draw a ring. */
    .panel:focus {
        outline: none;
    }
</style>
