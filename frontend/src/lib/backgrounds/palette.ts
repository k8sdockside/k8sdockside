// The colours a start page picture is drawn in: one of the start page's own
// colour schemes (see MOODS below), or the theme's, and by night or by day as
// the theme is dark or light.
//
// Either way the palette is a handful of roles -- the ground the scene stands
// on, the lines drawn across it, the light it is lit by -- and the scenes only
// ever ask for a role. A theme is a set of tokens meant for text and surfaces,
// not for a scene, so the roles are derived from them rather than read off.

/** A colour as three 0-255 channels. */
export type RGB = [number, number, number];

/** What a scene draws with. Every entry is a `#rrggbb` string. */
export interface Palette {
    /** Whether the theme is dark: a light one gets a scene by day. */
    dark: boolean;
    /** What everything stands on. */
    ground: string;
    /** A step further from the light than ground: shadows, the far distance. */
    shade: string;
    /** A step towards it: the lit middle of the scene. */
    lift: string;
    /** Structural lines drawn across the ground, meant to be faint. */
    line: string;
    /** The theme's accent, as it is. */
    accent: string;
    /** The brightest thing in the scene: edges catching the light, glows. */
    glow: string;
    /** A specular highlight. White by night, nearly white by day. */
    sheen: string;
    /** Four more hues to vary things by, from the theme's chart colours. */
    hues: string[];
}

const BLACK: RGB = [0, 0, 0];
const WHITE: RGB = [255, 255, 255];

/**
 * Reads a theme colour. Themes write hex, and now and then `rgb()`; anything
 * else falls back rather than failing, because a picture in the wrong shade
 * is better than no start page.
 */
export function parseColor(input: string | undefined, fallback: RGB): RGB {
    if (!input) return fallback;
    const value = input.trim().toLowerCase();

    const hex = value.match(/^#([0-9a-f]{3,8})$/);
    if (hex) {
        let digits = hex[1];
        if (digits.length === 3 || digits.length === 4) {
            digits = [...digits.slice(0, 3)].map((d) => d + d).join('');
        }
        if (digits.length < 6) return fallback;
        return [0, 2, 4].map((at) => parseInt(digits.slice(at, at + 2), 16)) as RGB;
    }

    const fn = value.match(/^rgba?\(([^)]+)\)$/);
    if (fn) {
        const parts = fn[1].split(/[\s,/]+/).filter(Boolean).slice(0, 3).map(Number);
        if (parts.length === 3 && parts.every((n) => Number.isFinite(n))) {
            return parts.map((n) => Math.max(0, Math.min(255, Math.round(n)))) as RGB;
        }
    }
    return fallback;
}

/** Writes a colour as `#rrggbb`. */
export function toHex(c: RGB): string {
    return '#' + c.map((n) => Math.round(Math.max(0, Math.min(255, n))).toString(16).padStart(2, '0')).join('');
}

/** A colour `t` of the way from `a` to `b`. */
export function mix(a: RGB, b: RGB, t: number): RGB {
    return [0, 1, 2].map((i) => a[i] + (b[i] - a[i]) * t) as RGB;
}

/** How bright a colour is to the eye, 0 to 1. */
export function luminance(c: RGB): number {
    return (0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]) / 255;
}

/**
 * The palette for a theme's resolved tokens.
 *
 * `dark` comes from the theme's base rather than from measuring its ground,
 * because the base is what the rest of the app goes by, and a theme whose
 * ground is a mid grey should still get the picture its author meant.
 */
export function paletteFor(tokens: Record<string, string>, dark: boolean): Palette {
    const bg = parseColor(tokens['bg'], dark ? [15, 19, 26] : [246, 247, 249]);
    const accent = parseColor(tokens['accent'], [86, 156, 255]);
    const hues = [1, 2, 3, 4].map((n) =>
        parseColor(tokens[`chart-${n}`], mix(accent, n % 2 ? WHITE : BLACK, 0.15 * n)),
    );

    if (dark) {
        const ground = mix(bg, BLACK, 0.3);
        return {
            dark,
            ground: toHex(ground),
            shade: toHex(mix(bg, BLACK, 0.7)),
            lift: toHex(mix(ground, accent, 0.14)),
            line: toHex(mix(ground, accent, 0.4)),
            accent: toHex(accent),
            glow: toHex(mix(accent, WHITE, 0.45)),
            sheen: '#ffffff',
            hues: hues.map(toHex),
        };
    }

    // By day the scene is pale and the drawing is in ink: the accent, taken
    // darker where it has to carry a line against a near-white ground.
    const ground = mix(bg, WHITE, 0.35);
    const ink = luminance(accent) > 0.55 ? mix(accent, BLACK, 0.35) : accent;
    return {
        dark,
        ground: toHex(ground),
        shade: toHex(mix(bg, ink, 0.16)),
        lift: toHex(mix(ground, WHITE, 0.6)),
        line: toHex(mix(ground, ink, 0.3)),
        accent: toHex(ink),
        glow: toHex(mix(ink, BLACK, 0.1)),
        sheen: '#ffffff',
        hues: hues.map((h) => toHex(luminance(h) > 0.6 ? mix(h, BLACK, 0.3) : h)),
    };
}

// ----- moods -----------------------------------------------------------------
//
// A theme's accent is one colour, and a start page that took every picture
// from it was the same picture in the same brown every quarter of an hour. A
// mood is a colour scheme of the start page's own: the pictures take turns
// through them, while the theme still decides the one thing it should -- night
// or day, a dark picture behind a dark window and a light one behind a light.

/** A colour scheme a picture can be drawn in, whatever the theme. */
export interface Mood {
    id: string;
    name: string;
    accent: string;
    /** Four more colours to vary things by, in the order scenes ask for them. */
    hues: [string, string, string, string];
}

export const MOODS: Mood[] = [
    { id: 'cyan', name: 'Neon Cyan', accent: '#22d3ee', hues: ['#22d3ee', '#818cf8', '#34d399', '#f472b6'] },
    { id: 'violet', name: 'Ultraviolet', accent: '#a78bfa', hues: ['#a78bfa', '#f472b6', '#60a5fa', '#fbbf24'] },
    { id: 'borealis', name: 'Borealis', accent: '#34d399', hues: ['#34d399', '#22d3ee', '#a78bfa', '#facc15'] },
    { id: 'sunset', name: 'Sunset', accent: '#fb7185', hues: ['#fb7185', '#f59e0b', '#c084fc', '#38bdf8'] },
    { id: 'glacier', name: 'Glacier', accent: '#60a5fa', hues: ['#60a5fa', '#a5b4fc', '#2dd4bf', '#e879f9'] },
    { id: 'crimson', name: 'Crimson', accent: '#f43f5e', hues: ['#f43f5e', '#fb923c', '#a855f7', '#38bdf8'] },
    { id: 'emerald', name: 'Emerald', accent: '#10b981', hues: ['#10b981', '#84cc16', '#06b6d4', '#f59e0b'] },
    { id: 'synthwave', name: 'Synthwave', accent: '#e879f9', hues: ['#e879f9', '#22d3ee', '#f472b6', '#facc15'] },
    { id: 'indigo', name: 'Indigo', accent: '#818cf8', hues: ['#818cf8', '#22d3ee', '#ec4899', '#a3e635'] },
];

/** One mood by id, if there is one by that name. */
export function moodById(id: string): Mood | undefined {
    return MOODS.find((m) => m.id === id);
}

/**
 * The palette for a mood, by night or by day. The ground is the mood's accent
 * in a whisper -- near black at night, near white by day -- so a picture reads
 * as one scheme rather than as coloured lines on the theme's own grey.
 */
export function moodPalette(mood: Mood, dark: boolean): Palette {
    const accent = parseColor(mood.accent, [86, 156, 255]);
    const bg = dark ? mix([9, 11, 19], accent, 0.045) : mix([246, 248, 252], accent, 0.05);
    const tokens: Record<string, string> = { bg: toHex(bg), accent: mood.accent };
    mood.hues.forEach((hue, i) => (tokens[`chart-${i + 1}`] = hue));
    return paletteFor(tokens, dark);
}

/**
 * Whether a theme wants a picture by night. Its base says so when it has one;
 * a theme that does not say is judged by how bright its window is.
 */
export function isDarkTheme(theme: { base?: string; resolved?: Record<string, string> } | null): boolean {
    if (theme?.base === 'light') return false;
    if (theme?.base === 'dark') return true;
    return luminance(parseColor(theme?.resolved?.['bg'], [15, 19, 26])) < 0.5;
}
