import { beforeEach, expect, test, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import { workspace } from '../../state/workspace.svelte';
import type { Plugin } from '../../plugins/types';

// Settings is the one place a switched-off plugin still appears: it is where it
// gets switched back on. Everywhere else it is gone, so if the card were hidden
// here too there would be no way back.

function plugin(id: string, name: string, disabled = false): Plugin {
    return {
        id, name, tagline: 'a solution', icon: 'puzzle', author: '', docs: '', description: '',
        requires: [{ kind: 'crd:things.acme.io', label: 'Things', optional: false }],
        pack: '', origin: 'builtin', disabled,
        views: [{ id: 'things', label: 'Things', icon: 'box', type: 'table', kind: 'pods', namespace: '', selector: '' }],
    };
}

const settle = () => new Promise((r) => setTimeout(r, 60));
const switchFor = (name: string) =>
    [...document.querySelectorAll('article.plugin')]
        .find((el) => el.textContent?.includes(name))
        ?.querySelector('input[type="checkbox"]') as HTMLInputElement | undefined;

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.pluginCatalogue = {
        plugins: [plugin('argocd', 'Argo CD'), plugin('flux', 'Flux', true)],
        dir: '/p', folders: [], problems: [],
    };
});

test('every plugin card carries a switch showing whether it is on', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    expect(switchFor('Argo CD')?.checked).toBe(true);
    // Still listed, and still reachable, precisely because it is switched off.
    expect(switchFor('Flux')).toBeDefined();
    expect(switchFor('Flux')?.checked).toBe(false);
});

test('flipping the switch sends the wanted state, not a toggle', async () => {
    const sent = vi.spyOn(workspace, 'setPluginEnabled').mockResolvedValue(undefined);
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    switchFor('Argo CD')!.click();
    await settle();

    expect(sent).toHaveBeenCalledWith('argocd', false);
    sent.mockRestore();
});

// Only what was installed into the plugins folder can be uninstalled: a
// built-in is switched off instead, and a folder the user added is theirs.
test('an installed plugin can be uninstalled from its card; a built-in or a watched folder’s cannot', async () => {
    workspace.pluginCatalogue = {
        plugins: [
            plugin('argocd', 'Argo CD'),
            { ...plugin('acme', 'Acme'), origin: '/p/k8sdockside-acme/plugin.json', repo: '/p/k8sdockside-acme' },
            { ...plugin('mine', 'Mine'), origin: '/elsewhere/mine/plugin.json' },
        ],
        dir: '/p', folders: ['/elsewhere'], problems: [],
    };
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    const uninstallFor = (name: string) =>
        [...document.querySelectorAll('article.plugin')]
            .find((el) => el.textContent?.includes(name))
            ?.querySelector('button.uninstall') as HTMLButtonElement | null | undefined;
    expect(uninstallFor('Argo CD')).toBeNull();
    expect(uninstallFor('Mine')).toBeNull();
    expect(uninstallFor('Acme')).toBeTruthy();
});

// The question is asked in the card, not with window.confirm: the macOS
// webview answers that with a silent "no", and the button did nothing.
test('uninstalling asks in the card what it deletes, and only Uninstall deletes it', async () => {
    workspace.pluginCatalogue = {
        plugins: [{ ...plugin('acme', 'Acme'), origin: '/p/k8sdockside-acme/plugin.json', repo: '/p/k8sdockside-acme' }],
        dir: '/p', folders: [], problems: [],
    };
    const preview = vi.spyOn(workspace, 'pluginRemoval').mockResolvedValue({ path: '/p/k8sdockside-acme', plugins: ['acme', 'acme-edge'] });
    const removed = vi.spyOn(workspace, 'uninstallPlugin').mockResolvedValue(true);
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    const card = () => document.querySelector('article.plugin')!;
    (card().querySelector('button.uninstall') as HTMLButtonElement).click();
    await settle();
    expect(preview).toHaveBeenCalledWith('acme');
    expect(removed).not.toHaveBeenCalled();
    const ask = card().querySelector('.uninstall-ask');
    expect(ask?.textContent).toContain('/p/k8sdockside-acme');
    expect(ask?.textContent).toContain('acme-edge');
    // The safe answer has the focus, so a stray Enter cannot delete.
    expect(document.activeElement?.classList.contains('uninstall-cancel')).toBe(true);

    (card().querySelector('button.uninstall-cancel') as HTMLButtonElement).click();
    await settle();
    expect(card().querySelector('.uninstall-ask')).toBeNull();
    expect(removed).not.toHaveBeenCalled();

    (card().querySelector('button.uninstall') as HTMLButtonElement).click();
    await settle();
    (card().querySelector('button.uninstall-confirm') as HTMLButtonElement).click();
    await settle();
    expect(removed).toHaveBeenCalledWith('acme');
    preview.mockRestore();
    removed.mockRestore();
});

// A plugin read from a watched folder is the user's checkout: listed apart,
// with no Update or Uninstall, and the published plugin it shares an id with
// is still offered -- after uninstalling the installed copy, that is how to
// get it back.
test('a watched folder’s plugin is listed apart, and the published one is still offered', async () => {
    workspace.pluginCatalogue = {
        plugins: [
            {
                ...plugin('vitistack', 'Vitistack'),
                origin: '/dev/k8sdockside-vitistack/plugin.json',
                repo: '/dev/k8sdockside-vitistack',
            },
        ],
        dir: '/p', folders: ['/dev/k8sdockside-vitistack'], problems: [],
    };
    workspace.knownPlugins = [
        {
            id: 'vitistack', name: 'Vitistack', tagline: 'supervisor clusters', icon: 'layers', description: 'Networks, clusters and machines.',
            repo: 'https://github.com/k8sdockside/vitistack.git', detect: [], links: [], official: true, installed: false,
        },
    ];
    const dropped = vi.spyOn(workspace, 'removePluginFolder').mockResolvedValue(undefined);
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    const headings = [...document.querySelectorAll('h3')].map((h) => h.textContent?.trim());
    expect(headings).toContain('From folders you watch');
    expect(headings).not.toContain('Installed');

    const card = [...document.querySelectorAll('article.plugin:not(.known)')].find((el) => el.textContent?.includes('Vitistack'))!;
    expect(card.querySelector('button.update')).toBeNull();
    expect(card.querySelector('button.uninstall')).toBeNull();
    expect(card.textContent).toContain('/dev/k8sdockside-vitistack');

    const offer = document.querySelector('article.plugin.known');
    expect(offer?.textContent).toContain('Vitistack');
    expect(offer?.textContent).toContain('/dev/k8sdockside-vitistack');

    (card.querySelector('button.unwatch') as HTMLButtonElement).click();
    await settle();
    expect(dropped).toHaveBeenCalledWith('/dev/k8sdockside-vitistack');
    dropped.mockRestore();
    workspace.knownPlugins = [];
});
