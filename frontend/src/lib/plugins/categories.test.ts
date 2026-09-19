import { describe, expect, test } from 'vitest';
import {
    ANY_PLUGIN,
    arrange,
    CATEGORIES,
    categoryCounts,
    categoryOf,
    matches,
    narrowed,
    type Listed,
} from './categories';

const plugin = (over: Partial<Listed> & Pick<Listed, 'id'>): Listed => ({
    name: over.id,
    tagline: '',
    category: 'other',
    description: '',
    ...over,
});

const list: Listed[] = [
    plugin({ id: 'longhorn', name: 'Longhorn', category: 'storage', tagline: 'distributed block storage', author: 'K8s Dockside' }),
    plugin({ id: 'cilium', name: 'Cilium', category: 'networking', description: 'eBPF networking and policy', author: 'K8s Dockside' }),
    plugin({ id: 'acme', name: 'Acme Mesh', category: 'networking', description: 'a service mesh', author: 'Acme' }),
    plugin({ id: 'odd', name: 'Odd one', category: '', description: 'nothing in particular' }),
];

describe('categories', () => {
    test('an unknown or empty category reads as Other', () => {
        expect(categoryOf('').id).toBe('other');
        expect(categoryOf('nonsense').id).toBe('other');
        expect(categoryOf('storage').label).toBe('Storage');
    });

    test('every category has a label, an icon and a note', () => {
        for (const category of CATEGORIES) {
            expect(category.label).not.toBe('');
            expect(category.icon).not.toBe('');
            expect(category.note).not.toBe('');
        }
    });

    test('counts leave out the categories nothing is in, and file the blank ones under Other', () => {
        expect(categoryCounts(list).map((c) => [c.category.id, c.count])).toEqual([
            ['storage', 1],
            ['networking', 2],
            ['other', 1],
        ]);
    });
});

describe('search', () => {
    test('a plugin is found by its description, not only its name', () => {
        expect(arrange(list, { ...ANY_PLUGIN, text: 'eBPF' }).map((p) => p.id)).toEqual(['cilium']);
        expect(arrange(list, { ...ANY_PLUGIN, text: 'block storage' }).map((p) => p.id)).toEqual(['longhorn']);
    });

    test('the category is searchable by its label as well as its id', () => {
        expect(arrange(list, { ...ANY_PLUGIN, text: 'networking' }).map((p) => p.id)).toEqual(['acme', 'cilium']);
    });

    test('every word has to match, so a second word narrows', () => {
        expect(arrange(list, { ...ANY_PLUGIN, text: 'mesh acme' }).map((p) => p.id)).toEqual(['acme']);
        expect(arrange(list, { ...ANY_PLUGIN, text: 'mesh longhorn' })).toEqual([]);
    });

    test('case and stray spaces do not matter', () => {
        expect(matches(list[0]!, { ...ANY_PLUGIN, text: '  LONGhorn ' })).toBe(true);
    });
});

describe('filter', () => {
    test('by category, with a blank category counting as Other', () => {
        expect(arrange(list, { ...ANY_PLUGIN, category: 'networking' }).map((p) => p.id)).toEqual(['acme', 'cilium']);
        expect(arrange(list, { ...ANY_PLUGIN, category: 'other' }).map((p) => p.id)).toEqual(['odd']);
    });

    test('a query that narrows nothing says so, which is how the lists know to stay whole', () => {
        expect(narrowed(ANY_PLUGIN)).toBe(false);
        expect(narrowed({ ...ANY_PLUGIN, text: '  ' })).toBe(false);
        expect(narrowed({ ...ANY_PLUGIN, category: 'storage' })).toBe(true);
    });
});

describe('order', () => {
    test('by name, A to Z', () => {
        expect(arrange(list, ANY_PLUGIN).map((p) => p.id)).toEqual(['acme', 'cilium', 'longhorn', 'odd']);
    });

    test('by category, in the order the filter offers them, then by name', () => {
        expect(arrange(list, { ...ANY_PLUGIN, sort: 'category' }).map((p) => p.id)).toEqual([
            'longhorn',
            'acme',
            'cilium',
            'odd',
        ]);
    });

    test('by author, with the ones crediting nobody first', () => {
        expect(arrange(list, { ...ANY_PLUGIN, sort: 'author' }).map((p) => p.id)).toEqual([
            'odd',
            'acme',
            'cilium',
            'longhorn',
        ]);
    });

    test('arranging does not disturb the list it was given', () => {
        const before = list.map((p) => p.id);
        arrange(list, { ...ANY_PLUGIN, sort: 'category' });
        expect(list.map((p) => p.id)).toEqual(before);
    });
});
