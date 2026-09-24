import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import SortableTable from './SortableTable.svelte';
import { MIN_COLUMN_WIDTH } from '../columns';
import type { Row } from '../state/adopt';

function row(id: string, ...cells: ([string] | [string, string])[]): Row {
    return {
        id,
        name: id,
        namespace: '',
        cells: cells.map(([text, sort]) => ({ text, tone: '', sort: sort ?? '', pills: null })),
    };
}

/** The rendered text of one column, top to bottom. */
function columnText(index: number): string[] {
    return [...document.querySelectorAll('tbody tr')].map(
        (tr) => tr.children[index]?.textContent?.trim() ?? '',
    );
}

const AGES = ['Name', 'Last Seen'];

// Ages are the case the sort key exists for: as text, "2h" < "3d" < "5m".
const byAge = [
    row('a', ['a'], ['5m', '300']),
    row('b', ['b'], ['2h', '7200']),
    row('c', ['c'], ['3d', '259200']),
];

test('rows keep the order they were given until a header is clicked', async () => {
    render(SortableTable, { columns: AGES, rows: byAge });

    await expect.element(page.getByRole('row').nth(1)).toBeVisible();
    expect(columnText(1)).toEqual(['5m', '2h', '3d']);
});

test('clicking a header sorts by the cell sort key, not its text', async () => {
    render(SortableTable, { columns: AGES, rows: [byAge[1], byAge[2], byAge[0]] });

    await page.getByRole('button', { name: 'Last Seen' }).click();

    // Text order would have given 2h, 3d, 5m.
    await expect.poll(() => columnText(1)).toEqual(['5m', '2h', '3d']);
});

test('clicking the same header again reverses it', async () => {
    render(SortableTable, { columns: AGES, rows: byAge });

    await page.getByRole('button', { name: 'Last Seen' }).click();
    await page.getByRole('button', { name: 'Last Seen' }).click();

    await expect.poll(() => columnText(1)).toEqual(['3d', '2h', '5m']);
});

test('a column without sort keys falls back to its text', async () => {
    render(SortableTable, {
        columns: AGES,
        rows: [row('c', ['charlie'], ['1m', '60']), row('a', ['alpha'], ['2m', '120'])],
    });

    await page.getByRole('button', { name: 'Name' }).click();

    await expect.poll(() => columnText(0)).toEqual(['alpha', 'charlie']);
});

test('the sorted column is announced to assistive tech', async () => {
    render(SortableTable, { columns: AGES, rows: byAge });

    await page.getByRole('button', { name: 'Name' }).click();

    await expect.poll(() => document.querySelectorAll('th[aria-sort="ascending"]').length).toBe(1);
});

test('an empty table says so in the caller\'s words', async () => {
    render(SortableTable, { columns: AGES, rows: [], empty: 'Nothing to report.' });

    await expect.element(page.getByText('Nothing to report.')).toBeVisible();
});

test('clicking a row reports it to the caller', async () => {
    let picked: Row | null = null;
    render(SortableTable, { columns: AGES, rows: byAge, onselect: (r: Row) => (picked = r) });

    await page.getByRole('cell', { name: 'b' }).click();

    expect(picked).not.toBeNull();
    expect(picked!.id).toBe('b');
});

// The sort is bindable so a caller can hand back the one the user left: a
// resource tab is rebuilt every time it is brought forward.
test('a sort handed in by the caller is applied from the start', async () => {
    render(SortableTable, { columns: AGES, rows: [byAge[1], byAge[2], byAge[0]], sortColumn: 1, sortDescending: true });

    await expect.poll(() => columnText(1)).toEqual(['3d', '2h', '5m']);
    expect(document.querySelector('th[aria-sort="descending"]')?.textContent).toContain('Last Seen');
});

// ----- hidden columns -------------------------------------------------------

const POD = ['Name', 'Ready', 'Node', 'Age'];
const pods = [
    row('a', ['api'], ['1/1'], ['node-1'], ['5m', '300']),
    row('b', ['web'], ['2/2'], ['node-2'], ['2h', '7200']),
];

/** The headings on screen, left to right. */
function headings(): string[] {
    return [...document.querySelectorAll('thead th')].map((th) => th.textContent?.trim() ?? '');
}

test('a hidden column is not drawn, in the head or the body', async () => {
    render(SortableTable, { columns: POD, rows: pods, hidden: ['Node'] });

    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();
    expect(headings()).toEqual(['Name', 'Ready', 'Age']);
    expect(columnText(2)).toEqual(['5m', '2h']);
});

// The cells arrive in the backend's order, so a column that is still shown has
// to remember which cell is its own rather than counting from the left.
test('hiding a column does not shift the cells of the ones after it', async () => {
    render(SortableTable, { columns: POD, rows: pods, hidden: ['Name', 'Ready'] });

    await expect.element(page.getByRole('button', { name: 'Node' })).toBeVisible();
    expect(columnText(0)).toEqual(['node-1', 'node-2']);
    expect(columnText(1)).toEqual(['5m', '2h']);
});

test('sorting a still-shown column sorts by its own cells', async () => {
    render(SortableTable, { columns: POD, rows: [pods[1], pods[0]], hidden: ['Name', 'Ready'] });

    await page.getByRole('button', { name: 'Age' }).click();

    await expect.poll(() => columnText(1)).toEqual(['5m', '2h']);
});

// Only reachable by hand-editing the settings file; the picker refuses the last
// tick. A table of nothing is a stack of blank rows with no way back.
test('the last column stands even when the settings say to hide it', async () => {
    render(SortableTable, { columns: ['Name'], rows: pods, hidden: ['Name'] });

    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();
    expect(headings()).toEqual(['Name']);
});

// ----- resizing -------------------------------------------------------------

/** The grip on one column's trailing edge. */
function grip(name: string): HTMLElement {
    return document.querySelector(`[aria-label="Resize ${name}"]`) as HTMLElement;
}

test('a width the caller gave pins the column, head and cells alike', async () => {
    render(SortableTable, { columns: POD, rows: pods, widths: { Name: 300 } });

    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();
    const th = document.querySelector('thead th') as HTMLElement;
    const td = document.querySelector('tbody td') as HTMLElement;
    // All three, because width alone is a suggestion in an auto-layout table.
    expect(th.style.width).toBe('300px');
    expect(th.style.minWidth).toBe('300px');
    expect(th.style.maxWidth).toBe('300px');
    expect(td.style.width).toBe('300px');
});

test('a column with no width set is left to size itself', async () => {
    render(SortableTable, { columns: POD, rows: pods, widths: { Name: 300 } });

    await expect.element(page.getByRole('button', { name: 'Ready' })).toBeVisible();
    expect((document.querySelectorAll('thead th')[1] as HTMLElement).style.width).toBe('');
});

// The grips only appear for a caller that can do something with the answer;
// the dashboard's events panel has nowhere to remember a width.
test('there are no grips when the caller cannot remember a width', async () => {
    render(SortableTable, { columns: POD, rows: pods });

    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();
    expect(document.querySelectorAll('[role="separator"]').length).toBe(0);
});

test('dragging an edge reports the width to the caller as it moves', async () => {
    const seen: [string, number][] = [];
    render(SortableTable, {
        columns: POD,
        rows: pods,
        onresize: (column: string, px: number) => seen.push([column, px]),
    });
    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();

    const handle = grip('Name');
    const from = handle.getBoundingClientRect();
    handle.setPointerCapture = () => {};
    handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: from.x, pointerId: 1 }));
    handle.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: from.x + 60, pointerId: 1 }));
    handle.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerId: 1 }));

    expect(seen.length).toBeGreaterThan(0);
    expect(seen[0][0]).toBe('Name');
    // The starting width is measured off the header, so the drag widens what
    // was there rather than jumping to a default.
    const th = document.querySelector('thead th') as HTMLElement;
    expect(seen[0][1]).toBeCloseTo(th.getBoundingClientRect().width + 60, -1);
});

test('a pointer move with no drag under way reports nothing', async () => {
    const seen: string[] = [];
    render(SortableTable, { columns: POD, rows: pods, onresize: (c: string) => seen.push(c) });
    await expect.element(page.getByRole('button', { name: 'Name' })).toBeVisible();

    grip('Name').dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: 400 }));

    expect(seen).toEqual([]);
});

test('a double click on an edge gives the column back to its contents', async () => {
    const cleared: string[] = [];
    render(SortableTable, {
        columns: POD,
        rows: pods,
        widths: { Node: 300 },
        onresize: () => {},
        onresizeend: (column: string) => cleared.push(column),
    });
    await expect.element(page.getByRole('button', { name: 'Node' })).toBeVisible();

    grip('Node').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));

    expect(cleared).toEqual(['Node']);
});

// A column is not something only a mouse can change.
test('the arrow keys step a column, and Home gives it back', async () => {
    const seen: [string, number][] = [];
    const cleared: string[] = [];
    render(SortableTable, {
        columns: POD,
        rows: pods,
        widths: { Age: 120 },
        onresize: (column: string, px: number) => seen.push([column, px]),
        onresizeend: (column: string) => cleared.push(column),
    });
    await expect.element(page.getByRole('button', { name: 'Age' })).toBeVisible();

    const handle = grip('Age');
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', shiftKey: true, bubbles: true }));
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }));

    expect(seen).toEqual([
        ['Age', 128],
        ['Age', 80],
    ]);
    expect(cleared).toEqual(['Age']);
});

test('a width is held inside the range a column can be found in', async () => {
    const seen: number[] = [];
    render(SortableTable, {
        columns: POD,
        rows: pods,
        widths: { Age: MIN_COLUMN_WIDTH },
        onresize: (_: string, px: number) => seen.push(px),
    });
    await expect.element(page.getByRole('button', { name: 'Age' })).toBeVisible();

    grip('Age').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', shiftKey: true, bubbles: true }));

    expect(seen).toEqual([MIN_COLUMN_WIDTH]);
});

// A long list draws only the rows in view: a row costs about 0.15ms, and a
// namespace of five thousand pods froze the window for most of a second when
// it opened. The scrollbar is still as long as the whole list, and scrolling
// brings the right rows in.
test('a long list draws only what is in view, and scrolling brings the rest', async () => {
    const box = document.createElement('div');
    box.style.cssText = 'height: 400px; overflow: auto;';
    document.body.appendChild(box);
    const long = Array.from({ length: 3000 }, (_, i) => ({
        id: `r${i}`,
        name: `pod-${i}`,
        namespace: 'default',
        cells: [{ text: `pod-${i}`, sort: '', tone: '', pills: [] }, { text: 'Running', sort: '', tone: '', pills: [] }],
    }));
    await render(SortableTable, { props: { columns: ['Name', 'Status'], rows: long }, target: box });
    const drawn = () => [...box.querySelectorAll('tbody tr[data-row]')].map((r) => r.querySelector('td')?.textContent ?? '');
    const frame = () => new Promise((r) => requestAnimationFrame(() => setTimeout(r, 0)));
    await frame();

    expect(drawn().length).toBeLessThan(200);
    expect(drawn()[0]).toBe('pod-0');
    // Every row is accounted for in the height, drawn or not.
    const rowHeight = box.querySelector<HTMLElement>('tbody tr[data-row]')!.offsetHeight;
    expect(box.scrollHeight).toBeGreaterThan(rowHeight * 2900);

    box.scrollTop = rowHeight * 1500;
    await frame();
    await frame();
    const now = drawn();
    expect(now).toContain('pod-1500');
    expect(now).not.toContain('pod-0');
    box.remove();
});
