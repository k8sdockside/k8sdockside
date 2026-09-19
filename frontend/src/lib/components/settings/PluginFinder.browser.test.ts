import { beforeEach, expect, test } from 'vitest';
import { render } from 'vitest-browser-svelte';
import { workspace } from '../../state/workspace.svelte';
import type { KnownPlugin, Plugin } from '../../plugins/types';

// The settings view lists a dozen plugins across four sections, and reading all
// of them is the wrong way to find one. The search, the category chips and the
// order narrow every section at once -- available, built in, installed and
// watched -- because "where is the one for storage" is not a question about
// which folder a plugin is in.

function plugin(id: string, name: string, category: string, tagline = 'a solution'): Plugin {
    return {
        id, name, tagline, category, icon: 'puzzle', author: 'K8s Dockside', docs: '', description: '',
        requires: [], pack: '', origin: 'builtin', disabled: false,
        views: [{ id: 'things', label: 'Things', icon: 'box', type: 'table', kind: 'pods', namespace: '', selector: '' }],
    };
}

function offer(id: string, name: string, category: string, description: string): KnownPlugin {
    return {
        id, name, tagline: '', category, icon: 'puzzle', description,
        repo: `https://github.com/k8sdockside/${id}.git`,
        detect: [], links: [], author: 'K8s Dockside', official: true, installed: false,
    };
}

const settle = () => new Promise((r) => setTimeout(r, 60));

const cardNames = () =>
    [...document.querySelectorAll('article.plugin .name')].map((el) => el.textContent?.trim() ?? '');

const chip = (label: string) =>
    [...document.querySelectorAll('button.chip')].find((el) => el.textContent?.includes(label)) as
        | HTMLButtonElement
        | undefined;

async function type(text: string): Promise<void> {
    const box = document.querySelector('.search input') as HTMLInputElement;
    box.value = text;
    box.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
}

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.pluginCatalogue = {
        plugins: [plugin('cilium', 'Cilium', 'networking'), plugin('argocd', 'Argo CD', 'delivery')],
        dir: '/p',
        folders: [],
        problems: [],
    };
    workspace.knownPlugins = [
        offer('longhorn', 'Longhorn', 'storage', 'Replicated block storage: volumes, replicas and disks.'),
        offer('metallb', 'MetalLB', 'networking', 'Load balancer addresses for bare-metal clusters.'),
    ];
});

test('the chips offer only the categories something is actually in, with counts', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    const chips = [...document.querySelectorAll('button.chip')].map((el) => el.textContent?.replace(/\s+/g, ' ').trim());
    expect(chips[0]).toBe('All 4');
    expect(chips.some((c) => c?.startsWith('Storage 1'))).toBe(true);
    expect(chips.some((c) => c?.startsWith('Networking 2'))).toBe(true);
    // Nothing is in these, so offering them would only empty the page.
    expect(chips.some((c) => c?.startsWith('Security'))).toBe(false);
    expect(chips.some((c) => c?.startsWith('Other'))).toBe(false);
});

test('a category narrows every section at once', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();
    expect(cardNames().sort()).toEqual(['Argo CD', 'Cilium', 'Longhorn', 'MetalLB']);

    chip('Networking')!.click();
    await settle();
    expect(cardNames().sort()).toEqual(['Cilium', 'MetalLB']);

    // The same chip again is how you get back, without hunting for a reset.
    chip('Networking')!.click();
    await settle();
    expect(cardNames()).toHaveLength(4);
});

test('the search matches the description, not only the name', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    await type('replicas');
    expect(cardNames()).toEqual(['Longhorn']);

    await type('bare-metal');
    expect(cardNames()).toEqual(['MetalLB']);
});

test('a search that finds nothing says so once, rather than as four empty lists', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    await type('nothing is called this');
    expect(cardNames()).toEqual([]);
    expect(document.querySelector('.nothing')?.textContent).toContain('No plugin matches that');
    expect([...document.querySelectorAll('h3')].map((h) => h.textContent)).not.toContain('Built in');

    (document.querySelector('.nothing button') as HTMLButtonElement).click();
    await settle();
    expect(cardNames()).toHaveLength(4);
});

test('a card’s category tag is also the filter', async () => {
    const PluginsSection = (await import('./PluginsSection.svelte')).default;
    render(PluginsSection);
    await settle();

    const tag = [...document.querySelectorAll('article.plugin button.tag')].find((el) =>
        el.textContent?.includes('Storage'),
    ) as HTMLButtonElement;
    tag.click();
    await settle();

    expect(cardNames()).toEqual(['Longhorn']);
});
