// One Helm release, read in full.
//
// The table of releases comes through the subscription machinery like any other
// kind (see ./subscriptions.ts, which fetches rather than watches them). This is
// the other half: what the drawer shows when one of those rows is opened, which
// is the release's own record rather than a describe report -- a release has no
// Kubernetes kind for the describe path to resolve, and asking it to try is
// where "unknown resource kind: helmreleases" came from.
//
// Like the other stores here, it knows nothing about the workspace: it is handed
// the three fields that name a release, and whoever asked owns telling the user
// how it went.

import { HelmService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as helmcli from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/helmcli/models.js';
import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

/** What names one release: a cluster, a namespace and a name. */
export interface ReleaseRef {
    contextId: string;
    namespace: string;
    name: string;
}

/** One release as the drawer has it: what was read, or why it could not be. */
export interface ReleaseState {
    detail: kube.HelmReleaseDetail | null;
    loading: boolean;
    error: string;
}

const UNREAD: ReleaseState = { detail: null, loading: false, error: '' };

/**
 * Where helm is, before anyone has asked.
 *
 * Not found rather than found: the buttons that need it start disabled and
 * become available when the probe says so, which is the safe way round. The
 * other way, they would be live for a moment on every machine without helm.
 */
const NO_TOOL: helmcli.Tool = { found: false, path: '', version: '', configured: false, reason: '' };

function key(ref: ReleaseRef): string {
    return `${ref.contextId}#${ref.namespace}#${ref.name}`;
}

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Helm {
    private states = $state<Record<string, ReleaseState>>({});
    /**
     * Where helm is on this machine.
     *
     * Held here rather than read at each button because four buttons and the
     * settings view all ask the same question, and because the answer changes
     * only when the user changes the setting -- at which point `probe` is
     * called again.
     */
    tool = $state<helmcli.Tool>(NO_TOOL);
    /** Whether the probe has ever run, so the bar can wait rather than flicker. */
    probed = $state(false);
    /**
     * Which read each release is waiting on. Two can be in flight at once -- a
     * refresh started while the first is still out -- and only the newest may
     * land, so a slow one answering late cannot put the drawer back to what it
     * said before.
     */
    private loads: Record<string, number> = {};

    /** What is known about a release. Never null: an unread one reads as empty. */
    stateOf(ref: ReleaseRef): ReleaseState {
        return this.states[key(ref)] ?? UNREAD;
    }

    /**
     * Reads one release.
     *
     * `quiet` re-reads without raising the loading flag, for a refresh after an
     * upgrade or a rollback: the drawer showing a moment-old release is better
     * than blanking it to "Reading…" every time something is done to it.
     */
    async load(ref: ReleaseRef, quiet = false): Promise<void> {
        const at = key(ref);
        const attempt = (this.loads[at] = (this.loads[at] ?? 0) + 1);

        const previous = this.states[at] ?? UNREAD;
        this.states[at] = { ...previous, loading: !quiet || previous.detail === null, error: '' };

        try {
            const detail = await HelmService.Detail(ref.contextId, ref.namespace, ref.name);
            if (this.loads[at] !== attempt) return;
            this.states[at] = { detail, loading: false, error: '' };
        } catch (err) {
            if (this.loads[at] !== attempt) return;
            // The detail is dropped rather than kept beside the error: a
            // release that has just been uninstalled elsewhere would otherwise
            // go on being shown, with a complaint next to it.
            this.states[at] = { detail: null, loading: false, error: message(err) };
        }
    }

    /**
     * Asks where helm is.
     *
     * A failure is recorded as "not found" rather than raised: the caller is a
     * component drawing buttons, and a cluster-less question that could not be
     * answered means the same thing to it as a definite no.
     */
    async probe(): Promise<helmcli.Tool> {
        try {
            this.tool = await HelmService.Tool();
        } catch (err) {
            this.tool = { ...NO_TOOL, reason: message(err) };
        }
        this.probed = true;
        return this.tool;
    }

    /**
     * Re-releases one release: a new chart version, new values, or both.
     *
     * The values are the complete set the release should have afterwards, not
     * additions to what it has -- what the editor shows is what the release
     * gets, and deleting a line deletes the value. See helmcli.UpgradeRequest.
     */
    async upgrade(
        ref: ReleaseRef,
        chart: string,
        version: string,
        values: string,
    ): Promise<string> {
        return this.change(ref, () =>
            HelmService.Upgrade(ref.contextId, ref.namespace, ref.name, chart, version, values),
        );
    }

    /** Returns a release to an earlier revision. Needs no chart reference. */
    async rollback(ref: ReleaseRef, revision: number): Promise<string> {
        return this.change(ref, () =>
            HelmService.Rollback(ref.contextId, ref.namespace, ref.name, revision),
        );
    }

    /**
     * Removes a release and everything it installed.
     *
     * Unlike the other two it does not re-read afterwards: there is nothing
     * left to read, and asking would show the user a failure where their
     * release used to be.
     */
    async uninstall(ref: ReleaseRef, keepHistory: boolean): Promise<string> {
        try {
            return await HelmService.Uninstall(ref.contextId, ref.namespace, ref.name, keepHistory);
        } catch (err) {
            throw new Error(message(err));
        }
    }

    /**
     * The versions of a chart the machine's repositories offer, newest first.
     *
     * An empty list is an ordinary answer rather than a failure: Helm's release
     * record does not say where a chart came from, so one installed from an OCI
     * registry or a local path is not in any index to be found in.
     */
    async versions(chart: string): Promise<helmcli.ChartVersion[]> {
        try {
            return (await HelmService.ChartVersions(chart)) ?? [];
        } catch (err) {
            throw new Error(message(err));
        }
    }

    /**
     * Runs one change and re-reads the release it changed, so the drawer shows
     * the new revision rather than the one that was there when the button was
     * pressed.
     *
     * The re-read is quiet: the release on screen is a moment out of date,
     * which is better than blanking the drawer to "Reading…" after every
     * upgrade.
     */
    private async change(ref: ReleaseRef, call: () => Promise<string>): Promise<string> {
        let output: string;
        try {
            output = await call();
        } catch (err) {
            // Even a failed upgrade may have moved the release -- a failed
            // revision is still a revision -- so the drawer is re-read either
            // way before the error is passed on.
            await this.load(ref, true);
            throw new Error(message(err));
        }
        await this.load(ref, true);
        return output;
    }

    /** Drops what is held about a release, with the panel that showed it. */
    forget(ref: ReleaseRef): void {
        const at = key(ref);
        // Takes the read counter with it, so whatever is in flight has already
        // lost and cannot fill in a drawer the user has closed.
        this.loads[at] = (this.loads[at] ?? 0) + 1;
        delete this.states[at];
    }
}

export const helm = new Helm();
