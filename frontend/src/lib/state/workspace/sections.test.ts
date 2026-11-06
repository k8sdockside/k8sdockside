import { describe, expect, it } from 'vitest';
import { orderSections, rearranged } from './sections';

const POD = ['resources', 'metrics', 'conditions', 'related', 'describe'];
const VM = ['metrics', 'kubevirt', 'conditions', 'related', 'plugin:kubevirt/machine', 'describe'];

describe('orderSections', () => {
    it('keeps the default order until the user chooses one', () => {
        expect(orderSections(POD, [])).toEqual(POD);
        expect(orderSections(POD, null)).toEqual(POD);
    });

    it('follows the stored order, leaving out what this object does not have', () => {
        expect(orderSections(POD, ['describe', 'kubevirt', 'conditions', 'resources', 'metrics', 'related'])).toEqual(['describe', 'conditions', 'resources', 'metrics', 'related']);
    });

    it("puts the app's own sections the order never saw next to their default neighbour", () => {
        // Arranged on a pod: the report first. A machine still gets its panel after the charts.
        const stored = ['describe', 'resources', 'metrics', 'conditions', 'related'];
        expect(orderSections(VM, stored)).toEqual(['describe', 'metrics', 'kubevirt', 'conditions', 'related', 'plugin:kubevirt/machine']);
    });

    it("puts a new plugin's section at the bottom by default", () => {
        const stored = ['describe', 'metrics', 'conditions', 'related'];
        expect(orderSections(['metrics', 'conditions', 'plugin:new/one', 'describe'], stored)).toEqual(['describe', 'metrics', 'conditions', 'plugin:new/one']);
    });

    it("or where the plugin's sections go by default, when asked", () => {
        const stored = ['describe', 'metrics', 'conditions', 'related'];
        expect(orderSections(['metrics', 'conditions', 'plugin:new/one', 'describe'], stored, 'default')).toEqual(['describe', 'metrics', 'conditions', 'plugin:new/one']);
        expect(orderSections(['metrics', 'plugin:new/one', 'conditions', 'describe'], stored, 'default')).toEqual(['describe', 'metrics', 'plugin:new/one', 'conditions']);
    });

    it('reads a stored list with a key twice as if it had it once', () => {
        expect(orderSections(['a', 'b'], ['b', 'a', 'b'])).toEqual(['b', 'a']);
    });
});

describe('rearranged', () => {
    it('starts an order from what was on screen', () => {
        expect(rearranged([], POD, ['describe', 'resources', 'metrics', 'conditions', 'related'])).toEqual(['describe', 'resources', 'metrics', 'conditions', 'related']);
    });

    it('keeps the place of sections that were not on screen', () => {
        const stored = ['kubevirt', 'describe', 'resources', 'metrics', 'conditions', 'related'];
        const before = orderSections(POD, stored);
        const after = ['resources', 'describe', 'metrics', 'conditions', 'related'];
        const next = rearranged(stored, before, after);
        expect(next[0]).toBe('kubevirt');
        expect(orderSections(POD, next)).toEqual(after);
    });

    it('gives a section new to the order a slot where it was shown', () => {
        const stored = ['describe', 'metrics', 'conditions', 'related'];
        const before = orderSections(VM, stored);
        const after = [...before];
        after.splice(after.indexOf('plugin:kubevirt/machine'), 1);
        after.unshift('plugin:kubevirt/machine');
        const next = rearranged(stored, before, after);
        expect(orderSections(VM, next)).toEqual(after);
        expect(next).toHaveLength(6);
    });
});
