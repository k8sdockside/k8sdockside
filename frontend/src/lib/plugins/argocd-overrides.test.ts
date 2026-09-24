import { beforeAll, describe, expect, test } from 'vitest';

// The built-in Argo CD plugin's overrides rules, run as the plugin's page runs
// them: the script loaded into a window of its own. They decide what patch an
// Application or ApplicationSet is sent, and whether the user is told the
// change will be undone -- which is what these check.

type Api = Record<string, (...args: unknown[]) => any>; // eslint-disable-line @typescript-eslint/no-explicit-any
let O: Api;

beforeAll(async () => {
    // Read from disk: the script lives outside the frontend, where Vite does
    // not serve from, and the frontend has no Node types.
    const fs = (await import('node:' + 'fs')) as { readFileSync(path: string, encoding: string): string };
    const cwd = (globalThis as unknown as { process: { cwd(): string } }).process.cwd();
    const source = fs.readFileSync(`${cwd}/../internal/plugins/builtin/ui/argocd/overrides.js`, 'utf8');
    const scope: Record<string, unknown> = {};
    new Function('window', source)(scope);
    O = scope.ArgoOverrides as Api;
});

const helmApp = () => ({
    metadata: { name: 'web', namespace: 'argocd' },
    spec: {
        source: {
            repoURL: 'https://charts.example.com',
            chart: 'web',
            targetRevision: '1.2.0',
            helm: { parameters: [{ name: 'replicas', value: '2' }], skipCrds: true },
        },
    },
});

describe('patches', () => {
    test('one source is sent as a diff of itself, leaving what the form does not cover', () => {
        const app = helmApp();
        const src = O.sourcesOf(app.spec).list[0];
        const form = O.readSource(src);
        form.targetRevision = '1.3.0';
        form.helm.parameters[0].value = '3';
        form.helm.parameters.push({ name: 'image.tag', value: 'v2', forceString: true });

        expect(O.patchFor(app.spec, ['spec'], [O.writeSource(src, form)])).toEqual({
            spec: {
                source: {
                    targetRevision: '1.3.0',
                    helm: {
                        parameters: [
                            { name: 'replicas', value: '3' },
                            { name: 'image.tag', value: 'v2', forceString: true },
                        ],
                    },
                },
            },
        });
        expect(O.describeChanges(O.readSource(src), form)).toEqual([
            'target revision 1.2.0 → 1.3.0',
            'replicas 2 → 3',
            'set image.tag=v2',
        ]);
    });

    test('emptying a list removes the field, and skipCrds survives', () => {
        const app = helmApp();
        const src = O.sourcesOf(app.spec).list[0];
        const form = O.readSource(src);
        form.helm.parameters = [];
        expect(O.patchFor(app.spec, ['spec'], [O.writeSource(src, form)])).toEqual({ spec: { source: { helm: { parameters: null } } } });
    });

    test('an untouched form is no patch at all', () => {
        const app = helmApp();
        const src = O.sourcesOf(app.spec).list[0];
        expect(O.patchFor(app.spec, ['spec'], [O.writeSource(src, O.readSource(src))])).toBeNull();
    });

    // A merge patch cannot change one item of a list, so several sources go
    // back whole -- and an ApplicationSet's go into its template.
    test('an ApplicationSet template with several sources is sent back whole', () => {
        const spec = {
            sources: [
                { repoURL: 'r', path: 'apps/{{.env}}', kustomize: { images: ['api=ghcr.io/acme/api:{{.tag}}'] } },
                { repoURL: 'v', ref: 'values' },
            ],
        };
        const sources = O.sourcesOf(spec);
        const form = O.readSource(sources.list[0]);
        form.kustomize.images = ['api=ghcr.io/acme/api:{{.tag}}-rc'];
        const patch = O.patchFor(spec, ['spec', 'template', 'spec'], [O.writeSource(sources.list[0], form), sources.list[1]]);
        expect(patch.spec.template.spec.sources).toHaveLength(2);
        expect(patch.spec.template.spec.sources[0].kustomize.images).toEqual(['api=ghcr.io/acme/api:{{.tag}}-rc']);
    });

    test('a replica count that is a number goes as a number', () => {
        const spec = { source: { repoURL: 'r', path: 'p', kustomize: {} } };
        const src = O.sourcesOf(spec).list[0];
        const form = O.readSource(src);
        form.kustomize.replicas.push({ name: 'api', count: '4' });
        expect(O.writeSource(src, form).kustomize.replicas).toEqual([{ name: 'api', count: 4 }]);
    });
});

describe('who owns an Application', () => {
    const tracked = {
        metadata: { name: 'web', namespace: 'argocd', annotations: { 'argocd.argoproj.io/tracking-id': 'root:argoproj.io/Application:argocd/web' } },
    };

    test('a parent that heals itself will put an override back', () => {
        const root = { metadata: { name: 'root', namespace: 'argocd' }, spec: { syncPolicy: { automated: { selfHeal: true } } } };
        expect(O.ownerOf(tracked, [root], [])).toMatchObject({ kind: 'parent', name: 'root', reverts: true });
    });

    test('a parent without self-heal only puts it back on its next sync', () => {
        const root = { metadata: { name: 'root', namespace: 'argocd' }, spec: {} };
        expect(O.ownerOf(tracked, [root], [])).toMatchObject({ kind: 'parent', reverts: false });
    });

    test('under label tracking, the instance label names the parent', () => {
        const child = { metadata: { name: 'web', namespace: 'argocd', labels: { 'app.kubernetes.io/instance': 'root' } } };
        expect(O.ownerOf(child, [{ metadata: { name: 'root' }, spec: {} }], [])).toMatchObject({ kind: 'parent', name: 'root' });
    });

    test('an ApplicationSet rewrites what it made, unless it is told to leave the source alone', () => {
        const made = { metadata: { name: 'api-dev', namespace: 'argocd', ownerReferences: [{ kind: 'ApplicationSet', name: 'envs' }] } };
        expect(O.ownerOf(made, [], [{ metadata: { name: 'envs' }, spec: {} }])).toMatchObject({ kind: 'appset', reverts: true });
        const ignoring = { metadata: { name: 'envs' }, spec: { ignoreApplicationDifferences: [{ jsonPointers: ['/spec/source/targetRevision'] }] } };
        expect(O.ownerOf(made, [], [ignoring])).toMatchObject({ kind: 'appset', reverts: false });
        const createOnly = { metadata: { name: 'envs' }, spec: { syncPolicy: { applicationsSync: 'create-only' } } };
        expect(O.ownerOf(made, [], [createOnly])).toMatchObject({ kind: 'appset', reverts: false });
    });

    test('a plain Application is its own', () => {
        expect(O.ownerOf({ metadata: { name: 'solo' } }, [], [])).toMatchObject({ kind: '', reverts: false });
    });
});

test('generators are read into what they feed the template', () => {
    const gens = O.generatorsOf({
        spec: { generators: [{ list: { elements: [{ env: 'dev', tag: 'v1' }] } }, { matrix: { generators: [{ clusters: {} }, { git: { repoURL: 'x', directories: [{ path: 'apps/*' }] } }] } }] },
    });
    expect(gens[0]).toMatchObject({ kind: 'list', elements: [{ env: 'dev', tag: 'v1' }] });
    expect(gens[1].parts.map((p: { kind: string }) => p.kind)).toEqual(['clusters', 'git']);
    expect(gens[1].parts[1].note).toBe('x · directories apps/*');
});
