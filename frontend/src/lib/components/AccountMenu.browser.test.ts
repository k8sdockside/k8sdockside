import { afterEach, beforeAll, beforeEach, expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import AccountMenu from './AccountMenu.svelte';
import TopBar from './TopBar.svelte';
import { session } from '../state/session.svelte';

// Who is signed in, in the web version's title bar. What is under test is what
// the menu offers for each kind of user, and that the desktop app's bar does
// not change at all.

const DESKTOP = session.info;

const WEB = {
    server: true,
    username: 'ada',
    name: 'Ada Lovelace',
    admin: false,
    accountUrl: '/-/account',
    adminUrl: '/-/admin',
    logoutUrl: '/-/logout',
};

const trigger = () => page.getByRole('button', { name: /^Account:/ });

async function openMenu(): Promise<void> {
    await trigger().click();
    await expect.element(page.getByRole('menu', { name: 'Account' })).toBeVisible();
}

function hrefs(): Record<string, string> {
    return Object.fromEntries(
        [...document.querySelectorAll<HTMLAnchorElement>('[role="menu"] a')].map((a) => [
            a.textContent?.trim() ?? '',
            a.getAttribute('href') ?? '',
        ]),
    );
}

// The session is asked once per window. Asking here, before any test sets
// what it says, means no answer can arrive in the middle of one and replace it.
beforeAll(async () => {
    await session.load();
});

beforeEach(() => {
    document.body.innerHTML = '';
    session.info = WEB;
});

afterEach(() => {
    session.info = DESKTOP;
});

test('names who is signed in', async () => {
    render(AccountMenu);

    await expect.element(trigger()).toBeVisible();
    await expect.element(page.getByText('Ada Lovelace')).toBeVisible();
});

test('falls back to the sign-in name when the user gave no other', async () => {
    session.info = { ...WEB, name: '' };
    render(AccountMenu);

    await expect.element(page.getByRole('button', { name: 'Account: ada' })).toBeVisible();
});

// Pages of the same site, so ordinary links that replace this page -- not
// addresses handed to the runtime or opened in a new tab.
test('opens onto the account page and signing out, as ordinary links', async () => {
    render(AccountMenu);
    await openMenu();

    expect(hrefs()).toEqual({ Account: '/-/account', 'Sign out': '/-/logout' });
    for (const link of document.querySelectorAll('[role="menu"] a')) {
        expect(link.getAttribute('target')).toBeNull();
    }
});

test('offers Administration to an administrator only', async () => {
    session.info = { ...WEB, admin: true };
    render(AccountMenu);
    await openMenu();

    expect(hrefs()).toEqual({ Account: '/-/account', Administration: '/-/admin', 'Sign out': '/-/logout' });
});

test('Escape closes the menu and returns focus to its button', async () => {
    render(AccountMenu);
    await openMenu();

    await page.getByRole('menu').element().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

    await expect.element(page.getByRole('menu')).not.toBeInTheDocument();
    expect(document.activeElement).toBe(trigger().element());
});

test('is in the web version\'s title bar', async () => {
    render(TopBar);

    await expect.element(trigger()).toBeVisible();
});

// The desktop app has one user and nothing to sign out of.
test('is not in the desktop app\'s title bar', async () => {
    session.info = DESKTOP;
    render(TopBar);

    await expect.element(page.getByRole('button', { name: /^Notifications/ })).toBeVisible();
    expect(document.querySelector('button[aria-label^="Account"]')).toBeNull();
});
