import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    BackgroundService: {
        Folder: vi.fn().mockResolvedValue({ path: '', images: [], problem: '' }),
        BrowseForFolder: vi.fn(),
        ClearFolder: vi.fn(),
        RevealFolder: vi.fn(),
    },
}));

const { isDarkTheme, luminance, mix, MOODS, moodPalette, paletteFor, parseColor, toHex } = await import('./palette');
const { renderScene, SCENES, seeded } = await import('./scenes');
const { backdrop, BUILTIN_PICTURES, stableSeed, THEME_MOOD } = await import('./backdrop.svelte');
const { BackgroundService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

const DARK = paletteFor({ bg: '#10151c', accent: '#4a86ff', 'chart-1': '#4a86ff', 'chart-2': '#f28e2b' }, true);
const LIGHT = paletteFor({ bg: '#f6f7f9', accent: '#2f6fed' }, false);

describe('colours', () => {
    test('theme colours are read in the spellings themes use', () => {
        expect(parseColor('#4a86ff', [0, 0, 0])).toEqual([74, 134, 255]);
        expect(parseColor('#fff', [0, 0, 0])).toEqual([255, 255, 255]);
        expect(parseColor('#11223380', [0, 0, 0])).toEqual([17, 34, 51]);
        expect(parseColor('rgba(255, 255, 255, 0.08)', [0, 0, 0])).toEqual([255, 255, 255]);
        expect(parseColor('rgb(10 20 30 / 50%)', [0, 0, 0])).toEqual([10, 20, 30]);
    });

    // A picture in the wrong shade is better than no start page.
    test('anything else falls back rather than failing', () => {
        expect(parseColor('papayawhip', [1, 2, 3])).toEqual([1, 2, 3]);
        expect(parseColor(undefined, [1, 2, 3])).toEqual([1, 2, 3]);
        expect(parseColor('#12', [1, 2, 3])).toEqual([1, 2, 3]);
    });

    test('mixing goes from one colour to the other', () => {
        expect(toHex(mix([0, 0, 0], [255, 255, 255], 0.5))).toBe('#808080');
        expect(toHex(mix([10, 20, 30], [10, 20, 30], 0.7))).toBe('#0a141e');
    });

    test('every role in a palette is a colour, by night and by day', () => {
        for (const palette of [DARK, LIGHT]) {
            for (const [role, value] of Object.entries(palette)) {
                if (role === 'dark') continue;
                for (const colour of [value].flat()) expect(colour, role).toMatch(/^#[0-9a-f]{6}$/);
            }
            expect(palette.hues).toHaveLength(4);
        }
        expect(DARK.dark).toBe(true);
        expect(LIGHT.dark).toBe(false);
    });

    // By day the scene is pale: its ground lighter than its lines.
    test('a light palette stands on a pale ground', () => {
        const ground = parseColor(LIGHT.ground, [0, 0, 0]);
        const line = parseColor(LIGHT.line, [0, 0, 0]);
        expect(ground.reduce((a, b) => a + b)).toBeGreaterThan(line.reduce((a, b) => a + b));
    });
});

describe('moods', () => {
    // A dark theme gets a picture by night and a light one a picture by day,
    // whatever colour scheme the picture is in.
    test('every mood is dark by night and light by day', () => {
        for (const mood of MOODS) {
            const night = luminance(parseColor(moodPalette(mood, true).ground, [0, 0, 0]));
            const day = luminance(parseColor(moodPalette(mood, false).ground, [0, 0, 0]));
            expect(night, mood.id).toBeLessThan(0.1);
            expect(day, mood.id).toBeGreaterThan(0.85);
        }
    });

    // The complaint that started this: a warm accent tinting the night made
    // every picture brown. The ground stays cool or neutral, never warm.
    test('the night ground is never warm, whatever the mood', () => {
        for (const mood of MOODS) {
            const [r, g, b] = parseColor(moodPalette(mood, true).ground, [0, 0, 0]);
            expect(b, mood.id).toBeGreaterThanOrEqual(r);
            expect(Math.max(r, g, b) - Math.min(r, g, b), mood.id).toBeLessThan(16);
        }
    });

    test('mood ids are unique, and each has four hues', () => {
        expect(new Set(MOODS.map((m) => m.id)).size).toBe(MOODS.length);
        for (const mood of MOODS) expect(mood.hues).toHaveLength(4);
    });

    test('a theme is dark or light by its base, and by its window when it has none', () => {
        expect(isDarkTheme({ base: 'dark' })).toBe(true);
        expect(isDarkTheme({ base: 'light' })).toBe(false);
        expect(isDarkTheme({ base: '', resolved: { bg: '#fafafa' } })).toBe(false);
        expect(isDarkTheme({ base: '', resolved: { bg: '#101418' } })).toBe(true);
        expect(isDarkTheme(null)).toBe(true);
    });
});

describe('scenes', () => {
    test('the generator is seeded: the same seed, the same numbers', () => {
        const a = seeded(7);
        const b = seeded(7);
        const c = seeded(8);
        const first = [a(), a(), a()];
        expect([b(), b(), b()]).toEqual(first);
        expect([c(), c(), c()]).not.toEqual(first);
        for (const x of first) {
            expect(x).toBeGreaterThanOrEqual(0);
            expect(x).toBeLessThan(1);
        }
    });

    test.each(SCENES.map((s) => [s.id]))('%s draws a whole SVG document that parses', (id) => {
        for (const palette of [DARK, LIGHT]) {
            const svg = renderScene(id, 1234, palette);
            expect(svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg"')).toBe(true);
            expect(svg).toContain('preserveAspectRatio="xMidYMid slice"');
            expect(svg).not.toContain('NaN');
            expect(svg).not.toContain('undefined');
            const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
            expect(doc.getElementsByTagName('parsererror')).toHaveLength(0);
            // Kept light enough to rasterise quickly whenever the pane moves.
            expect(svg.length).toBeLessThan(700_000);
        }
    });

    // Why a pinned picture can be kept: it is drawn again exactly.
    test('a scene drawn twice from one seed is the same picture, and another seed is not', () => {
        for (const scene of SCENES) {
            expect(renderScene(scene.id, 99, DARK)).toBe(renderScene(scene.id, 99, DARK));
            expect(renderScene(scene.id, 99, DARK)).not.toBe(renderScene(scene.id, 100, DARK));
        }
    });

    test('a scene wears the palette it is given', () => {
        expect(renderScene('glass', 5, DARK)).toContain(DARK.glow);
        expect(renderScene('glass', 5, LIGHT)).toContain(LIGHT.glow);
    });

    test('scene ids are unique, and an unknown one draws the first', () => {
        expect(new Set(SCENES.map((s) => s.id)).size).toBe(SCENES.length);
        expect(renderScene('nope', 3, DARK)).toBe(renderScene(SCENES[0].id, 3, DARK));
    });
});

describe('the rotation', () => {
    const builtin = { source: 'builtin' as const, pinned: '', minutes: 15, palette: 'varied' as const };

    beforeEach(() => {
        localStorage.clear();
        backdrop.folder = { path: '', images: [], problem: '' };
    });

    test('every built-in scene is offered, each at a seed of its own', () => {
        expect(BUILTIN_PICTURES.map((p) => p.id)).toEqual(SCENES.map((s) => `scene:${s.id}`));
        expect(stableSeed('glass')).toBe(stableSeed('glass'));
        expect(stableSeed('glass')).not.toBe(stableSeed('mesh'));
    });

    test('moving on shows the next picture, drawn afresh', () => {
        const before = backdrop.current(builtin)!;
        backdrop.next();
        const after = backdrop.current(builtin)!;

        const ids = BUILTIN_PICTURES.map((p) => p.id);
        expect(ids.indexOf(after.id)).toBe((ids.indexOf(before.id) + 1) % ids.length);
        expect(after.kind === 'scene' && before.kind === 'scene' && after.seed !== before.seed).toBe(true);
    });

    // The rotation survives a restart, so "every quarter of an hour" means
    // that rather than "every launch".
    test('where it has got to is kept in the browser', () => {
        backdrop.next();
        const saved = JSON.parse(localStorage.getItem('k8sdockside.backdrop')!);
        expect(saved).toMatchObject({ index: expect.any(Number), seed: expect.any(Number), since: expect.any(Number) });
    });

    test('a picture changes only once its time is up', () => {
        backdrop.next();
        const now = Date.now();
        const shown = backdrop.current(builtin)!.id;

        expect(backdrop.advanceIfDue(15, now + 14 * 60_000)).toBe(false);
        expect(backdrop.current(builtin)!.id).toBe(shown);

        expect(backdrop.advanceIfDue(15, now + 15 * 60_000 + 1)).toBe(true);
        expect(backdrop.current(builtin)!.id).not.toBe(shown);
    });

    test('a kept picture stays whatever the rotation does', () => {
        const pinned = { ...builtin, pinned: 'scene:orbit' };
        expect(backdrop.current(pinned)!.id).toBe('scene:orbit');
        backdrop.next();
        const kept = backdrop.current(pinned)!;
        expect(kept.id).toBe('scene:orbit');
        // And it is the same picture each time, not a new one.
        expect(kept.kind === 'scene' && kept.seed).toBe(stableSeed('orbit'));
    });

    // Neighbours in the gallery never share a colour scheme.
    test('each built-in picture has a mood of its own', () => {
        const moods = BUILTIN_PICTURES.map((p) => (p.kind === 'scene' ? p.mood : ''));
        for (let i = 1; i < moods.length; i++) expect(moods[i]).not.toBe(moods[i - 1]);
        for (const m of moods) expect(MOODS.map((x) => x.id)).toContain(m);
    });

    test('each turn is drawn in a different mood from the one before', () => {
        for (let k = 0; k < 20; k++) {
            const before = backdrop.current(builtin)!;
            backdrop.next();
            const after = backdrop.current(builtin)!;
            expect(after.kind === 'scene' && before.kind === 'scene' && after.mood !== before.mood).toBe(true);
        }
    });

    test('matching the theme draws every picture in its colours', () => {
        const theme = { ...builtin, palette: 'theme' as const };
        const shown = backdrop.current(theme)!;
        expect(shown.kind === 'scene' && shown.mood).toBe(THEME_MOOD);
        const kept = backdrop.current({ ...theme, pinned: 'scene:mesh' })!;
        expect(kept.kind === 'scene' && kept.mood).toBe(THEME_MOOD);
    });

    // One picture, drawn by night for a dark theme and by day for a light one.
    test('a picture is drawn by night or by day as the theme is dark or light', () => {
        const picture = BUILTIN_PICTURES[1];
        const dark = backdrop.urlFor(picture, { id: 'a', base: 'dark', resolved: {} });
        const light = backdrop.urlFor(picture, { id: 'b', base: 'light', resolved: {} });
        expect(light).not.toBe(dark);
        // And one dark theme draws it the same as another: the mood decides the colours.
        expect(backdrop.urlFor(picture, { id: 'c', base: 'dark', resolved: { accent: '#ff0000' } })).toBe(dark);
    });

    test('no picture is no picture', () => {
        expect(backdrop.current({ ...builtin, source: 'none' })).toBeNull();
    });

    // A folder with nothing in it is not a reason for a blank start page.
    test('an empty folder falls back to the built-in pictures', () => {
        expect(backdrop.picturesFor('folder')).toBe(BUILTIN_PICTURES);
    });

    test('a folder of images is shown instead, by name', async () => {
        vi.mocked(BackgroundService.Folder).mockResolvedValueOnce({
            path: '/home/u/Pictures',
            images: [
                { name: 'a.png', url: '/user-backgrounds/a.png?v=1' },
                { name: 'b.jpg', url: '/user-backgrounds/b.jpg?v=1' },
            ],
            problem: '',
        });
        await backdrop.loadFolder();

        const pictures = backdrop.picturesFor('folder');
        expect(pictures.map((p) => p.id)).toEqual(['file:a.png', 'file:b.jpg']);
        const shown = backdrop.current({ ...builtin, source: 'folder', pinned: 'file:b.jpg' })!;
        expect(shown.kind === 'file' && shown.url).toBe('/user-backgrounds/b.jpg?v=1');
        expect(backdrop.urlFor(shown, null)).toBe('/user-backgrounds/b.jpg?v=1');
    });

    test('a scene is drawn once and then reused', () => {
        const theme = { id: 'nord', base: 'dark', resolved: { bg: '#2e3440', accent: '#88c0d0' } };
        const picture = BUILTIN_PICTURES[0];
        const url = backdrop.urlFor(picture, theme);
        expect(url).toMatch(/^(blob:|data:image\/svg\+xml)/);
        expect(backdrop.urlFor(picture, theme)).toBe(url);
    });

    // In the theme's own colours, another theme is another picture.
    test('a picture in the theme\'s colours is drawn again for another theme', () => {
        const theme = { id: 'nord', base: 'dark', resolved: { bg: '#2e3440', accent: '#88c0d0' } };
        const picture = { ...BUILTIN_PICTURES[0], mood: THEME_MOOD } as (typeof BUILTIN_PICTURES)[number];
        const url = backdrop.urlFor(picture, theme);
        expect(backdrop.urlFor(picture, theme)).toBe(url);
        expect(backdrop.urlFor(picture, { ...theme, id: 'fjord' })).not.toBe(url);
    });
});
