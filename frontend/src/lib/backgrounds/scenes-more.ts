// The second set of start page pictures: wider, calmer scenes than the first,
// several of them without a hard edge anywhere, which suits a page with text
// on it. Drawn with the same helpers and to the same size -- see draw.ts.

import {
    between,
    blur,
    boxFaces,
    type Iso,
    linear,
    n,
    pick,
    pool,
    project,
    pts,
    radial,
    type Scene,
    vignette,
    wheel,
} from './draw';
import { mix, parseColor, toHex, type RGB } from './palette';

type Pt = [number, number];

const rgb = (hex: string): RGB => parseColor(hex, [128, 128, 128]);

/**
 * Ribbons of traffic flowing across the frame, each a band of colour with
 * fine strands running along it.
 */
const streams: Scene = {
    id: 'streams',
    name: 'Streams',
    blurb: 'Ribbons of traffic flowing across the cluster.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.7);
        let defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.5, p.ground, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            radial('light', [
                [0, p.glow, p.dark ? 0.18 : 0.3],
                [1, p.glow, 0],
            ]) +
            blur('far', 2.5) +
            vig.defs;

        const xs: number[] = [];
        for (let x = -40; x <= w + 40; x += 16) xs.push(x);

        const ribbons: string[] = [];
        const count = 6;
        for (let k = 0; k < count; k++) {
            const a = p.hues[k % 4];
            const b = p.hues[(k + 1 + Math.floor(r() * 2)) % 4];
            defs += linear(`rib${k}`, [
                [0, a, 0],
                [0.25, a, p.dark ? 0.42 : 0.3],
                [0.65, b, p.dark ? 0.38 : 0.26],
                [1, b, 0],
            ], { x1: 0, y1: 0, x2: 1, y2: 0 });

            const base = h * (0.12 + (0.78 * (k + r() * 0.6)) / count);
            const a1 = between(r, 40, 130);
            const f1 = between(r, 0.0018, 0.004);
            const p1 = r() * 6.28;
            const a2 = between(r, 12, 45);
            const f2 = between(r, 0.005, 0.01);
            const p2 = r() * 6.28;
            const thick = between(r, 50, 140);
            const tf = between(r, 0.002, 0.005);
            const tp = r() * 6.28;
            const tilt = between(r, -0.16, 0.16);
            const top = (x: number) => base + tilt * (x - w / 2) + a1 * Math.sin(x * f1 + p1) + a2 * Math.sin(x * f2 + p2);
            const span = (x: number) => thick * (0.35 + 0.65 * (0.5 + 0.5 * Math.sin(x * tf + tp)));

            const upper: Pt[] = xs.map((x) => [x, top(x)]);
            const lower: Pt[] = xs.map((x): Pt => [x, top(x) + span(x)]).reverse();
            const strands: string[] = [];
            const lines = 12;
            for (let s = 0; s <= lines; s++) {
                const t = s / lines + between(r, -0.02, 0.02);
                strands.push(
                    `<polyline points="${pts(xs.map((x): Pt => [x, top(x) + span(x) * t]))}" stroke-opacity="${n(0.1 + 0.32 * Math.sin(Math.PI * Math.min(1, Math.max(0, t))))}"/>`,
                );
            }
            const sparks: string[] = [];
            for (let q = 0; q < 22; q++) {
                const x = r() * w;
                sparks.push(`<circle cx="${n(x)}" cy="${n(top(x) + span(x) * r())}" r="${n(between(r, 0.8, 2.4))}" fill-opacity="${n(between(r, 0.3, 0.9))}"/>`);
            }
            const far = k % 3 === 0;
            ribbons.push(
                `<g${far ? ' filter="url(#far)" opacity=".75"' : ''}>` +
                    `<polygon points="${pts([...upper, ...lower])}" fill="url(#rib${k})"/>` +
                    `<g fill="none" stroke="${p.glow}" stroke-width="1">${strands.join('')}</g>` +
                    `<g fill="${p.dark ? p.sheen : p.glow}">${sparks.join('')}</g>` +
                    `</g>`,
            );
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('light', w * between(r, 0.3, 0.7), h * between(r, 0.3, 0.6), w * 0.55, h * 0.5) +
            ribbons.join('') +
            vig.body
        );
    },
};

/**
 * A wireframe globe with its regions lit and the traffic between them arcing
 * over the surface: a cluster spread across the world.
 */
const regions: Scene = {
    id: 'regions',
    name: 'Regions',
    blurb: 'A wireframe globe, with traffic arcing between its regions.',
    draw(d) {
        const { r, p, w, h } = d;
        const cx = w * between(r, 0.55, 0.68);
        const cy = h * between(r, 0.5, 0.6);
        const R = h * between(r, 0.36, 0.43);
        const tilt = between(r, 0.3, 0.5);
        const spin = r() * Math.PI * 2;
        const vig = vignette(d, 0.7);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.55, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: cy / h, r: 0.85 }) +
            radial('sphere', [
                [0, p.lift, p.dark ? 0.95 : 0.9],
                [0.7, p.ground, p.dark ? 0.92 : 0.85],
                [1, p.shade, p.dark ? 0.95 : 0.6],
            ], { cx: 0.38, cy: 0.35, r: 0.75 }) +
            radial('atmo', [
                [0.8, p.accent, 0],
                [0.9, p.glow, p.dark ? 0.35 : 0.3],
                [1, p.glow, 0],
            ]) +
            blur('glow', 4) +
            vig.defs;

        type V = [number, number, number];
        const unit = (lat: number, lon: number): V => [Math.cos(lat) * Math.cos(lon), Math.sin(lat), Math.cos(lat) * Math.sin(lon)];
        // Spun about the axis, then tipped towards the viewer; z > 0 faces us.
        const view = (v: V, rad = R): [number, number, number] => {
            const x1 = v[0] * Math.cos(spin) - v[2] * Math.sin(spin);
            const z1 = v[0] * Math.sin(spin) + v[2] * Math.cos(spin);
            const y2 = v[1] * Math.cos(tilt) - z1 * Math.sin(tilt);
            const z2 = v[1] * Math.sin(tilt) + z1 * Math.cos(tilt);
            return [cx + x1 * rad, cy - y2 * rad, z2];
        };

        const front: string[] = [];
        const back: string[] = [];
        const trace = (points: [number, number, number][]) => {
            let run: Pt[] = [];
            let facing = points[0][2] >= 0;
            const flush = () => {
                if (run.length > 1) (facing ? front : back).push(`<polyline points="${pts(run)}"/>`);
            };
            for (const [x, y, z] of points) {
                if (z >= 0 !== facing) {
                    run.push([x, y]);
                    flush();
                    run = [[x, y]];
                    facing = z >= 0;
                } else {
                    run.push([x, y]);
                }
            }
            flush();
        };
        const deg = Math.PI / 180;
        for (let lat = -75; lat <= 75; lat += 15) {
            const ring: [number, number, number][] = [];
            for (let lon = 0; lon <= 360; lon += 4) ring.push(view(unit(lat * deg, lon * deg)));
            trace(ring);
        }
        for (let lon = 0; lon < 360; lon += 15) {
            const arc: [number, number, number][] = [];
            for (let lat = -90; lat <= 90; lat += 4) arc.push(view(unit(lat * deg, lon * deg)));
            trace(arc);
        }

        // The regions, and the traffic between the ones facing us.
        const sites: V[] = [];
        for (let k = 0; k < 24; k++) sites.push(unit(between(r, -55, 65) * deg, r() * Math.PI * 2));
        const visible = sites.filter((v) => view(v)[2] > 0.12);
        const arcs: string[] = [];
        const glows: string[] = [];
        for (let k = 0; k < Math.min(11, visible.length * 2); k++) {
            const a = pick(r, visible);
            const b = pick(r, visible);
            if (a === b) continue;
            const dot = Math.max(-1, Math.min(1, a[0] * b[0] + a[1] * b[1] + a[2] * b[2]));
            const omega = Math.acos(dot);
            if (omega < 0.2) continue;
            const line: Pt[] = [];
            for (let s = 0; s <= 40; s++) {
                const t = s / 40;
                const ka = Math.sin((1 - t) * omega) / Math.sin(omega);
                const kb = Math.sin(t * omega) / Math.sin(omega);
                const v: V = [a[0] * ka + b[0] * kb, a[1] * ka + b[1] * kb, a[2] * ka + b[2] * kb];
                const [x, y] = view(v, R * (1 + 0.22 * omega * Math.sin(Math.PI * t)));
                line.push([x, y]);
            }
            const hue = pick(r, p.hues);
            arcs.push(`<polyline points="${pts(line)}" stroke="${hue}"/>`);
            glows.push(`<polyline points="${pts(line)}" stroke="${hue}"/>`);
        }
        const dots = visible
            .map((v) => {
                const [x, y, z] = view(v);
                const s = 2.5 + z * 3;
                return `<circle cx="${n(x)}" cy="${n(y)}" r="${n(s * 2.6)}" fill="${p.glow}" fill-opacity=".18"/><circle cx="${n(x)}" cy="${n(y)}" r="${n(s)}" fill="${p.dark ? p.sheen : p.glow}"/>`;
            })
            .join('');

        const stars: string[] = [];
        for (let k = 0; k < 200; k++) {
            stars.push(`<circle cx="${n(r() * w)}" cy="${n(r() * h)}" r="${n(between(r, 0.4, 1.4))}" fill-opacity="${n(between(r, 0.15, 0.8))}"/>`);
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g fill="${p.dark ? p.sheen : p.line}">${stars.join('')}</g>` +
            `<circle cx="${n(cx)}" cy="${n(cy)}" r="${n(R * 1.18)}" fill="url(#atmo)"/>` +
            `<g fill="none" stroke="${p.line}" stroke-opacity=".35" stroke-width="1">${back.join('')}</g>` +
            `<circle cx="${n(cx)}" cy="${n(cy)}" r="${n(R)}" fill="url(#sphere)"/>` +
            `<g fill="none" stroke="${p.glow}" stroke-opacity=".38" stroke-width="1.1">${front.join('')}</g>` +
            `<g fill="none" stroke-width="5" stroke-opacity=".35" filter="url(#glow)">${glows.join('')}</g>` +
            `<g fill="none" stroke-width="1.8" stroke-linecap="round">${arcs.join('')}</g>` +
            dots +
            vig.body
        );
    },
};

/**
 * Down a tunnel towards the light, the walls running past: traffic on its
 * way in.
 */
const warp: Scene = {
    id: 'warp',
    name: 'Ingress',
    blurb: 'Down a tunnel of light, with traffic streaming past on its way in.',
    draw(d) {
        const { r, p, w, h } = d;
        const cx = w * between(r, 0.42, 0.58);
        const cy = h * between(r, 0.4, 0.55);
        const vig = vignette(d, 0.85);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.5, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: cy / h, r: 0.8 }) +
            radial('core', [
                [0, p.sheen, p.dark ? 0.95 : 0.9],
                [0.3, p.glow, p.dark ? 0.6 : 0.4],
                [1, p.accent, 0],
            ]) +
            blur('glow', 3) +
            vig.defs;

        const reach = Math.max(w, h) * 1.1;
        const rings: string[] = [];
        const N = 22;
        for (let k = 1; k <= N; k++) {
            const t = k / N;
            const s = Math.pow(t, 2.4);
            const hw = s * reach;
            const hh = s * reach * 0.62;
            rings.push(
                `<rect x="${n(cx - hw)}" y="${n(cy - hh)}" width="${n(hw * 2)}" height="${n(hh * 2)}" rx="${n(4 + 30 * s)}" ` +
                    `stroke="${k % 4 === 0 ? pick(r, p.hues) : p.glow}" stroke-opacity="${n(0.06 + 0.45 * t)}" stroke-width="${n(0.8 + 1.4 * t)}"/>`,
            );
        }
        const rails: string[] = [];
        for (let m = 0; m <= 12; m++) {
            const f = m / 12;
            const x = cx + (f * 2 - 1) * reach;
            const y = cy + (f * 2 - 1) * reach * 0.62;
            rails.push(`<line x1="${n(cx)}" y1="${n(cy)}" x2="${n(x)}" y2="${n(cy + reach * 0.62)}"/>`);
            rails.push(`<line x1="${n(cx)}" y1="${n(cy)}" x2="${n(x)}" y2="${n(cy - reach * 0.62)}"/>`);
            rails.push(`<line x1="${n(cx)}" y1="${n(cy)}" x2="${n(cx - reach)}" y2="${n(y)}"/>`);
            rails.push(`<line x1="${n(cx)}" y1="${n(cy)}" x2="${n(cx + reach)}" y2="${n(y)}"/>`);
        }
        const streaks: string[] = [];
        for (let k = 0; k < 190; k++) {
            const a = r() * Math.PI * 2;
            const r1 = Math.pow(r(), 0.7) * reach * 0.75 + 40;
            const len = r1 * between(r, 0.12, 0.5);
            const ex = Math.cos(a);
            const ey = Math.sin(a) * 0.62;
            streaks.push(
                `<line x1="${n(cx + ex * r1)}" y1="${n(cy + ey * r1)}" x2="${n(cx + ex * (r1 + len))}" y2="${n(cy + ey * (r1 + len))}" ` +
                    `stroke="${r() < 0.5 ? p.glow : pick(r, p.hues)}" stroke-opacity="${n(between(r, 0.2, 0.85))}" stroke-width="${n(between(r, 0.8, 2.6))}"/>`,
            );
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g stroke="${p.line}" stroke-opacity=".3" stroke-width="1">${rails.join('')}</g>` +
            `<g fill="none">${rings.join('')}</g>` +
            `<g stroke-linecap="round" filter="url(#glow)" opacity=".7">${streaks.slice(0, 60).join('')}</g>` +
            `<g stroke-linecap="round">${streaks.join('')}</g>` +
            `<ellipse cx="${n(cx)}" cy="${n(cy)}" rx="220" ry="150" fill="url(#core)"/>` +
            wheel(cx, cy, 26, p.dark ? p.shade : p.sheen, 2.5, 0.7) +
            vig.body
        );
    },
};

/**
 * Down the cold aisle of a data centre in one-point perspective: racks lit
 * along both walls, light bars on their edges, panels overhead, and the
 * floor polished enough to carry all of it.
 */
const racks: Scene = {
    id: 'racks',
    name: 'Data Center',
    blurb: 'Down the cold aisle of a data centre, racks lit on both sides and the floor shining.',
    draw(d) {
        const { r, p, w, h } = d;
        const vx = w * between(r, 0.44, 0.56);
        const vy = h * between(r, 0.4, 0.48);
        const F = h * 0.55;
        const aisle = between(r, 1.0, 1.2);
        const floorY = 1;
        const topY = -between(r, 1.15, 1.35);
        const ceilY = topY - 0.45;
        const P = (x: number, y: number, z: number): Pt => [vx + (x * F) / z, vy + (y * F) / z];
        const Z0 = 0.55;
        const ZF = 30;
        const RACK = 0.62;
        const GAP = 0.05;
        const vig = vignette(d, 0.8);

        const rackFill = p.dark ? toHex(mix(rgb(p.shade), [0, 0, 0], 0.2)) : p.lift;
        const ceiling = p.dark ? toHex(mix(rgb(p.shade), [0, 0, 0], 0.35)) : toHex(mix(rgb(p.ground), rgb(p.accent), 0.06));
        const defs =
            linear('ceil', [
                [0, ceiling, 1],
                [1, p.ground, 1],
            ]) +
            linear('floor', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            linear('face', [
                [0, p.glow, p.dark ? 0.12 : 0.08],
                [0.5, p.glow, 0],
                [1, p.glow, p.dark ? 0.05 : 0.03],
            ]) +
            radial('end', [
                [0, p.sheen, p.dark ? 0.9 : 0.85],
                [0.25, p.glow, p.dark ? 0.5 : 0.3],
                [1, p.accent, 0],
            ]) +
            blur('glow', 3) +
            blur('soft', 4) +
            vig.defs;

        const zs: number[] = [];
        for (let z = Z0; z < ZF; z += RACK + GAP) zs.push(z);

        const faces: string[] = [];
        const bars: string[] = [];
        // LED strips by width -- near, middle, far -- and by colour.
        const leds: Record<string, string[]>[] = [{}, {}, {}];
        const mirrored: Record<string, string[]> = {};
        const seg = (a: Pt, b: Pt) => `M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}`;
        for (const side of [-1, 1]) {
            const X = side * aisle;
            for (const z1 of zs) {
                const z2 = z1 + RACK;
                faces.push(`<polygon points="${pts([P(X, topY, z1), P(X, topY, z2), P(X, floorY, z2), P(X, floorY, z1)])}"/>`);
                const bucket = z1 < 1.6 ? 0 : z1 < 5 ? 1 : 2;
                const rows = 9;
                for (let k = 0; k < rows; k++) {
                    const y = topY + ((k + 0.6) / (rows + 0.2)) * (floorY - topY);
                    for (let m = 0; m < 4; m++) {
                        if (r() < 0.3) continue;
                        const za = z1 + 0.06 + (m * (RACK - 0.12)) / 4;
                        const zb = za + ((RACK - 0.12) / 4) * between(r, 0.35, 0.8);
                        const colour = r() < 0.72 ? p.accent : pick(r, p.hues);
                        (leds[bucket][colour] ??= []).push(seg(P(X, y, za), P(X, y, zb)));
                        const yr = 2 * floorY - y;
                        (mirrored[colour] ??= []).push(seg(P(X, yr, za), P(X, yr, zb)));
                    }
                }
                const [bx1, by1] = P(X, topY + 0.04, z1 + 0.015);
                const [bx2, by2] = P(X, floorY - 0.04, z1 + 0.015);
                bars.push(`<line x1="${n(bx1)}" y1="${n(by1)}" x2="${n(bx2)}" y2="${n(by2)}" stroke-width="${n(Math.max(0.6, 4 / z1))}"/>`);
            }
        }

        // The floor's tiles, and the panels and trays overhead.
        const tiles: string[] = [];
        for (let k = 0; k <= 5; k++) {
            const x = -aisle + (k * 2 * aisle) / 5;
            tiles.push(seg(P(x, floorY, Z0), P(x, floorY, ZF)));
        }
        for (let z = Z0; z < ZF; z += RACK + GAP) tiles.push(seg(P(-aisle, floorY, z), P(aisle, floorY, z)));
        const panels: string[] = [];
        for (let z = 1.1; z < ZF; z += 1.6) {
            panels.push(`<polygon points="${pts([P(-0.32, ceilY, z), P(0.32, ceilY, z), P(0.32, ceilY, z + 0.9), P(-0.32, ceilY, z + 0.9)])}"/>`);
        }
        const trays = [-1, 1]
            .flatMap((s) => [seg(P(s * (aisle - 0.12), ceilY + 0.1, Z0), P(s * (aisle - 0.12), ceilY + 0.1, ZF)), seg(P(s * (aisle - 0.3), ceilY + 0.1, Z0), P(s * (aisle - 0.3), ceilY + 0.1, ZF))])
            .join('');

        const widths = [4, 2, 1];
        const ledSvg = leds
            .map((groups, k) =>
                Object.entries(groups)
                    .map(([colour, segs]) => `<path d="${segs.join('')}" stroke="${colour}" stroke-width="${widths[k]}"/>`)
                    .join(''),
            )
            .join('');
        const mirrorSvg = Object.entries(mirrored)
            .map(([colour, segs]) => `<path d="${segs.join('')}" stroke="${colour}"/>`)
            .join('');

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${n(vy)}" fill="url(#ceil)"/>` +
            `<rect y="${n(vy)}" width="${w}" height="${n(h - vy)}" fill="url(#floor)"/>` +
            `<path d="${trays}" stroke="${p.line}" stroke-opacity=".55" stroke-width="1.5"/>` +
            `<g fill="${p.dark ? p.sheen : p.glow}" fill-opacity="${p.dark ? 0.5 : 0.18}">${panels.join('')}</g>` +
            `<g fill="${p.glow}" opacity=".5" filter="url(#glow)">${panels.join('')}</g>` +
            `<path d="${tiles.join('')}" stroke="${p.line}" stroke-opacity=".35" stroke-width="1"/>` +
            `<g fill="none" stroke-width="1.8" stroke-linecap="round" opacity="${p.dark ? 0.35 : 0.25}" filter="url(#soft)">${mirrorSvg}</g>` +
            `<g fill="${rackFill}" stroke="${p.glow}" stroke-opacity=".22" stroke-width="1">${faces.join('')}</g>` +
            `<g fill="url(#face)">${faces.join('')}</g>` +
            `<g fill="none" stroke-linecap="round">${ledSvg}</g>` +
            `<g stroke="${p.glow}" stroke-opacity=".5" filter="url(#glow)">${bars.join('')}</g>` +
            `<g stroke="${p.dark ? p.sheen : p.glow}" stroke-opacity=".75">${bars.join('')}</g>` +
            `<ellipse cx="${n(vx)}" cy="${n(vy)}" rx="${n(w * 0.2)}" ry="${n(h * 0.24)}" fill="url(#end)"/>` +
            vig.body
        );
    },
};

/**
 * A low-poly field of facets catching one light, with a constellation of
 * nodes drawn across it.
 */
const crystal: Scene = {
    id: 'crystal',
    name: 'Crystal',
    blurb: 'A low-poly field of facets catching the light, with a constellation of nodes across it.',
    draw(d) {
        const { r, p, w, h } = d;
        const cols = 18;
        const rows = 12;
        const cw = w / (cols - 2);
        const ch = h / (rows - 2);
        const grid: Pt[][] = [];
        for (let y = 0; y < rows; y++) {
            const row: Pt[] = [];
            for (let x = 0; x < cols; x++) {
                row.push([(x - 1) * cw + between(r, -0.38, 0.38) * cw, (y - 1) * ch + between(r, -0.38, 0.38) * ch]);
            }
            grid.push(row);
        }
        const light: Pt = [w * between(r, 0.2, 0.8), h * between(r, 0.15, 0.55)];
        const shade = rgb(p.shade);
        const ground = rgb(p.ground);
        const sheen = rgb(p.sheen);
        const hues = p.hues.map(rgb);
        const facets: string[] = [];
        const face = (a: Pt, b: Pt, c: Pt) => {
            const mx = (a[0] + b[0] + c[0]) / 3;
            const my = (a[1] + b[1] + c[1]) / 3;
            const lit = Math.max(0, Math.min(1, Math.pow(Math.max(0, 1 - Math.hypot(mx - light[0], my - light[1]) / (w * 0.85)), 1.5) + between(r, -0.07, 0.07)));
            const hue = mix(mix(hues[0], hues[1], Math.max(0, Math.min(1, mx / w))), hues[2], Math.max(0, Math.min(1, my / h)) * 0.45);
            const colour = p.dark
                ? mix(mix(shade, hue, 0.06 + 0.36 * lit), sheen, 0.08 * lit * lit)
                : mix(mix(ground, sheen, 0.5 * lit), hue, 0.1 + 0.32 * (1 - lit));
            const hex = toHex(colour);
            facets.push(`<polygon points="${pts([a, b, c])}" fill="${hex}" stroke="${hex}" stroke-width=".8"/>`);
        };
        for (let y = 0; y < rows - 1; y++) {
            for (let x = 0; x < cols - 1; x++) {
                const a = grid[y][x];
                const b = grid[y][x + 1];
                const c = grid[y + 1][x + 1];
                const e = grid[y + 1][x];
                if (r() < 0.5) {
                    face(a, b, c);
                    face(a, c, e);
                } else {
                    face(a, b, e);
                    face(b, c, e);
                }
            }
        }
        const wire: string[] = [];
        for (let y = 0; y < rows; y++) wire.push(`<polyline points="${pts(grid[y])}"/>`);
        for (let x = 0; x < cols; x++) wire.push(`<polyline points="${pts(grid.map((row) => row[x]))}"/>`);

        const nodes: Pt[] = [];
        for (let k = 0; k < 16; k++) nodes.push(grid[1 + Math.floor(r() * (rows - 2))][1 + Math.floor(r() * (cols - 2))]);
        const links: string[] = [];
        nodes.forEach((a, k) => {
            const near = nodes
                .filter((b) => b !== a)
                .sort((b, c) => Math.hypot(a[0] - b[0], a[1] - b[1]) - Math.hypot(a[0] - c[0], a[1] - c[1]))
                .slice(0, k % 3 === 0 ? 2 : 1);
            for (const b of near) links.push(`<line x1="${n(a[0])}" y1="${n(a[1])}" x2="${n(b[0])}" y2="${n(b[1])}"/>`);
        });
        const dots = nodes
            .map(([x, y]) => `<circle cx="${n(x)}" cy="${n(y)}" r="9" fill="${p.glow}" fill-opacity=".2"/><circle cx="${n(x)}" cy="${n(y)}" r="3.2" fill="${p.dark ? p.sheen : p.glow}"/>`)
            .join('');
        const vig = vignette(d, 0.6);
        return (
            `<defs>${vig.defs}${radial('light', [
                [0, p.sheen, p.dark ? 0.16 : 0.4],
                [1, p.sheen, 0],
            ])}</defs>` +
            `<rect width="${w}" height="${h}" fill="${p.ground}"/>` +
            facets.join('') +
            `<g fill="none" stroke="${p.glow}" stroke-opacity="${p.dark ? 0.09 : 0.12}" stroke-width="1">${wire.join('')}</g>` +
            pool('light', light[0], light[1], w * 0.4, h * 0.4) +
            `<g stroke="${p.glow}" stroke-opacity=".5" stroke-width="1.2">${links.join('')}</g>` +
            dots +
            vig.body
        );
    },
};

/**
 * The stack, floating: four planes one above the other -- nodes, pods,
 * containers, the network -- with light running between them.
 */
const layers: Scene = {
    id: 'layers',
    name: 'Layers',
    blurb: 'The stack as floating planes, with light running between the layers.',
    draw(d) {
        const { r, p, w, h } = d;
        const S = 8;
        const tw = between(r, 84, 96);
        const gap = between(r, 105, 125);
        const count = 4;
        const cx = w * between(r, 0.5, 0.62);
        const bottom = h * 0.5 + ((count - 1) * gap) / 2 + (S * tw) / 4;
        const iso: Iso = { tw, ox: cx, oy: bottom - (S * tw) / 2 };
        const vig = vignette(d, 0.8);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.6, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: 0.5, r: 0.8 }) +
            radial('under', [
                [0, p.accent, p.dark ? 0.35 : 0.25],
                [1, p.accent, 0],
            ]) +
            blur('glow', 5) +
            blur('far', 3) +
            vig.defs;

        const drawn: string[] = [];
        const beams: string[] = [];
        let below: Pt[] = [];
        for (let k = 0; k < count; k++) {
            const z = k * gap;
            const hue = p.hues[k % 4];
            const slab = boxFaces(iso, 0, 0, S, S, 7, z);
            const grid: string[] = [];
            for (let m = 1; m < S; m++) {
                const a = project(iso, m, 0, z + 7);
                const b = project(iso, m, S, z + 7);
                const c = project(iso, 0, m, z + 7);
                const e = project(iso, S, m, z + 7);
                grid.push(`M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}M${n(c[0])} ${n(c[1])}L${n(e[0])} ${n(e[1])}`);
            }
            let body =
                `<polygon points="${pts(slab.left)}" fill="${hue}" fill-opacity="${p.dark ? 0.35 : 0.45}"/>` +
                `<polygon points="${pts(slab.right)}" fill="${hue}" fill-opacity="${p.dark ? 0.22 : 0.3}"/>` +
                `<polygon points="${pts(slab.top)}" fill="${hue}" fill-opacity="${p.dark ? 0.12 : 0.16}" stroke="${p.glow}" stroke-opacity=".7" stroke-width="1.3"/>` +
                `<path d="${grid.join('')}" stroke="${hue}" stroke-opacity=".3" stroke-width="1"/>`;

            // What stands on this layer, back to front.
            const cells: Pt[] = [];
            for (let q = 0; q < 12; q++) cells.push([Math.floor(r() * S), Math.floor(r() * S)]);
            const seen = new Set<string>();
            const here: Pt[] = [];
            for (const [i, j] of cells.sort((a, b) => a[0] + a[1] - (b[0] + b[1]))) {
                if (seen.has(`${i},${j}`)) continue;
                seen.add(`${i},${j}`);
                const tall = between(r, 14, 40);
                const box = boxFaces(iso, i + 0.18, j + 0.18, 0.64, 0.64, tall, z + 7);
                const face = pick(r, p.hues);
                body +=
                    `<polygon points="${pts(box.left)}" fill="${face}" fill-opacity="${p.dark ? 0.55 : 0.6}"/>` +
                    `<polygon points="${pts(box.right)}" fill="${face}" fill-opacity="${p.dark ? 0.35 : 0.42}"/>` +
                    `<polygon points="${pts(box.top)}" fill="${p.dark ? p.sheen : face}" fill-opacity="${p.dark ? 0.55 : 0.75}"/>`;
                here.push(project(iso, i + 0.5, j + 0.5, z + 7));
            }
            // Light from the layer below up to this one.
            for (const [x, y] of below.slice(0, 3)) {
                const [tx, ty] = pick(r, here.length ? here : [[x, y - gap] as Pt]);
                beams.push(`<line x1="${n(x)}" y1="${n(y)}" x2="${n(tx)}" y2="${n(ty)}"/>`);
            }
            below = here;
            drawn.push(`<g>${body}</g>`);
        }

        const motes: string[] = [];
        for (let k = 0; k < 90; k++) {
            motes.push(`<circle cx="${n(r() * w)}" cy="${n(r() * h)}" r="${n(between(r, 0.6, 2.2))}" fill-opacity="${n(between(r, 0.15, 0.6))}"/>`);
        }
        const [ux, uy] = project(iso, S / 2, S / 2);

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g fill="${p.glow}" filter="url(#far)">${motes.join('')}</g>` +
            pool('under', ux, uy + 20, S * tw * 0.8, S * tw * 0.3) +
            drawn.join('') +
            `<g stroke="${p.glow}" stroke-width="3" stroke-opacity=".5" filter="url(#glow)">${beams.join('')}</g>` +
            `<g stroke="${p.sheen}" stroke-width="1" stroke-opacity=".8">${beams.join('')}</g>` +
            vig.body
        );
    },
};

/**
 * Beacons sending out rings across a field of dots, lighting the dots where
 * the rings pass: service discovery, drawn.
 */
const beacons: Scene = {
    id: 'beacons',
    name: 'Beacons',
    blurb: 'Services calling out across a field of dots, lighting it where their rings pass.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.75);
        const step = 26;
        const defs =
            linear('bg', [
                [0, p.ground, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            `<pattern id="dots" width="${step}" height="${step}" patternUnits="userSpaceOnUse">` +
            `<circle cx="${step / 2}" cy="${step / 2}" r="1.1" fill="${p.line}" fill-opacity=".6"/></pattern>` +
            radial('core', [
                [0, p.sheen, 1],
                [0.35, p.glow, 0.8],
                [1, p.accent, 0],
            ]) +
            blur('glow', 6) +
            vig.defs;

        // One to a quarter of the frame, so they spread rather than bunch.
        const quarters: Pt[] = [[0, 0], [1, 0], [0, 1], [1, 1]];
        for (let k = quarters.length - 1; k > 0; k--) {
            const m = Math.floor(r() * (k + 1));
            [quarters[k], quarters[m]] = [quarters[m], quarters[k]];
        }
        const sources = quarters.map(([qx, qy], k) => ({
            x: (qx * 0.5 + between(r, 0.12, 0.4)) * w,
            y: (qy * 0.5 + between(r, 0.12, 0.4)) * h,
            gap: between(r, 34, 48),
            hue: p.hues[k % 4],
        }));
        const RINGS = 14;
        const rings: string[] = [];
        for (const s of sources) {
            for (let k = 1; k <= RINGS; k++) {
                rings.push(
                    `<circle cx="${n(s.x)}" cy="${n(s.y)}" r="${n(k * s.gap)}" stroke="${s.hue}" stroke-opacity="${n(0.5 * (1 - k / (RINGS + 1)))}"${k % 3 === 0 ? ' stroke-dasharray="3 7"' : ''}/>`,
                );
            }
        }
        // The dots a ring is passing through light up in its colour.
        const lit: Record<string, string[]> = {};
        for (let x = step / 2; x < w; x += step) {
            for (let y = step / 2; y < h; y += step) {
                for (const s of sources) {
                    const dist = Math.hypot(x - s.x, y - s.y);
                    const k = Math.round(dist / s.gap);
                    if (k < 1 || k > RINGS || Math.abs(dist - k * s.gap) > 3.2) continue;
                    (lit[s.hue] ??= []).push(`M${n(x)} ${n(y)}h.01`);
                    break;
                }
            }
        }
        const litSvg = Object.entries(lit)
            .map(([hue, segs]) => `<path d="${segs.join('')}" stroke="${hue}"/>`)
            .join('');
        const links: string[] = [];
        for (let k = 0; k < sources.length; k++) {
            const a = sources[k];
            const b = sources[(k + 1) % sources.length];
            links.push(`<line x1="${n(a.x)}" y1="${n(a.y)}" x2="${n(b.x)}" y2="${n(b.y)}"/>`);
        }
        const cores = sources
            .map((s) => `<circle cx="${n(s.x)}" cy="${n(s.y)}" r="34" fill="url(#core)"/><circle cx="${n(s.x)}" cy="${n(s.y)}" r="5" fill="${p.sheen}"/>`)
            .join('');

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<rect width="${w}" height="${h}" fill="url(#dots)"/>` +
            `<g fill="none" stroke-width="1.2">${rings.join('')}</g>` +
            `<g fill="none" stroke-width="4.5" stroke-linecap="round">${litSvg}</g>` +
            `<g stroke="${p.glow}" stroke-opacity=".3" stroke-width="1.2" stroke-dasharray="2 8">${links.join('')}</g>` +
            `<g filter="url(#glow)" opacity=".8">${cores}</g>` +
            cores +
            vig.body
        );
    },
};

/**
 * A city of glass on the harbour at night: towers in three layers with haze
 * between them, neon running up their edges, traffic trailing light through
 * the sky, and all of it running into the water below.
 */
const skyline: Scene = {
    id: 'skyline',
    name: 'Harbour City',
    blurb: 'A city of glass towers on the harbour at night, its lights running down into the water.',
    draw(d) {
        const { r, p, w, h } = d;
        const hy = h * between(r, 0.64, 0.7);
        const accent = rgb(p.accent);
        const glassTop = toHex(mix(rgb(p.lift), accent, p.dark ? 0.28 : 0.12));
        const glassLow = p.dark ? p.shade : toHex(mix(rgb(p.ground), accent, 0.22));
        const farFill = toHex(mix(rgb(p.ground), accent, p.dark ? 0.16 : 0.1));
        const defs =
            linear('sky', [
                [0, p.shade, 1],
                [0.6, p.ground, 1],
                [1, p.accent, p.dark ? 0.4 : 0.22],
            ]) +
            linear('glass', [
                [0, glassTop, 1],
                [0.6, glassLow, 1],
                [1, glassLow, 1],
            ]) +
            linear('haze', [
                [0, p.accent, 0],
                [1, p.accent, p.dark ? 0.28 : 0.16],
            ]) +
            linear('sea', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            linear('fade', [
                [0, '#ffffff', 0.75],
                [1, '#ffffff', 0.04],
            ]) +
            linear('beam', [
                [0, p.glow, 0],
                [1, p.glow, p.dark ? 0.26 : 0.18],
            ]) +
            blur('glow', 3) +
            blur('soft', 2.5) +
            blur('trail', 4);

        const neon: Record<string, string[]> = {};
        const floors: Record<string, string[]> = {};
        const streaks: Record<string, string[]> = {};
        const beacons: string[] = [];
        const boards: string[] = [];
        const tops: Pt[] = [];

        /** One layer of towers: 0 the far fog, 1 the middle, 2 the front. */
        const layer = (detail: 0 | 1 | 2, lo: number, hi: number, minW: number, maxW: number, fill: string) => {
            let body = '';
            const scan: string[] = [];
            const edge: string[] = [];
            let x = -30;
            while (x < w + 30) {
                const tw = between(r, minW, maxW);
                const centre = 1 - Math.min(1, Math.abs(x + tw / 2 - w * 0.52) / (w * 0.7));
                const th = between(r, lo, hi) * (0.4 + 0.6 * centre);
                const tiers = detail > 0 && r() < 0.5 ? 2 + Math.floor(r() * 2) : 1;
                let y = hy;
                let cx = x;
                let cw = tw;
                let left = th;
                for (let t = 0; t < tiers; t++) {
                    const part = t === tiers - 1 ? left : left * between(r, 0.45, 0.7);
                    const top = y - part;
                    if (t === tiers - 1 && detail > 0 && r() < 0.3) {
                        // A slanted crown.
                        const drop = cw * between(r, 0.25, 0.6);
                        const [ly, ry] = r() < 0.5 ? [top + drop, top] : [top, top + drop];
                        body += `<polygon points="${pts([[cx, y], [cx + cw, y], [cx + cw, ry], [cx, ly]])}" fill="${fill}"/>`;
                        edge.push(`M${n(cx)} ${n(y)}V${n(ly)}L${n(cx + cw)} ${n(ry)}V${n(y)}`);
                    } else {
                        body += `<rect x="${n(cx)}" y="${n(top)}" width="${n(cw)}" height="${n(part)}" fill="${fill}"/>`;
                        edge.push(`M${n(cx)} ${n(y)}V${n(top)}H${n(cx + cw)}V${n(y)}`);
                    }
                    if (detail > 0) {
                        for (let sy = top + 5; sy < y - 3; sy += detail === 2 ? 6 : 8) {
                            scan.push(`M${n(cx + 2)} ${n(sy)}H${n(cx + cw - 2)}`);
                            // Now and then a floor with its lights on.
                            if (r() < (detail === 2 ? 0.07 : 0.04)) {
                                const from = cx + 3 + r() * cw * 0.4;
                                const to = Math.min(cx + cw - 3, from + cw * between(r, 0.2, 0.6));
                                const colour = r() < 0.6 ? p.glow : pick(r, p.hues);
                                (floors[colour] ??= []).push(`M${n(from)} ${n(sy)}H${n(to)}`);
                            }
                        }
                    }
                    left -= part;
                    y = top;
                    const shrink = cw * between(r, 0.14, 0.3);
                    cx += shrink / 2;
                    cw -= shrink;
                }
                const mid = cx + cw / 2;
                if (detail === 2 && r() < 0.4) {
                    const colour = pick(r, p.hues);
                    const at = r() < 0.5 ? mid : r() < 0.5 ? x + 3 : x + tw - 3;
                    (neon[colour] ??= []).push(`M${n(at)} ${n(hy - 6)}V${n(y + 8)}`);
                    (streaks[colour] ??= []).push(`M${n(at)} ${n(hy + 6)}v${n(between(r, 80, 200))}`);
                }
                if (detail > 0 && r() < 0.45) {
                    const mast = between(r, 14, 46);
                    edge.push(`M${n(mid)} ${n(y)}V${n(y - mast)}`);
                    beacons.push(`M${n(mid)} ${n(y - mast)}h.01`);
                    tops.push([mid, y - mast]);
                }
                if (detail === 2 && r() < 0.16) {
                    const bw = Math.min(tw - 8, between(r, 22, 40));
                    const bh = between(r, 30, 60);
                    const by = between(r, y + 20, hy - bh - 20);
                    const hue = pick(r, p.hues);
                    boards.push(`<rect x="${n(x + (tw - bw) / 2)}" y="${n(by)}" width="${n(bw)}" height="${n(bh)}" fill="${hue}" fill-opacity=".35" stroke="${hue}" stroke-opacity=".9"/>`);
                }
                x += tw + between(r, 2, 12) * (detail === 2 ? 3 : 1);
            }
            return { body, scan: scan.join(''), edge: edge.join('') };
        };

        const far = layer(0, 120, 360, 30, 90, farFill);
        const middle = layer(1, 100, 330, 36, 90, 'url(#glass)');
        const front = layer(2, 60, 280, 50, 120, 'url(#glass)');

        // Searchlights from a few of the rooftops, and traffic in the sky.
        const beams = tops
            .filter(() => r() < 0.12)
            .slice(0, 3)
            .map(([x, y]) => {
                const lean = between(r, -260, 260);
                return `<polygon points="${pts([[x - 3, y], [x + 3, y], [x + lean + 50, -20], [x + lean - 50, -20]])}" fill="url(#beam)"/>`;
            })
            .join('');
        const trails: string[] = [];
        for (let k = 0; k < 7; k++) {
            const x0 = between(r, -120, w * 0.6);
            const x1 = x0 + between(r, w * 0.3, w * 0.6);
            const y0 = hy - between(r, 90, 380);
            const y1 = y0 + between(r, -70, 70);
            const bend = between(r, -90, 40);
            trails.push(`<path d="M${n(x0)} ${n(y0)}Q${n((x0 + x1) / 2)} ${n((y0 + y1) / 2 + bend)} ${n(x1)} ${n(y1)}" stroke="${pick(r, p.hues)}"/>`);
        }
        const stars: string[] = [];
        for (let k = 0; k < (p.dark ? 110 : 30); k++) stars.push(`M${n(r() * w)} ${n(r() * hy * 0.55)}h.01`);

        // Cranes along the quay, drawn in light.
        const cranes: string[] = [];
        for (const x of [between(r, 40, 180), between(r, w - 300, w - 140)]) {
            const tall = between(r, 170, 240);
            const reach = between(r, 120, 190) * (x < w / 2 ? 1 : -1);
            cranes.push(
                `M${n(x)} ${n(hy)}V${n(hy - tall)}M${n(x + 30)} ${n(hy)}V${n(hy - tall)}` +
                    `M${n(x)} ${n(hy - tall * 0.35)}L${n(x + 30)} ${n(hy - tall * 0.65)}M${n(x + 30)} ${n(hy - tall * 0.35)}L${n(x)} ${n(hy - tall * 0.65)}` +
                    `M${n(x - 24 * Math.sign(reach))} ${n(hy - tall)}H${n(x + 30 + reach)}M${n(x + 15)} ${n(hy - tall)}V${n(hy - tall - 34)}L${n(x + 30 + reach)} ${n(hy - tall)}`,
            );
        }

        const grouped = (groups: Record<string, string[]>, attrs: string) =>
            Object.entries(groups)
                .map(([colour, segs]) => `<path d="${segs.join('')}" stroke="${colour}" ${attrs}/>`)
                .join('');
        const quayLights: string[] = [];
        for (let x = 20; x < w; x += between(r, 26, 60)) quayLights.push(`M${n(x)} ${n(hy - 2)}h.01`);
        const ripples: string[] = [];
        for (let y = hy + 4; y < h; y += between(r, 4, 9)) ripples.push(`M0 ${n(y)}H${w}`);

        const city =
            `<g id="city">` +
            beams +
            far.body +
            `<rect y="${n(hy - 300)}" width="${w}" height="300" fill="url(#haze)"/>` +
            middle.body +
            `<path d="${middle.scan}" stroke="${p.glow}" stroke-opacity=".08" stroke-width="1"/>` +
            `<path d="${middle.edge}" stroke="${p.glow}" stroke-opacity=".35" stroke-width="1" fill="none"/>` +
            `<g fill="none" stroke-width="1.3" stroke-opacity=".55" stroke-linecap="round">${trails.join('')}</g>` +
            `<g fill="none" stroke-width="5" stroke-opacity=".3" filter="url(#trail)">${trails.join('')}</g>` +
            `<rect y="${n(hy - 160)}" width="${w}" height="160" fill="url(#haze)"/>` +
            front.body +
            `<path d="${front.scan}" stroke="${p.glow}" stroke-opacity=".1" stroke-width="1"/>` +
            `<path d="${front.edge}" stroke="${p.glow}" stroke-opacity=".6" stroke-width="1.2" fill="none"/>` +
            boards.join('') +
            `<g fill="none" stroke-width="2" stroke-opacity=".75">${grouped(floors, '')}</g>` +
            `<g fill="none" stroke-width="5" stroke-opacity=".45" filter="url(#glow)">${grouped(neon, '')}</g>` +
            `<g fill="none" stroke-width="2" stroke-linecap="round">${grouped(neon, '')}</g>` +
            `<path d="${cranes.join('')}" stroke="${p.glow}" stroke-opacity=".7" stroke-width="1.6" fill="none"/>` +
            `<path d="${cranes.join('')}" stroke="${p.glow}" stroke-opacity=".35" stroke-width="5" fill="none" filter="url(#glow)"/>` +
            `<path d="${beacons.join('')}" stroke="${p.hues[1]}" stroke-width="5" stroke-linecap="round"/>` +
            `</g>`;

        return (
            `<defs>${defs}<mask id="mirror"><rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#fade)"/></mask></defs>` +
            `<rect width="${w}" height="${n(hy)}" fill="url(#sky)"/>` +
            `<path d="${stars.join('')}" stroke="${p.dark ? p.sheen : p.glow}" stroke-opacity=".6" stroke-width="1.4" stroke-linecap="round"/>` +
            city +
            `<rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#sea)"/>` +
            `<g mask="url(#mirror)" opacity=".85"><use href="#city" transform="translate(0 ${n(hy * 2)}) scale(1 -1)" filter="url(#soft)"/></g>` +
            `<g fill="none" stroke-width="3" stroke-opacity=".5" stroke-linecap="round" filter="url(#glow)">${grouped(streaks, '')}</g>` +
            `<path d="${ripples.join('')}" stroke="${p.shade}" stroke-opacity=".3" stroke-width="1.4"/>` +
            `<line x1="0" y1="${n(hy)}" x2="${w}" y2="${n(hy)}" stroke="${p.glow}" stroke-width="1.5" stroke-opacity=".7"/>` +
            `<path d="${quayLights.join('')}" stroke="${p.hues[3]}" stroke-width="3.5" stroke-linecap="round"/>`
        );
    },
};

/** The second set, in the order the gallery shows them after the first. */
export const MORE_SCENES: Scene[] = [streams, regions, warp, racks, crystal, layers, beacons, skyline];
