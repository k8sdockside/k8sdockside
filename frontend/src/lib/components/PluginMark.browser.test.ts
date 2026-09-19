import { expect, test } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PluginMark from './PluginMark.svelte';

// The three sources a mark can come from, and the order between them. The
// order is the whole point of the component: a plugin that ships a logo must
// win over the mark the app happens to carry for the same id, or a plugin
// could never change its own look; and anything unknown has to land on a
// plain icon rather than on nothing.

function mount(props: Record<string, unknown>) {
    document.body.innerHTML = '';
    render(PluginMark, { props } as never);
}

test('a plugin that ships a logo is drawn with it, from its own ui folder', () => {
    mount({ id: 'cilium', icon: 'share', logo: 'logo.svg', size: 14 });

    const img = document.querySelector('img.mark');
    expect(img?.getAttribute('src')).toBe('/plugin-ui/cilium/logo.svg');
    expect(img?.getAttribute('width')).toBe('14');
    // Decorative: the plugin's name is always beside it in the row.
    expect(img?.getAttribute('alt')).toBe('');
});

test('a plugin with no logo falls back to the mark the app carries for it', () => {
    mount({ id: 'cilium', icon: 'share', size: 14 });

    // The project's own logo, carried by the app, not the stroked icon set.
    expect(document.querySelector('img.mark')?.getAttribute('src')).toBe('/plugin-marks/cilium.svg');
    expect(document.querySelector('svg.icon')).toBeNull();
});

test('an offer is drawn with a mark although nothing is installed', () => {
    // No logo, because there are no files on this machine yet -- which is
    // exactly when telling ten plugins apart matters most.
    mount({ id: 'cert-manager', icon: 'lock', size: 18 });

    expect(document.querySelector('img.mark')?.getAttribute('src')).toBe('/plugin-marks/cert-manager.svg');
    expect(document.querySelector('svg.icon')).toBeNull();
});

test("a plugin the app has no mark for keeps its manifest's icon", () => {
    mount({ id: 'someone-elses-plugin', icon: 'rocket', size: 16 });

    expect(document.querySelector('.mark')).toBeNull();
    expect(document.querySelector('svg.icon')).not.toBeNull();
});

// Every id the listing claims a mark for must have a file behind it, and that
// file has to be drawable. The listing and the files are written together by
// cmd/pluginmarks, and this is what says so out loud if they ever come apart
// -- a missing one shows as a blank space in the sidebar, not as an error.
test('every listed mark has a file behind it that will draw', async () => {
    const { markFor } = await import('../plugins/marks');
    const ids = ['calico', 'cert-manager', 'cilium', 'flannel', 'image-inventory',
                 'kubeovn', 'kubevirt', 'metallb', 'optimization', 'vitistack'];

    for (const id of ids) {
        const url = markFor(id);
        expect(url, `${id} is not listed`).toBe(`/plugin-marks/${id}.svg`);

        const res = await fetch(url);
        expect(res.ok, `${id} has no file at ${url}`).toBe(true);
        const svg = await res.text();
        // Drawn through <img>, so it is parsed as XML: anything unbalanced or
        // without a viewBox does not draw, and shows as nothing at all.
        expect(svg, `${id} has no viewBox, so it will not scale`).toContain('viewBox=');
        expect(new DOMParser().parseFromString(svg, 'image/svg+xml').querySelector('parsererror'), `${id} is not well-formed XML`).toBeNull();
    }
});

test('a plugin the app carries nothing for is not asked for a file', async () => {
    const { markFor } = await import('../plugins/marks');
    expect(markFor('someone-elses-plugin')).toBe('');
});

test('a logo that will not load falls through instead of leaving a broken image', async () => {
    mount({ id: 'cilium', icon: 'share', logo: 'missing.svg', size: 14 });

    const img = document.querySelector('img.mark') as HTMLImageElement;
    expect(img).not.toBeNull();
    img.dispatchEvent(new Event('error'));
    await new Promise((r) => setTimeout(r, 50));

    // Back to the mark the app carries for it, not to a broken image.
    expect(document.querySelector('img.mark')?.getAttribute('src')).toBe('/plugin-marks/cilium.svg');
});
