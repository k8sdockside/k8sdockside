// Whether a newer release of the app exists, and whether the user has heard.
//
// The backend does the asking -- once shortly after launch and every few hours
// after, or whenever this side asks -- and pushes what it learnt. This side
// keeps only the latest answer, and offers the two things that can be done with
// it: open the release, or mark the notice as read so the bell goes quiet.
//
// None of it applies to the web version. There the app is whatever the server
// was deployed with, and a newer release is news for whoever runs the server,
// not for the person in the browser, who could neither download nor install
// it. So there nothing is asked for, and nothing is ever unread.

import { Events } from '@wailsio/runtime';
import { UpdateService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import type * as main from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services/models.js';
import { session } from './session.svelte';

/** What the backend knows about releases. */
export type UpdateStatus = main.UpdateStatus;

/** One published release, as much of it as the app shows. */
export type Release = NonNullable<UpdateStatus['latest']>;

/** The status before the backend has been asked anything. */
const UNKNOWN: UpdateStatus = { current: '', latest: null, newer: false, unread: false, checkedAt: '', error: '', install: '', download: '' };

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Updates {
    /** The latest answer, whether pushed or asked for. */
    status = $state<UpdateStatus>(UNKNOWN);
    /** True while a check this side asked for is in flight. */
    checking = $state(false);
    /** True once the backend has been asked what it knows, whatever it knew. */
    loaded = $state(false);

    constructor() {
        Events.On('update:status', (event: { data: UpdateStatus }) => {
            if (event.data) this.status = event.data;
        });
    }

    /** The newest release, once a check has succeeded. */
    get latest(): Release | null {
        return this.status.latest;
    }

    /** Whether a release newer than this build exists. Never, in the web version. */
    get available(): boolean {
        if (session.server) return false;
        return this.status.newer && this.status.latest !== null;
    }

    /** Whether that release is still news: newer, and not yet marked as read. */
    get unread(): boolean {
        if (session.server) return false;
        return this.status.unread;
    }

    /**
     * The address of the latest release's file for this install, or empty
     * when the backend found none -- it knows what this build is, and the
     * release page is always there instead.
     */
    get download(): string {
        return this.status.download ?? '';
    }

    /** The file the download is, for the button to name. */
    get downloadName(): string {
        const at = this.download.lastIndexOf('/');
        return at === -1 ? this.download : this.download.slice(at + 1);
    }

    /** Reads what the backend already knows, without asking GitHub. */
    async load(): Promise<void> {
        // The bell mounts before the window has heard which version it is,
        // so it waits for that rather than asking first and ignoring the
        // answer. The session is asked once; this shares that question.
        await session.load();
        if (session.server) {
            this.loaded = true;
            return;
        }
        try {
            this.status = await UpdateService.Status();
        } catch {
            // The bell is decoration until something is known, and a backend
            // that cannot answer yet is not worth a message in the status bar.
            // The first automatic check arrives as a push a few seconds in.
        } finally {
            this.loaded = true;
        }
    }

    /** Asks GitHub now, whether or not automatic checks are on. Not in the web version. */
    async check(): Promise<void> {
        if (session.server) return;
        this.checking = true;
        try {
            this.status = await UpdateService.Check();
        } catch (err) {
            // A check GitHub refused comes back as a status carrying the
            // reason, so this is the call itself failing. What was known
            // before is kept, with the failure beside it.
            this.status = { ...this.status, error: message(err) };
        } finally {
            this.checking = false;
        }
    }

    /** Puts the notice about the latest release away, here and on disk. */
    async markRead(): Promise<void> {
        this.status = await UpdateService.MarkRead();
    }

    /** Sends the latest release's page to the browser. */
    async openRelease(): Promise<void> {
        await UpdateService.OpenRelease();
    }

    /** Sends the latest release's file for this install to the browser. */
    async openDownload(): Promise<void> {
        await UpdateService.OpenDownload();
    }
}

export const updates = new Updates();
