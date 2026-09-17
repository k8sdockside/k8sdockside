import { describe, expect, test } from 'vitest';
import { actionsFor, actionsForVM, type ActionId } from './actions';
import { DASHBOARD, HELM_RELEASES, PORT_FORWARDS, SETTINGS, customKindFor } from './catalogue';

/** The action ids a kind offers, which is all these tests care about. */
function ids(kind: string): ActionId[] {
    return actionsFor(kind).map((a) => a.id);
}

describe('what every object can do', () => {
    test.each(['configmaps', 'secrets', 'clusterroles', 'persistentvolumes'])(
        '%s can be edited and deleted',
        (kind) => {
            expect(ids(kind)).toEqual(['edit', 'delete']);
        },
    );

    // A custom resource is an object like any other. Nothing here is compiled
    // in per kind, so a CRD nobody has heard of gets the same two.
    test('so can a custom resource', () => {
        expect(ids(customKindFor('certificates.cert-manager.io'))).toEqual(['edit', 'delete']);
    });

    // A Helm release is a Secret the backend decodes rather than a Kubernetes
    // object, so it gets neither Edit nor Delete: deleting the Secret would
    // leave every object the release installed running, with nothing left that
    // knows they belong together. What it gets instead are Helm's own verbs.
    test('a Helm release gets Helm verbs rather than object ones', () => {
        expect(ids(HELM_RELEASES)).toEqual(['values', 'rollback', 'uninstall']);
    });

    // Reading a release needs nothing installed; changing one runs helm. The
    // bar uses this to disable what it cannot do, with the reason.
    test('the two that run helm say so, and the one that only reads does not', () => {
        const needs = Object.fromEntries(
            actionsFor(HELM_RELEASES).map((action) => [action.id, action.needsHelm === true]),
        );
        expect(needs).toEqual({ values: false, rollback: true, uninstall: true });
    });

    // The forwards view is a list of the app's own tunnels rather than a
    // listing of objects, so there is nothing here to act on either.
    test.each([DASHBOARD, SETTINGS, PORT_FORWARDS])('the %s view offers nothing', (kind) => {
        expect(actionsFor(kind)).toEqual([]);
    });
});

describe('what particular kinds can do', () => {
    test('a pod offers its logs, and to be evicted rather than deleted', () => {
        expect(ids('pods')).toEqual(['edit', 'logs', 'shell', 'forward', 'evict', 'delete']);
    });

    // A workload's logs are every container of every pod its selector finds,
    // which is the view worth having while a rollout is happening.
    test.each(['deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs'])(
        'a %s offers its logs too',
        (kind) => {
            expect(ids(kind)).toContain('logs');
        },
    );

    // Nothing here has containers, so there is nothing to follow.
    test.each(['configmaps', 'nodes', 'services', 'secrets'])('a %s offers no logs', (kind) => {
        expect(ids(kind)).not.toContain('logs');
    });

    test('a node can be cordoned and drained, and says what is on it first', () => {
        // Pods comes before both: "what is actually running here" is the
        // question asked immediately before cordoning or draining.
        expect(ids('nodes')).toEqual(['edit', 'shell', 'nodepods', 'cordon', 'drain', 'delete']);
    });

    // A shell on a node is a privileged pod created on it rather than an exec,
    // but from the bar it is the same button.
    test('a node offers a shell, and nothing to forward', () => {
        expect(ids('nodes')).toContain('shell');
        expect(ids('nodes')).not.toContain('forward');
    });

    // A service has no containers to exec into: what it has is ports, which
    // land on the pods behind it.
    test('a service offers a forward but no shell', () => {
        expect(ids('services')).toEqual(['edit', 'forward', 'delete']);
    });

    // Nothing here runs a container, so there is nothing to open a shell in.
    test.each(['configmaps', 'secrets', 'ingresses', 'namespaces'])('a %s offers no shell', (kind) => {
        expect(ids(kind)).not.toContain('shell');
    });

    // A CronJob's containers belong to the Jobs it creates, not to it: there is
    // no pod of its own to attach to, though its logs gather from all of them.
    test('a cron job offers logs but no shell', () => {
        expect(ids('cronjobs')).toContain('logs');
        expect(ids('cronjobs')).not.toContain('shell');
    });

    test('a statefulset can be scaled, restarted and rolled back', () => {
        expect(ids('statefulsets')).toEqual(['edit', 'logs', 'shell', 'forward', 'scale', 'restart', 'undo', 'delete']);
    });

    // Only a Deployment's rollout can be paused: the other two have no such
    // field, and roll pod by pod under their own update strategy.
    test('a deployment can also have its rollout paused', () => {
        expect(ids('deployments')).toEqual([
            'edit',
            'logs',
            'shell',
            'forward',
            'scale',
            'restart',
            'undo',
            'pause',
            'delete',
        ]);
    });

    // A DaemonSet runs one pod per node, so there is no replica count to set --
    // but it does roll, and keeps a history to roll back through.
    test('a daemonset restarts and rolls back but does not scale', () => {
        expect(ids('daemonsets')).toEqual(['edit', 'logs', 'shell', 'forward', 'restart', 'undo', 'delete']);
    });

    // A ReplicaSet's history is its Deployment's; rolling back is done there.
    test.each(['replicasets', 'jobs', 'pods'])('a %s has no rollout to go back through', (kind) => {
        expect(ids(kind)).not.toContain('undo');
    });

    test('a cron job can be run now and suspended', () => {
        expect(ids('cronjobs')).toEqual(['edit', 'logs', 'trigger', 'suspend', 'delete']);
    });

    test('a job can be suspended but not run again', () => {
        expect(ids('jobs')).toContain('suspend');
        expect(ids('jobs')).not.toContain('trigger');
    });

    // Both answers ask first: neither can be taken back.
    test('a signing request can be approved or denied, after a question', () => {
        expect(ids('certificatesigningrequests')).toEqual(['edit', 'approve', 'deny', 'delete']);
        for (const action of actionsFor('certificatesigningrequests')) {
            if (action.id === 'approve' || action.id === 'deny') expect(action.form).toBe('confirm');
        }
    });

    test('evicting asks first, and rollback asks which revision', () => {
        expect(actionsFor('pods').find((a) => a.id === 'evict')?.form).toBe('confirm');
        expect(actionsFor('deployments').find((a) => a.id === 'undo')?.form).toBe('history');
    });

    // A ReplicaSet has a replica count, but rolling one means nothing: the
    // Deployment above it owns the template that a restart would stamp.
    test('a replicaset scales but does not restart', () => {
        expect(ids('replicasets')).toEqual(['edit', 'logs', 'shell', 'forward', 'scale', 'delete']);
    });
});

describe('how the bar treats each one', () => {
    test('delete asks first, and is marked as the dangerous one', () => {
        const del = actionsFor('pods').find((a) => a.id === 'delete');
        expect(del?.form).toBe('confirm');
        expect(del?.tone).toBe('danger');
    });

    // Draining a node moves every workload off it. That is not a one-click
    // action however routine it feels.
    test('drain asks first too', () => {
        expect(actionsFor('nodes').find((a) => a.id === 'drain')?.form).toBe('confirm');
    });

    test('scale asks for a number', () => {
        expect(actionsFor('deployments').find((a) => a.id === 'scale')?.form).toBe('number');
    });

    // Which port, which local port and whether to open a browser are three
    // things a button cannot guess, so a forward asks before it opens one.
    test('a forward asks which port', () => {
        expect(actionsFor('services').find((a) => a.id === 'forward')?.form).toBe('ports');
    });

    // A shell that needed a form filled in first is not a shell anybody would
    // use: which shell and which terminal are settings, answered once.
    test('a shell just opens', () => {
        expect(actionsFor('pods').find((a) => a.id === 'shell')?.form).toBe('immediate');
    });

    test('cordon and restart just happen', () => {
        expect(actionsFor('nodes').find((a) => a.id === 'cordon')?.form).toBe('immediate');
        expect(actionsFor('deployments').find((a) => a.id === 'restart')?.form).toBe('immediate');
    });

    test('every action has a label and an icon', () => {
        for (const kind of ['nodes', 'deployments', 'daemonsets', 'pods', 'services']) {
            for (const action of actionsFor(kind)) {
                expect(action.label, `${kind}/${action.id}`).not.toBe('');
                expect(action.icon, `${kind}/${action.id}`).not.toBe('');
            }
        }
    });
});

// The virtctl set, minus the two that are a terminal rather than a command.
// Which buttons the bar draws follows what the machine is doing: offering Stop
// on a stopped machine is a button that errors, and offering Pause and Resume
// together is a bar that cannot say which one the machine needs.
describe('what a virtual machine offers', () => {
    const vm = (over: Partial<{ running: boolean; paused: boolean; migratable: boolean }> = {}) => ({
        running: true,
        paused: false,
        migratable: true,
        ...over,
    });
    const idsOf = (state: Parameters<typeof actionsForVM>[0]) => actionsForVM(state).map((a) => a.id);

    test('a stopped machine can only be started', () => {
        expect(idsOf(vm({ running: false }))).toEqual(['vmstart']);
    });

    test('a running one offers everything that acts on a live guest', () => {
        expect(idsOf(vm())).toEqual(['vmpause', 'vmrestart', 'vmsoftreboot', 'vmmigrate', 'vmstop']);
    });

    test('a paused one offers Resume in place of Pause, not beside it', () => {
        const ids = idsOf(vm({ paused: true }));

        expect(ids).toContain('vmunpause');
        expect(ids).not.toContain('vmpause');
    });

    // Passed-through hardware is why a guest cannot be moved, and the button
    // would produce a migration that fails a moment later.
    test('a guest that cannot be moved is not offered a migration', () => {
        expect(idsOf(vm({ migratable: false }))).not.toContain('vmmigrate');
    });

    // Both take the guest down, and a virtual machine is not a pod: nothing
    // reschedules it, and what was running inside was not built to be killed.
    test('the two that take the guest down ask first', () => {
        for (const id of ['vmstop', 'vmrestart']) {
            const action = actionsForVM(vm()).find((a) => a.id === id);
            expect(action?.form).toBe('confirm');
        }
    });

    test('the ones undone by the button that replaces them do not', () => {
        expect(actionsForVM(vm({ running: false }))[0].form).toBe('immediate');
        expect(actionsForVM(vm()).find((a) => a.id === 'vmpause')?.form).toBe('immediate');
    });
});
