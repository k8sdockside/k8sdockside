import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

const OpenURL = vi.fn();

vi.mock('@wailsio/runtime', () => ({
    Browser: { OpenURL },
}));

const { openExternal, onExternalClick } = await import('./links');
const { notices } = await import('./state/notices.svelte');
const { session } = await import('./state/session.svelte');

const DESKTOP = session.info;
const WEB = { ...DESKTOP, server: true, admin: false, username: 'ada', accountUrl: '/-/account', adminUrl: '/-/admin', logoutUrl: '/-/logout' };

let windowOpen: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
    OpenURL.mockReset().mockResolvedValue(undefined);
    windowOpen = vi.spyOn(window, 'open').mockReturnValue(null);
    notices.dismiss();
});

afterEach(() => {
    windowOpen.mockRestore();
    session.info = DESKTOP;
});

describe('in the desktop app', () => {
    // The webview opens no windows of its own, so a link out has to be handed
    // to the machine's browser.
    test('a link goes to the browser through the runtime', async () => {
        await openExternal('https://kubernetes.io/docs/');

        expect(OpenURL).toHaveBeenCalledWith('https://kubernetes.io/docs/');
        expect(windowOpen).not.toHaveBeenCalled();
    });

    test('a browser that will not open is said, with the address to copy', async () => {
        OpenURL.mockRejectedValueOnce(new Error('no browser'));

        await openExternal('https://kubernetes.io/docs/');

        expect(notices.current?.text).toBe('Could not open https://kubernetes.io/docs/ in your browser');
    });
});

describe('in the web version', () => {
    beforeEach(() => {
        session.info = WEB;
    });

    // The runtime would open it on the server, where nobody is looking.
    test('a link opens in a new tab of the browser the window is in', async () => {
        await openExternal('https://kubernetes.io/docs/');

        expect(windowOpen).toHaveBeenCalledWith('https://kubernetes.io/docs/', '_blank', 'noopener,noreferrer');
        expect(OpenURL).not.toHaveBeenCalled();
    });

    test('the click handler keeps the page where it is and opens the tab', () => {
        const event = new MouseEvent('click', { cancelable: true });

        onExternalClick('https://github.com/k8sdockside/k8sdockside')(event);

        expect(event.defaultPrevented).toBe(true);
        expect(windowOpen).toHaveBeenCalledWith('https://github.com/k8sdockside/k8sdockside', '_blank', 'noopener,noreferrer');
    });
});

// A plugin's docs link comes from a file, and "open whatever this says" is
// not a thing to offer a file -- in either version.
describe.each([
    ['the desktop app', () => (session.info = DESKTOP)],
    ['the web version', () => (session.info = WEB)],
])('in %s, an address that is not a web page', (_name, arrange) => {
    test.each(['file:///etc/passwd', 'javascript:alert(1)', 'not a url'])('is refused: %s', async (url) => {
        arrange();

        await openExternal(url);

        expect(OpenURL).not.toHaveBeenCalled();
        expect(windowOpen).not.toHaveBeenCalled();
        expect(notices.current?.text).toBe(`Not a web address: ${url}`);
    });
});
