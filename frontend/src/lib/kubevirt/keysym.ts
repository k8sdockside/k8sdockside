// A browser key, as the X11 keysym VNC carries.
//
// KeyboardEvent.key is what the key means on the user's layout -- "a", "A",
// "F3", "ArrowUp" -- and that is what a keysym is too, which is why it is used
// rather than `code`: typing on a Norwegian keyboard puts Norwegian letters on
// the guest's screen, as long as the guest's layout agrees.

const NAMED: Record<string, number> = {
    Backspace: 0xff08,
    Tab: 0xff09,
    Enter: 0xff0d,
    Pause: 0xff13,
    ScrollLock: 0xff14,
    Escape: 0xff1b,
    Delete: 0xffff,
    Home: 0xff50,
    ArrowLeft: 0xff51,
    ArrowUp: 0xff52,
    ArrowRight: 0xff53,
    ArrowDown: 0xff54,
    PageUp: 0xff55,
    PageDown: 0xff56,
    End: 0xff57,
    Insert: 0xff63,
    ContextMenu: 0xff67,
    NumLock: 0xff7f,
    Shift: 0xffe1,
    Control: 0xffe3,
    CapsLock: 0xffe5,
    Meta: 0xffe7,
    Alt: 0xffe9,
    AltGraph: 0xfe03,
    OS: 0xffeb,
};

/** The keysym for a KeyboardEvent.key, or null for one there is none for ("Dead", "Unidentified"). */
export function keysymFor(key: string): number | null {
    if (key in NAMED) return NAMED[key]!;
    const fn = /^F([1-9]|1[0-9]|2[0-4])$/.exec(key);
    if (fn) return 0xffbe + Number(fn[1]) - 1;
    // One character: Latin-1 is its own keysym; everything else is the
    // Unicode code point with 0x01000000 added.
    const chars = [...key];
    if (chars.length === 1) {
        const cp = chars[0]!.codePointAt(0)!;
        if (cp < 0x20) return null;
        return cp < 0x100 ? cp : 0x01000000 + cp;
    }
    return null;
}

/** A key press that is not a key on a keyboard here: what the strip of buttons sends. */
export interface Chord {
    label: string;
    title: string;
    /** Held in order, then let go in reverse. */
    keys: string[];
}

export const CHORDS: Chord[] = [
    { label: 'Esc', title: 'Escape', keys: ['Escape'] },
    { label: 'Tab', title: 'Tab', keys: ['Tab'] },
    { label: '↑', title: 'Arrow up', keys: ['ArrowUp'] },
    { label: '↓', title: 'Arrow down', keys: ['ArrowDown'] },
    { label: '←', title: 'Arrow left', keys: ['ArrowLeft'] },
    { label: '→', title: 'Arrow right', keys: ['ArrowRight'] },
    ...Array.from({ length: 12 }, (_, i) => ({ label: `F${i + 1}`, title: `F${i + 1}`, keys: [`F${i + 1}`] })),
    { label: 'Ctrl+Alt+Del', title: 'Ctrl+Alt+Delete', keys: ['Control', 'Alt', 'Delete'] },
];

/** The down and up events of a chord, as the backend takes them. */
export function chordKeys(chord: Chord): { keysym: number; down: boolean }[] {
    const syms = chord.keys.map(keysymFor).filter((k): k is number => k !== null);
    return [...syms.map((keysym) => ({ keysym, down: true })), ...syms.reverse().map((keysym) => ({ keysym, down: false }))];
}
