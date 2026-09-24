// What can be done to an object, by kind.
//
// One table rather than a chain of conditions in the panel, so that the answer
// to "what can I do to a StatefulSet" is in one place and adding a kind is a
// line rather than a branch. Nothing here calls a cluster: it decides which
// buttons are drawn, and the store beside it does the work.

import { ACCESS_OVERVIEW, DASHBOARD, HELM_RELEASES, PORT_FORWARDS, SETTINGS } from './catalogue';

export type ActionId =
    | 'edit'
    | 'logs'
    | 'shell'
    | 'forward'
    | 'scale'
    | 'restart'
    | 'undo'
    | 'pause'
    | 'suspend'
    | 'trigger'
    | 'evict'
    | 'approve'
    | 'deny'
    | 'nodepods'
    | 'vmstart'
    | 'vmstop'
    | 'vmrestart'
    | 'vmpause'
    | 'vmunpause'
    | 'vmsoftreboot'
    | 'vmmigrate'
    | 'cordon'
    | 'drain'
    | 'compare'
    | 'delete'
    | 'values'
    | 'rollback'
    | 'uninstall';

/**
 * How choosing an action behaves.
 *
 * `immediate` runs at once, `confirm` replaces the bar with a question naming
 * the object, and `number` asks for a replica count first. `revision` picks one
 * of a Helm release's revisions, and `history` one of a workload's.
 */
export type ActionForm = 'immediate' | 'confirm' | 'number' | 'ports' | 'revision' | 'history';

export interface Action {
    id: ActionId;
    label: string;
    icon: string;
    form: ActionForm;
    /** Marks the action that cannot be undone, so the bar can colour it apart. */
    tone?: 'danger';
    /**
     * Marks an action that runs helm rather than talking to the API server.
     *
     * The bar disables these, with the reason, on a machine where helm was not
     * found. Reading a release needs nothing installed -- the record is a
     * Secret the app decodes -- but changing one is Helm's own operation. See
     * internal/helmcli.
     */
    needsHelm?: boolean;
}

const EDIT: Action = { id: 'edit', label: 'Edit', icon: 'edit', form: 'immediate' };
const LOGS: Action = { id: 'logs', label: 'Logs', icon: 'rows', form: 'immediate' };
/**
 * A terminal in the object. Immediate: which shell, and which of the settings'
 * terminals opens it, are answered from the preferences rather than asked here
 * -- a shell you have to fill in a form for is not a shell you would use.
 */
const SHELL: Action = { id: 'shell', label: 'Shell', icon: 'terminal', form: 'immediate' };
/**
 * A local port into the object. It asks first, because there are three answers
 * it cannot guess: which port, on which local port, and whether to open a
 * browser on it.
 */
const FORWARD: Action = { id: 'forward', label: 'Forward', icon: 'forward', form: 'ports' };
const SCALE: Action = { id: 'scale', label: 'Scale', icon: 'scale', form: 'number' };
/**
 * The same object in another cluster, diffed. Offered on everything that is
 * an object someone set up -- which is everything but an event, a record of
 * one moment in one cluster that has no counterpart anywhere else.
 */
const COMPARE: Action = { id: 'compare', label: 'Compare', icon: 'columns', form: 'immediate' };
const NOT_COMPARABLE = ['events'];
const RESTART: Action = { id: 'restart', label: 'Restart', icon: 'repeat', form: 'immediate' };
/**
 * Back to an earlier revision of a workload: `kubectl rollout undo`. It asks
 * which one, from the history the cluster keeps -- a Deployment's ReplicaSets,
 * a StatefulSet's or DaemonSet's ControllerRevisions -- and the controller
 * then rolls to it like any other change.
 *
 * Not Helm's Rollback, which is a different operation on a different thing and
 * so a different id, even though both say the same word on the button.
 */
const UNDO: Action = { id: 'undo', label: 'Rollback', icon: 'history', form: 'history' };
/**
 * Holding a Deployment's rollout, or letting it go on. Its label follows the
 * Deployment, as Cordon's follows the node: a paused one offers to resume.
 */
const PAUSE: Action = { id: 'pause', label: 'Pause rollout', icon: 'pause', form: 'immediate' };
/**
 * Stopping a CronJob from starting Jobs, or a Job from running pods. Its label
 * follows the object too. Immediate, because it is undone by the button that
 * replaces it.
 */
const SUSPEND: Action = { id: 'suspend', label: 'Suspend', icon: 'pause', form: 'immediate' };
/** A CronJob's job, now, outside its schedule: `kubectl create job --from`. */
const TRIGGER: Action = { id: 'trigger', label: 'Run now', icon: 'play', form: 'immediate' };
/**
 * Moving a pod the polite way: through the eviction API, which a disruption
 * budget can refuse. Asks first, since the pod's containers are stopped either
 * way, and it is not danger-toned, since whatever manages the pod replaces it.
 */
const EVICT: Action = { id: 'evict', label: 'Evict', icon: 'exit', form: 'confirm' };
/**
 * Answering a certificate signing request. Both ask first: approving one hands
 * out a certificate the cluster will trust, and neither answer can be taken
 * back. Only offered while the request is unanswered -- see ANSWERS.
 */
const APPROVE: Action = { id: 'approve', label: 'Approve', icon: 'check', form: 'confirm' };
const DENY: Action = { id: 'deny', label: 'Deny', icon: 'close', form: 'confirm' };
/**
 * What can be done to a virtual machine: the lifecycle half of virtctl.
 *
 * Which of them the bar actually draws depends on what the machine is doing --
 * a stopped one offers Start and nothing else, a paused one offers Unpause --
 * and that is decided from the cluster's own answer rather than from the last
 * button pressed. See actionsForVM.
 *
 * Stop and Restart ask first. Both take the guest down, and a virtual machine
 * is not a pod: nothing reschedules it, and whatever was running inside it was
 * not designed to be killed and replaced. Start, Pause and Unpause are
 * immediate, because each is undone by the button that replaces it.
 */
const VM_START: Action = { id: 'vmstart', label: 'Start', icon: 'rocket', form: 'immediate' };
const VM_STOP: Action = { id: 'vmstop', label: 'Stop', icon: 'close', form: 'confirm', tone: 'danger' };
const VM_RESTART: Action = { id: 'vmrestart', label: 'Restart', icon: 'repeat', form: 'confirm' };
const VM_PAUSE: Action = { id: 'vmpause', label: 'Pause', icon: 'clock', form: 'immediate' };
const VM_UNPAUSE: Action = { id: 'vmunpause', label: 'Resume', icon: 'rocket', form: 'immediate' };
/**
 * A reboot asked of the guest rather than done to it: KubeVirt sends the ACPI
 * signal and the operating system shuts itself down properly. It needs the
 * guest agent, and it is the polite version of Restart.
 */
const VM_SOFT_REBOOT: Action = { id: 'vmsoftreboot', label: 'Reboot', icon: 'refresh', form: 'confirm' };
/**
 * Move the guest to another node without stopping it. Offered only where
 * KubeVirt says the guest is live-migratable -- a machine with passed-through
 * hardware is not, and the button would produce a migration that fails a moment
 * later.
 */
const VM_MIGRATE: Action = { id: 'vmmigrate', label: 'Migrate', icon: 'forward', form: 'confirm' };

/** The KubeVirt kinds the lifecycle actions are offered on. */
export const VM_KINDS = ['crd:virtualmachines.kubevirt.io', 'crd:virtualmachineinstances.kubevirt.io'];

/**
 * What a virtual machine offers right now.
 *
 * Not a fixed list, because most of these are only meaningful in one state:
 * offering Stop on a stopped machine is a button that errors, and offering both
 * Pause and Resume is a bar that cannot say which one the machine needs.
 */
export function actionsForVM(state: { running: boolean; paused: boolean; migratable: boolean }): Action[] {
    if (!state.running) {
        // A machine with no guest can only be started. Restarting one is
        // starting it, and the word for that is Start.
        return [VM_START];
    }
    const out: Action[] = [state.paused ? VM_UNPAUSE : VM_PAUSE, VM_RESTART, VM_SOFT_REBOOT];
    if (state.migratable) out.push(VM_MIGRATE);
    out.push(VM_STOP);
    return out;
}

/**
 * The pods placed on a node.
 *
 * An action rather than a link on the node's name in the list. The name is how
 * a row's describe panel opens, in every table -- a kind that quietly meant
 * something else by it would be a different app per table -- and this belongs
 * next to Cordon and Drain anyway: "what is actually running here" is the
 * question asked immediately before both of them.
 */
const NODE_PODS: Action = { id: 'nodepods', label: 'Pods', icon: 'box', form: 'immediate' };
/** Its label follows the node: a cordoned one offers to be uncordoned instead. */
const CORDON: Action = { id: 'cordon', label: 'Cordon', icon: 'shield', form: 'immediate' };
// Draining moves every workload off a node. Routine, and never a single click.
const DRAIN: Action = { id: 'drain', label: 'Drain', icon: 'server', form: 'confirm' };
const DELETE: Action = { id: 'delete', label: 'Delete', icon: 'trash', form: 'confirm', tone: 'danger' };

/**
 * The three things that can be done to a Helm release.
 *
 * Values opens the release's user-supplied values in the dock, exactly as Edit
 * opens an object's YAML: the same gesture on the same editor. There is no
 * separate Upgrade button, and that is deliberate rather than an omission --
 * an upgrade is a chart, a version and a set of values applied together, so
 * splitting it into a button here and a document there would mean choosing a
 * version without seeing the values it applies to. The editor carries all
 * three and its save button is the upgrade.
 *
 * It needs no helm to open: reading the values is reading the release, which
 * the app does itself. Upgrading from it does, and the editor says so there.
 */
const VALUES: Action = { id: 'values', label: 'Values', icon: 'edit', form: 'immediate' };
/**
 * Back to an earlier revision, which asks which one.
 *
 * Unlike an upgrade it needs no chart: the revision being returned to stored
 * its own rendered manifest, so a release whose chart nobody can find any more
 * can still be rolled back.
 */
const ROLLBACK: Action = { id: 'rollback', label: 'Rollback', icon: 'undo', form: 'revision', needsHelm: true };
/**
 * Removing the release and everything it installed. Danger-toned and confirmed,
 * like Delete, and for more reason than Delete has: one release is many objects.
 */
const UNINSTALL: Action = {
    id: 'uninstall',
    label: 'Uninstall',
    icon: 'trash',
    form: 'confirm',
    tone: 'danger',
    needsHelm: true,
};

/**
 * The kinds that have logs: a pod, and the workloads whose own pods can be
 * found from a selector. Everything else either runs no containers or has no
 * way to say which pods are its.
 */
const LOGGABLE = [
    'pods',
    'deployments',
    'statefulsets',
    'daemonsets',
    'replicasets',
    'replicationcontrollers',
    'jobs',
    'cronjobs',
];

/**
 * The kinds a shell can be opened in.
 *
 * A pod runs containers, and a workload resolves to one of its pods -- the same
 * reach Logs has, and for the same reason. A node is here too and is a
 * different thing entirely: there is no exec against a machine, so it is a
 * privileged pod created on it and chrooted into its filesystem. See
 * internal/kube/nodeshell.go.
 */
const SHELLABLE = [
    'pods',
    'deployments',
    'statefulsets',
    'daemonsets',
    'replicasets',
    'replicationcontrollers',
    'jobs',
    'nodes',
];

/**
 * The kinds a port can be forwarded from.
 *
 * A Service is the common case and the one that needs the most work behind it:
 * its ports exist only in the cluster's routing, so forwarding one means
 * finding a pod behind it and the port on that pod the service port lands on.
 * A workload forwards through one of its pods, exactly as a shell does.
 */
const FORWARDABLE = [
    'pods',
    'services',
    'deployments',
    'statefulsets',
    'daemonsets',
    'replicasets',
    'replicationcontrollers',
];

/** The kinds that keep a rollout history to go back to. */
const UNDOABLE = ['deployments', 'statefulsets', 'daemonsets'];

/** The kinds with a spec.suspend. */
const SUSPENDABLE = ['cronjobs', 'jobs'];

/** The certificate signing requests, which are the kind with answers to give. */
export const CSRS = 'certificatesigningrequests';

/** The actions that only mean something while a request is unanswered. */
export const ANSWERS: ActionId[] = ['approve', 'deny'];

/** The kinds that carry a replica count, which is what Scale sets. */
const SCALABLE = ['deployments', 'statefulsets', 'replicasets', 'replicationcontrollers'];

/**
 * The kinds that own a pod template, which is what Restart stamps.
 *
 * ReplicaSets are deliberately absent although they have a template: the
 * Deployment above one owns it, and a restart stamped here would be undone by
 * the next reconcile.
 */
const ROLLABLE = ['deployments', 'statefulsets', 'daemonsets'];

/**
 * The views that are not objects at all: places in the app rather than things
 * in a cluster.
 *
 * Helm releases used to be here, and are not any more. A release is still not a
 * Kubernetes object -- it is a Secret the backend decodes -- which is why it
 * gets its own four actions below rather than Edit and Delete: deleting the
 * Secret would leave every object the release installed running, with nothing
 * left that knows they belong together.
 */
const NOT_AN_OBJECT = [DASHBOARD, SETTINGS, PORT_FORWARDS, ACCESS_OVERVIEW];

/** The actions offered for one kind, in the order the bar draws them. */
export function actionsFor(kind: string): Action[] {
    if (NOT_AN_OBJECT.includes(kind)) return [];
    if (kind === HELM_RELEASES) return [VALUES, ROLLBACK, UNINSTALL];

    const out: Action[] = [EDIT];
    if (LOGGABLE.includes(kind)) out.push(LOGS);
    if (SHELLABLE.includes(kind)) out.push(SHELL);
    if (FORWARDABLE.includes(kind)) out.push(FORWARD);
    if (SCALABLE.includes(kind)) out.push(SCALE);
    if (ROLLABLE.includes(kind)) out.push(RESTART);
    if (UNDOABLE.includes(kind)) out.push(UNDO);
    if (kind === 'deployments') out.push(PAUSE);
    if (kind === 'cronjobs') out.push(TRIGGER);
    if (SUSPENDABLE.includes(kind)) out.push(SUSPEND);
    if (kind === 'pods') out.push(EVICT);
    if (kind === CSRS) out.push(APPROVE, DENY);
    if (kind === 'nodes') out.push(NODE_PODS, CORDON, DRAIN);
    if (!NOT_COMPARABLE.includes(kind)) out.push(COMPARE);
    out.push(DELETE);
    return out;
}
