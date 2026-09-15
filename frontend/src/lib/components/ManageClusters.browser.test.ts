import { afterEach, beforeAll, beforeEach, expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Sidebar from './Sidebar.svelte';
import { session } from '../state/session.svelte';
import { workspace } from '../state/workspace.svelte';

// Where clusters come from, in each version. The desktop app adds kubeconfigs
// from the user's own disk; the web version has no picker that could reach it,
// so an administrator uploads them on the gateway's page instead -- and only an
// administrator may change the list everyone shares.

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

const CTX = { id: '/c::prod', name: 'prod', cluster: 'prod', user: 'admin', namespace: '', server: '', file: '/c', current: false };

const manage = () => document.querySelector<HTMLAnchorElement>('a[aria-label="Manage clusters"]');
const buttonsNamed = (prefix: string) => document.querySelectorAll(`button[aria-label^="${prefix}"]`).length;

// Asked once, before anything sets what it says, so no answer can arrive in
// the middle of a test and replace it.
beforeAll(async () => {
    await session.load();
});

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [CTX] }];
    workspace.settings.preferences.showKubeconfigNames = true;
    workspace.loaded = true;
});

afterEach(() => {
    session.info = DESKTOP;
});

test('the desktop app adds kubeconfigs from disk, and removes them', async () => {
    render(Sidebar);

    await expect.element(page.getByRole('button', { name: 'Add kubeconfig files' })).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Watch a folder of kubeconfigs' })).toBeVisible();
    expect(manage()).toBeNull();
    // The file's heading and the context's own row.
    expect(buttonsNamed('Remove')).toBe(2);
});

test('an administrator in the web version gets the clusters page instead of the pickers', async () => {
    session.info = { ...WEB, admin: true };
    render(Sidebar);

    await expect.element(page.getByRole('link', { name: 'Manage clusters' })).toBeVisible();
    expect(manage()?.getAttribute('href')).toBe('/-/admin/clusters');
    // Same tab: a page of the same site, not an address handed elsewhere.
    expect(manage()?.getAttribute('target')).toBeNull();
    expect(buttonsNamed('Add kubeconfig')).toBe(0);
    expect(buttonsNamed('Watch a folder')).toBe(0);
    expect(buttonsNamed('Remove')).toBe(2);
});

test('anyone else in the web version sees the clusters and changes none of them', async () => {
    session.info = WEB;
    render(Sidebar);

    await expect.element(page.getByText('prod')).toBeVisible();
    expect(manage()).toBeNull();
    expect(buttonsNamed('Add kubeconfig')).toBe(0);
    expect(buttonsNamed('Remove')).toBe(0);
});

test('with no clusters, the web version says who adds them', async () => {
    workspace.files = [];
    session.info = WEB;
    render(Sidebar);

    await expect.element(page.getByText('An administrator adds clusters under Administration → Clusters.')).toBeVisible();
    expect(buttonsNamed('Add')).toBe(0);
});
