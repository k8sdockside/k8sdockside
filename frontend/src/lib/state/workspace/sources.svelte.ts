// Loading, and the kubeconfig sources: files, folders, contexts removed or brought back, connecting and letting go.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { KubeconfigService, ResourceService, SettingsService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { adoptFiles, adoptSettings } from '../adopt';
import { changes } from '../changes.svelte';
import { editors } from '../editor.svelte';
import { forwards } from '../forwards.svelte';
import { PANE_IDS } from '../panes';
import { clusters } from '../health.svelte';
import { fleet } from '../fleet.svelte';
import { notices } from '../notices.svelte';
import { session } from '../session.svelte';
import { detail } from '../detail.svelte';
import {
    isAppTab,
    message,
} from './helpers';
import { WorkspaceBase } from './base.svelte';

export abstract class WorkspaceSources extends WorkspaceBase {
    // ----- loading -------------------------------------------------------

    /** Loads settings and kubeconfigs, then restores the tabs from last time. */
    async load(): Promise<void> {
        try {
            this.settings = adoptSettings(await SettingsService.Get());
            this.configPath = await SettingsService.ConfigPath();
        } catch (err) {
            notices.fail(`Could not read settings: ${message(err)}`);
        }
        // Before the kubeconfig sync, which talks to clusters and can take a
        // while: the theme is what the user sees first, and waiting on a
        // cluster to find out what colour the window is would be the wrong way
        // round. The plugins go with it because the sidebar draws their rows
        // and the tab bar titles their tabs, both before any cluster answers.
        await Promise.all([this.loadThemes(), this.loadPlugins()]);
        // The forwards from last session, as rows waiting to be reconnected.
        // Nothing is dialled here: see PortForwardService for why launching the
        // app must not open every tunnel in the list.
        //
        // Not awaited -- the window should not wait on the forward list to come
        // up -- but the failure is still caught: an unhandled rejection here is
        // reported as an error against whatever happens to be running, and the
        // user is told nothing about the one read that actually failed.
        void forwards.load().catch((err: unknown) => {
            notices.fail(`Could not read the port forwards: ${message(err)}`);
        });
        await this.sync({ restoreTabs: true });
        this.loaded = true;
    }


    /**
     * Rescans every kubeconfig source. `restoreTabs` is for the initial load,
     * which reopens last session's tabs; a later sync keeps the tabs that are
     * open and only drops those whose context has gone.
     */
    async sync({ restoreTabs = false } = {}): Promise<void> {
        this.syncing = true;
        try {
            this.files = adoptFiles(await KubeconfigService.Sync());
            clusters.prune(this.contexts.map((c) => c.id));
            this.pruneCustomKinds();
            if (restoreTabs) {
                this.restorePanes();
            } else {
                this.dropTabsForMissingContexts();
                clusters.recheck();
                this.recheckCustomKinds();
                // Only for a sync the user asked for: a rescan that finds
                // nothing new looks identical to one that did not run.
                notices.inform(
                    `${this.contexts.length} context${this.contexts.length === 1 ? '' : 's'} ` +
                        `in ${this.files.length} file${this.files.length === 1 ? '' : 's'}`,
                );
            }
            this.ensureSelection();
        } catch (err) {
            notices.fail(`Sync failed: ${message(err)}`);
        } finally {
            this.syncing = false;
        }
    }

    /** Opens the native picker and adds whichever kubeconfig the user chose. */
    async addFile(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.BrowseForFile());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            // Picking several files at once can partly succeed. The message
            // names what failed, but the ones that worked are already stored,
            // so the sidebar has to be brought up to date regardless.
            notices.fail(message(err));
            await this.reload();
        }
    }

    /** Watches a folder, adding every kubeconfig in it. */
    async addFolder(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.BrowseForFolder());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Shows a hidden kubeconfig again. */
    async restoreFile(path: string): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.RestoreFile(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Shows a removed context again. */
    async restoreContext(id: string): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.RestoreContext(id));
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Asks before dropping a kubeconfig source, when the user has turned that
     * on. Every one of them is undoable -- a hidden file and a removed context
     * are both listed under Hidden, and a folder can be re-added -- so this is
     * off by default and exists for people who would rather not have to undo.
     */
    private confirmRemoval(question: string): boolean {
        if (!this.confirmSourceRemoval) return true;
        if (typeof window === 'undefined' || !window.confirm) return true;
        return window.confirm(question);
    }

    /** Stops watching a folder; its configs leave the sidebar with it. */
    async removeFolder(path: string): Promise<void> {
        if (!this.confirmRemoval(`Stop watching ${path}?\n\nIts kubeconfigs leave the sidebar with it.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveFolder(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Re-reads the current file list without rescanning the disk. */
    private async reload(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.Files());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch {
            // Already reporting the failure that got us here.
        }
    }

    /** Forgets a kubeconfig the user added, closing any tabs that depended on it. */
    async removeFile(path: string): Promise<void> {
        if (!this.confirmRemoval(`Remove ${path}?\n\nAny tabs open against its contexts will close.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveFile(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Removes one context from this app, leaving its file and the rest of the
     * contexts in it alone. Nothing is written to the kubeconfig: the removal
     * is the app's own, and it is listed under Hidden in the sidebar until it
     * is undone -- see restoreContext.
     */
    async removeContext(contextId: string): Promise<void> {
        const name = this.contexts.find((c) => c.id === contextId)?.name ?? contextId;
        if (!this.confirmRemoval(`Remove ${name}?\n\nAny tabs open on it will close. The kubeconfig is not changed, and a rescan will not bring it back — it is listed under Hidden in the sidebar until you show it again.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveContext(contextId));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Whether a context is connected, as far as the window can tell: it has
     * been asked whether it answers, or something is open on it. What the
     * sidebar offers Disconnect for.
     */
    isConnected(contextId: string): boolean {
        return clusters.of(contextId).status !== 'unknown' || this.allTabs.some((t) => !isAppTab(t) && t.contextId === contextId);
    }

    /** The contexts that are connected, in the sidebar's order. */
    connectedContexts = $derived(this.contexts.filter((c) => this.isConnected(c.id)));

    /**
     * Lets go of a context without removing it. Its tabs close, its port
     * forwards stop, its tree folds away and the app forgets that it
     * answered, so it looks as it did before it was first opened -- and
     * nothing talks to the cluster until it is opened again, which connects
     * afresh.
     *
     * Asked first only when an editor on it holds changes that are not in
     * the cluster: those are lost with the tab.
     */
    async disconnect(contextId: string, { quiet = false } = {}): Promise<void> {
        const context = this.contexts.find((c) => c.id === contextId);
        const name = context ? this.displayName(context) : contextId;
        const unsaved = this.allTabs.filter((t) => t.contextId === contextId && editors.isDirty(t.id)).length;
        if (
            unsaved > 0 &&
            typeof window !== 'undefined' &&
            window.confirm &&
            !window.confirm(`Disconnect from ${name}?\n\n${unsaved === 1 ? 'An editor has' : `${unsaved} editors have`} changes that are not in the cluster, and will be closed without them.`)
        ) {
            return;
        }

        for (const pane of PANE_IDS) this.retain(pane, (tab) => isAppTab(tab) || tab.contextId !== contextId);
        if (detail.target?.contextId === contextId) detail.close();
        for (const forward of forwards.forContext(contextId)) {
            if (forward.state === 'active' || forward.state === 'connecting') forwards.stop(forward.id);
        }
        this.expanded = this.expanded.filter((id) => id !== contextId);
        clusters.forget(contextId);
        fleet.forget(contextId);
        // What it served and which plugins it had go too: a context opened
        // again is asked afresh, which is how a product installed while it was
        // let go of gets its plugin back.
        this.forgetDefinitions(contextId);

        try {
            await ResourceService.Disconnect(contextId);
        } catch (err) {
            notices.fail(message(err));
            return;
        }
        if (!quiet) notices.inform(`Disconnected from ${name}`);
    }

    /** Disconnects every connected context. */
    async disconnectAll(): Promise<void> {
        const connected = this.connectedContexts.map((c) => c.id);
        for (const id of connected) await this.disconnect(id, { quiet: true });
        if (connected.length > 0) {
            notices.inform(`Disconnected from ${connected.length} context${connected.length === 1 ? '' : 's'}`);
        }
    }
}
