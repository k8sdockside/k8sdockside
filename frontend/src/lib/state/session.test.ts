import { beforeAll, beforeEach, describe, expect, test, vi } from 'vitest';

const Info = vi.fn();

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    SessionService: { Info },
}));

// The first load of the module compiles it, runes and all, and in a full run --
// every other file compiling beside it -- that can take longer than a test's
// five seconds. It is paid once here, where the time belongs to no test, so
// fresh() below only re-evaluates a module that is already compiled.
beforeAll(async () => {
    await import('./session.svelte');
}, 30_000);

/** An answer as the web version's backend sends it. */
function web(over: Record<string, unknown> = {}) {
    return {
        server: true,
        username: 'ada',
        name: 'Ada Lovelace',
        admin: false,
        accountUrl: '/-/account',
        adminUrl: '/-/admin',
        logoutUrl: '/-/logout',
        ...over,
    };
}

/**
 * A store that has not been asked anything yet.
 *
 * The session is asked once per window and keeps the answer, so each test
 * needs a window of its own: the module is loaded afresh rather than given a
 * way to forget, which nothing outside a test should have.
 */
async function fresh() {
    vi.resetModules();
    return (await import('./session.svelte')).session;
}

beforeEach(() => {
    Info.mockReset().mockResolvedValue({ server: false, username: '', name: '', admin: true, accountUrl: '', adminUrl: '', logoutUrl: '' });
});

describe('before the backend has answered', () => {
    // The safe way round: a window that has not heard yet is the desktop app,
    // which loses nothing.
    test('it is the desktop app, and the user may change everything', async () => {
        const session = await fresh();

        expect(session.loaded).toBe(false);
        expect(session.server).toBe(false);
        expect(session.admin).toBe(true);
        expect(session.clustersUrl).toBe('');
    });
});

describe('the answer', () => {
    test('from the desktop app keeps every feature', async () => {
        const session = await fresh();

        await session.load();

        expect(session.loaded).toBe(true);
        expect(session.server).toBe(false);
        expect(session.admin).toBe(true);
    });

    test('from the web version names the user and the pages', async () => {
        Info.mockResolvedValueOnce(web());
        const session = await fresh();

        await session.load();

        expect(session.server).toBe(true);
        expect(session.admin).toBe(false);
        expect(session.username).toBe('ada');
        expect(session.displayName).toBe('Ada Lovelace');
        expect(session.accountUrl).toBe('/-/account');
        expect(session.logoutUrl).toBe('/-/logout');
        expect(session.clustersUrl).toBe('/-/admin/clusters');
    });

    test('falls back to the sign-in name when the user gave no other', async () => {
        Info.mockResolvedValueOnce(web({ name: '' }));
        const session = await fresh();

        await session.load();

        expect(session.displayName).toBe('ada');
    });

    test('an administrator may change what everyone shares', async () => {
        Info.mockResolvedValueOnce(web({ admin: true, adminUrl: '/-/admin/' }));
        const session = await fresh();

        await session.load();

        expect(session.admin).toBe(true);
        // A trailing slash on the address does not make a double one.
        expect(session.clustersUrl).toBe('/-/admin/clusters');
    });

    // Only the web version can take the user's rights away. Whatever else a
    // desktop answer says, the one user there owns everything.
    test('says admin in the desktop app, whatever it carries', async () => {
        Info.mockResolvedValueOnce({ ...web(), server: false, admin: false });
        const session = await fresh();

        await session.load();

        expect(session.server).toBe(false);
        expect(session.admin).toBe(true);
        expect(session.accountUrl).toBe('');
    });
});

describe('a backend that cannot say', () => {
    test('an older one, without the service, is the desktop app', async () => {
        Info.mockRejectedValueOnce(new Error('unknown method'));
        const session = await fresh();

        await expect(session.load()).resolves.toBeUndefined();

        expect(session.loaded).toBe(true);
        expect(session.server).toBe(false);
        expect(session.admin).toBe(true);
    });

    // A reply that is not a session at all -- whatever a server answers an
    // unknown call with -- must not be read as one.
    test('an answer that is not a session is the desktop app', async () => {
        Info.mockResolvedValueOnce('<!doctype html>');
        const session = await fresh();

        await session.load();

        expect(session.server).toBe(false);
        expect(session.admin).toBe(true);
    });
});

test('is asked once, however many parts of the window want to know', async () => {
    Info.mockResolvedValue(web());
    const session = await fresh();

    await Promise.all([session.load(), session.load()]);
    await session.load();

    expect(Info).toHaveBeenCalledOnce();
    expect(session.server).toBe(true);
});
