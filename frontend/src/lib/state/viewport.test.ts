import { describe, expect, test } from 'vitest';
import { followFocus, type ActiveTabs } from './viewport.svelte';

const START: ActiveTabs = { left: 'clusters', main: null, right: null, bottom: null };
const everything = () => true;

describe('which pane a small screen shows', () => {
    test('picking a cluster in the tree shows the dashboard it opened', () => {
        const after = { ...START, main: 'resource:prod#dashboard##' };
        expect(followFocus('left', START, after, everything)).toBe('main');
    });

    test('tapping a row shows its details, wherever that tab lives', () => {
        const before = { ...START, main: 'resource:prod#pods##' };
        expect(followFocus('main', before, { ...before, right: 'details' }, everything)).toBe('right');
        expect(followFocus('main', before, { ...before, bottom: 'details' }, everything)).toBe('bottom');
    });

    // The tree is where a tap came from, not where it was going: a tap that
    // also moved the tree's own tab must still land on what it opened.
    test('when several panes move, the one furthest from the tree wins', () => {
        const after = { left: 'clusters-2', main: 'resource:prod#pods##', right: null, bottom: null };
        expect(followFocus('left', START, after, everything)).toBe('main');
    });

    test('nothing moving leaves the screen where it is', () => {
        expect(followFocus('bottom', START, START, everything)).toBe('bottom');
    });

    test('a pane whose last tab closed hands the screen back to main', () => {
        const before = { ...START, right: 'details' };
        const after = { ...START, right: null };
        expect(followFocus('right', before, after, (pane) => pane !== 'right')).toBe('main');
    });

    test('closing a tab is not a request to look at the pane it was in', () => {
        const before = { ...START, main: 'a', bottom: 'logs' };
        const after = { ...START, main: 'a', bottom: null };
        expect(followFocus('main', before, after, everything)).toBe('main');
    });
});
