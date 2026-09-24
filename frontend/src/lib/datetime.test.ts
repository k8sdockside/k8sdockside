import { afterEach, describe, expect, test, vi } from 'vitest';
import {
    DEFAULT_DATETIME,
    formatAge,
    formatDate,
    formatDateTime,
    formatDay,
    formatTime,
    setDateTimeSettings,
    timeCell,
    type DateTimeSettings,
} from './datetime.svelte';

// 14:05:09 UTC: an afternoon, so 12- and 24-hour clocks differ, on a day that
// is the 24th wherever the tests run.
const AT = '2026-09-24T14:05:09Z';

afterEach(() => setDateTimeSettings(DEFAULT_DATETIME));

describe('dates', () => {
    test.each([
        ['iso', '2026-09-24', '09-24'],
        ['dmy', '24.09.2026', '24.09'],
        ['mdy', '09/24/2026', '09/24'],
    ] as const)('%s', (dates, full, day) => {
        setDateTimeSettings({ dates, zone: 'utc' });
        expect(formatDate(AT)).toBe(full);
        expect(formatDay(AT)).toBe(day);
    });

    test('in words names the month', () => {
        setDateTimeSettings({ dates: 'long', zone: 'utc' });
        expect(formatDate(AT)).toMatch(/Sep/);
        expect(formatDate(AT)).toMatch(/2026/);
    });

    test('something that is not a date is nothing, not "Invalid Date"', () => {
        expect(formatDate('')).toBe('');
        expect(formatDateTime('soon')).toBe('');
    });
});

describe('times', () => {
    test('a 24-hour clock', () => {
        setDateTimeSettings({ clock: '24h', zone: 'utc' });
        expect(formatTime(AT)).toMatch(/^14[:.]05$/);
        expect(formatTime(AT, { seconds: true })).toMatch(/^14[:.]05[:.]09$/);
    });

    test('a 12-hour clock', () => {
        setDateTimeSettings({ clock: '12h', zone: 'utc' });
        expect(formatTime(AT)).toMatch(/^0?2[:.]05/);
        expect(formatTime(AT)).toMatch(/PM|pm|p\.m\./i);
    });

    // A time shown with its date in UTC says so, since everything else on
    // the machine is in local time.
    test('UTC is marked', () => {
        setDateTimeSettings({ clock: '24h', dates: 'iso', zone: 'utc' });
        expect(formatDateTime(AT)).toMatch(/^2026-09-24 14[:.]05 UTC$/);
    });
});

describe('table time cells', () => {
    test('read as the age by default, with the moment on hover', () => {
        setDateTimeSettings({ dates: 'iso', clock: '24h', zone: 'utc' });
        expect(timeCell('5m', AT)).toEqual({ text: expect.stringMatching(/^\d+[smhd]/), title: expect.stringMatching(/^2026-09-24 14[:.]05[:.]09 UTC$/) });
    });

    test('read as the moment when asked, with the age on hover', () => {
        setDateTimeSettings({ dates: 'iso', clock: '24h', zone: 'utc', ages: 'absolute' });
        expect(timeCell('5m', AT)).toEqual({ text: expect.stringMatching(/^2026-09-24 14[:.]05 UTC$/), title: expect.stringMatching(/ ago$/) });
    });

    test('a cell with no moment is left as it is', () => {
        expect(timeCell('<none>', '')).toEqual({ text: '<none>', title: '' });
    });

    // Seconds for the first ten minutes, as kubectl counts them; the
    // backend's steps after that.
    test.each([
        [45, '45s'],
        [125, '2m5s'],
        [300, '5m'],
        [599, '9m59s'],
        [15 * 60, '15m'],
        [3 * 3600 + 20 * 60, '3h20m'],
        [12 * 3600 + 20 * 60, '12h'],
        [3 * 86400 + 4 * 3600, '3d4h'],
        [12 * 86400, '12d'],
    ])('%is ago reads as %s', (seconds, text) => {
        const now = Date.parse(AT);
        expect(formatAge(now - seconds * 1000, now)).toBe(text);
    });

    // The backend writes a cell's text when the list changes; a quiet list
    // would otherwise say "0s" about a new pod for as long as nothing else
    // happened.
    test('a young cell counts up on its own', () => {
        vi.useFakeTimers({ now: Date.parse(AT) });
        try {
            const created = new Date(Date.parse(AT) - 5_000).toISOString();
            expect(timeCell('0s', created).text).toBe('5s');
            vi.advanceTimersByTime(7_000);
            expect(timeCell('0s', created).text).toBe('12s');
        } finally {
            vi.useRealTimers();
        }
    });
});

// The plugin SDK writes dates with a copy of these rules, in plain script, so
// a plugin's page reads like the app beside it. This keeps the copy honest.
// The SDK as the text a plugin's frame loads. Read from disk rather than
// imported: it lives outside the frontend, where Vite will not serve from, and
// the frontend has no Node types -- hence the untyped dynamic import.
async function sdkSource(): Promise<string> {
    const fs = (await import('node:' + 'fs')) as { readFileSync(path: string, encoding: string): string };
    const cwd = (globalThis as unknown as { process: { cwd(): string } }).process.cwd();
    return fs.readFileSync(`${cwd}/../internal/plugins/sdk/k8sdockside.js`, 'utf8');
}

describe('the plugin SDK writes dates the way the app does', () => {
    test.each([
        { clock: '24h', dates: 'iso', zone: 'utc', ages: 'relative' },
        { clock: '12h', dates: 'mdy', zone: 'utc', ages: 'absolute' },
        { clock: 'system', dates: 'dmy', zone: 'utc', ages: 'relative' },
    ] as DateTimeSettings[])('%o', async (settings) => {
        setDateTimeSettings(settings);
        const source = await sdkSource();
        // The helpers take their settings from the app's messages; feed them
        // one by calling the handler the SDK registers.
        let handler: ((e: { data: unknown; source?: unknown }) => void) | null = null;
        const parent = { postMessage() {} };
        const fakeWindow = {
            parent,
            addEventListener(type: string, fn: (e: { data: unknown }) => void) {
                if (type === 'message') handler = fn;
            },
        } as unknown as Record<string, unknown>;
        new Function('window', 'document', 'ResizeObserver', source)(fakeWindow, document, undefined);
        const sdk = fakeWindow.k8sdockside as { format: Record<string, (w: string, o?: unknown) => string> };
        handler!({ data: { protocol: 'k8sdockside/plugin@1', event: 'datetime', data: settings }, source: parent });

        expect(sdk.format.date(AT)).toBe(formatDate(AT));
        expect(sdk.format.day(AT)).toBe(formatDay(AT));
        expect(sdk.format.time(AT)).toBe(formatTime(AT));
        expect(sdk.format.dateTime(AT, { seconds: true })).toBe(formatDateTime(AT, { seconds: true }));
        const now = Date.parse(AT);
        for (const seconds of [45, 125, 900, 12_000, 300_000, 1_100_000]) {
            const at = new Date(now - seconds * 1000).toISOString();
            expect((sdk.format.age as unknown as (w: string, n: number) => string)(at, now)).toBe(formatAge(at, now));
        }
    });
});
