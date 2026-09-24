<!--
  A sortable listing of rows in the shape the backend produces for every kind.

  It is shared by the resource tabs and the dashboard's events panel so there is
  one sorting implementation rather than two that can disagree: sorting by a
  cell's key rather than its text is the sort of detail that gets fixed in one
  copy and not the other.

  Sorting only: filtering, loading and error states belong to the caller, which
  knows what it is listing and what an empty one means.
-->
<script lang="ts">
    import { timeCell } from '../datetime.svelte';
    import type { Row } from '../state/adopt';
    import type { Snippet } from 'svelte';
    import { clampColumnWidth, MAX_COLUMN_WIDTH, MIN_COLUMN_WIDTH, visibleColumns } from '../columns';
    import Icon from './Icon.svelte';

    interface Props {
        columns: string[];
        rows: Row[];
        /**
         * The width in px the user dragged each column to, by column key. A
         * column not in here sizes itself to its contents, which is what every
         * table does until somebody drags an edge.
         */
        widths?: Record<string, number>;
        /** The columns turned off, by column key. See ../columns.ts. */
        hidden?: string[];
        /**
         * Called as an edge is dragged, with the width to remember. On every
         * pointer move rather than on release, so the rows resize under the
         * pointer; the caller's write is debounced.
         */
        onresize?: (column: string, px: number) => void;
        /** Called on a double click on an edge: the column sizes itself again. */
        onresizeend?: (column: string) => void;
        /** Row to mark as selected, if the caller tracks one. */
        selectedRowId?: string | null;
        /** Called when a row is clicked. */
        onselect?: (row: Row) => void;
        /** What to show in place of rows when there are none. */
        empty?: string;
        /** Overrides how one cell renders, for callers with a link in a column. */
        cell?: Snippet<[Row, number]>;
        /**
         * The rows that are ticked, by id, when the caller offers a selection.
         * The checkbox column is drawn only when this is given: a selection
         * with nothing to do to it would be clutter.
         */
        picked?: Set<string> | null;
        /**
         * Called with the rows to tick or untick. Shift on a checkbox sweeps
         * from the last row ticked to this one in the order on screen -- which
         * only this component knows, since it is the one sorting.
         */
        onpick?: (rows: Row[], on: boolean) => void;
        /**
         * The column the rows are sorted by, and which way. Bindable, so a
         * caller can keep a sort across the table's lifetime: a resource tab
         * is destroyed when another is brought forward and built again on the
         * way back, and a sort that reset each time made "the newest pods"
         * something to ask for again at every switch.
         *
         * Null means "the order the caller gave", which the backend has
         * already put in each kind's natural order -- events most recent
         * first, everything else by namespace and name. Sorting before the
         * user asks would undo that.
         */
        sortColumn?: number | null;
        sortDescending?: boolean;
    }

    let {
        columns,
        rows,
        widths = {},
        hidden = [],
        onresize,
        onresizeend,
        selectedRowId = null,
        onselect,
        empty = 'Nothing here.',
        cell,
        picked = null,
        onpick,
        sortColumn = $bindable(null),
        sortDescending = $bindable(false),
    }: Props = $props();

    /** The row last ticked on its own, which is where a shift-click sweeps from. */
    let anchor: string | null = null;

    function pick(row: Row, at: number, event: MouseEvent): void {
        if (!picked) return;
        const on = !picked.has(row.id);
        if (event.shiftKey && anchor !== null) {
            const from = sorted.findIndex((r) => r.id === anchor);
            if (from !== -1) {
                const [a, b] = from < at ? [from, at] : [at, from];
                onpick?.(sorted.slice(a, b + 1), on);
                return;
            }
        }
        anchor = row.id;
        onpick?.([row], on);
    }

    /**
     * A click on the row opens it. With ⌘ or Ctrl held it ticks it instead,
     * and with Shift it sweeps: the gestures every file list has taught, so
     * the checkboxes are the visible way and not the only one.
     */
    function press(row: Row, at: number, event: MouseEvent): void {
        if (picked && (event.metaKey || event.ctrlKey || event.shiftKey)) {
            pick(row, at, event);
            return;
        }
        onselect?.(row);
    }

    let allPicked = $derived.by(() => {
        const set = picked;
        return !!set && sorted.length > 0 && sorted.every((r) => set.has(r.id));
    });
    let somePicked = $derived.by(() => {
        const set = picked;
        return !!set && !allPicked && sorted.some((r) => set.has(r.id));
    });

    /** The header checkbox: everything on screen, or nothing. */
    function pickAll(): void {
        onpick?.(sorted, !allPicked);
    }

    /** Puts a checkbox in its third state, which is a property and not an attribute. */
    function indeterminate(node: HTMLInputElement, value: boolean) {
        node.indeterminate = value;
        return {
            update(next: boolean) {
                node.indeterminate = next;
            },
        };
    }

    /**
     * What a cell sorts by: its sort key where it has one, its text otherwise.
     * An age reads "3d" and sorts by seconds; a volume reads "500Mi" and sorts
     * by bytes. Comparing the text would order them as words.
     */
    function sortKey(value: { text: string; sort: string } | undefined): string {
        return value?.sort || value?.text || '';
    }

    let sorted = $derived.by(() => {
        if (sortColumn === null) return rows;

        const column = Math.min(sortColumn, Math.max(0, (columns.length || 1) - 1));
        const out = [...rows].sort((a, b) =>
            sortKey(a.cells[column]).localeCompare(sortKey(b.cells[column]), undefined, {
                numeric: true,
                sensitivity: 'base',
            }),
        );
        return sortDescending ? out.reverse() : out;
    });

    function sortBy(index: number): void {
        if (sortColumn === index) {
            sortDescending = !sortDescending;
        } else {
            sortColumn = index;
            sortDescending = false;
        }
    }

    // ----- widths ---------------------------------------------------------

    /**
     * The columns actually drawn, each still carrying where its cell sits in
     * the row the backend sent. Hiding a column must not shift the cells of the
     * ones after it, and the sort is by that index too.
     */
    let shown = $derived(visibleColumns(columns, hidden));

    /**
     * A pinned column's width, as three properties rather than one.
     *
     * `width` alone is a suggestion in an auto-layout table: the browser still
     * measures the content and widens the column past it if the text is longer.
     * The other two are what actually hold the column at what it was dragged
     * to, with the cell's own overflow rules doing the ellipsis.
     *
     * Columns the user has not touched get nothing at all, so the table sizes
     * itself exactly as it always did.
     */
    function sized(key: string): string {
        const px = widths[key];
        return px ? `width:${px}px;min-width:${px}px;max-width:${px}px` : '';
    }

    /** The drag in progress, if any: which column, and where it started. */
    let drag = $state<{ key: string; from: number; width: number } | null>(null);

    /**
     * Starts a drag on one column's trailing edge.
     *
     * The starting width is measured off the header rather than read from
     * `widths`, because a column the user has not touched has no width there
     * yet -- and the whole point of the first drag is to pin it to what it is
     * showing now rather than jumping to some default.
     *
     * The pointer is captured so the drag survives leaving the grip, which it
     * does at once: the pointer is outrunning the column it is widening.
     */
    function grab(event: PointerEvent, key: string): void {
        const th = (event.currentTarget as HTMLElement).closest('th');
        if (!th) return;
        event.preventDefault();
        event.stopPropagation();
        drag = { key, from: event.clientX, width: th.getBoundingClientRect().width };
        (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    }

    function move(event: PointerEvent): void {
        if (!drag) return;
        onresize?.(drag.key, clampColumnWidth(drag.width + (event.clientX - drag.from)));
    }

    function drop(): void {
        drag = null;
    }

    /**
     * Keyboard resizing, so a column is not something only a mouse can change.
     * The arrows step, and Home gives the column back to its contents.
     */
    function nudge(event: KeyboardEvent, key: string): void {
        const step = event.shiftKey ? 40 : 8;
        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
            const th = (event.currentTarget as HTMLElement).closest('th');
            if (!th) return;
            event.preventDefault();
            const from = widths[key] ?? th.getBoundingClientRect().width;
            onresize?.(key, clampColumnWidth(from + (event.key === 'ArrowRight' ? step : -step)));
        } else if (event.key === 'Home') {
            event.preventDefault();
            onresizeend?.(key);
        }
    }
</script>

<table>
    <thead>
        <tr>
            {#if picked}
                <th class="pick">
                    <input
                        type="checkbox"
                        checked={allPicked}
                        use:indeterminate={somePicked}
                        onchange={pickAll}
                        aria-label="Select all"
                        title={allPicked ? 'Clear the selection' : 'Select every row shown'}
                    />
                </th>
            {/if}
            <!-- Unkeyed, by position: a CRD may declare a printer column
                 called "Name" beside the Name the app puts first, and a keyed
                 block throws on the repeat where an unkeyed one renders it.
                 The settings key that tells those two apart is column.key. -->
            {#each shown as column (column.index)}
                <th
                    class:sorted={sortColumn === column.index}
                    class:pinned={!!widths[column.key]}
                    style={sized(column.key)}
                    aria-sort={sortColumn === column.index
                        ? sortDescending
                            ? 'descending'
                            : 'ascending'
                        : 'none'}
                >
                    <button onclick={() => sortBy(column.index)}>
                        <span class="name">{column.name}</span>
                        {#if sortColumn === column.index}
                            <Icon name={sortDescending ? 'chevron-down' : 'chevron-right'} size={11} />
                        {/if}
                    </button>
                    {#if onresize}
                        <!-- The column's trailing edge, drawn as the ARIA
                             window splitter a pane divider is: a focusable
                             separator, which the a11y rules below read as
                             static because they only key off the role. -->
                        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                        <div
                            class="grip"
                            class:dragging={drag?.key === column.key}
                            role="separator"
                            aria-orientation="vertical"
                            aria-label="Resize {column.name}"
                            aria-valuenow={widths[column.key]}
                            aria-valuemin={MIN_COLUMN_WIDTH}
                            aria-valuemax={MAX_COLUMN_WIDTH}
                            tabindex="0"
                            onpointerdown={(event) => grab(event, column.key)}
                            onpointermove={move}
                            onpointerup={drop}
                            onpointercancel={drop}
                            onkeydown={(event) => nudge(event, column.key)}
                            ondblclick={() => onresizeend?.(column.key)}
                            title="Drag to resize {column.name}. Double click to fit its contents."
                        ></div>
                    {/if}
                </th>
            {/each}
        </tr>
    </thead>
    <tbody>
        {#each sorted as row, at (row.id)}
            <tr
                class:selected={selectedRowId === row.id}
                class:picked={picked?.has(row.id)}
                onclick={(event) => press(row, at, event)}
            >
                {#if picked}
                    <td class="pick">
                        <input
                            type="checkbox"
                            checked={picked.has(row.id)}
                            onclick={(event) => {
                                event.stopPropagation();
                                pick(row, at, event);
                            }}
                            aria-label="Select {row.name}"
                        />
                    </td>
                {/if}
                <!-- The shown columns rather than the row's cells, so hiding
                     one takes its cells with it. Each still knows where its own
                     cell sits in the row the backend sent. -->
                {#each shown as column (column.index)}
                    {@const value = row.cells[column.index]}
                    <td class={value?.tone} style={sized(column.key)}>
                        {#if cell}{@render cell(row, column.index)}{:else if value?.at}{@const shown = timeCell(value.text, value.at)}<span title={shown.title}>{shown.text}</span>{:else}{value?.text ?? ''}{/if}
                    </td>
                {/each}
            </tr>
        {:else}
            <tr class="none">
                <td colspan={shown.length + (picked ? 1 : 0)}>{empty}</td>
            </tr>
        {/each}
    </tbody>
</table>

<style>
    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 12px;
    }

    thead th {
        position: sticky;
        top: 0;
        z-index: 1;
        background: var(--bg);
        border-bottom: 1px solid var(--border);
        text-align: left;
        font-weight: 500;
        padding: 0;
        white-space: nowrap;
    }

    thead button {
        display: flex;
        align-items: center;
        gap: 4px;
        width: 100%;
        padding: 7px 12px;
        color: var(--text-faint);
        font-size: 11px;
        letter-spacing: 0.03em;
    }

    /* A heading dragged narrower than its own word ellipses rather than
       widening the column back out from under the pointer. */
    thead .name {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    /* The trailing edge, sitting half over the boundary so the pointer finds it
       from either side. Invisible until the header is hovered: the line between
       columns is the affordance, and drawing a handle on every one of them
       would be a row of furniture above the data. */
    .grip {
        position: absolute;
        top: 0;
        right: -3px;
        z-index: 2;
        width: 7px;
        height: 100%;
        cursor: col-resize;
        touch-action: none;
    }

    .grip::after {
        content: '';
        position: absolute;
        top: 4px;
        bottom: 4px;
        left: 3px;
        width: 1px;
        background: var(--border);
        opacity: 0;
        transition: opacity 90ms ease;
    }

    thead tr:hover .grip::after,
    .grip:focus-visible::after,
    .grip.dragging::after {
        opacity: 1;
    }

    .grip:hover::after,
    .grip:focus-visible::after,
    .grip.dragging::after {
        background: var(--accent);
        width: 2px;
    }

    .grip:focus-visible {
        outline: none;
    }

    thead button:hover {
        color: var(--text);
    }

    th.sorted button {
        color: var(--text);
    }

    /* A column held at a width the user chose. The heading ellipses like a cell
       rather than forcing the column wider than they asked for. */
    th.pinned button {
        overflow: hidden;
    }

    tbody tr {
        cursor: default;
        border-bottom: 1px solid var(--border-soft);
    }

    tbody tr:hover {
        background: var(--bg-hover);
    }

    tbody tr.selected {
        background: var(--bg-active);
    }

    /* Ticked rows read as one set: the accent, kept apart from hover so a
       sweep can still be seen while the pointer moves over it. */
    tbody tr.picked {
        background: color-mix(in srgb, var(--accent) 14%, transparent);
    }

    tbody tr.picked:hover {
        background: color-mix(in srgb, var(--accent) 22%, transparent);
    }

    th.pick,
    td.pick {
        width: 30px;
        padding: 0 0 0 12px;
        vertical-align: middle;
    }

    .pick input {
        display: block;
        margin: 0;
        accent-color: var(--accent);
        cursor: pointer;
    }

    td {
        /* The density preference, set on the root by the shell. This is what
           "compact" actually changes -- the row height follows its cells. */
        padding: var(--cell-pad-y, 6px) 12px;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        max-width: 340px;
    }

    /* The tone the backend put on the cell, so status colouring is decided once. */
    td.ok {
        color: var(--ok);
    }

    td.warn {
        color: var(--warn);
    }

    td.error {
        color: var(--error);
    }

    td.info {
        color: var(--text-dim);
    }

    tr.none td {
        padding: 22px 16px;
        color: var(--text-faint);
        text-align: left;
    }

    tr.none:hover {
        background: none;
    }
</style>
