// The pictures the start page draws for itself.
//
// Each scene is a function from a seed and a palette to an SVG document, so
// the same scene is a different picture every time it comes round -- a new
// seed -- and always in the colours of the theme in use. Nothing is fetched and
// nothing ships as an image file: the start page asks for a scene, gets a
// string, and shows it.
//
// They are drawn at one size, WIDTH x HEIGHT, and scaled by the browser with
// `slice`, the way `background-size: cover` would. A few hundred to a couple of
// thousand elements each, and at most a couple of blur filters, because the
// whole picture is rasterised again whenever the pane changes size.

import {
    between,
    blur,
    boxFaces,
    type Draw,
    type Iso,
    linear,
    n,
    pick,
    pool,
    project,
    pts,
    radial,
    type Scene,
    seeded,
    vignette,
    wheel,
} from './draw';
import { mix, parseColor, toHex, type Palette } from './palette';
import { MORE_SCENES } from './scenes-more';
import { THIRD_SCENES } from './scenes-third';

export { seeded, type Scene } from './draw';

export const WIDTH = 1600;
export const HEIGHT = 1000;

// ----- the scenes ------------------------------------------------------------

/**
 * Glass cubes standing on a circuit board, lit from above: the one the start
 * page was asked to look like. Tilt-shift -- the far rows and the nearest
 * blurred -- is what gives a flat projection its depth.
 */
const glass: Scene = {
    id: 'glass',
    name: 'Glass Cluster',
    blurb: 'Glass cubes on a circuit board, one for every workload.',
    draw(d) {
        const { r, p, w, h } = d;
        const iso: Iso = { tw: between(r, 118, 150), ox: w / 2 + between(r, -80, 80), oy: -h * 0.55 };
        const vig = vignette(d, 0.85);

        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.55, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            linear('top', [
                [0, p.glow, p.dark ? 0.55 : 0.5],
                [1, p.accent, p.dark ? 0.12 : 0.18],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            linear('topLit', [
                [0, p.sheen, p.dark ? 0.85 : 0.95],
                [1, p.glow, p.dark ? 0.45 : 0.35],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            linear('left', [
                [0, p.accent, p.dark ? 0.4 : 0.28],
                [1, p.shade, p.dark ? 0.85 : 0.5],
            ]) +
            linear('right', [
                [0, p.glow, p.dark ? 0.3 : 0.35],
                [1, p.ground, p.dark ? 0.75 : 0.55],
            ]) +
            linear('sheen', [
                [0, p.sheen, 0],
                [0.5, p.sheen, p.dark ? 0.22 : 0.5],
                [1, p.sheen, 0],
            ], { x1: 0, y1: 0, x2: 1, y2: 0.3 }) +
            radial('light', [
                [0, p.glow, p.dark ? 0.28 : 0.35],
                [1, p.glow, 0],
            ]) +
            blur('far', 2.6) +
            blur('near', 1.8) +
            blur('shadow', 7) +
            vig.defs;

        const board: string[] = [];
        const shadows: string[] = [];
        const bands: Record<'far' | 'mid' | 'near', string[]> = { far: [], mid: [], near: [] };

        // The board: grid lines along both axes, and traces between pads.
        const span = 40;
        for (let k = -span; k <= span; k++) {
            const a = project(iso, k, -span);
            const b = project(iso, k, span);
            const c = project(iso, -span, k);
            const e = project(iso, span, k);
            board.push(`<line x1="${n(a[0])}" y1="${n(a[1])}" x2="${n(b[0])}" y2="${n(b[1])}"/>`);
            board.push(`<line x1="${n(c[0])}" y1="${n(c[1])}" x2="${n(e[0])}" y2="${n(e[1])}"/>`);
        }
        const traces: string[] = [];
        for (let t = 0; t < 110; t++) {
            let i = Math.floor(between(r, -4, 30)) + 0.5;
            let j = Math.floor(between(r, -4, 30)) + 0.5;
            const path: [number, number][] = [project(iso, i, j)];
            for (let s = 0; s < 3 + Math.floor(r() * 4); s++) {
                const len = 0.5 + Math.floor(r() * 3);
                if (r() < 0.5) i += r() < 0.5 ? len : -len;
                else j += r() < 0.5 ? len : -len;
                path.push(project(iso, i, j));
            }
            traces.push(`<polyline points="${pts(path)}"/>`);
            const [ex, ey] = path[path.length - 1];
            traces.push(`<circle cx="${n(ex)}" cy="${n(ey)}" r="3"/>`);
        }

        // What stands on it, back to front.
        const cells: { i: number; j: number }[] = [];
        for (let i = -12; i <= 36; i++) {
            for (let j = -12; j <= 36; j++) {
                const [x, y] = project(iso, i + 0.5, j + 0.5);
                if (x < -iso.tw || x > w + iso.tw || y < -iso.tw || y > h + iso.tw * 1.2) continue;
                cells.push({ i, j });
            }
        }
        cells.sort((a, b) => a.i + a.j - (b.i + b.j) || a.i - b.i);

        for (const { i, j } of cells) {
            const roll = r();
            const [, cy] = project(iso, i + 0.5, j + 0.5);
            const depth = cy / h;
            const band = depth < 0.28 ? 'far' : depth > 0.93 ? 'near' : 'mid';

            if (roll < 0.14) {
                // A chip: a flat dark slab with pins.
                const s = between(r, 0.5, 0.8);
                const o = (1 - s) / 2;
                const f = boxFaces(iso, i + o, j + o, s, s, 6);
                bands[band].push(
                    `<g><polygon points="${pts(f.left)}" fill="${p.shade}"/><polygon points="${pts(f.right)}" fill="${p.shade}"/>` +
                        `<polygon points="${pts(f.top)}" fill="${p.ground}" stroke="${p.line}" stroke-width="1"/></g>`,
                );
                continue;
            }
            if (roll > 0.46) continue;

            const s = between(r, 0.42, 0.86);
            const o = (1 - s) / 2;
            const tall = s * iso.tw * 0.6 * between(r, 0.75, 1.7);
            const f = boxFaces(iso, i + o, j + o, s, s, tall);
            const lit = r() < 0.22;
            const [fx, fy] = f.front;

            shadows.push(
                `<polygon points="${pts([f.back, f.right_, [f.right_[0] + tall * 0.5, f.right_[1] + tall * 0.2], [fx + tall * 0.5, fy + tall * 0.2], f.front, f.left_])}"/>`,
            );

            const hidden = [f.up(f.back), f.back];
            bands[band].push(
                `<g>` +
                    // Seen through the glass: the edges at the back.
                    `<polyline points="${pts([f.left_, f.back, f.right_])}" fill="none" stroke="${p.glow}" stroke-opacity=".22" stroke-width="1"/>` +
                    `<line x1="${n(hidden[0][0])}" y1="${n(hidden[0][1])}" x2="${n(hidden[1][0])}" y2="${n(hidden[1][1])}" stroke="${p.glow}" stroke-opacity=".22" stroke-width="1"/>` +
                    `<polygon points="${pts(f.left)}" fill="url(#left)"/>` +
                    `<polygon points="${pts(f.right)}" fill="url(#right)"/>` +
                    `<polygon points="${pts(f.right)}" fill="url(#sheen)"/>` +
                    `<polygon points="${pts(f.top)}" fill="url(#${lit ? 'topLit' : 'top'})"/>` +
                    // The edges catching the light, the front one brightest.
                    `<polygon points="${pts([...f.top])}" fill="none" stroke="${p.glow}" stroke-opacity=".75" stroke-width="1.2" stroke-linejoin="round"/>` +
                    `<polyline points="${pts([f.left_, f.front, f.right_])}" fill="none" stroke="${p.glow}" stroke-opacity=".45" stroke-width="1"/>` +
                    `<line x1="${n(fx)}" y1="${n(fy)}" x2="${n(fx)}" y2="${n(fy - tall)}" stroke="${p.sheen}" stroke-opacity="${p.dark ? 0.8 : 0.9}" stroke-width="1.4"/>` +
                    `</g>`,
            );
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g stroke="${p.line}" stroke-opacity=".28" stroke-width="1">${board.join('')}</g>` +
            `<g stroke="${p.accent}" stroke-opacity=".32" stroke-width="1.6" fill="${p.ground}">${traces.join('')}</g>` +
            pool('light', w * between(r, 0.3, 0.7), h * 0.4, w * 0.55, h * 0.5) +
            `<g fill="${p.shade}" fill-opacity="${p.dark ? 0.7 : 0.35}" filter="url(#shadow)">${shadows.join('')}</g>` +
            `<g filter="url(#far)" opacity=".7">${bands.far.join('')}</g>` +
            `<g>${bands.mid.join('')}</g>` +
            `<g filter="url(#near)">${bands.near.join('')}</g>` +
            vig.body
        );
    },
};

/**
 * A cluster as a network: nodes as hexagons, the pods on them as dots around,
 * and traffic running along the links. A far, blurred copy behind gives it a
 * second plane.
 */
const mesh: Scene = {
    id: 'mesh',
    name: 'Pod Mesh',
    blurb: 'Nodes, their pods and the traffic between them.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.8);
        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.6, p.ground, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            radial('haloA', [
                [0, p.accent, p.dark ? 0.3 : 0.2],
                [1, p.accent, 0],
            ]) +
            radial('haloB', [
                [0, p.hues[1], p.dark ? 0.22 : 0.16],
                [1, p.hues[1], 0],
            ]) +
            radial('node', [
                [0, p.glow, 0.9],
                [1, p.accent, 0.1],
            ]) +
            `<pattern id="hex" width="42" height="72.7" patternUnits="userSpaceOnUse">` +
            `<path d="M21 0 L42 12.1 L42 36.4 L21 48.5 L0 36.4 L0 12.1 Z M21 48.5 L21 72.7" fill="none" stroke="${p.line}" stroke-opacity=".16" stroke-width="1"/>` +
            `</pattern>` +
            blur('far', 3) +
            blur('glow', 5) +
            vig.defs;

        function network(count: number, spread: number, minGap: number) {
            const nodes: { x: number; y: number; big: boolean }[] = [];
            for (let tries = 0; nodes.length < count && tries < count * 40; tries++) {
                const x = between(r, -40, w + 40);
                const y = between(r, -40, h + 40);
                if (nodes.every((o) => Math.hypot(o.x - x, o.y - y) > minGap)) nodes.push({ x, y, big: r() < 0.12 });
            }
            const links: [number, number][] = [];
            nodes.forEach((a, ai) => {
                const near = nodes
                    .map((b, bi) => ({ bi, dist: Math.hypot(a.x - b.x, a.y - b.y) }))
                    .filter((o) => o.bi !== ai && o.dist < spread)
                    .sort((x, y) => x.dist - y.dist)
                    .slice(0, 2 + Math.floor(r() * 2));
                for (const { bi } of near) if (!links.some(([x, y]) => (x === bi && y === ai) || (x === ai && y === bi))) links.push([ai, bi]);
            });
            return { nodes, links };
        }

        const hexagon = (x: number, y: number, s: number) => {
            const c: [number, number][] = [];
            for (let i = 0; i < 6; i++) c.push([x + s * Math.cos((Math.PI / 3) * i + Math.PI / 6), y + s * Math.sin((Math.PI / 3) * i + Math.PI / 6)]);
            return pts(c);
        };

        // The far plane: smaller, dimmer, out of focus.
        const far = network(46, 200, 70);
        const farBody =
            far.links.map(([a, b]) => `<line x1="${n(far.nodes[a].x)}" y1="${n(far.nodes[a].y)}" x2="${n(far.nodes[b].x)}" y2="${n(far.nodes[b].y)}"/>`).join('') +
            far.nodes.map((o) => `<polygon points="${hexagon(o.x, o.y, 5)}"/>`).join('');

        // The near one.
        const near = network(34, 330, 120);
        const lines: string[] = [];
        const hot: string[] = [];
        const packets: string[] = [];
        for (const [a, b] of near.links) {
            const A = near.nodes[a];
            const B = near.nodes[b];
            const busy = r() < 0.3;
            lines.push(`<line x1="${n(A.x)}" y1="${n(A.y)}" x2="${n(B.x)}" y2="${n(B.y)}" stroke-opacity="${busy ? 0.7 : 0.3}"/>`);
            if (busy) {
                hot.push(`<line x1="${n(A.x)}" y1="${n(A.y)}" x2="${n(B.x)}" y2="${n(B.y)}"/>`);
                for (let k = 0; k < 2; k++) {
                    const t = r();
                    packets.push(`<circle cx="${n(A.x + (B.x - A.x) * t)}" cy="${n(A.y + (B.y - A.y) * t)}" r="${n(between(r, 2, 3.5))}"/>`);
                }
            }
        }
        const glyphs: string[] = [];
        const pods: string[] = [];
        for (const o of near.nodes) {
            const s = o.big ? between(r, 20, 28) : between(r, 8, 13);
            glyphs.push(
                `<polygon points="${hexagon(o.x, o.y, s)}" fill="${p.ground}" stroke="${p.glow}" stroke-width="${o.big ? 2 : 1.4}"/>` +
                    `<polygon points="${hexagon(o.x, o.y, s * 0.55)}" fill="url(#node)"/>`,
            );
            if (o.big) glyphs.push(wheel(o.x, o.y, s * 2.1, p.glow, 1.4, 0.45));
            const count = o.big ? 7 : Math.floor(r() * 5);
            for (let k = 0; k < count; k++) {
                const a = r() * Math.PI * 2;
                const rad = s + between(r, 12, 30);
                pods.push(`<circle cx="${n(o.x + Math.cos(a) * rad)}" cy="${n(o.y + Math.sin(a) * rad)}" r="${n(between(r, 2, 4))}" fill="${pick(r, p.hues)}"/>`);
            }
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<rect width="${w}" height="${h}" fill="url(#hex)"/>` +
            pool('haloA', w * between(r, 0.2, 0.45), h * between(r, 0.3, 0.6), w * 0.45, h * 0.55) +
            pool('haloB', w * between(r, 0.6, 0.85), h * between(r, 0.4, 0.8), w * 0.4, h * 0.5) +
            `<g filter="url(#far)" opacity=".45" stroke="${p.line}" stroke-width="1" fill="${p.line}">${farBody}</g>` +
            `<g stroke="${p.accent}" stroke-width="1.3">${lines.join('')}</g>` +
            `<g stroke="${p.glow}" stroke-width="3" stroke-opacity=".5" filter="url(#glow)">${hot.join('')}</g>` +
            `<g fill="${p.sheen}" fill-opacity=".9">${packets.join('')}</g>` +
            `<g fill-opacity=".85">${pods.join('')}</g>` +
            glyphs.join('') +
            vig.body
        );
    },
};

/**
 * The dockside itself: a container terminal at night, stacks in rows between
 * the lanes and a gantry crane over one of them, lit by the yard lights.
 */
const port: Scene = {
    id: 'port',
    name: 'Container Yard',
    blurb: 'Stacks of containers between the lanes of a terminal, under a gantry crane.',
    draw(d) {
        const { r, p, w, h } = d;
        const iso: Iso = { tw: between(r, 54, 64), ox: w / 2 + between(r, -60, 60), oy: -h * 0.62 };
        const vig = vignette(d, 0.85);
        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.5, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            radial('lamp', [
                [0, p.dark ? p.glow : p.sheen, p.dark ? 0.22 : 0.55],
                [1, p.dark ? p.glow : p.sheen, 0],
            ]) +
            blur('far', 2.4) +
            blur('near', 1.6) +
            vig.defs;

        const lanes: string[] = [];
        for (let k = -40; k <= 60; k++) {
            const a = project(iso, k, -40);
            const b = project(iso, k, 60);
            if (k % 7 === 0) lanes.push(`<line x1="${n(a[0])}" y1="${n(a[1])}" x2="${n(b[0])}" y2="${n(b[1])}" stroke-dasharray="14 12"/>`);
        }

        // Stacks, back to front: rows along i, a lane every seventh row of j.
        const boxes: { key: number; band: 'far' | 'mid' | 'near'; svg: string }[] = [];
        const shadeRGB = parseColor(p.shade, [0, 0, 0]);
        const sheenRGB = parseColor(p.sheen, [255, 255, 255]);
        const faces = p.hues.map((hue) => {
            const c = parseColor(hue, [128, 128, 128]);
            return p.dark
                ? { top: toHex(mix(mix(c, shadeRGB, 0.45), sheenRGB, 0.06)), left: toHex(mix(c, shadeRGB, 0.68)), right: toHex(mix(c, shadeRGB, 0.8)) }
                : { top: toHex(mix(c, sheenRGB, 0.62)), left: toHex(mix(c, sheenRGB, 0.4)), right: toHex(mix(c, sheenRGB, 0.2)) };
        });
        for (let j = -30; j < 60; j++) {
            // Every seventh row is a lane, two wide.
            if (((j % 7) + 7) % 7 < 2) continue;
            for (let i = -30; i < 70; i += 3) {
                const [x, y] = project(iso, i + 1, j + 0.5);
                if (x < -120 || x > w + 120 || y < -80 || y > h + 160) continue;
                const height = r() < 0.3 ? 0 : 1 + Math.floor(Math.pow(r(), 1.6) * 3);
                const [, cy] = project(iso, i + 1.5, j + 0.5);
                const depth = cy / h;
                const band = depth < 0.26 ? 'far' : depth > 0.94 ? 'near' : 'mid';
                let svg = '';
                for (let level = 0; level < height; level++) {
                    const face = pick(r, faces);
                    const f = boxFaces(iso, i + 0.1, j + 0.08, 2.8, 0.84, 24, level * 25);
                    const ribs: string[] = [];
                    for (let k = 1; k < 6; k++) {
                        const t = k / 6;
                        const a = [f.left[0][0] + (f.left[1][0] - f.left[0][0]) * t, f.left[0][1] + (f.left[1][1] - f.left[0][1]) * t];
                        ribs.push(`M${n(a[0])} ${n(a[1])}v-24`);
                    }
                    svg +=
                        `<polygon points="${pts(f.left)}" fill="${face.left}"/>` +
                        `<polygon points="${pts(f.right)}" fill="${face.right}"/>` +
                        `<polygon points="${pts(f.top)}" fill="${face.top}" stroke="${p.glow}" stroke-opacity="${p.dark ? 0.35 : 0.3}" stroke-width=".8"/>` +
                        `<path d="${ribs.join('')}"/>`;
                }
                if (svg) boxes.push({ key: i + j, band, svg });
            }
        }
        boxes.sort((a, b) => a.key - b.key);
        const bands = { far: '', mid: '', near: '' };
        for (const b of boxes) bands[b.band] += b.svg;

        // Yard lights, and the crane over one lane.
        const lights: string[] = [];
        for (let k = 0; k < 7; k++) {
            lights.push(pool('lamp', between(r, 0, w), between(r, h * 0.15, h * 0.95), between(r, 160, 260), between(r, 90, 150)));
        }
        const laneJ = 7 * Math.round(between(r, 1, 3));
        const craneI = between(r, 4, 14);
        const legs = [
            project(iso, craneI, laneJ - 1.2),
            project(iso, craneI, laneJ + 1.2),
            project(iso, craneI + 2.5, laneJ - 1.2),
            project(iso, craneI + 2.5, laneJ + 1.2),
        ];
        const tall = 190;
        const crane =
            `<g stroke="${p.glow}" stroke-width="3" stroke-opacity=".75" fill="none" stroke-linecap="round">` +
            legs.map(([x, y]) => `<line x1="${n(x)}" y1="${n(y)}" x2="${n(x)}" y2="${n(y - tall)}"/>`).join('') +
            `<polygon points="${pts(legs.map(([x, y]) => [x, y - tall] as [number, number]).sort((a, b) => a[0] - b[0]))}"/>` +
            `<line x1="${n(legs[0][0])}" y1="${n(legs[0][1] - tall)}" x2="${n(legs[0][0] - 170)}" y2="${n(legs[0][1] - tall - 85)}"/>` +
            `</g>`;

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g stroke="${p.line}" stroke-opacity=".45" stroke-width="2">${lanes.join('')}</g>` +
            lights.join('') +
            `<g stroke="${p.shade}" stroke-opacity=".4">` +
            `<g filter="url(#far)" opacity=".75">${bands.far}</g>` +
            `<g>${bands.mid}</g>` +
            `</g>` +
            crane +
            `<g stroke="${p.shade}" stroke-opacity=".4" filter="url(#near)">${bands.near}</g>` +
            vig.body
        );
    },
};

/**
 * A horizon at dusk: the helm wheel setting behind wireframe mountains, over a
 * grid floor that runs away to it.
 */
const horizon: Scene = {
    id: 'horizon',
    name: 'Helm Horizon',
    blurb: 'The helm wheel setting behind wireframe mountains over a grid floor.',
    draw(d) {
        const { r, p, w, h } = d;
        const hy = h * between(r, 0.55, 0.62);
        const cx = w * between(r, 0.38, 0.62);
        const sr = h * between(r, 0.2, 0.27);
        const sy = hy - sr * 0.45;
        const defs =
            linear('sky', [
                [0, p.shade, 1],
                [0.6, p.ground, 1],
                [1, p.accent, p.dark ? 0.55 : 0.35],
            ]) +
            linear('sun', [
                [0, p.glow, 1],
                [1, p.hues[1], 0.9],
            ]) +
            linear('floor', [
                [0, p.accent, p.dark ? 0.25 : 0.18],
                [0.25, p.shade, p.dark ? 0.9 : 0.5],
                [1, p.shade, 1],
            ]) +
            linear('ridge', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            radial('haze', [
                [0, p.glow, p.dark ? 0.45 : 0.4],
                [1, p.glow, 0],
            ]) +
            blur('glow', 8) +
            // The sun is cut by bars that thicken towards the horizon.
            `<mask id="bars"><rect width="${w}" height="${h}" fill="#fff"/>` +
            Array.from({ length: 7 }, (_, k) => {
                const y = sy + sr * (0.1 + k * 0.13);
                return `<rect x="0" y="${n(y)}" width="${w}" height="${n(2 + k * 2.2)}" fill="#000"/>`;
            }).join('') +
            `</mask>`;

        const stars: string[] = [];
        for (let k = 0; k < 170; k++) {
            stars.push(`<circle cx="${n(r() * w)}" cy="${n(r() * hy * 0.85)}" r="${n(between(r, 0.5, 1.6))}" fill-opacity="${n(between(r, 0.2, 0.9))}"/>`);
        }

        function ridge(base: number, lift: number, step: number) {
            const top: [number, number][] = [];
            for (let x = -40; x <= w + 40; x += step * between(r, 0.6, 1.4)) {
                top.push([x, base - lift * (0.25 + 0.75 * Math.pow(r(), 1.6)) * (0.6 + 0.4 * Math.sin(x / 190 + r()))]);
            }
            const wire = top.map(([x, y], k) => {
                const nx = top[k + 1];
                return (
                    `<line x1="${n(x)}" y1="${n(y)}" x2="${n(x + between(r, -30, 30))}" y2="${n(base)}"/>` +
                    (nx ? `<line x1="${n(x)}" y1="${n(y)}" x2="${n((x + nx[0]) / 2)}" y2="${n(base)}"/>` : '')
                );
            });
            return {
                fill: `<polygon points="${pts([[-40, base], ...top, [w + 40, base]])}" fill="url(#ridge)"/>`,
                wire: wire.join(''),
                edge: `<polyline points="${pts(top)}" fill="none"/>`,
            };
        }
        const backRidge = ridge(hy, h * 0.2, 70);
        const frontRidge = ridge(hy, h * 0.11, 55);

        const grid: string[] = [];
        for (let k = 1; k <= 16; k++) {
            const y = hy + (h - hy) * Math.pow(k / 16, 2.2);
            grid.push(`<line x1="0" y1="${n(y)}" x2="${w}" y2="${n(y)}" stroke-opacity="${n(0.15 + 0.6 * (k / 16))}"/>`);
        }
        for (let k = -24; k <= 24; k++) {
            grid.push(`<line x1="${n(cx + k * 18)}" y1="${n(hy)}" x2="${n(cx + k * 190)}" y2="${h}" stroke-opacity=".5"/>`);
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${n(hy)}" fill="url(#sky)"/>` +
            `<g fill="${p.dark ? p.sheen : p.glow}">${stars.join('')}</g>` +
            pool('haze', cx, hy, w * 0.5, h * 0.28) +
            `<circle cx="${n(cx)}" cy="${n(sy)}" r="${n(sr * 1.08)}" fill="${p.glow}" opacity=".35" filter="url(#glow)"/>` +
            `<circle cx="${n(cx)}" cy="${n(sy)}" r="${n(sr)}" fill="url(#sun)" mask="url(#bars)"/>` +
            wheel(cx, sy, sr * 0.62, p.shade, 4, 0.35) +
            backRidge.fill +
            `<g stroke="${p.glow}" stroke-opacity=".22" stroke-width="1">${backRidge.wire}</g>` +
            `<g stroke="${p.glow}" stroke-opacity=".6" stroke-width="1.4">${backRidge.edge}</g>` +
            frontRidge.fill +
            `<g stroke="${p.accent}" stroke-opacity=".3" stroke-width="1">${frontRidge.wire}</g>` +
            `<g stroke="${p.glow}" stroke-opacity=".7" stroke-width="1.4">${frontRidge.edge}</g>` +
            `<rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#floor)"/>` +
            `<g stroke="${p.accent}" stroke-width="1.2">${grid.join('')}</g>` +
            `<line x1="0" y1="${n(hy)}" x2="${w}" y2="${n(hy)}" stroke="${p.glow}" stroke-width="2.5" filter="url(#glow)"/>` +
            `<line x1="0" y1="${n(hy)}" x2="${w}" y2="${n(hy)}" stroke="${p.sheen}" stroke-opacity=".8" stroke-width="1"/>` +
            `<ellipse cx="${n(cx)}" cy="${n(hy + 40)}" rx="${n(sr * 1.3)}" ry="30" fill="${p.glow}" opacity=".18" filter="url(#glow)"/>`
        );
    },
};

/**
 * A circuit board seen from straight above: traces routed at right angles and
 * diagonals, pads and vias, a few chips -- one wearing the wheel -- and pulses
 * of light running along some of the traces.
 */
const circuit: Scene = {
    id: 'circuit',
    name: 'Control Plane',
    blurb: 'A circuit board from above, with signals running along its traces.',
    draw(d) {
        const { r, p, w, h } = d;
        const g = 24;
        const vig = vignette(d, 0.75);
        const defs =
            linear('bg', [
                [0, p.ground, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            radial('light', [
                [0, p.glow, p.dark ? 0.22 : 0.3],
                [1, p.glow, 0],
            ]) +
            linear('chip', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            blur('glow', 3.5) +
            vig.defs;

        const chips: { x: number; y: number; cw: number; ch: number }[] = [];
        for (let tries = 0; chips.length < 7 && tries < 200; tries++) {
            const cw = g * Math.round(between(r, 4, 9));
            const ch = g * Math.round(between(r, 3, 7));
            const x = g * Math.round(between(r, 0, (w - cw) / g));
            const y = g * Math.round(between(r, 0, (h - ch) / g));
            if (chips.every((c) => x > c.x + c.cw + g * 3 || x + cw < c.x - g * 3 || y > c.y + c.ch + g * 3 || y + ch < c.y - g * 3)) {
                chips.push({ x, y, cw, ch });
            }
        }

        const dirs: [number, number][] = [[1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1], [0, -1], [1, -1]];
        const traces: string[] = [];
        const pulses: string[] = [];
        const pads: string[] = [];
        for (let t = 0; t < 120; t++) {
            let x = g * Math.round(r() * (w / g));
            let y = g * Math.round(r() * (h / g));
            let dir = Math.floor(r() * 4) * 2;
            const path: [number, number][] = [[x, y]];
            const steps = 6 + Math.floor(r() * 28);
            for (let s = 0; s < steps; s++) {
                if (r() < 0.16) dir = (dir + (r() < 0.5 ? 1 : 7)) % 8;
                x += dirs[dir][0] * g;
                y += dirs[dir][1] * g;
                path.push([x, y]);
            }
            const lit = r() < 0.22;
            traces.push(`<polyline points="${pts(path)}" stroke="${lit ? p.accent : p.line}" stroke-opacity="${lit ? 0.75 : 0.5}"/>`);
            if (lit) {
                pulses.push(`<polyline points="${pts(path)}" stroke-dasharray="${n(between(r, 26, 60))} 4000" stroke-dashoffset="${n(-between(r, 0, steps * g))}"/>`);
            }
            const [ex, ey] = path[path.length - 1];
            pads.push(`<circle cx="${ex}" cy="${ey}" r="${lit ? 4.5 : 3.5}"/>`);
            pads.push(`<circle cx="${path[0][0]}" cy="${path[0][1]}" r="2.5"/>`);
        }

        const chipSvg = chips
            .map((c, k) => {
                const pins: string[] = [];
                for (let x = c.x + g / 2; x < c.x + c.cw; x += g / 2) {
                    pins.push(`<line x1="${x}" y1="${c.y - 7}" x2="${x}" y2="${c.y}"/><line x1="${x}" y1="${c.y + c.ch}" x2="${x}" y2="${c.y + c.ch + 7}"/>`);
                }
                for (let y = c.y + g / 2; y < c.y + c.ch; y += g / 2) {
                    pins.push(`<line x1="${c.x - 7}" y1="${y}" x2="${c.x}" y2="${y}"/><line x1="${c.x + c.cw}" y1="${y}" x2="${c.x + c.cw + 7}" y2="${y}"/>`);
                }
                const mark =
                    k === 0
                        ? wheel(c.x + c.cw / 2, c.y + c.ch / 2, Math.min(c.cw, c.ch) * 0.3, p.glow, 2.2, 0.8)
                        : `<rect x="${c.x + 10}" y="${c.y + 10}" width="${n(c.cw * 0.4)}" height="4" rx="2" fill="${p.line}" fill-opacity=".8"/>`;
                return (
                    `<g stroke="${p.line}" stroke-width="2">${pins.join('')}</g>` +
                    `<rect x="${c.x}" y="${c.y}" width="${c.cw}" height="${c.ch}" rx="6" fill="url(#chip)" stroke="${p.glow}" stroke-opacity=".45"/>` +
                    mark
                );
            })
            .join('');

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('light', w * between(r, 0.25, 0.75), h * between(r, 0.25, 0.6), w * 0.5, h * 0.55) +
            `<g fill="none" stroke-width="2.2" stroke-linejoin="round">${traces.join('')}</g>` +
            `<g fill="none" stroke="${p.sheen}" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" filter="url(#glow)">${pulses.join('')}</g>` +
            `<g fill="${p.ground}" stroke="${p.glow}" stroke-opacity=".7" stroke-width="1.5">${pads.join('')}</g>` +
            chipSvg +
            vig.body
        );
    },
};

/**
 * A honeycomb of workloads on a tilted plane, raised where the cluster is busy.
 */
const hive: Scene = {
    id: 'hive',
    name: 'Workload Hive',
    blurb: 'A honeycomb of workloads, raised and lit where the cluster is busiest.',
    draw(d) {
        const { r, p, w, h } = d;
        const size = between(r, 40, 48);
        const vig = vignette(d, 0.85);
        const dx = size * 1.5;
        const dy = size * Math.sqrt(3);
        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.55, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            linear('cap', [
                [0, p.glow, p.dark ? 0.9 : 0.8],
                [1, p.accent, p.dark ? 0.6 : 0.65],
            ]) +
            radial('light', [
                [0, p.glow, p.dark ? 0.2 : 0.3],
                [1, p.glow, 0],
            ]) +
            // The idle floor, drawn once as a pattern rather than cell by cell.
            `<pattern id="comb" width="${n(dx * 2)}" height="${n(dy)}" patternUnits="userSpaceOnUse">` +
            `<path d="M${n(size * 0.96)} ${n(dy / 2)} L${n(size * 0.48)} ${n(dy / 2 - dy * 0.48)} L${n(-size * 0.48)} ${n(dy / 2 - dy * 0.48)} ` +
            `M${n(size * 0.96)} ${n(dy / 2)} L${n(size * 0.48)} ${n(dy / 2 + dy * 0.48)} L${n(-size * 0.48)} ${n(dy / 2 + dy * 0.48)} ` +
            `M${n(size * 0.96)} ${n(dy / 2)} L${n(dx * 2 - size * 0.96)} ${n(dy / 2)} ` +
            `M${n(dx * 2 - size * 0.48)} ${n(dy / 2 - dy * 0.48)} L${n(dx * 2 + size * 0.48)} ${n(dy / 2 - dy * 0.48)} ` +
            `M${n(dx * 2 - size * 0.48)} ${n(dy / 2 + dy * 0.48)} L${n(dx * 2 + size * 0.48)} ${n(dy / 2 + dy * 0.48)}" ` +
            `fill="none" stroke="${p.line}" stroke-opacity=".4" stroke-width="1.2"/>` +
            `</pattern>` +
            blur('glow', 6) +
            vig.defs;

        // Where it is busy: a handful of tight clusters rather than one field.
        const peaks = Array.from({ length: 9 }, () => ({
            x: between(r, 0, w * 1.2),
            y: between(r, 0, h * 1.8),
            s: between(r, 70, 150),
        }));
        const level = (x: number, y: number) =>
            Math.min(1, peaks.reduce((sum, k) => sum + Math.exp(-((x - k.x) ** 2 + (y - k.y) ** 2) / (2 * k.s * k.s)), 0));

        const hexPts = (x: number, y: number, s: number) => {
            const c: [number, number][] = [];
            for (let i = 0; i < 6; i++) c.push([x + s * Math.cos((Math.PI / 3) * i), y + s * Math.sin((Math.PI / 3) * i)]);
            return c;
        };

        const cells: { x: number; y: number; l: number }[] = [];
        for (let col = -3; col * dx < w * 1.25; col++) {
            for (let row = -3; row * dy < h * 1.9; row++) {
                const x = col * dx;
                const y = row * dy + (col % 2 ? dy / 2 : 0);
                const l = level(x, y);
                if (l > 0.18) cells.push({ x, y, l });
            }
        }
        cells.sort((a, b) => a.y - b.y);

        const body: string[] = [];
        const glows: string[] = [];
        for (const c of cells) {
            const lift = c.l > 0.3 ? c.l * 70 : 0;
            const top = hexPts(c.x, c.y - lift, size * 0.92);
            if (lift > 0) {
                const base = hexPts(c.x, c.y, size * 0.92);
                body.push(
                    `<polygon points="${pts([base[0], base[1], base[2], base[3], top[3], top[2], top[1], top[0]])}" ` +
                        `fill="${p.shade}" fill-opacity="${p.dark ? 0.95 : 0.45}" stroke="${p.line}" stroke-opacity=".5"/>`,
                );
            }
            const busy = c.l > 0.72;
            body.push(
                `<polygon points="${pts(top)}" fill="${busy ? 'url(#cap)' : p.lift}" fill-opacity="${busy ? 1 : 0.5 + c.l * 0.5}" ` +
                    `stroke="${busy ? p.sheen : p.glow}" stroke-opacity="${busy ? 0.7 : 0.2 + c.l * 0.4}" stroke-width="${busy ? 1.4 : 1}"/>`,
            );
            if (busy) glows.push(`<polygon points="${pts(top)}"/>`);
            if (c.l > 0.4 && r() < 0.45) {
                for (let k = 0; k < 3; k++) {
                    const a = r() * Math.PI * 2;
                    body.push(`<circle cx="${n(c.x + Math.cos(a) * size * 0.4)}" cy="${n(c.y - lift + Math.sin(a) * size * 0.4)}" r="3.5" fill="${pick(r, p.hues)}"/>`);
                }
            }
        }

        // The plane is tilted away from the viewer: squashed, and turned a little.
        const tilt = `translate(${n(w * 0.5)} ${n(h * 0.1)}) rotate(${n(between(r, -10, -4))}) scale(1 0.56) translate(${n(-w * 0.62)} ${n(-h * 0.2)})`;
        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('light', w * between(r, 0.3, 0.7), h * 0.45, w * 0.5, h * 0.5) +
            `<g transform="${tilt}">` +
            `<rect x="${n(-dx * 3)}" y="${n(-dy * 3)}" width="${n(w * 1.4)}" height="${n(h * 2.1)}" fill="url(#comb)"/>` +
            `<g fill="${p.glow}" opacity=".45" filter="url(#glow)">${glows.join('')}</g>` +
            body.join('') +
            `</g>` +
            vig.body
        );
    },
};

/**
 * The control plane as a star with the cluster in orbit round it: nodes on
 * tilted rings, their pods circling them, in a field of stars.
 */
const orbit: Scene = {
    id: 'orbit',
    name: 'Orbit',
    blurb: 'Nodes in orbit round the control plane, among the stars.',
    draw(d) {
        const { r, p, w, h } = d;
        const cx = w * between(r, 0.55, 0.68);
        const cy = h * between(r, 0.42, 0.55);
        const tilt = between(r, -24, -10);
        const vig = vignette(d, 0.75);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.6, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: cy / h, r: 0.9 }) +
            radial('neb0', [
                [0, p.accent, p.dark ? 0.35 : 0.2],
                [1, p.accent, 0],
            ]) +
            radial('neb1', [
                [0, p.hues[1], p.dark ? 0.25 : 0.14],
                [1, p.hues[1], 0],
            ]) +
            radial('neb2', [
                [0, p.hues[2], p.dark ? 0.2 : 0.12],
                [1, p.hues[2], 0],
            ]) +
            radial('core', [
                [0, p.sheen, 1],
                [0.25, p.glow, 0.9],
                [1, p.accent, 0],
            ]) +
            p.hues
                .map((hue, k) =>
                    radial(`planet${k}`, [
                        [0, p.sheen, 0.9],
                        [0.35, hue, 1],
                        [1, p.shade, 1],
                    ], { cx: 0.35, cy: 0.3, r: 0.75 }),
                )
                .join('') +
            blur('glow', 10) +
            vig.defs;

        const stars: string[] = [];
        for (let k = 0; k < 260; k++) {
            stars.push(`<circle cx="${n(r() * w)}" cy="${n(r() * h)}" r="${n(between(r, 0.4, 1.5))}" fill-opacity="${n(between(r, 0.15, 0.85))}"/>`);
        }

        const rings: string[] = [];
        const behind: string[] = [];
        const front: string[] = [];
        const rad = (deg: number) => (deg * Math.PI) / 180;
        const place = (rx: number, ry: number, t: number): [number, number] => {
            const x = rx * Math.cos(t);
            const y = ry * Math.sin(t);
            const a = rad(tilt);
            return [cx + x * Math.cos(a) - y * Math.sin(a), cy + x * Math.sin(a) + y * Math.cos(a)];
        };
        for (let k = 0; k < 5; k++) {
            const rx = 150 + k * between(r, 95, 125);
            const ry = rx * between(r, 0.26, 0.34);
            rings.push(
                `<ellipse cx="${n(cx)}" cy="${n(cy)}" rx="${n(rx)}" ry="${n(ry)}" transform="rotate(${n(tilt)} ${n(cx)} ${n(cy)})" ` +
                    `stroke-opacity="${n(0.2 + 0.12 * (k % 2))}"${k % 2 ? ' stroke-dasharray="2 7"' : ''}/>`,
            );
            const count = 1 + Math.floor(r() * 3);
            for (let m = 0; m < count; m++) {
                const t = r() * Math.PI * 2;
                const [x, y] = place(rx, ry, t);
                const size = between(r, 6, 15) * (Math.sin(t) > 0 ? 1.15 : 0.8);
                const hueIndex = Math.floor(r() * p.hues.length);
                const trail: string[] = [];
                for (let s = 1; s <= 8; s++) {
                    const [ax, ay] = place(rx, ry, t - s * 0.035);
                    const [bx, by] = place(rx, ry, t - (s - 1) * 0.035);
                    trail.push(`<line x1="${n(ax)}" y1="${n(ay)}" x2="${n(bx)}" y2="${n(by)}" stroke-opacity="${n(0.7 - s * 0.08)}"/>`);
                }
                const moons: string[] = [];
                for (let s = 0; s < 3; s++) {
                    const a = r() * Math.PI * 2;
                    moons.push(`<circle cx="${n(x + Math.cos(a) * size * 2)}" cy="${n(y + Math.sin(a) * size * 0.8)}" r="1.8"/>`);
                }
                const svg =
                    `<g stroke="${p.glow}" stroke-width="2" stroke-linecap="round">${trail.join('')}</g>` +
                    `<circle cx="${n(x)}" cy="${n(y)}" r="${n(size)}" fill="url(#planet${hueIndex})"/>` +
                    `<g fill="${p.glow}" fill-opacity=".8">${moons.join('')}</g>`;
                (Math.sin(t) < 0 ? behind : front).push(svg);
            }
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('neb0', w * between(r, 0.15, 0.4), h * between(r, 0.2, 0.7), w * 0.4, h * 0.45) +
            pool('neb1', w * between(r, 0.5, 0.9), h * between(r, 0.1, 0.5), w * 0.35, h * 0.35) +
            pool('neb2', w * between(r, 0.3, 0.8), h * between(r, 0.6, 1), w * 0.4, h * 0.35) +
            `<g fill="${p.dark ? p.sheen : p.glow}">${stars.join('')}</g>` +
            `<g fill="none" stroke="${p.glow}" stroke-width="1.2">${rings.join('')}</g>` +
            behind.join('') +
            `<circle cx="${n(cx)}" cy="${n(cy)}" r="120" fill="url(#core)" opacity=".8"/>` +
            `<circle cx="${n(cx)}" cy="${n(cy)}" r="44" fill="${p.glow}" opacity=".5" filter="url(#glow)"/>` +
            wheel(cx, cy, 38, p.dark ? p.shade : p.sheen, 3, 0.75) +
            front.join('') +
            vig.body
        );
    },
};

/** Every built-in picture, in the order the gallery shows them. */
export const SCENES: Scene[] = [glass, mesh, port, horizon, circuit, hive, orbit, ...MORE_SCENES, ...THIRD_SCENES];

/** One scene by id, if there is one by that name. */
export function sceneById(id: string): Scene | undefined {
    return SCENES.find((s) => s.id === id);
}

/**
 * Draws a scene as a complete SVG document.
 *
 * `slice` is the SVG spelling of `cover`: the picture fills whatever shape it
 * is put in and loses its edges rather than gaining bars.
 */
export function renderScene(id: string, seed: number, palette: Palette): string {
    const scene = sceneById(id) ?? SCENES[0];
    const body = scene.draw({ r: seeded(seed), p: palette, w: WIDTH, h: HEIGHT });
    // Laid under every scene: a sky that fades towards its horizon would
    // otherwise let whatever is behind the picture show through there.
    const ground = `<rect width="${WIDTH}" height="${HEIGHT}" fill="${palette.ground}"/>`;
    return (
        `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${WIDTH} ${HEIGHT}" ` +
        `width="${WIDTH}" height="${HEIGHT}" preserveAspectRatio="xMidYMid slice">${ground}${body}</svg>`
    );
}
