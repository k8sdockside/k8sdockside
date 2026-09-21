// What the start page's pictures are drawn with: a seeded generator, a few
// SVG helpers, and the isometric plane several scenes stand on. The scenes
// themselves are in scenes.ts and scenes-more.ts.

import type { Palette } from './palette';

/** One built-in picture. */
export interface Scene {
    id: string;
    name: string;
    /** What it shows, for the settings gallery's tooltip. */
    blurb: string;
    draw(d: Draw): string;
}

/** What a scene is drawn with. */
export interface Draw {
    /** A random number in [0, 1), from the seed. */
    r: () => number;
    p: Palette;
    w: number;
    h: number;
}

// ----- randomness ------------------------------------------------------------

/**
 * A small seeded generator (mulberry32). Seeded rather than Math.random so a
 * picture can be drawn again exactly -- the pinned one, the settings thumbnail
 * of the one on screen, and a test.
 */
export function seeded(seed: number): () => number {
    let a = seed >>> 0 || 0x9e3779b9;
    return () => {
        a = (a + 0x6d2b79f5) >>> 0;
        let t = a;
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}

export const between = (r: () => number, lo: number, hi: number) => lo + (hi - lo) * r();
export const pick = <T>(r: () => number, items: T[]): T => items[Math.floor(r() * items.length)];

// ----- drawing helpers -------------------------------------------------------

/** A number as the file wants it: one decimal, which is finer than a pixel. */
export const n = (x: number) => (Math.round(x * 10) / 10).toString();

/** A list of points as a polygon's or polyline's `points`. */
export const pts = (list: [number, number][]) => list.map(([x, y]) => `${n(x)},${n(y)}`).join(' ');

/** A linear gradient from `from` to `to`, top to bottom unless told otherwise. */
export function linear(
    id: string,
    stops: [number, string, number][],
    dir: { x1?: number; y1?: number; x2?: number; y2?: number } = {},
): string {
    const { x1 = 0, y1 = 0, x2 = 0, y2 = 1 } = dir;
    return (
        `<linearGradient id="${id}" x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}">` +
        stops.map(([o, c, a]) => `<stop offset="${o}" stop-color="${c}" stop-opacity="${a}"/>`).join('') +
        `</linearGradient>`
    );
}

export function radial(id: string, stops: [number, string, number][], at = { cx: 0.5, cy: 0.5, r: 0.5 }): string {
    return (
        `<radialGradient id="${id}" cx="${at.cx}" cy="${at.cy}" r="${at.r}">` +
        stops.map(([o, c, a]) => `<stop offset="${o}" stop-color="${c}" stop-opacity="${a}"/>`).join('') +
        `</radialGradient>`
    );
}

export const blur = (id: string, sd: number) =>
    `<filter id="${id}" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="${sd}"/></filter>`;

/** A soft light laid over part of the scene. */
export const pool = (id: string, x: number, y: number, rx: number, ry: number) =>
    `<ellipse cx="${n(x)}" cy="${n(y)}" rx="${n(rx)}" ry="${n(ry)}" fill="url(#${id})"/>`;

/**
 * The edge of the frame, darkened, so the middle of the scene -- where the
 * start page's text is -- reads as lit and the corners fall away.
 */
export function vignette(d: Draw, strength = 0.8): { defs: string; body: string } {
    return {
        defs: radial('vig', [
            [0.45, d.p.shade, 0],
            [1, d.p.shade, strength],
        ], { cx: 0.5, cy: 0.45, r: 0.75 }),
        body: `<rect width="${d.w}" height="${d.h}" fill="url(#vig)"/>`,
    };
}

/**
 * The helm wheel, Kubernetes' own mark: a heptagon with seven spokes. Drawn
 * as lines only, so it can be laid over anything.
 */
export function wheel(cx: number, cy: number, r: number, stroke: string, width: number, opacity: number): string {
    const corners: [number, number][] = [];
    const spokes: string[] = [];
    for (let i = 0; i < 7; i++) {
        const a = -Math.PI / 2 + (i * 2 * Math.PI) / 7;
        corners.push([cx + r * Math.cos(a), cy + r * Math.sin(a)]);
        spokes.push(
            `<line x1="${n(cx + r * 0.28 * Math.cos(a))}" y1="${n(cy + r * 0.28 * Math.sin(a))}" ` +
                `x2="${n(cx + r * 1.12 * Math.cos(a))}" y2="${n(cy + r * 1.12 * Math.sin(a))}"/>`,
        );
    }
    return (
        `<g fill="none" stroke="${stroke}" stroke-width="${n(width)}" stroke-opacity="${opacity}" stroke-linecap="round" stroke-linejoin="round">` +
        `<polygon points="${pts(corners)}"/>` +
        `<circle cx="${n(cx)}" cy="${n(cy)}" r="${n(r * 0.28)}"/>` +
        spokes.join('') +
        `</g>`
    );
}

// ----- isometric boxes -------------------------------------------------------

/**
 * The isometric plane: cell (i, j) on the ground at elevation z, projected
 * 2:1. Scenes set the tile size and where the origin sits.
 */
export interface Iso {
    tw: number;
    ox: number;
    oy: number;
}

export const project = (iso: Iso, i: number, j: number, z = 0): [number, number] => [
    iso.ox + ((i - j) * iso.tw) / 2,
    iso.oy + ((i + j) * iso.tw) / 4 - z,
];

/** The three visible faces of a box standing on the plane. */
export function boxFaces(iso: Iso, i: number, j: number, li: number, lj: number, h: number, z = 0) {
    const back = project(iso, i, j, z);
    const right = project(iso, i + li, j, z);
    const front = project(iso, i + li, j + lj, z);
    const left = project(iso, i, j + lj, z);
    const up = (p: [number, number]): [number, number] => [p[0], p[1] - h];
    return {
        top: [up(back), up(right), up(front), up(left)] as [number, number][],
        left: [left, front, up(front), up(left)] as [number, number][],
        right: [front, right, up(right), up(front)] as [number, number][],
        back,
        front,
        left_: left,
        right_: right,
        up,
    };
}
