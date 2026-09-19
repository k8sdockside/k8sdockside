import { beforeEach, describe, expect, test, vi } from 'vitest';

const HelmSubscribe = vi.fn();
const HelmUnsubscribe = vi.fn();
const HelmSetNamespaces = vi.fn();
const Subscribe = vi.fn();
const Unsubscribe = vi.fn();
const SetNamespaces = vi.fn();

let handler: (event: { data: unknown }) => void = () => {};

vi.mock('@wailsio/runtime', () => ({
    Events: { On: vi.fn((_: string, fn: (event: { data: unknown }) => void) => { handler = fn; }) },
}));
vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    LogService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn().mockResolvedValue('logs-1'),
        Close: vi.fn(),
    },
    HelmService: {
        Subscribe: HelmSubscribe,
        Unsubscribe: HelmUnsubscribe,
        SetNamespaces: HelmSetNamespaces,
    },
    ResourceService: { Subscribe, Unsubscribe, SetNamespaces },
}));

const { subscribe } = await import('./subscriptions');

const EMPTY = { kind: 'helmreleases', columns: ['Name'], rows: [], namespaced: true, error: '' };

/** A snapshot arriving from the backend for one subscription. */
function push(subscriptionId: string, table = EMPTY): void {
    handler({ data: { subscriptionId, table } });
}

beforeEach(() => {
    HelmSubscribe.mockReset().mockResolvedValue('sub-h');
    HelmUnsubscribe.mockReset();
    HelmSetNamespaces.mockReset();
    Subscribe.mockReset().mockResolvedValue('sub-1');
    Unsubscribe.mockReset();
    SetNamespaces.mockReset();
});

// Releases are watched like anything else, but the watch is on the Secrets that
// hold them rather than on a kind, so it is opened through their own service.
// What these pin down is that the *only* difference is which service is called:
// everything after that -- the event, the routing, the closing -- is shared, and
// a second path through it is how the two drift apart.
describe('Helm releases', () => {
    test('open a watch through the Helm service, not a one-shot read', async () => {
        subscribe('ctx', 'helmreleases', [], vi.fn(), vi.fn());

        await vi.waitFor(() => expect(HelmSubscribe).toHaveBeenCalledWith('ctx', []));
        expect(Subscribe).not.toHaveBeenCalled();
    });

    test('their rows arrive on the same event as any other kind', async () => {
        const onTable = vi.fn();
        subscribe('ctx', 'helmreleases', [], onTable, vi.fn());
        await vi.waitFor(() => expect(HelmSubscribe).toHaveBeenCalled());

        push('sub-h');

        expect(onTable).toHaveBeenCalledOnce();
    });

    test('keep repainting, which is the point of watching them', async () => {
        const onTable = vi.fn();
        subscribe('ctx', 'helmreleases', [], onTable, vi.fn());
        await vi.waitFor(() => expect(HelmSubscribe).toHaveBeenCalled());

        push('sub-h');
        push('sub-h');
        push('sub-h');

        expect(onTable).toHaveBeenCalledTimes(3);
    });

    test('report a failure to open rather than throwing', async () => {
        HelmSubscribe.mockRejectedValueOnce(new Error('forbidden'));
        const onError = vi.fn();

        subscribe('ctx', 'helmreleases', [], vi.fn(), onError);

        await vi.waitFor(() => expect(onError).toHaveBeenCalledWith('forbidden'));
    });

    test('re-point through the Helm service when the namespaces change', async () => {
        const sub = subscribe('ctx', 'helmreleases', [], vi.fn(), vi.fn());
        await vi.waitFor(() => expect(HelmSubscribe).toHaveBeenCalled());

        sub.setNamespaces(['prod', 'staging']);

        expect(HelmSetNamespaces).toHaveBeenCalledWith('sub-h', ['prod', 'staging']);
        expect(SetNamespaces).not.toHaveBeenCalled();
    });

    test('are closed through the Helm service too', async () => {
        const sub = subscribe('ctx', 'helmreleases', [], vi.fn(), vi.fn());
        await vi.waitFor(() => expect(HelmSubscribe).toHaveBeenCalled());

        sub.close();

        expect(HelmUnsubscribe).toHaveBeenCalledWith('sub-h');
        expect(Unsubscribe).not.toHaveBeenCalled();
    });

    // A tab closed before the backend answers must not leave a watch running
    // with nobody reading it.
    test('closing before the watch opens still closes it', async () => {
        let settle!: (id: string) => void;
        HelmSubscribe.mockReturnValueOnce(new Promise<string>((r) => { settle = r; }));
        const onTable = vi.fn();

        const sub = subscribe('ctx', 'helmreleases', [], onTable, vi.fn());
        sub.close();
        settle('sub-h');
        await new Promise((r) => setTimeout(r, 10));

        expect(HelmUnsubscribe).toHaveBeenCalledWith('sub-h');
        push('sub-h');
        expect(onTable).not.toHaveBeenCalled();
    });
});

describe('every other kind', () => {
    test('opens a watch through the resource service', async () => {
        subscribe('ctx', 'pods', [], vi.fn(), vi.fn());

        await vi.waitFor(() => expect(Subscribe).toHaveBeenCalledWith('ctx', 'pods', []));
        expect(HelmSubscribe).not.toHaveBeenCalled();
    });

    test('is closed and re-pointed through the resource service', async () => {
        const sub = subscribe('ctx', 'pods', [], vi.fn(), vi.fn());
        await vi.waitFor(() => expect(Subscribe).toHaveBeenCalled());

        sub.setNamespaces(['prod']);
        sub.close();

        expect(SetNamespaces).toHaveBeenCalledWith('sub-1', ['prod']);
        expect(Unsubscribe).toHaveBeenCalledWith('sub-1');
        expect(HelmUnsubscribe).not.toHaveBeenCalled();
    });
});
