// Themes and the preferences: terminal, helm, the start page, metrics.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { MetricsService, SettingsService, ThemeService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import {
    adoptSettings,
    type AlertMode,
    type ContextSort,
    type Density,
    type Settings,
} from '../adopt';
import { changes } from '../changes.svelte';
import { terminals } from '../terminals.svelte';
import { type DateTimeSettings } from '../../datetime.svelte';
import { notices } from '../notices.svelte';
import { rememberSection } from '../../components/settings/section.svelte';
import { adoptSource, type MetricsSource } from '../../charts/adopt';
import { adoptCatalogue, adoptTokens } from '../../theme/adopt';
import {
    message,
} from './helpers';
import { WorkspacePlugins } from './plugins.svelte';

export abstract class WorkspacePreferences extends WorkspacePlugins {
    /**
     * Reads the theme catalogue. Also the "reload" the settings view offers,
     * which is how a theme edited in an editor gets picked up without
     * restarting the app.
     */
    async loadThemes(): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.List());
            if (this.themeTokens.length === 0) {
                this.themeTokens = adoptTokens(await ThemeService.Tokens());
            }
        } catch (err) {
            notices.fail(`Could not read themes: ${message(err)}`);
        }
    }

    /** Wears a theme. The id is stored as given, whether or not we have it. */
    setTheme(theme: string): void {
        this.updatePreferences({ theme });
    }

    // ----- themes ---------------------------------------------------------

    /** Rereads the theme folders, picking up files edited since launch. */
    async reloadThemes(): Promise<void> {
        await this.loadThemes();
        const count = this.themes.length;
        notices.inform(`${count} theme${count === 1 ? '' : 's'} available`);
    }

    /** Opens the themes folder in the file manager, creating it if need be. */
    async revealThemeDir(): Promise<void> {
        try {
            await ThemeService.RevealDir();
        } catch (err) {
            notices.fail(`Could not open the themes folder: ${message(err)}`);
        }
    }

    /**
     * Writes a starter theme into the themes folder and reloads, so "write your
     * own" begins with a file that already works rather than a blank one.
     */
    async createExampleTheme(): Promise<void> {
        try {
            const path = await ThemeService.CreateExample();
            await this.loadThemes();
            notices.inform(`Wrote ${path}`);
        } catch (err) {
            notices.fail(`Could not write a starter theme: ${message(err)}`);
        }
    }

    /** Opens the native picker and reads themes from the folder chosen. */
    async addThemeFolder(): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.BrowseForFolder());
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not add the folder: ${message(err)}`);
        }
    }

    /** Stops reading themes from a folder. Nothing on disk is touched. */
    async removeThemeFolder(path: string): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.RemoveFolder(path));
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not drop the folder: ${message(err)}`);
        }
    }

    setDensity(density: Density): void {
        this.updatePreferences({ density });
    }

    setRestoreTabs(restoreTabs: boolean): void {
        this.updatePreferences({ restoreTabs });
    }

    setConfirmSourceRemoval(confirmSourceRemoval: boolean): void {
        this.updatePreferences({ confirmSourceRemoval });
    }

    setCheckForUpdates(checkForUpdates: boolean): void {
        this.updatePreferences({ checkForUpdates });
    }

    setDesktopNotifications(desktopNotifications: boolean): void {
        this.updatePreferences({ desktopNotifications });
    }

    /** Where cluster alerts go from now on. */
    setAlertMode(alerts: AlertMode): void {
        this.updatePreferences({ alerts, desktopNotifications: alerts === 'system' });
    }

    /** Snoozes the cluster alerts until a moment, or ends a snooze with null. */
    snoozeAlerts(until: Date | null): void {
        this.updatePreferences({ alertsSnoozedUntil: until ? until.toISOString() : '' });
    }

    /** Changes one or more of how dates and times are written. */
    setDateTime(patch: Partial<DateTimeSettings>): void {
        this.updatePreferences({ dateTime: { ...this.settings.preferences.dateTime, ...patch } });
    }

    setShowKubeconfigNames(showKubeconfigNames: boolean): void {
        this.updatePreferences({ showKubeconfigNames });
    }

    setContextSort(contextSort: ContextSort): void {
        this.updatePreferences({ contextSort });
    }

    /**
     * Steps the sidebar's sort on to the next order. One button rather than a
     * menu: there are three orders, the header is already crowded at narrow
     * widths, and the button draws which one is on.
     */
    cycleContextSort(): void {
        const orders: ContextSort[] = ['name', 'name-desc', 'kubeconfig'];
        const at = orders.indexOf(this.contextSort);
        this.setContextSort(orders[(at + 1) % orders.length]);
    }

    setShowLineNumbers(showLineNumbers: boolean): void {
        this.updatePreferences({ showLineNumbers });
    }

    // ----- terminals ------------------------------------------------------

    /** The built-in terminal's type size and how many lines it keeps. */
    terminalFontSize = $derived(this.settings.preferences.terminal.fontSize);
    terminalScrollback = $derived(this.settings.preferences.terminal.scrollback);

    /**
     * Changes part of the terminal settings.
     *
     * A patch rather than the whole record, because the settings view edits one
     * field at a time and every one of them is written through the preferences
     * writer the rest of this block uses -- one writer for the section, which is
     * what stops two of them undoing each other.
     */
    setTerminal(patch: Partial<Settings['preferences']['terminal']>): void {
        this.updatePreferences({ terminal: { ...this.settings.preferences.terminal, ...patch } });
    }

    // ----- helm -----------------------------------------------------------

    /** Where helm is, and how it is run when a release is changed. */
    helm = $derived(this.settings.preferences.helm);

    /**
     * Changes part of the helm settings. A patch, for the reason setTerminal
     * takes one: the settings view edits a field at a time through one writer.
     *
     * --atomic waits whether or not waiting was asked for, so turning it on
     * turns waiting on here too. The Go store enforces the same thing on read;
     * doing it here as well is what stops the checkbox sitting visibly off
     * beside the flag that implies it until the settings are next loaded.
     */
    setHelm(patch: Partial<Settings['preferences']['helm']>): void {
        const next = { ...this.settings.preferences.helm, ...patch };
        if (next.atomic) next.wait = true;
        this.updatePreferences({ helm: next });
    }

    // ----- the start page's picture -----------------------------------------

    /** Where the start page's picture comes from, which one is kept, and how often it changes. */
    background = $derived(this.settings.preferences.background);

    /**
     * Changes part of the background settings. A patch, for the reason
     * setTerminal takes one: the settings view edits a field at a time.
     */
    setBackground(patch: Partial<Settings['preferences']['background']>): void {
        this.updatePreferences({ background: { ...this.settings.preferences.background, ...patch } });
    }

    /** Opens Settings on the start page's section. */
    openBackgroundSettings(): void {
        rememberSection('startpage');
        this.openSettings();
    }

    /** Sets the window every chart on screen covers, in minutes. */
    setMetricsRange(metricsRange: number): void {
        this.updatePreferences({ metricsRange });
    }

    // ----- metrics --------------------------------------------------------

    /**
     * Where a context's metrics come from, asking the first time it is wanted.
     *
     * Returns null until the answer arrives rather than blocking: this is read
     * from a component's render, and the context settings panel showing "…" for
     * a moment is better than the sidebar waiting on a cluster.
     */
    metricsSourceFor(contextId: string): MetricsSource | null {
        const known = this.metricsSources[contextId];
        if (known) return known;
        void this.loadMetricsSource(contextId);
        return null;
    }

    private loading = new Set<string>();

    private async loadMetricsSource(contextId: string): Promise<void> {
        if (this.loading.has(contextId)) return;
        this.loading.add(contextId);
        try {
            const source = await MetricsService.Source(contextId);
            this.metricsSources = { ...this.metricsSources, [contextId]: adoptSource(source) };
        } catch {
            // Left unanswered rather than recorded as a failure: the panel that
            // actually draws charts reports its own errors, and this is only
            // what the settings row shows.
        } finally {
            this.loading.delete(contextId);
        }
    }

    /**
     * Points a context at a Prometheus, or clears the override with an empty
     * value. Returns the reason it was refused, or the empty string.
     */
    async setMetricsEndpoint(contextId: string, value: string): Promise<string> {
        try {
            const source = await MetricsService.SetEndpoint(contextId, value);
            this.metricsSources = { ...this.metricsSources, [contextId]: adoptSource(source) };
            this.settings = adoptSettings(await SettingsService.Get());
            return '';
        } catch (err) {
            return message(err);
        }
    }
}
