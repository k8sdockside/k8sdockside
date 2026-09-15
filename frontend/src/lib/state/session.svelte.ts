// Which version of the app this window is, and who is using it.
//
// The same bundle is served by the desktop app and by the web version, where
// the Go services run as a server behind a sign-in page. Most of the window is
// identical in both; what is not is everything that needs the user's own
// machine -- a file picker, a port on localhost, a terminal emulator, a release
// to download -- and, in the web version, what a signed-in user who is not an
// administrator may change. The window cannot tell which it is from anything it
// can see, so it asks, once, and every part that differs reads the answer here.
//
// Until the answer arrives, and if it never does, this says "desktop". That is
// the safe way round: the desktop app must never lose a feature because a call
// failed, and a web version that briefly offers a button the backend would
// refuse anyway has cost nothing but a flicker.

// Read off the module rather than named in the import. Every other store names
// its service, but this is the one thing every part of the window asks about,
// so it is reached from nearly everywhere -- including from under test doubles
// of the services module written before this service existed. A named import
// of a name such a module does not have fails as the module is linked, where
// nothing can catch it; read at call time, it fails inside the `try` below and
// is answered like any other backend that could not say.
import * as services from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import type * as main from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services/models.js';

/** What the backend says about this window. */
export type SessionInfo = main.SessionInfo;

/** The desktop app: one user who owns everything, and no pages of its own. */
const DESKTOP: SessionInfo = {
    server: false,
    username: '',
    name: '',
    admin: true,
    accountUrl: '',
    adminUrl: '',
    logoutUrl: '',
};

function text(value: unknown): string {
    return typeof value === 'string' ? value : '';
}

/**
 * The answer as this side will use it.
 *
 * Checked field by field rather than trusted whole: a backend without the
 * service may answer with whatever it answers unknown calls with, and anything
 * that is not plainly the web version is taken as the desktop app -- which is
 * also why `admin` is only read when `server` is true. Desktop is always admin.
 */
function adopt(info: unknown): SessionInfo {
    if (!info || typeof info !== 'object') return DESKTOP;
    const answer = info as Partial<Record<keyof SessionInfo, unknown>>;
    if (answer.server !== true) return DESKTOP;
    return {
        server: true,
        username: text(answer.username),
        name: text(answer.name),
        admin: answer.admin === true,
        accountUrl: text(answer.accountUrl),
        adminUrl: text(answer.adminUrl),
        logoutUrl: text(answer.logoutUrl),
    };
}

class Session {
    /** The answer, or the desktop app's until there is one. */
    info = $state<SessionInfo>(DESKTOP);
    /** True once the backend has been asked, whatever it said. */
    loaded = $state(false);

    /** The one question in flight or answered, shared by everyone who asks. */
    private asking: Promise<void> | null = null;

    /** Whether this is the web version, which leaves out what needs the user's machine. */
    get server(): boolean {
        return this.info.server;
    }

    /**
     * Whether the user may change what everyone shares: clusters, plugins,
     * themes. Always true in the desktop app.
     */
    get admin(): boolean {
        return this.info.admin;
    }

    get username(): string {
        return this.info.username;
    }

    get name(): string {
        return this.info.name;
    }

    /** The name to show for the user: the one they gave, or the one they sign in with. */
    get displayName(): string {
        return this.info.name || this.info.username;
    }

    get accountUrl(): string {
        return this.info.accountUrl;
    }

    get adminUrl(): string {
        return this.info.adminUrl;
    }

    get logoutUrl(): string {
        return this.info.logoutUrl;
    }

    /**
     * The administration page clusters are added on, or empty where there is
     * none. In the web version a cluster is a kubeconfig uploaded there, not a
     * file picked from a disk the browser cannot see.
     */
    get clustersUrl(): string {
        const base = this.info.adminUrl.replace(/\/+$/, '');
        return base ? `${base}/clusters` : '';
    }

    /**
     * Asks the backend which version this is. Asked once: the answer cannot
     * change while the window is open, and several parts of the window want it
     * as they start, so the second caller waits on the first one's question
     * rather than asking again.
     */
    load(): Promise<void> {
        this.asking ??= this.ask();
        return this.asking;
    }

    private async ask(): Promise<void> {
        try {
            this.info = adopt(await services.SessionService.Info());
        } catch {
            // An older backend, or none yet: the desktop app, which is what
            // this already says. Not worth a line in the status bar -- there
            // is nothing the user could do about it, and nothing is missing.
        } finally {
            this.loaded = true;
        }
    }
}

export const session = new Session();
