// How dates and times are written, everywhere in the window.
//
// One place for it because the choice is the user's and it has to hold
// everywhere at once: a 24-hour clock in the dashboard and a 12-hour one in
// the event timeline would be worse than either. The settings store holds the
// choice; the app shell hands it here (see App.svelte), and every component
// formats through these functions rather than calling toLocaleTimeString on
// its own. Plugins are handed the same choice through the bridge, and the SDK
// formats with the same rules -- see internal/plugins/sdk/k8sdockside.js,
// which must be kept in step with this file.
//
// The settings are $state, so a component that formats in its template
// redraws when the user changes them.

export type Clock = 'system' | '24h' | '12h';
export type Dates = 'system' | 'iso' | 'dmy' | 'mdy' | 'long';
export type Zone = 'local' | 'utc';
export type Ages = 'relative' | 'absolute';

export interface DateTimeSettings {
    clock: Clock;
    dates: Dates;
    zone: Zone;
    /** Whether a table's time column says "5m" or the moment itself. */
    ages: Ages;
}

export const DEFAULT_DATETIME: DateTimeSettings = { clock: 'system', dates: 'system', zone: 'local', ages: 'relative' };

let current = $state<DateTimeSettings>({ ...DEFAULT_DATETIME });

/** The settings in force. */
export function dateTimeSettings(): DateTimeSettings {
    return current;
}

/** Puts new settings in force. Called by the app shell as the preferences change. */
export function setDateTimeSettings(next: Partial<DateTimeSettings>): void {
    current = { ...DEFAULT_DATETIME, ...next };
}

type When = Date | string | number;

function toDate(when: When): Date | null {
    const d = when instanceof Date ? when : new Date(when);
    return Number.isNaN(d.getTime()) ? null : d;
}

function zone(): string | undefined {
    return current.zone === 'utc' ? 'UTC' : undefined;
}

/** The hour cycle the clock setting asks for, or the locale's. */
function hourCycle(): Intl.DateTimeFormatOptions {
    if (current.clock === '24h') return { hourCycle: 'h23' };
    if (current.clock === '12h') return { hourCycle: 'h12' };
    return {};
}

/** Year, month and day as numbers in the chosen zone. */
function parts(d: Date): { y: string; m: string; d: string } {
    const out = { y: '', m: '', d: '' };
    for (const p of new Intl.DateTimeFormat('en-CA', {
        timeZone: zone(),
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
    }).formatToParts(d)) {
        if (p.type === 'year') out.y = p.value;
        if (p.type === 'month') out.m = p.value;
        if (p.type === 'day') out.d = p.value;
    }
    return out;
}

/** A date: "2026-09-24", "24.09.2026", "09/24/2026", "24 Sep 2026", or the locale's. */
export function formatDate(when: When): string {
    const d = toDate(when);
    if (!d) return '';
    const p = parts(d);
    switch (current.dates) {
        case 'iso':
            return `${p.y}-${p.m}-${p.d}`;
        case 'dmy':
            return `${p.d}.${p.m}.${p.y}`;
        case 'mdy':
            return `${p.m}/${p.d}/${p.y}`;
        case 'long':
            return d.toLocaleDateString(undefined, { timeZone: zone(), day: 'numeric', month: 'short', year: 'numeric' });
        default:
            return d.toLocaleDateString(undefined, { timeZone: zone() });
    }
}

/**
 * A day of the year without the year, for a chart's axis: "09-24", "24.09",
 * "09/24", "24 Sep".
 */
export function formatDay(when: When): string {
    const d = toDate(when);
    if (!d) return '';
    const p = parts(d);
    switch (current.dates) {
        case 'iso':
            return `${p.m}-${p.d}`;
        case 'dmy':
            return `${p.d}.${p.m}`;
        case 'mdy':
            return `${p.m}/${p.d}`;
        default:
            return d.toLocaleDateString(undefined, { timeZone: zone(), day: 'numeric', month: 'short' });
    }
}

/** A time of day, with seconds when asked for. */
export function formatTime(when: When, { seconds = false } = {}): string {
    const d = toDate(when);
    if (!d) return '';
    return d.toLocaleTimeString(undefined, {
        timeZone: zone(),
        hour: '2-digit',
        minute: '2-digit',
        ...(seconds ? { second: '2-digit' } : {}),
        ...hourCycle(),
    });
}

/** A date and a time, and "UTC" after it when that is the zone. */
export function formatDateTime(when: When, { seconds = false } = {}): string {
    const d = toDate(when);
    if (!d) return '';
    const text = `${formatDate(d)} ${formatTime(d, { seconds })}`;
    return current.zone === 'utc' ? `${text} UTC` : text;
}

/**
 * How long ago, as the tables write an age. The backend's rules (see age in
 * internal/kube/render.go), with seconds for the first ten minutes the way
 * kubectl counts them -- "12s", "2m5s" -- because a pod that has just been
 * made is exactly the one somebody is watching:
 *
 *     45s, 2m5s, 15m, 3h20m, 12h, 3d4h, 12d
 */
export function formatAge(when: When, now = Date.now()): string {
    const d = toDate(when);
    if (!d) return '';
    const s = Math.max(0, Math.floor((now - d.getTime()) / 1000));
    if (s < 60) return `${s}s`;
    if (s < 600) return s % 60 ? `${Math.floor(s / 60)}m${s % 60}s` : `${s / 60}m`;
    const minutes = Math.floor(s / 60);
    if (minutes < 60) return `${minutes}m`;
    if (minutes < 60 * 48) {
        const h = Math.floor(minutes / 60);
        const m = minutes % 60;
        return h < 10 && m > 0 ? `${h}h${m}m` : `${h}h`;
    }
    const days = Math.floor(minutes / (60 * 24));
    const hours = Math.floor((minutes % (60 * 24)) / 60);
    return days < 10 && hours > 0 ? `${days}d${hours}h` : `${days}d`;
}

// ----- the clock ages are counted against -----------------------------------
//
// Two readings of the same clock: one that moves every second and one that
// moves every minute. An age under ten minutes reads the first, so a new pod
// counts up as it is watched; anything older reads the second, and so a table
// of a thousand old pods is redrawn once a minute rather than every second.
// Svelte tracks what each cell actually read, which is what makes the split
// work: an old cell never depends on the fast clock at all.

const SECONDS_SHOWN = 10 * 60 * 1000;

const clock = $state({ second: Date.now(), minute: Date.now() });
let ticking = false;

function tick(): void {
    if (ticking || typeof window === 'undefined') return;
    ticking = true;
    setInterval(() => {
        // Nothing to count for while nobody can see it; the first tick after
        // the window is shown again brings every age up to date.
        if (document.visibilityState === 'hidden') return;
        const now = Date.now();
        clock.second = now;
        if (Math.floor(now / 60_000) !== Math.floor(clock.minute / 60_000)) clock.minute = now;
    }, 1000);
}

/** An age that counts up on screen: every second while young, every minute after. */
export function liveAge(when: When): string {
    const d = toDate(when);
    if (!d) return '';
    tick();
    const now = Date.now();
    // Read for what it makes redraw, not for the time: the clock is only as
    // current as its last tick, and the first frame must not be a second
    // behind.
    void (now - d.getTime() < SECONDS_SHOWN ? clock.second : clock.minute);
    return formatAge(d, now);
}

/**
 * A table's time cell as it should read: its age, counting up, or the
 * moment itself when the user asked for that. The other form goes in the
 * tooltip, so whichever is on screen, the other is a hover away.
 */
export function timeCell(text: string, at: string | undefined): { text: string; title: string } {
    if (!at) return { text, title: '' };
    // Counted here rather than taken from the backend's text, which is as old
    // as the last change to the list: a quiet list would otherwise say "0s"
    // about a pod for as long as nothing else happened.
    const age = liveAge(at) || text;
    if (current.ages === 'absolute') return { text: formatDateTime(at), title: `${age} ago` };
    return { text: age, title: formatDateTime(at, { seconds: true }) };
}
