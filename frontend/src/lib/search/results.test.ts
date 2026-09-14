import { describe, expect, it } from 'vitest';
import type { Plugin, PluginViewSpec } from '../plugins/types';
import { focusHash, groupHits, hitKey, nameTerms, pluginLinksFor, searchable } from './results';

function hit(contextId: string, apiKind: string, namespace: string, name: string, kind = apiKind.toLowerCase() + 's') {
    return { contextId, kind, apiKind, namespace, name };
}

function view(spec: Partial<PluginViewSpec> & { id: string }): PluginViewSpec {
    return { label: spec.id, icon: 'puzzle', type: 'table', kind: '', namespace: '', selector: '', ...spec };
}

function plugin(id: string, views: PluginViewSpec[], extra: Partial<Plugin> = {}): Plugin {
    return {
        id,
        name: id.toUpperCase(),
        tagline: '',
        icon: 'puzzle',
        author: '',
        docs: '',
        description: '',
        requires: [],
        views,
        origin: 'builtin',
        pack: '',
        disabled: false,
        ...extra,
    };
}

const APPS = 'crd:applications.argoproj.io';

describe('searchable', () => {
    it('wants two characters', () => {
        expect(searchable(' a ')).toBe(false);
        expect(searchable('ab')).toBe(true);
    });
});

describe('nameTerms', () => {
    it('leaves the filters out', () => {
        expect(nameTerms('Web kind:pods ns:prod label:a=b api')).toEqual(['web', 'api']);
    });
});

describe('groupHits', () => {
    it('keeps the sidebar order of contexts, and counts where each group starts', () => {
        const groups = groupHits(
            [hit('b', 'Pod', 'x', 'one'), hit('a', 'Pod', 'x', 'two'), hit('b', 'Pod', 'x', 'three'), hit('z', 'Pod', 'x', 'four')],
            ['a', 'b'],
        );
        expect(groups.map((g) => g.contextId)).toEqual(['a', 'b', 'z']);
        expect(groups.map((g) => g.offset)).toEqual([0, 1, 3]);
    });

    it('puts the exact name first, then names that start with it', () => {
        const [group] = groupHits(
            [hit('a', 'Event', 'x', 'web.17a'), hit('a', 'ReplicaSet', 'x', 'my-web-7f'), hit('a', 'Deployment', 'x', 'web'), hit('a', 'Pod', 'x', 'web-7f-abc')],
            ['a'],
            'web',
        );
        expect(group.hits.map((h) => h.name)).toEqual(['web', 'web.17a', 'web-7f-abc', 'my-web-7f']);
    });

    it('sorts the rest by kind, namespace and name', () => {
        const [group] = groupHits(
            [hit('a', 'Service', 'b', 'x'), hit('a', 'Pod', 'b', 'x'), hit('a', 'Pod', 'a', 'x10'), hit('a', 'Pod', 'a', 'x9')],
            ['a'],
            'zzz',
        );
        expect(group.hits.map((h) => `${h.apiKind}/${h.namespace}/${h.name}`)).toEqual([
            'Pod/a/x9',
            'Pod/a/x10',
            'Pod/b/x',
            'Service/b/x',
        ]);
    });
});

describe('hitKey', () => {
    it('tells the same name in two kinds apart', () => {
        expect(hitKey(hit('a', 'Pod', 'x', 'web'))).not.toBe(hitKey(hit('a', 'Service', 'x', 'web')));
    });
});

describe('focusHash', () => {
    it('fills the placeholders, encoded', () => {
        expect(focusHash('selected={namespace}/{name}', 'argo cd', 'a&b')).toBe('selected=argo%20cd/a%26b');
    });

    it('falls back to the default', () => {
        expect(focusHash('', 'ns', 'n')).toBe('namespace=ns&name=n');
    });
});

describe('pluginLinksFor', () => {
    const argo = plugin('argocd', [
        view({ id: 'board', label: 'Application board', type: 'custom', focus: { kind: APPS, hash: 'selected={namespace}/{name}' } }),
        view({ id: 'applications', label: 'Applications', kind: APPS }),
        view({ id: 'components', kind: 'deployments', selector: 'app.kubernetes.io/part-of=argocd' }),
        view({ id: 'overview-ish', type: 'custom' }),
    ]);
    const app = hit('ctx', 'Application', 'argocd', 'guestbook', APPS);

    it('offers the board opened on the object first, then the table', () => {
        const links = pluginLinksFor(app, [argo]);
        expect(links.map((l) => [l.tabKind, l.custom, l.hash])).toEqual([
            ['plugin:argocd/board', true, 'selected=argocd/guestbook'],
            ['plugin:argocd/applications', false, ''],
        ]);
    });

    it('leaves out a table narrowed by labels', () => {
        expect(pluginLinksFor(hit('ctx', 'Deployment', 'argocd', 'argocd-server'), [argo])).toEqual([]);
    });

    it('offers a pinned table only for objects in its namespace', () => {
        const pinned = plugin('flux', [view({ id: 'sources', kind: 'services', namespace: 'flux-system' })]);
        expect(pluginLinksFor(hit('ctx', 'Service', 'flux-system', 'x'), [pinned])).toHaveLength(1);
        expect(pluginLinksFor(hit('ctx', 'Service', 'default', 'x'), [pinned])).toHaveLength(0);
    });

    it('offers nothing from a plugin that is switched off', () => {
        expect(pluginLinksFor(app, [{ ...argo, disabled: true }])).toEqual([]);
    });
});
