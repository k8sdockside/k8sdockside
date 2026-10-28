import { beforeEach, expect, test } from 'vitest';

// The plugin SDK's ui.section, as a plugin's frame runs it: the real script,
// loaded into a stand-in window whose parent is this test. It has to fold the
// way the app's own sections do and keep its folds where the app keeps them.

const PROTOCOL = 'k8sdockside/plugin@1';

async function sdkSource(): Promise<string> {
    const fs = (await import('node:' + 'fs')) as { readFileSync(path: string, encoding: string): string };
    const cwd = (globalThis as unknown as { process: { cwd(): string } }).process.cwd();
    return fs.readFileSync(`${cwd}/../internal/plugins/sdk/k8sdockside.js`, 'utf8');
}

interface Sent {
    id: number;
    method: string;
    params: Record<string, unknown>;
}

interface Section {
    root: HTMLElement;
    body: HTMLElement;
    isFolded(): boolean;
    setCount(c: number | string): void;
}

async function loadSdk(folded: string[]) {
    const sent: Sent[] = [];
    let handler: ((e: { data: unknown; source?: unknown }) => void) | null = null;
    const parent = {
        postMessage(msg: Sent) {
            sent.push(msg);
            // Answer every request at once; hello with the folds the app keeps.
            queueMicrotask(() =>
                handler!({
                    data: { protocol: PROTOCOL, id: msg.id, result: msg.method === 'hello' ? { folded } : null },
                    source: parent,
                }),
            );
        },
    };
    const fakeWindow = {
        parent,
        addEventListener(type: string, fn: (e: { data: unknown }) => void) {
            if (type === 'message') handler = fn;
        },
    } as unknown as Record<string, unknown>;
    new Function('window', 'document', 'ResizeObserver', await sdkSource())(fakeWindow, document, undefined);
    const sdk = fakeWindow.k8sdockside as {
        ready(): Promise<unknown>;
        ui: { section(o: Record<string, unknown>): Section };
    };
    await sdk.ready();
    return { sdk, sent };
}

beforeEach(() => {
    document.head.innerHTML = '';
    document.body.innerHTML = '';
});

test('a section draws its heading and folds when the heading is clicked', async () => {
    const { sdk, sent } = await loadSdk([]);
    const sec = sdk.ui.section({ id: 'disks', title: 'Disks', count: 2, parent: document.body });

    expect(sec.root.textContent).toContain('Disks');
    expect(sec.root.textContent).toContain('2');
    expect(sec.isFolded()).toBe(false);

    sec.root.querySelector('button')!.click();

    expect(sec.isFolded()).toBe(true);
    expect(sec.root.classList.contains('k8sd-folded')).toBe(true);
    expect(sent.find((m) => m.method === 'section.fold')?.params).toEqual({ id: 'disks', folded: true });
});

test('a section the user folded before starts folded, and the styles are added once', async () => {
    const { sdk } = await loadSdk(['disks']);

    expect(sdk.ui.section({ id: 'disks', title: 'Disks' }).isFolded()).toBe(true);
    expect(sdk.ui.section({ id: 'nics', title: 'Interfaces' }).isFolded()).toBe(false);
    expect(document.querySelectorAll('#k8sd-section-css')).toHaveLength(1);
});

// A section that starts folded is remembered when it is opened, so "never
// touched" and "opened" are not the same entry.
test('open: false starts folded until the user opens it', async () => {
    const { sdk, sent } = await loadSdk([]);
    const sec = sdk.ui.section({ id: 'events', title: 'Events', open: false });
    expect(sec.isFolded()).toBe(true);

    sec.root.querySelector('button')!.click();

    expect(sec.isFolded()).toBe(false);
    expect(sent.find((m) => m.method === 'section.fold')?.params).toEqual({ id: 'events#open', folded: true });
    // Drawn again on the next poll, it stays as the user left it.
    expect(sdk.ui.section({ id: 'events', title: 'Events', open: false }).isFolded()).toBe(false);
});
