import { describe, expect, test, vi } from 'vitest';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    TerminalService: {
        DebugPresets: vi.fn().mockResolvedValue([{ image: 'busybox', label: 'BusyBox', hint: 'tiny' }]),
    },
}));

const { blankDebugRequest, debugImageChoices, debugPresets, DEBUG_PROFILES, splitCommand, splitList } = await import(
    './debug.svelte'
);

describe('a blank request', () => {
    // kubectl's own defaults, so that a launch that changes nothing does what
    // `kubectl debug -it` would.
    test('holds kubectl debug defaults', () => {
        const request = blankDebugRequest('copy', 'shop', 'web');
        expect(request.copyTo).toBe('web-debug');
        expect(request.interactive).toBe(true);
        expect(request.shareProcesses).toBe(true);
        expect(request.keepInitContainers).toBe(true);
        expect(request.keepLabels).toBe(false);
        expect(request.keepLiveness).toBe(false);
        expect(request.replace).toBe(false);
    });

    test('names no copy outside copy mode', () => {
        expect(blankDebugRequest('ephemeral', 'shop', 'web').copyTo).toBe('');
    });
});

describe('splitting what was typed', () => {
    test('a command splits on spaces, with quotes grouping', () => {
        expect(splitCommand(`sh -c 'echo hello world'`)).toEqual(['sh', '-c', 'echo hello world']);
        expect(splitCommand('  ')).toEqual([]);
        expect(splitCommand(`echo ""`)).toEqual(['echo', '']);
    });

    test('a list splits on commas and lines, not on spaces', () => {
        expect(splitList('A=1, B=two words\nC=3,,')).toEqual(['A=1', 'B=two words', 'C=3']);
    });
});

describe('the image picker', () => {
    test('offers the presets, then the added images, then a default in neither', () => {
        const presets = [{ image: 'busybox', label: 'BusyBox', hint: '' }];
        const got = debugImageChoices(presets, ['ghcr.io/a', 'busybox'], 'ghcr.io/elsewhere');
        expect(got.map((c) => c.image)).toEqual(['busybox', 'ghcr.io/a', 'ghcr.io/elsewhere']);
    });

    test('reads the presets from the backend once', async () => {
        await debugPresets.load();
        await debugPresets.load();
        expect(debugPresets.images()).toEqual(['busybox']);
    });
});

test('general is the default profile, and legacy is marked deprecated', () => {
    expect(DEBUG_PROFILES[0].value).toBe('general');
    expect(DEBUG_PROFILES.find((p) => p.value === 'legacy')?.label).toMatch(/deprecated/);
});
