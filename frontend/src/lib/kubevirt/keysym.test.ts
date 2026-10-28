import { expect, test } from 'vitest';
import { CHORDS, chordKeys, keysymFor } from './keysym';

test('named keys, F-keys and characters become X11 keysyms', () => {
    expect(keysymFor('ArrowUp')).toBe(0xff52);
    expect(keysymFor('ArrowDown')).toBe(0xff54);
    expect(keysymFor('Enter')).toBe(0xff0d);
    expect(keysymFor('F1')).toBe(0xffbe);
    expect(keysymFor('F3')).toBe(0xffc0);
    expect(keysymFor('F12')).toBe(0xffc9);
    expect(keysymFor('a')).toBe(0x61);
    expect(keysymFor('A')).toBe(0x41);
    // Latin-1 is its own keysym; beyond it, the code point plus 0x01000000.
    expect(keysymFor('ø')).toBe(0xf8);
    expect(keysymFor('€')).toBe(0x01000000 + 0x20ac);
});

test('a key with no keysym is none', () => {
    expect(keysymFor('Dead')).toBeNull();
    expect(keysymFor('Unidentified')).toBeNull();
});

test('a chord is held in order and let go in reverse', () => {
    const cad = CHORDS.find((c) => c.label === 'Ctrl+Alt+Del')!;
    expect(chordKeys(cad)).toEqual([
        { keysym: 0xffe3, down: true },
        { keysym: 0xffe9, down: true },
        { keysym: 0xffff, down: true },
        { keysym: 0xffff, down: false },
        { keysym: 0xffe9, down: false },
        { keysym: 0xffe3, down: false },
    ]);
});
