// The third set of start page pictures: maps, instruments and the sea. Drawn
// with the same helpers and to the same size as the others -- see draw.ts.

import { between, blur, boxFaces, type Iso, linear, n, pick, pool, project, pts, radial, type Scene, vignette, wheel } from './draw';
import { mix, parseColor, toHex, type RGB } from './palette';

type Pt = [number, number];

const rgb = (hex: string): RGB => parseColor(hex, [128, 128, 128]);

/** A dot, as a zero-length stroke with a round cap: far smaller than a circle each. */
const dot = (x: number, y: number) => `M${n(x)} ${n(y)}h.01`;

/** Paths grouped by stroke colour, drawn as one path each. */
function grouped(groups: Record<string, string[]>, attrs: string): string {
    return Object.entries(groups)
        .map(([colour, segs]) => `<path d="${segs.join('')}" stroke="${colour}" ${attrs}/>`)
        .join('');
}

/**
 * A contour map of an imagined landscape, the nodes marked on its peaks and
 * the routes between them dashed across it.
 */
const terrain: Scene = {
    id: 'terrain',
    name: 'Terrain',
    blurb: "A contour map of the cluster's landscape, with its nodes marked on the peaks.",
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.7);
        const bumps = Array.from({ length: 14 }, () => ({
            x: between(r, -100, w + 100),
            y: between(r, -100, h + 100),
            s: between(r, 90, 260),
            a: between(r, -1, 1.6),
        }));
        const fx = between(r, 0.002, 0.005);
        const fy = between(r, 0.002, 0.005);
        const px = r() * 6.28;
        const py = r() * 6.28;
        const field = (x: number, y: number) =>
            bumps.reduce((sum, b) => sum + b.a * Math.exp(-((x - b.x) ** 2 + (y - b.y) ** 2) / (2 * b.s * b.s)), 0) +
            0.25 * Math.sin(x * fx + px) * Math.cos(y * fy + py);

        const step = 18;
        const cols = Math.ceil(w / step) + 1;
        const rows = Math.ceil(h / step) + 1;
        const grid: number[][] = [];
        let lo = Infinity;
        let hi = -Infinity;
        for (let j = 0; j < rows; j++) {
            const row: number[] = [];
            for (let i = 0; i < cols; i++) {
                const v = field(i * step, j * step);
                row.push(v);
                lo = Math.min(lo, v);
                hi = Math.max(hi, v);
            }
            grid.push(row);
        }

        // Marching squares, one level at a time.
        const LEVELS = 18;
        const lines: string[] = [];
        for (let k = 1; k < LEVELS; k++) {
            const v = lo + ((hi - lo) * k) / LEVELS;
            const segs: string[] = [];
            const seg = (a: Pt, b: Pt) => segs.push(`M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}`);
            for (let j = 0; j < rows - 1; j++) {
                for (let i = 0; i < cols - 1; i++) {
                    const a = grid[j][i];
                    const b = grid[j][i + 1];
                    const c = grid[j + 1][i + 1];
                    const e = grid[j + 1][i];
                    const idx = (a > v ? 8 : 0) | (b > v ? 4 : 0) | (c > v ? 2 : 0) | (e > v ? 1 : 0);
                    if (idx === 0 || idx === 15) continue;
                    const x = i * step;
                    const y = j * step;
                    const at = (p1: number, p2: number) => (v - p1) / (p2 - p1);
                    const T: Pt = [x + step * at(a, b), y];
                    const R: Pt = [x + step, y + step * at(b, c)];
                    const B: Pt = [x + step * at(e, c), y + step];
                    const L: Pt = [x, y + step * at(a, e)];
                    switch (idx) {
                        case 1:
                        case 14:
                            seg(L, B);
                            break;
                        case 2:
                        case 13:
                            seg(B, R);
                            break;
                        case 3:
                        case 12:
                            seg(L, R);
                            break;
                        case 4:
                        case 11:
                            seg(T, R);
                            break;
                        case 5:
                            seg(L, T);
                            seg(B, R);
                            break;
                        case 6:
                        case 9:
                            seg(T, B);
                            break;
                        case 7:
                        case 8:
                            seg(L, T);
                            break;
                        case 10:
                            seg(T, R);
                            seg(L, B);
                            break;
                    }
                }
            }
            const t = k / LEVELS;
            const colour = toHex(mix(mix(rgb(p.hues[0]), rgb(p.hues[1]), Math.min(1, t * 1.4)), rgb(p.glow), Math.max(0, t - 0.6)));
            const index = k % 5 === 0;
            lines.push(`<path d="${segs.join('')}" stroke="${colour}" stroke-opacity="${n(index ? 0.85 : 0.32 + 0.4 * t)}" stroke-width="${index ? 1.9 : 1.1}"/>`);
        }

        // The highest ground gets a node, and the nodes a route between them.
        const peaks = bumps
            .filter((b) => b.a > 0 && b.x > 60 && b.x < w - 60 && b.y > 60 && b.y < h - 60)
            .sort((a, b) => b.a - a.a)
            .slice(0, 4);
        const routes = peaks
            .slice(1)
            .map((b, k) => `<line x1="${n(peaks[k].x)}" y1="${n(peaks[k].y)}" x2="${n(b.x)}" y2="${n(b.y)}"/>`)
            .join('');
        const marks = peaks
            .map(
                (b, k) =>
                    `<circle cx="${n(b.x)}" cy="${n(b.y)}" r="16" fill="none" stroke="${p.glow}" stroke-opacity=".6"/>` +
                    `<circle cx="${n(b.x)}" cy="${n(b.y)}" r="4.5" fill="${p.dark ? p.sheen : p.glow}"/>` +
                    (k === 0 ? wheel(b.x, b.y, 30, p.glow, 1.6, 0.7) : ''),
            )
            .join('');
        const graticule: string[] = [];
        for (let x = 100; x < w; x += 100) graticule.push(`M${x} 0V${h}`);
        for (let y = 100; y < h; y += 100) graticule.push(`M0 ${y}H${w}`);

        return (
            `<defs>${linear('bg', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 })}${vig.defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<path d="${graticule.join('')}" stroke="${p.line}" stroke-opacity=".25" stroke-width="1"/>` +
            `<g fill="none" stroke-linejoin="round">${lines.join('')}</g>` +
            `<g stroke="${p.glow}" stroke-opacity=".55" stroke-width="1.5" stroke-dasharray="7 6">${routes}</g>` +
            marks +
            vig.body
        );
    },
};

/**
 * Columns of log lines falling like rain, brightest at the newest line and
 * fading up the column.
 */
const logs: Scene = {
    id: 'logs',
    name: 'Log Stream',
    blurb: 'Columns of log lines raining down, brightest where they are newest.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.8);
        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.5, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            radial('light', [
                [0, p.glow, p.dark ? 0.16 : 0.25],
                [1, p.glow, 0],
            ]) +
            blur('far', 2.2) +
            blur('glow', 3) +
            vig.defs;

        const layer = (gap: number, width: number, weight: number, far: boolean) => {
            const levels: Record<string, string[]>[] = [{}, {}, {}, {}, {}];
            const heads: string[] = [];
            for (let x = between(r, 0, gap); x < w; x += gap * between(r, 0.8, 1.2)) {
                const drops = 1 + Math.floor(r() * 2);
                for (let k = 0; k < drops; k++) {
                    const head = between(r, -50, h + 150);
                    const len = between(r, 90, 420);
                    const tint = r() < 0.82 ? p.glow : pick(r, p.hues);
                    let y = head;
                    let first = true;
                    while (y > head - len) {
                        const dash = between(r, 4, 14);
                        const t = (head - y) / len;
                        if (first) {
                            heads.push(`M${n(x)} ${n(y)}v${n(-dash)}`);
                            first = false;
                        } else {
                            const level = Math.min(4, Math.floor((1 - t) * 5));
                            (levels[level][tint] ??= []).push(`M${n(x)} ${n(y)}v${n(-dash)}`);
                        }
                        y -= dash + between(r, 3, 6);
                    }
                }
            }
            const body = levels
                .map((groups, level) => grouped(groups, `stroke-opacity="${n(weight * (0.1 + level * 0.17))}"`))
                .join('');
            const bright = `<path d="${heads.join('')}" stroke="${p.dark ? p.sheen : p.glow}" stroke-opacity="${n(weight * 0.95)}"/>`;
            return (
                `<g fill="none" stroke-width="${width}" stroke-linecap="round"${far ? ' filter="url(#far)"' : ''}>` +
                body +
                bright +
                (far ? '' : `<g filter="url(#glow)" opacity=".7">${bright}</g>`) +
                `</g>`
            );
        };

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('light', w * between(r, 0.3, 0.7), h * 0.45, w * 0.5, h * 0.5) +
            layer(13, 2, 0.45, true) +
            layer(24, 3.2, 1, false) +
            vig.body
        );
    },
};

/**
 * Northern lights over a fjord: curtains of colour above the mountains, the
 * whole sky doubled in the still water, a few lights along the shore.
 */
const fjord: Scene = {
    id: 'fjord',
    name: 'Fjord Lights',
    blurb: 'Northern lights over the mountains, mirrored in a still fjord.',
    draw(d) {
        const { r, p, w, h } = d;
        const hy = h * between(r, 0.62, 0.7);
        let defs =
            linear('sky', [
                [0, p.shade, 1],
                [0.7, p.ground, 1],
                [1, p.lift, 1],
            ]) +
            linear('sea', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            linear('fade', [
                [0, '#ffffff', 0.7],
                [1, '#ffffff', 0.05],
            ]) +
            blur('soft', 3.5) +
            blur('glow', 4);

        const stars: string[] = [];
        for (let k = 0; k < (p.dark ? 220 : 60); k++) stars.push(dot(r() * w, r() * hy * 0.85));

        const xs: number[] = [];
        for (let x = -40; x <= w + 40; x += 12) xs.push(x);
        const curtains: string[] = [];
        for (let k = 0; k < 3; k++) {
            const hue = p.hues[k % 4];
            const hue2 = p.hues[(k + 2) % 4];
            const b0 = hy * between(r, 0.4, 0.62);
            const a1 = between(r, 40, 110);
            const f1 = between(r, 0.002, 0.005);
            const p1 = r() * 6.28;
            const a2 = between(r, 10, 40);
            const f2 = between(r, 0.007, 0.012);
            const p2 = r() * 6.28;
            const tall = between(r, 140, 260);
            const f3 = between(r, 0.004, 0.009);
            const p3 = r() * 6.28;
            const base = (x: number) => b0 + a1 * Math.sin(x * f1 + p1) + a2 * Math.sin(x * f2 + p2);
            const height = (x: number) => tall * (0.3 + 0.7 * (0.5 + 0.5 * Math.sin(x * f3 + p3)));
            const tops = xs.map((x) => base(x) - height(x));
            const bottoms = xs.map(base);
            defs += `<linearGradient id="aur${k}" gradientUnits="userSpaceOnUse" x1="0" y1="${n(Math.min(...tops))}" x2="0" y2="${n(Math.max(...bottoms))}">` +
                `<stop offset="0" stop-color="${hue}" stop-opacity="0"/>` +
                `<stop offset=".55" stop-color="${hue}" stop-opacity="${p.dark ? 0.32 : 0.26}"/>` +
                `<stop offset=".88" stop-color="${hue2}" stop-opacity="${p.dark ? 0.55 : 0.4}"/>` +
                `<stop offset="1" stop-color="${hue2}" stop-opacity=".1"/></linearGradient>`;
            const band = `<polygon points="${pts([...xs.map((x, i): Pt => [x, tops[i]]), ...xs.map((x, i): Pt => [x, bottoms[i]]).reverse()])}" fill="url(#aur${k})"/>`;
            const streaks: string[] = [];
            for (let x = -20; x < w + 20; x += 5) {
                const b = base(x);
                streaks.push(`M${n(x)} ${n(b)}v${n(-height(x) * between(r, 0.4, 1))}`);
            }
            curtains.push(`${band}<path d="${streaks.join('')}" stroke="${hue}" stroke-opacity=".16" stroke-width="2"/>`);
        }

        const ridge = (lift: number, step: number): Pt[] => {
            const top: Pt[] = [[-40, hy]];
            for (let x = -40; x <= w + 40; x += step * between(r, 0.6, 1.4)) {
                top.push([x, hy - lift * (0.2 + 0.8 * Math.pow(r(), 1.3))]);
            }
            top.push([w + 40, hy]);
            return top;
        };
        const far = p.dark ? toHex(mix(rgb(p.shade), rgb(p.lift), 0.5)) : toHex(mix(rgb(p.ground), rgb(p.accent), 0.14));
        const near = p.dark ? p.shade : toHex(mix(rgb(p.ground), rgb(p.accent), 0.26));
        const lights: string[] = [];
        for (let k = 0; k < 9; k++) lights.push(dot(between(r, 0.05, 0.95) * w, hy - between(r, 1, 5)));

        const above =
            `<g id="above">` +
            `<g filter="url(#soft)">${curtains.join('')}</g>` +
            `<polygon points="${pts(ridge(h * 0.2, 70))}" fill="${far}"/>` +
            `<polygon points="${pts(ridge(h * 0.12, 45))}" fill="${near}"/>` +
            `<path d="${lights.join('')}" stroke="${p.hues[3]}" stroke-width="4" stroke-linecap="round"/>` +
            `</g>`;
        const ripples: string[] = [];
        for (let y = hy + 3; y < h; y += between(r, 4, 8)) ripples.push(`M0 ${n(y)}H${w}`);

        return (
            `<defs>${defs}<mask id="mirror"><rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#fade)"/></mask></defs>` +
            `<rect width="${w}" height="${n(hy)}" fill="url(#sky)"/>` +
            `<path d="${stars.join('')}" stroke="${p.dark ? p.sheen : p.glow}" stroke-opacity=".7" stroke-width="1.6" stroke-linecap="round"/>` +
            above +
            `<rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#sea)"/>` +
            `<g mask="url(#mirror)" opacity=".8"><use href="#above" transform="translate(0 ${n(hy * 2)}) scale(1 -1)"/></g>` +
            `<path d="${ripples.join('')}" stroke="${p.shade}" stroke-opacity=".3" stroke-width="1.4"/>` +
            `<path d="${lights.join('')}" stroke="${p.hues[3]}" stroke-width="6" stroke-linecap="round" filter="url(#glow)" opacity=".7"/>`
        );
    },
};

/**
 * A radar screen mid-sweep: the beam passing, and the pods it has just found
 * brightest behind it.
 */
const radar: Scene = {
    id: 'radar',
    name: 'Radar',
    blurb: 'A radar sweep picking up pods as it passes.',
    draw(d) {
        const { r, p, w, h } = d;
        const cx = w * between(r, 0.55, 0.68);
        const cy = h * between(r, 0.45, 0.58);
        const R = h * between(r, 0.5, 0.62);
        const vig = vignette(d, 0.75);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.6, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: cy / h, r: 0.8 }) +
            blur('glow', 4) +
            vig.defs;

        const grid: string[] = [];
        for (let x = 0; x < w; x += 40) grid.push(`M${x} 0V${h}`);
        for (let y = 0; y < h; y += 40) grid.push(`M0 ${y}H${w}`);
        const rings = Array.from({ length: 6 }, (_, k) => `<circle cx="${n(cx)}" cy="${n(cy)}" r="${n((R * (k + 1)) / 6)}" stroke-opacity="${k === 5 ? 0.55 : 0.25}"/>`).join('');
        const spokes: string[] = [];
        for (let k = 0; k < 12; k++) {
            const a = (k * Math.PI) / 6;
            spokes.push(`M${n(cx)} ${n(cy)}L${n(cx + R * Math.cos(a))} ${n(cy + R * Math.sin(a))}`);
        }
        const ticks: string[] = [];
        for (let k = 0; k < 72; k++) {
            const a = (k * Math.PI) / 36;
            const len = k % 6 === 0 ? 16 : 7;
            ticks.push(`M${n(cx + R * Math.cos(a))} ${n(cy + R * Math.sin(a))}L${n(cx + (R - len) * Math.cos(a))} ${n(cy + (R - len) * Math.sin(a))}`);
        }

        // The beam: a fan of thin wedges fading behind the leading edge.
        const lead = r() * Math.PI * 2;
        const span = Math.PI / 3;
        const WEDGES = 30;
        const wedges: string[] = [];
        for (let k = 0; k < WEDGES; k++) {
            const a2 = lead - (k * span) / WEDGES;
            const a1 = a2 - span / WEDGES - 0.004;
            wedges.push(
                `<path d="M${n(cx)} ${n(cy)}L${n(cx + R * Math.cos(a1))} ${n(cy + R * Math.sin(a1))}A${n(R)} ${n(R)} 0 0 1 ${n(cx + R * Math.cos(a2))} ${n(cy + R * Math.sin(a2))}Z" fill-opacity="${n(0.3 * Math.pow(1 - k / WEDGES, 1.6))}"/>`,
            );
        }
        const edge = `<line x1="${n(cx)}" y1="${n(cy)}" x2="${n(cx + R * Math.cos(lead))}" y2="${n(cy + R * Math.sin(lead))}"/>`;

        const blips: string[] = [];
        const echoes: string[] = [];
        const tags: string[] = [];
        for (let k = 0; k < 38; k++) {
            const a = r() * Math.PI * 2;
            const dist = Math.sqrt(r()) * R * 0.95;
            const x = cx + dist * Math.cos(a);
            const y = cy + dist * Math.sin(a);
            const age = (((lead - a) % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2) / (Math.PI * 2);
            const fresh = Math.pow(1 - age, 2);
            const hue = pick(r, p.hues);
            blips.push(`<circle cx="${n(x)}" cy="${n(y)}" r="${n(2.5 + 3 * fresh)}" fill="${hue}" fill-opacity="${n(0.25 + 0.75 * fresh)}"/>`);
            if (age < 0.18) {
                echoes.push(`<circle cx="${n(x)}" cy="${n(y)}" r="${n(10 + 8 * fresh)}" stroke="${hue}" stroke-opacity="${n(0.6 * fresh)}"/>`);
                tags.push(
                    `<path d="M${n(x + 6)} ${n(y - 6)}l18 -14h40" stroke="${p.glow}" stroke-opacity=".5" fill="none"/>` +
                        `<rect x="${n(x + 26)}" y="${n(y - 27)}" width="${n(between(r, 24, 44))}" height="4" rx="2" fill="${p.glow}" fill-opacity=".6"/>` +
                        `<rect x="${n(x + 26)}" y="${n(y - 16)}" width="${n(between(r, 14, 30))}" height="3" rx="1.5" fill="${p.glow}" fill-opacity=".35"/>`,
                );
            }
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<path d="${grid.join('')}" stroke="${p.line}" stroke-opacity=".18" stroke-width="1"/>` +
            `<g fill="none" stroke="${p.glow}" stroke-width="1.2">${rings}</g>` +
            `<path d="${spokes.join('')}" stroke="${p.line}" stroke-opacity=".5" stroke-dasharray="2 6"/>` +
            `<path d="${ticks.join('')}" stroke="${p.glow}" stroke-opacity=".5" stroke-width="1.2"/>` +
            `<g fill="${p.accent}">${wedges.join('')}</g>` +
            `<g stroke="${p.glow}" stroke-width="6" stroke-opacity=".5" filter="url(#glow)">${edge}</g>` +
            `<g stroke="${p.sheen}" stroke-width="1.8">${edge}</g>` +
            `<g fill="none" stroke-width="1.3">${echoes.join('')}</g>` +
            blips.join('') +
            tags.join('') +
            wheel(cx, cy, 14, p.glow, 1.5, 0.8) +
            vig.body
        );
    },
};

/**
 * The drawings: a helm wheel and a container set out on squared paper, with
 * their centre lines, dimensions and title block.
 */
const blueprint: Scene = {
    id: 'blueprint',
    name: 'Blueprint',
    blurb: 'The drawings for a helm wheel and a container, as a draughtsman would set them out.',
    draw(d) {
        const { r, p, w, h } = d;
        const paper = p.dark ? toHex(mix(rgb(p.shade), rgb(p.accent), 0.12)) : toHex(mix(rgb(p.ground), rgb(p.accent), 0.08));
        const ink = p.dark ? p.sheen : p.glow;
        const vig = vignette(d, 0.6);
        const defs =
            radial('bg', [
                [0, p.dark ? toHex(mix(rgb(paper), rgb(p.accent), 0.12)) : p.lift, 1],
                [1, paper, 1],
            ], { cx: 0.5, cy: 0.45, r: 0.8 }) +
            `<marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 1L10 5L0 9z" fill="${ink}" fill-opacity=".8"/></marker>` +
            vig.defs;

        const minor: string[] = [];
        const major: string[] = [];
        for (let x = 0; x <= w; x += 20) (x % 100 === 0 ? major : minor).push(`M${x} 0V${h}`);
        for (let y = 0; y <= h; y += 20) (y % 100 === 0 ? major : minor).push(`M0 ${y}H${w}`);

        const label = (x: number, y: number, width: number) =>
            `<rect x="${n(x - width / 2)}" y="${n(y - 3)}" width="${n(width)}" height="5" rx="1" fill="${ink}" fill-opacity=".55"/>`;
        const dim = (x1: number, y1: number, x2: number, y2: number) =>
            `<line x1="${n(x1)}" y1="${n(y1)}" x2="${n(x2)}" y2="${n(y2)}" marker-start="url(#arrow)" marker-end="url(#arrow)"/>`;

        // The helm.
        const hx = w * between(r, 0.62, 0.72);
        const hyy = h * between(r, 0.38, 0.46);
        const R = h * between(r, 0.2, 0.25);
        const helm =
            `<g fill="none" stroke="${ink}" stroke-opacity=".35" stroke-dasharray="14 4 3 4">` +
            `<line x1="${n(hx - R * 1.5)}" y1="${n(hyy)}" x2="${n(hx + R * 1.5)}" y2="${n(hyy)}"/>` +
            `<line x1="${n(hx)}" y1="${n(hyy - R * 1.5)}" x2="${n(hx)}" y2="${n(hyy + R * 1.5)}"/>` +
            `<circle cx="${n(hx)}" cy="${n(hyy)}" r="${n(R * 1.25)}"/>` +
            `</g>` +
            wheel(hx, hyy, R, ink, 2, 0.85) +
            `<circle cx="${n(hx)}" cy="${n(hyy)}" r="${n(R * 0.12)}" fill="none" stroke="${ink}" stroke-opacity=".7" stroke-width="1.5"/>` +
            `<g stroke="${ink}" stroke-opacity=".6" stroke-width="1">` +
            `<line x1="${n(hx - R * 1.12)}" y1="${n(hyy + R * 0.2)}" x2="${n(hx - R * 1.12)}" y2="${n(hyy + R * 1.55)}"/>` +
            `<line x1="${n(hx + R * 1.12)}" y1="${n(hyy + R * 0.2)}" x2="${n(hx + R * 1.12)}" y2="${n(hyy + R * 1.55)}"/>` +
            dim(hx - R * 1.12, hyy + R * 1.45, hx + R * 1.12, hyy + R * 1.45) +
            `</g>` +
            label(hx, hyy + R * 1.45 - 10, 60);

        // A container, side on.
        const cx0 = w * between(r, 0.06, 0.12);
        const cy0 = h * between(r, 0.56, 0.62);
        const cw = between(r, 400, 460);
        const ch = cw * 0.4;
        const ribs: string[] = [];
        for (let x = cx0 + 16; x < cx0 + cw - 10; x += 14) ribs.push(`M${n(x)} ${n(cy0 + 8)}V${n(cy0 + ch - 8)}`);
        const corners = [
            [cx0, cy0],
            [cx0 + cw - 16, cy0],
            [cx0, cy0 + ch - 12],
            [cx0 + cw - 16, cy0 + ch - 12],
        ]
            .map(([x, y]) => `<rect x="${n(x)}" y="${n(y)}" width="16" height="12"/>`)
            .join('');
        const container =
            `<g fill="none" stroke="${ink}" stroke-width="1.6" stroke-opacity=".85">` +
            `<rect x="${n(cx0)}" y="${n(cy0)}" width="${n(cw)}" height="${n(ch)}"/>${corners}</g>` +
            `<path d="${ribs.join('')}" stroke="${ink}" stroke-opacity=".35"/>` +
            `<g stroke="${ink}" stroke-opacity=".6" stroke-width="1">` +
            `<line x1="${n(cx0)}" y1="${n(cy0 - 8)}" x2="${n(cx0)}" y2="${n(cy0 - 44)}"/>` +
            `<line x1="${n(cx0 + cw)}" y1="${n(cy0 - 8)}" x2="${n(cx0 + cw)}" y2="${n(cy0 - 44)}"/>` +
            dim(cx0, cy0 - 34, cx0 + cw, cy0 - 34) +
            `<line x1="${n(cx0 + cw + 8)}" y1="${n(cy0)}" x2="${n(cx0 + cw + 44)}" y2="${n(cy0)}"/>` +
            `<line x1="${n(cx0 + cw + 8)}" y1="${n(cy0 + ch)}" x2="${n(cx0 + cw + 44)}" y2="${n(cy0 + ch)}"/>` +
            dim(cx0 + cw + 34, cy0, cx0 + cw + 34, cy0 + ch) +
            `</g>` +
            label(cx0 + cw / 2, cy0 - 44, 70);

        // The same container in isometric wireframe, hidden edges dashed.
        const ix = w * between(r, 0.16, 0.26);
        const iy = h * between(r, 0.22, 0.28);
        const L = 200;
        const D = 80;
        const H = 80;
        const iso = (x: number, y: number, z: number): Pt => [ix + (x - y) * 0.866, iy + (x + y) * 0.5 - z];
        const e = (a: Pt, b: Pt) => `M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}`;
        const P = {
            a: iso(0, 0, 0), b: iso(L, 0, 0), c: iso(L, D, 0), dd: iso(0, D, 0),
            A: iso(0, 0, H), B: iso(L, 0, H), C: iso(L, D, H), DD: iso(0, D, H),
        };
        const seen = [e(P.b, P.c), e(P.c, P.dd), e(P.A, P.B), e(P.B, P.C), e(P.C, P.DD), e(P.DD, P.A), e(P.b, P.B), e(P.c, P.C), e(P.dd, P.DD)];
        const hidden = [e(P.a, P.b), e(P.a, P.dd), e(P.a, P.A)];
        const wire =
            `<path d="${seen.join('')}" stroke="${ink}" stroke-opacity=".8" stroke-width="1.5" fill="none"/>` +
            `<path d="${hidden.join('')}" stroke="${ink}" stroke-opacity=".45" stroke-dasharray="6 5" fill="none"/>`;

        // The title block, and notes.
        const tbx = w - 360;
        const tby = h - 150;
        const block =
            `<g fill="none" stroke="${ink}" stroke-opacity=".7" stroke-width="1.3">` +
            `<rect x="${tbx}" y="${tby}" width="320" height="110"/>` +
            `<path d="M${tbx} ${tby + 40}H${tbx + 320}M${tbx + 110} ${tby}V${tby + 110}M${tbx + 110} ${tby + 75}H${tbx + 320}M${tbx + 230} ${tby + 40}V${tby + 110}"/>` +
            `</g>` +
            wheel(tbx + 55, tby + 55, 24, ink, 1.4, 0.75) +
            label(tbx + 215, tby + 20, 150) +
            label(tbx + 170, tby + 57, 70) +
            label(tbx + 275, tby + 57, 50) +
            label(tbx + 170, tby + 93, 60) +
            label(tbx + 275, tby + 93, 40);
        const notes = Array.from({ length: 5 }, (_, k) =>
            `<rect x="${n(w * 0.06)}" y="${n(h * 0.08 + k * 16)}" width="${n(between(r, 90, 240))}" height="5" rx="1" fill="${ink}" fill-opacity="${k === 0 ? 0.6 : 0.3}"/>`,
        ).join('');

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<path d="${minor.join('')}" stroke="${ink}" stroke-opacity=".06" stroke-width="1"/>` +
            `<path d="${major.join('')}" stroke="${ink}" stroke-opacity=".13" stroke-width="1"/>` +
            helm +
            container +
            wire +
            block +
            notes +
            vig.body
        );
    },
};

/**
 * A spiral galaxy of pods, arms trailing round a bright control plane.
 */
const galaxy: Scene = {
    id: 'galaxy',
    name: 'Galaxy',
    blurb: 'A spiral of countless pods turning round the control plane.',
    draw(d) {
        const { r, p, w, h } = d;
        const cx = w * between(r, 0.45, 0.6);
        const cy = h * between(r, 0.42, 0.55);
        const Rm = w * between(r, 0.42, 0.5);
        const tilt = between(r, 0.4, 0.55);
        const rot = between(r, -0.5, 0.5);
        const arms = r() < 0.5 ? 2 : 3;
        const vig = vignette(d, 0.7);
        const defs =
            radial('bg', [
                [0, p.lift, 1],
                [0.6, p.ground, 1],
                [1, p.shade, 1],
            ], { cx: cx / w, cy: cy / h, r: 0.85 }) +
            radial('core', [
                [0, p.sheen, 1],
                [0.25, p.glow, 0.8],
                [1, p.accent, 0],
            ]) +
            p.hues
                .slice(0, 3)
                .map((hue, k) =>
                    radial(`neb${k}`, [
                        [0, hue, p.dark ? 0.2 : 0.14],
                        [1, hue, 0],
                    ]),
                )
                .join('') +
            blur('glow', 12) +
            vig.defs;

        const place = (ang: number, rad: number): Pt => {
            const x = Math.cos(ang) * rad;
            const y = Math.sin(ang) * rad * tilt;
            return [cx + x * Math.cos(rot) - y * Math.sin(rot), cy + x * Math.sin(rot) + y * Math.cos(rot)];
        };
        const small: Record<string, string[]> = {};
        const large: Record<string, string[]> = {};
        const nebulae: string[] = [];
        for (let i = 0; i < 1700; i++) {
            const t = Math.pow(r(), 0.85);
            const arm = Math.floor(r() * arms);
            const ang = (arm * 2 * Math.PI) / arms + t * 3.3 * Math.PI + (r() - 0.5) * 0.9 * (1 - t * 0.4);
            const rad = t * Rm + (r() - 0.5) * Rm * 0.08;
            const [x, y] = place(ang, rad);
            if (x < -10 || x > w + 10 || y < -10 || y > h + 10) continue;
            const colour = t < 0.14 ? p.sheen : r() < 0.45 ? p.glow : pick(r, p.hues);
            ((r() < 0.12 ? large : small)[colour] ??= []).push(dot(x, y));
            if (i % 140 === 0) nebulae.push(pool(`neb${i % 3}`, x, y, between(r, 90, 180), between(r, 60, 110)));
        }
        const field: string[] = [];
        for (let k = 0; k < 220; k++) field.push(dot(r() * w, r() * h));
        const coreR = Rm * 0.26;

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<path d="${field.join('')}" stroke="${p.dark ? p.sheen : p.line}" stroke-opacity=".5" stroke-width="1.4" stroke-linecap="round"/>` +
            nebulae.join('') +
            `<g fill="none" stroke-linecap="round" stroke-opacity=".75">${grouped(small, 'stroke-width="1.6"')}${grouped(large, 'stroke-width="3.2"')}</g>` +
            `<ellipse cx="${n(cx)}" cy="${n(cy)}" rx="${n(coreR)}" ry="${n(coreR * tilt * 1.3)}" transform="rotate(${n((rot * 180) / Math.PI)} ${n(cx)} ${n(cy)})" fill="url(#core)"/>` +
            `<circle cx="${n(cx)}" cy="${n(cy)}" r="${n(coreR * 0.35)}" fill="${p.glow}" opacity=".45" filter="url(#glow)"/>` +
            vig.body
        );
    },
};

/**
 * Lights out of focus behind the glass, and the cluster sharp in front of
 * them: the calmest of the pictures.
 */
const bokeh: Scene = {
    id: 'bokeh',
    name: 'Bokeh',
    blurb: 'City lights out of focus, with the cluster sharp in front of them.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.7);
        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.55, p.ground, 1],
                [1, p.shade, 1],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            p.hues
                .map((hue, k) =>
                    radial(`disc${k}`, [
                        [0, hue, p.dark ? 0.2 : 0.14],
                        [0.82, hue, p.dark ? 0.28 : 0.2],
                        [1, hue, p.dark ? 0.5 : 0.34],
                    ]),
                )
                .join('') +
            blur('far', 9) +
            blur('mid', 3) +
            vig.defs;

        const centres = Array.from({ length: 3 }, () => [between(r, 0.1, 0.9) * w, between(r, 0.15, 0.85) * h] as Pt);
        const disc = (rad: number) => {
            const [ox, oy] = pick(r, centres);
            const spread = w * 0.28;
            const x = ox + (r() + r() - 1) * spread;
            const y = oy + (r() + r() - 1) * spread * 0.6;
            return `<circle cx="${n(x)}" cy="${n(y)}" r="${n(rad)}" fill="url(#disc${Math.floor(r() * 4)})" opacity="${n(between(r, 0.45, 1))}"/>`;
        };
        const far = Array.from({ length: 42 }, () => disc(between(r, 50, 150))).join('');
        const mid = Array.from({ length: 30 }, () => disc(between(r, 16, 60))).join('');

        const nodes: Pt[] = Array.from({ length: 16 }, () => [between(r, 0.05, 0.95) * w, between(r, 0.08, 0.92) * h] as Pt);
        const links: string[] = [];
        nodes.forEach((a, k) => {
            const b = nodes
                .filter((x) => x !== a)
                .sort((x, y) => Math.hypot(a[0] - x[0], a[1] - x[1]) - Math.hypot(a[0] - y[0], a[1] - y[1]))[k % 2];
            links.push(`M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}`);
        });

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<g filter="url(#far)">${far}</g>` +
            `<g filter="url(#mid)">${mid}</g>` +
            `<path d="${links.join('')}" stroke="${p.glow}" stroke-opacity=".28" stroke-width="1"/>` +
            `<path d="${nodes.map(([x, y]) => dot(x, y)).join('')}" stroke="${p.dark ? p.sheen : p.glow}" stroke-width="5" stroke-linecap="round" stroke-opacity=".85"/>` +
            vig.body
        );
    },
};

/**
 * The cluster's metrics charted in light: lines over soft areas, a threshold
 * one of them has crossed, and a histogram along the floor.
 */
const telemetry: Scene = {
    id: 'telemetry',
    name: 'Telemetry',
    blurb: 'Metrics from across the cluster, charted in light.',
    draw(d) {
        const { r, p, w, h } = d;
        const vig = vignette(d, 0.75);
        const SERIES = 4;
        let defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.5, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            blur('glow', 4) +
            vig.defs;

        const grid: string[] = [];
        for (let y = 40; y < h; y += 50) grid.push(`M0 ${y}H${w}`);
        for (let x = 40; x < w; x += 80) grid.push(`M${x} 0V${h}`);

        const series: string[] = [];
        const glows: string[] = [];
        const markers: string[] = [];
        let spike: Pt = [w / 2, h / 2];
        for (let k = 0; k < SERIES; k++) {
            const hue = p.hues[k % 4];
            defs += linear(`area${k}`, [
                [0, hue, p.dark ? 0.32 : 0.24],
                [1, hue, 0],
            ]);
            const yb = h * (0.28 + k * 0.17);
            const amp = between(r, 40, 90);
            const raw: number[] = [];
            let v = 0;
            for (let x = -20; x <= w + 20; x += 10) {
                v = v * 0.92 + (r() - 0.5) * 0.9;
                raw.push(v);
            }
            const smooth = raw.map((_, i) => {
                const win = raw.slice(Math.max(0, i - 3), i + 4);
                return win.reduce((a, b) => a + b, 0) / win.length;
            });
            const peak = Math.max(...smooth.map(Math.abs)) || 1;
            const line: Pt[] = smooth.map((s, i) => [-20 + i * 10, yb - (s / peak) * amp]);
            const floor = yb + amp * 0.8;
            series.push(
                `<polygon points="${pts([...line, [w + 20, floor], [-20, floor]])}" fill="url(#area${k})"/>` +
                    `<polyline points="${pts(line)}" fill="none" stroke="${hue}" stroke-width="2" stroke-linejoin="round"/>`,
            );
            glows.push(`<polyline points="${pts(line)}" fill="none" stroke="${hue}" stroke-width="6"/>`);
            // A few readings marked, and one reading worth a threshold.
            for (let m = 0; m < 3; m++) {
                const [x, y] = pick(r, line.slice(10, -10));
                markers.push(`<circle cx="${n(x)}" cy="${n(y)}" r="7" fill="none" stroke="${hue}" stroke-opacity=".6"/><circle cx="${n(x)}" cy="${n(y)}" r="3" fill="${p.dark ? p.sheen : hue}"/>`);
            }
            if (k === 0) spike = line.reduce((a, b) => (b[1] < a[1] ? b : a));
        }
        const threshold = spike[1] + 14;

        const bars: string[] = [];
        let level = between(r, 0.3, 0.7);
        for (let x = 20; x < w; x += 18) {
            level = Math.max(0.1, Math.min(1, level + (r() - 0.5) * 0.25));
            const bh = level * 90;
            bars.push(`M${x} ${n(h - 20)}V${n(h - 20 - bh)}`);
        }

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            `<path d="${grid.join('')}" stroke="${p.line}" stroke-opacity=".22" stroke-width="1"/>` +
            `<path d="${bars.join('')}" stroke="${p.accent}" stroke-opacity=".28" stroke-width="12"/>` +
            `<g filter="url(#glow)" opacity=".45">${glows.join('')}</g>` +
            series.join('') +
            `<line x1="0" y1="${n(threshold)}" x2="${w}" y2="${n(threshold)}" stroke="${p.hues[1]}" stroke-opacity=".7" stroke-width="1.4" stroke-dasharray="10 6"/>` +
            `<circle cx="${n(spike[0])}" cy="${n(spike[1])}" r="18" fill="none" stroke="${p.hues[1]}" stroke-opacity=".7"/>` +
            markers.join('') +
            vig.body
        );
    },
};

/**
 * A container ship of glass and light crossing a dark harbour: its cargo lit
 * from within, a neon waterline, a wake of light behind it, and a beacon
 * tower sweeping its beam across the water. Drawn on the same isometric
 * plane as the glass cubes and the container yard.
 */
const crossing: Scene = {
    id: 'crossing',
    name: 'Night Crossing',
    blurb: 'A container ship of glass and light heading out across a dark harbour.',
    draw(d) {
        const { r, p, w, h } = d;
        const tw = between(r, 40, 46);
        const L = Math.round(between(r, 22, 26));
        const B = 6;
        const BOW = 5;
        const ic = (L + BOW) / 2;
        const jc = B / 2;
        const iso: Iso = { tw, ox: 0, oy: 0 };
        iso.ox = w * between(r, 0.48, 0.56) - ((ic - jc) * tw) / 2;
        iso.oy = h * between(r, 0.52, 0.58) - ((ic + jc) * tw) / 4;
        const P = (i: number, j: number, z = 0) => project(iso, i, j, z);
        const Hh = tw * 1.1;
        const vig = vignette(d, 0.85);

        const beacon: Pt = [L + BOW + between(r, 4, 7), -between(r, 5, 8)];
        const beaconTall = between(r, 230, 290);
        const lamp = P(beacon[0] + 0.45, beacon[1] + 0.45, beaconTall + 10);
        const angle = between(r, -2.95, -2.55);
        const reach = 1700;
        const far1: Pt = [lamp[0] + reach * Math.cos(angle - 0.07), lamp[1] + reach * Math.sin(angle - 0.07)];
        const far2: Pt = [lamp[0] + reach * Math.cos(angle + 0.07), lamp[1] + reach * Math.sin(angle + 0.07)];

        const defs =
            linear('bg', [
                [0, p.shade, 1],
                [0.55, p.ground, 1],
                [1, p.shade, 1],
            ]) +
            radial('light', [
                [0, p.glow, p.dark ? 0.22 : 0.3],
                [1, p.glow, 0],
            ]) +
            linear('gtop', [
                [0, p.glow, p.dark ? 0.55 : 0.5],
                [1, p.accent, p.dark ? 0.14 : 0.2],
            ], { x1: 0, y1: 0, x2: 1, y2: 1 }) +
            linear('gleft', [
                [0, p.accent, p.dark ? 0.42 : 0.3],
                [1, p.shade, p.dark ? 0.9 : 0.5],
            ]) +
            linear('gright', [
                [0, p.glow, p.dark ? 0.3 : 0.35],
                [1, p.ground, p.dark ? 0.8 : 0.55],
            ]) +
            `<linearGradient id="beam" gradientUnits="userSpaceOnUse" x1="${n(lamp[0])}" y1="${n(lamp[1])}" x2="${n(lamp[0] + reach * Math.cos(angle))}" y2="${n(lamp[1] + reach * Math.sin(angle))}">` +
            `<stop offset="0" stop-color="${p.glow}" stop-opacity="${p.dark ? 0.45 : 0.3}"/><stop offset="1" stop-color="${p.glow}" stop-opacity="0"/></linearGradient>` +
            blur('glow', 3) +
            blur('soft', 4) +
            blur('wide', 10) +
            vig.defs;

        // The water: the plane's grid, a scatter of ripples, distant lights.
        const grid: string[] = [];
        for (let k = -60; k <= 90; k += 2) {
            const a = P(k, -60);
            const b = P(k, 90);
            const c = P(-60, k);
            const e = P(90, k);
            grid.push(`M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}M${n(c[0])} ${n(c[1])}L${n(e[0])} ${n(e[1])}`);
        }
        const ripples: string[] = [];
        for (let k = 0; k < 160; k++) {
            const i = between(r, -40, 70);
            const j = between(r, -40, 50);
            const [x, y] = P(i, j);
            if (x < 0 || x > w || y < 0 || y > h) continue;
            const [x2, y2] = P(i + between(r, 0.6, 1.6), j);
            ripples.push(`M${n(x)} ${n(y)}L${n(x2)} ${n(y2)}`);
        }
        const buoys: string[] = [];
        for (let k = 0; k < 16; k++) buoys.push(`M${n(r() * w)} ${n(between(r, 0.02, 0.3) * h)}h.01`);

        // The wake, spreading and fading behind the stern.
        const wake: string[] = [];
        const foam: string[] = [];
        for (const [j0, spread] of [[0.3, -0.3], [B - 0.3, 0.3]] as [number, number][]) {
            for (let k = 0; k < 8; k++) {
                const t1 = k * 1.6;
                const t2 = t1 + 1.6;
                const a = P(-t1, j0 + spread * t1);
                const b = P(-t2, j0 + spread * t2);
                wake.push(`<line x1="${n(a[0])}" y1="${n(a[1])}" x2="${n(b[0])}" y2="${n(b[1])}" stroke-opacity="${n(0.8 * Math.pow(1 - k / 8, 1.5))}"/>`);
            }
        }
        for (let k = 0; k < 90; k++) {
            const t = Math.pow(r(), 0.8) * 13;
            const [x, y] = P(-t, B / 2 + (r() - 0.5) * (B + t * 0.6));
            foam.push(`M${n(x)} ${n(y)}h.01`);
        }
        const [s0x, s0y] = P(0, B / 2);
        const [s1x, s1y] = P(-12, B / 2);

        /** The ship, above the water or -- mirrored -- below it. */
        const ship = (mirror: boolean) => {
            const s = mirror ? -1 : 1;
            const plan: Pt[] = [[0, 0], [L, 0], [L + BOW, B / 2], [L, B], [0, B]];
            const sides = plan
                .map((a, k) => [a, plan[(k + 1) % plan.length]] as [Pt, Pt])
                .sort(([a1, b1], [a2, b2]) => a1[0] + a1[1] + b1[0] + b1[1] - (a2[0] + a2[1] + b2[0] + b2[1]))
                .map(([a, b]) => {
                    const facing = b[1] === B && a[1] === B ? 'gleft' : a[0] >= L ? 'gright' : null;
                    const quad = [P(a[0], a[1]), P(b[0], b[1]), P(b[0], b[1], s * Hh), P(a[0], a[1], s * Hh)];
                    return `<polygon points="${pts(quad)}" fill="${facing ? `url(#${facing})` : p.shade}"/>`;
                })
                .join('');
            const deck = `<polygon points="${pts(plan.map(([i, j]) => P(i, j, s * Hh)))}" fill="${p.dark ? toHex(mix(rgb(p.shade), rgb(p.accent), 0.12)) : p.lift}" stroke="${p.glow}" stroke-opacity=".55"/>`;

            // Containers: glass boxes, lit from inside, in bays along the deck.
            const boxes: { key: number; svg: string }[] = [];
            for (let i = 5.4; i + 2 <= L - 0.4; i += 2.15) {
                for (let j = 0.45; j + 0.95 <= B - 0.4; j += 1.03) {
                    const stack = 1 + Math.floor(r() * 4);
                    for (let level = 0; level < stack; level++) {
                        const f = boxFaces(iso, i, j, 2, 0.95, s * 14, s * (Hh + level * 14.5));
                        const hue = r() < 0.5 ? p.glow : pick(r, p.hues);
                        boxes.push({
                            key: i + j + level * 0.01,
                            svg:
                                `<polygon points="${pts(f.left)}" fill="${hue}" fill-opacity="${p.dark ? 0.32 : 0.35}"/>` +
                                `<polygon points="${pts(f.right)}" fill="${hue}" fill-opacity="${p.dark ? 0.18 : 0.22}"/>` +
                                `<polygon points="${pts(f.top)}" fill="${hue}" fill-opacity="${p.dark ? 0.5 : 0.55}" stroke="${p.glow}" stroke-opacity=".6" stroke-width=".8"/>`,
                        });
                    }
                }
            }
            boxes.sort((a, b) => a.key - b.key);

            // The bridge at the stern, with its windows lit.
            const bridge = boxFaces(iso, 0.8, 0.6, 3.2, B - 1.2, s * 64, s * Hh);
            const windows: string[] = [];
            for (const up of [16, 28, 40, 52]) {
                const a = P(1.1, B - 0.6, s * (Hh + up));
                const b = P(3.8, B - 0.6, s * (Hh + up));
                const c = P(4.0, 0.9, s * (Hh + up));
                const e = P(4.0, B - 0.9, s * (Hh + up));
                windows.push(`M${n(a[0])} ${n(a[1])}L${n(b[0])} ${n(b[1])}M${n(c[0])} ${n(c[1])}L${n(e[0])} ${n(e[1])}`);
            }
            const mastFoot = P(2.4, B / 2, s * (Hh + 64));
            const mastTop = P(2.4, B / 2, s * (Hh + 110));
            const waterline = [P(0, B, s * 6), P(L, B, s * 6), P(L + BOW, B / 2, s * 6)];

            return (
                sides +
                deck +
                `<polyline points="${pts(waterline)}" fill="none" stroke="${p.hues[0]}" stroke-width="2.2"/>` +
                `<polyline points="${pts(waterline)}" fill="none" stroke="${p.hues[0]}" stroke-width="7" stroke-opacity=".5" filter="url(#glow)"/>` +
                `<polygon points="${pts(bridge.left)}" fill="url(#gleft)"/>` +
                `<polygon points="${pts(bridge.right)}" fill="url(#gright)"/>` +
                `<polygon points="${pts(bridge.top)}" fill="url(#gtop)" stroke="${p.glow}" stroke-opacity=".7"/>` +
                `<path d="${windows.join('')}" stroke="${p.sheen}" stroke-opacity=".85" stroke-width="1.6"/>` +
                boxes.map((b) => b.svg).join('') +
                `<line x1="${n(mastFoot[0])}" y1="${n(mastFoot[1])}" x2="${n(mastTop[0])}" y2="${n(mastTop[1])}" stroke="${p.glow}" stroke-width="1.5"/>` +
                `<path d="M${n(mastTop[0])} ${n(mastTop[1])}h.01" stroke="${p.hues[1]}" stroke-width="6" stroke-linecap="round"/>`
            );
        };

        // The beacon tower, glass like the rest, and its beam.
        const tower = boxFaces(iso, beacon[0], beacon[1], 0.9, 0.9, beaconTall);
        const towerSvg =
            `<polygon points="${pts(tower.left)}" fill="url(#gleft)"/>` +
            `<polygon points="${pts(tower.right)}" fill="url(#gright)"/>` +
            `<polygon points="${pts(tower.top)}" fill="url(#gtop)"/>` +
            `<ellipse cx="${n(lamp[0])}" cy="${n(lamp[1])}" rx="26" ry="11" fill="none" stroke="${p.glow}" stroke-width="2"/>` +
            `<circle cx="${n(lamp[0])}" cy="${n(lamp[1])}" r="7" fill="${p.sheen}"/>` +
            `<circle cx="${n(lamp[0])}" cy="${n(lamp[1])}" r="16" fill="${p.glow}" opacity=".6" filter="url(#glow)"/>`;

        return (
            `<defs>${defs}</defs>` +
            `<rect width="${w}" height="${h}" fill="url(#bg)"/>` +
            pool('light', w * 0.5, h * 0.5, w * 0.55, h * 0.5) +
            `<path d="${grid.join('')}" stroke="${p.line}" stroke-opacity=".22" stroke-width="1"/>` +
            `<path d="${ripples.join('')}" stroke="${p.glow}" stroke-opacity=".2" stroke-width="1.2" stroke-linecap="round"/>` +
            `<path d="${buoys.join('')}" stroke="${p.hues[3]}" stroke-width="4" stroke-linecap="round" filter="url(#soft)"/>` +
            `<polygon points="${pts([lamp, far1, far2])}" fill="url(#beam)"/>` +
            `<line x1="${n(s0x)}" y1="${n(s0y)}" x2="${n(s1x)}" y2="${n(s1y)}" stroke="${p.glow}" stroke-width="40" stroke-opacity=".12" filter="url(#wide)"/>` +
            `<g stroke="${p.glow}" stroke-width="2" stroke-linecap="round">${wake.join('')}</g>` +
            `<path d="${foam.join('')}" stroke="${p.dark ? p.sheen : p.glow}" stroke-opacity=".45" stroke-width="2.2" stroke-linecap="round"/>` +
            `<g opacity=".28" filter="url(#soft)">${ship(true)}</g>` +
            ship(false) +
            towerSvg +
            vig.body
        );
    },
};

/**
 * A wireframe ocean rolling in towards the viewer under a low sun.
 */
const swell: Scene = {
    id: 'swell',
    name: 'Swell',
    blurb: 'A wireframe ocean rolling towards you under a low sun.',
    draw(d) {
        const { r, p, w, h } = d;
        const hy = h * between(r, 0.34, 0.42);
        const cx = w * between(r, 0.4, 0.6);
        const sun: Pt = [w * between(r, 0.3, 0.7), hy - between(r, 20, 70)];
        // By day the ink colour would put a dark band on the horizon; the haze
        // there is the accent, faint.
        const haze = p.dark ? p.glow : p.accent;
        const defs =
            linear('sky', [
                [0, p.shade, 1],
                [0.65, p.ground, 1],
                [1, haze, p.dark ? 0.3 : 0.16],
            ]) +
            linear('sea', [
                [0, p.lift, 1],
                [1, p.shade, 1],
            ]) +
            `<linearGradient id="depth" gradientUnits="userSpaceOnUse" x1="0" y1="${n(hy)}" x2="0" y2="${h}">` +
            `<stop offset="0" stop-color="#fff" stop-opacity=".08"/><stop offset=".4" stop-color="#fff" stop-opacity=".55"/><stop offset="1" stop-color="#fff" stop-opacity="1"/></linearGradient>` +
            radial('sun', [
                [0, p.sheen, 1],
                [0.4, haze, p.dark ? 0.9 : 0.3],
                [1, haze, 0],
            ]) +
            blur('glow', 8);

        const a1 = between(r, 14, 28);
        const k1 = between(r, 0.004, 0.008);
        const k2 = between(r, 0.006, 0.012);
        const a2 = between(r, 6, 14);
        const k3 = between(r, 0.01, 0.02);
        const k4 = between(r, 0.004, 0.009);
        const ph1 = r() * 6.28;
        const ph2 = r() * 6.28;
        const lift = (X: number, Z: number) => a1 * Math.sin(X * k1 + Z * k2 + ph1) + a2 * Math.sin(X * k3 - Z * k4 + ph2);
        const F = 900;
        const cam = 130;
        const project = (X: number, Z: number): Pt => [cx + (X * F) / Z, hy + ((cam - lift(X, Z)) * F) / Z];

        const ROWS = 42;
        const near = 160;
        const far = 5000;
        const zs = Array.from({ length: ROWS }, (_, k) => near * Math.pow(far / near, k / (ROWS - 1)));
        const rowsSvg: string[] = [];
        for (const Z of zs) {
            const span = ((w / 2 + 200) * Z) / F;
            const line: Pt[] = [];
            for (let X = -span; X <= span; X += span / 60) line.push(project(X, Z));
            rowsSvg.push(`<polyline points="${pts(line)}"/>`);
        }
        const colsSvg: string[] = [];
        const reachX = ((w / 2 + 200) * far) / F;
        for (let X = -reachX; X <= reachX; X += reachX / 40) {
            const line = zs.map((Z) => project(X, Z)).filter(([x, y]) => x > -300 && x < w + 300 && y < h + 300);
            if (line.length > 1) colsSvg.push(`<polyline points="${pts(line)}"/>`);
        }
        const specks: string[] = [];
        for (let k = 0; k < 60; k++) specks.push(dot(sun[0] + (r() - 0.5) * 300, hy + Math.pow(r(), 2) * (h - hy) * 0.6));

        return (
            `<defs>${defs}<mask id="fadeout"><rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#depth)"/></mask></defs>` +
            `<rect width="${w}" height="${n(hy)}" fill="url(#sky)"/>` +
            `<circle cx="${n(sun[0])}" cy="${n(sun[1])}" r="140" fill="url(#sun)" opacity=".7"/>` +
            `<circle cx="${n(sun[0])}" cy="${n(sun[1])}" r="42" fill="${p.sheen}" fill-opacity="${p.dark ? 0.9 : 0.95}"/>` +
            `<rect y="${n(hy)}" width="${w}" height="${n(h - hy)}" fill="url(#sea)"/>` +
            `<ellipse cx="${n(sun[0])}" cy="${n(hy + 60)}" rx="160" ry="50" fill="${p.dark ? haze : p.sheen}" opacity="${p.dark ? 0.3 : 0.7}" filter="url(#glow)"/>` +
            `<g mask="url(#fadeout)" fill="none">` +
            `<g stroke="${p.line}" stroke-width="1">${colsSvg.join('')}</g>` +
            `<g stroke="${p.glow}" stroke-width="1.3">${rowsSvg.join('')}</g>` +
            `</g>` +
            `<path d="${specks.join('')}" stroke="${p.sheen}" stroke-opacity=".6" stroke-width="2" stroke-linecap="round"/>` +
            `<line x1="0" y1="${n(hy)}" x2="${w}" y2="${n(hy)}" stroke="${haze}" stroke-width="2" stroke-opacity="${p.dark ? 0.6 : 0.3}" filter="url(#glow)"/>`
        );
    },
};

/** The third set, in the order the gallery shows them after the others. */
export const THIRD_SCENES: Scene[] = [terrain, logs, fjord, radar, blueprint, galaxy, bokeh, telemetry, crossing, swell];
