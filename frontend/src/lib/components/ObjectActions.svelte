<!--
  The bar of things you can do to the object the panel is describing.

  Which buttons appear is the catalogue's answer (../actions.ts) and what they
  do is the store's (../state/actions.svelte.ts). What is here is the middle:
  asking before the two that cannot be undone, holding the replica count while
  it is dragged, stepped or typed, and showing a drain as it works.

  A question replaces the bar rather than opening a dialog over it, so that
  "Delete web?" is read in the same place the button was pressed and cannot be
  mistaken for a question about something else.
-->
<script lang="ts">
    import { singularFor } from '../catalogue';
    import { ANSWERS, actionsFor, actionsForVM, type Action, type ActionId } from '../actions';
    import { actions, type DrainOptions, type RolloutRevision } from '../state/actions.svelte';
    import { forwards, type PortOption } from '../state/forwards.svelte';
    import { helm } from '../state/helm.svelte';
    import { session } from '../state/session.svelte';
    import { workspace, type DetailTarget } from '../state/workspace.svelte';
    import Icon from './Icon.svelte';
    import { notices } from '../state/notices.svelte';
    import { detail } from '../state/detail.svelte';
    import { PluginService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type { OfferedAction } from '../plugins/types';

    // Named `object` rather than `target`: `target` is one of Svelte's own
    // mount options, and a prop by that name is taken for the element to mount
    // into rather than passed through.
    let { object }: { object: DetailTarget } = $props();

    /**
     * A virtual machine's bar is built from what the machine is doing rather
     * than from its kind alone -- a stopped one offers Start, a paused one
     * Resume -- so the lifecycle buttons go in front of Edit and Delete once
     * the cluster has answered. Until it has, the bar is the ordinary two.
     */
    /**
     * What each lifecycle operation is called once it has been asked for.
     *
     * The past tense of the button is not always the button: pressing Migrate
     * asks for a migration rather than completing one, and saying "migrated"
     * would claim a guest had moved when it has only started moving.
     */
    const VM_DONE: Record<string, string> = {
        vmstart: 'starting',
        vmstop: 'stopping',
        vmrestart: 'restarting',
        vmpause: 'paused',
        vmunpause: 'resumed',
        vmsoftreboot: 'rebooting',
        vmmigrate: 'migrating',
    };

    let facts = $derived(actions.stateOf(object));
    /**
     * A plugin from outside the app that brings its own buttons for this kind
     * takes over from the app's built-in product buttons for it, rather than
     * the bar carrying two Starts.
     */
    let pluginOwnsKind = $derived(workspace.pluginActsOn(object.contextId, object.kind, { external: true }));
    /**
     * Forward is left out of the web version, where the port it opens would be
     * one on the server rather than on the machine the user is sitting at. A
     * signing request's answers are left out once it has one: approving a
     * request that was denied is refused by the API server, and offering it
     * would be a button that only ever fails.
     */
    let available = $derived(
        (facts.vm.isMachine && !pluginOwnsKind
            ? [...actionsForVM(facts.vm), ...actionsFor(object.kind)]
            : actionsFor(object.kind)
        ).filter(
            (action) =>
                !(session.server && action.id === 'forward') && !(ANSWERS.includes(action.id) && !facts.pending),
        ),
    );

    // ----- buttons from plugins ----------------------------------------------

    /**
     * What plugins offer on this object right now. The backend reads the
     * object and leaves out any whose conditions it does not meet, so a
     * stopped machine offers Start and a running one Pause.
     */
    let offered = $state<OfferedAction[]>([]);
    /** The plugin action waiting on its confirmation, if any. */
    let askingPlugin = $state<OfferedAction | null>(null);
    let hasPluginActions = $derived(workspace.pluginActsOn(object.contextId, object.kind));

    async function loadOffered(ref: DetailTarget): Promise<void> {
        try {
            const got = (await PluginService.ObjectActions(ref.contextId, ref.kind, ref.namespace, ref.name)) ?? [];
            // The panel may have moved to another object while this was asked.
            if (ref.contextId !== object.contextId || ref.name !== object.name || ref.kind !== object.kind) return;
            offered = got as OfferedAction[];
        } catch {
            // A read that failed leaves the buttons as they were: the next
            // poll will try again, and the describe report says what is wrong.
        }
    }

    // Read on every new object, and again every few seconds, because what a
    // plugin offers follows the object's state and that state moves by itself
    // -- a machine that was Starting is Running a moment later.
    $effect(() => {
        const ref = { ...object };
        offered = [];
        askingPlugin = null;
        if (!hasPluginActions) return;
        void loadOffered(ref);
        const timer = setInterval(() => void loadOffered(ref), 5000);
        return () => clearInterval(timer);
    });

    function choosePlugin(action: OfferedAction): void {
        if (action.confirm) {
            askingPlugin = action;
            return;
        }
        void runPlugin(action);
    }

    async function runPlugin(action: OfferedAction): Promise<void> {
        busy = true;
        const ref = { ...object };
        try {
            const created = await PluginService.RunAction(
                ref.contextId,
                action.pluginId,
                action.id,
                ref.namespace,
                ref.name,
            );
            const said = action.done || `${action.label}: ${ref.name}`;
            notices.inform(created ? `${said} — created ${created}` : said);
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            askingPlugin = null;
            busy = false;
            // Straight away and once more shortly, so the bar moves on as soon
            // as the cluster does rather than on the next poll.
            void loadOffered(ref);
            setTimeout(() => void loadOffered(ref), 1500);
        }
    }
    let drain = $derived(actions.drainOf(object));

    /**
     * Whether the actions that run helm can be offered.
     *
     * Reading a release needs nothing installed -- it is a Secret the app
     * decodes -- but rolling one back or uninstalling it is Helm's own
     * operation. A button that cannot work is disabled with the reason on it
     * rather than left to fail when it is pressed. See internal/helmcli.
     */
    let helmMissing = $derived(helm.probed && !helm.tool.found);

    /** The release's revisions, for the rollback picker. */
    let revisions = $derived(helm.stateOf(object).detail?.revisions ?? []);
    /** Which revision the picker is on. Null until the release has been read. */
    let revision = $state<number | null>(null);
    /** Whether an uninstall keeps the release's records, so it can be rolled back. */
    let keepHistory = $state(false);

    /**
     * A workload's rollout history, read when its Rollback is pressed rather
     * than with the object: it is a listing of ReplicaSets or revisions, and
     * most panels are opened without anyone wanting it.
     */
    let history = $state<RolloutRevision[]>([]);
    let historyError = $state('');
    let loadingHistory = $state(false);
    /** Which revision the workload's picker is on. */
    let undoTo = $state<number | null>(null);
    let undoChoice = $derived(history.find((r) => r.revision === undoTo) ?? null);

    /**
     * What a drain is told beyond kubectl's defaults: the flags k9s's drain
     * dialog offers, plus kubectl's --pod-selector and --disable-eviction.
     * Opened on the defaults every time, so a Force ticked for one node is not
     * quietly carried to the next.
     *
     * The two numbers are nullable because that is what a number field bound
     * to an empty box holds, and empty is the answer that means "the default":
     * each pod's own grace period, and no timeout.
     */
    let drainFlags = $state({ deleteEmptyDirData: false, force: false, disableEviction: false, podSelector: '' });
    let gracePeriod = $state<number | null>(null);
    let drainTimeout = $state<number | null>(null);
    /** Set when the drain would destroy something the defaults would leave alone. */
    let drainRisky = $derived(drainFlags.force || drainFlags.deleteEmptyDirData || drainFlags.disableEviction);

    /** The action waiting on an answer -- a confirmation, a number, a port -- if any. */
    let asking = $state<ActionId | null>(null);
    let busy = $state(false);

    /**
     * The replica count being chosen. Nullable because that is what a number
     * field bound to an empty box holds.
     */
    let replicas = $state<number | null>(0);
    /**
     * Where the scale slider ends. Fixed when the form opens -- double what is
     * running, rounded up to five and never under ten -- so the track does not
     * rescale under the pointer mid-drag. Typing past it moves the end out to
     * the typed number rather than refusing it.
     */
    let scaleCeiling = $state(10);
    let sliderMax = $derived(Math.max(scaleCeiling, replicas ?? 0));
    let sliderEl = $state<HTMLInputElement | null>(null);
    /** The chosen count as the cluster would take it, or null for anything it would not. */
    let wanted = $derived(replicas !== null && Number.isInteger(replicas) && replicas >= 0 ? replicas : null);
    /** Where the slider sits: the chosen count, or what is running while the box holds nonsense. */
    let shownReplicas = $derived(wanted ?? facts.replicas);
    let scaleChange = $derived(wanted === null ? 0 : wanted - facts.replicas);
    /** Scaling to what is already running is a write that changes nothing. */
    let canScale = $derived(!busy && wanted !== null && wanted !== facts.replicas);

    // A range input clamps a value set before its max has caught up, which is
    // what typing past the end does: the number and the end move together.
    $effect(() => {
        if (!sliderEl) return;
        sliderEl.max = String(sliderMax);
        sliderEl.value = String(shownReplicas);
    });

    function openScale(): void {
        replicas = facts.replicas;
        scaleCeiling = Math.max(10, Math.ceil((facts.replicas * 2) / 5) * 5);
    }

    function stepReplicas(by: number): void {
        replicas = Math.max(0, shownReplicas + by);
    }

    /** Enter in the scale form applies it, as it would in any one-field form. */
    function applyOnEnter(event: KeyboardEvent): void {
        if (event.key !== 'Enter' || !canScale || wanted === null) return;
        event.preventDefault();
        void perform('scale', wanted);
    }

    /**
     * What a forward could be opened on, read from the object when the form is
     * opened. A pod or a workload answers with its container ports, a service
     * with its own -- which are not the same thing, and the difference is why
     * this is a list from the cluster rather than a number field.
     */
    let ports = $state<PortOption[]>([]);
    let portsError = $state('');
    let loadingPorts = $state(false);
    /**
     * The chosen remote port, and the local one.
     *
     * Both are nullable because that is what a number field bound to an empty
     * box holds, and for the local one empty is a real answer: it means "any
     * free port", which is what the app asks for unless told otherwise.
     */
    let remotePort = $state<number | null>(null);
    let localPort = $state<number | null>(null);
    let openBrowser = $state(true);
    /** The confirmation's safe button, focused so a stray Enter cannot destroy. */
    let cancelEl = $state<HTMLButtonElement | null>(null);

    /** What the object is, for the questions: "pod web", "node wrkr01". */
    let subject = $derived(`${singularFor(object.kind)} ${object.name}`);

    /**
     * The same object as the lifecycle questions name it.
     *
     * Not `subject`, which for these kinds reads "Virtualmachineinstance web" --
     * a plural turned singular by dropping a letter, which is the right answer
     * for a custom resource nobody has a word for and the wrong one here. The
     * lifecycle actions run against the VirtualMachine either way (a running
     * instance carries its machine's name), so both kinds are one machine to
     * these questions.
     */
    let machine = $derived(`virtual machine ${object.name}`);

    // A new object is a new bar. Read what its buttons need to say, and drop
    // any half-asked question belonging to the object we have left.
    $effect(() => {
        const ref = { ...object };
        asking = null;
        ports = [];
        portsError = '';
        revision = null;
        keepHistory = false;
        history = [];
        historyError = '';
        undoTo = null;
        void actions.load(ref);
    });

    // Where helm is, asked once. Only the release bar needs it, so an object's
    // never makes the call.
    $effect(() => {
        if (available.some((a) => a.needsHelm) && !helm.probed) void helm.probe();
    });

    // The revision to roll back to, defaulted to the one before the current --
    // which is what "roll back" means when nobody says otherwise.
    $effect(() => {
        if (revision === null && revisions.length > 1) {
            revision = revisions.find((r) => !r.current)?.revision ?? null;
        }
    });

    // Focus the safe answer as soon as a question appears. The replica count
    // is not a question, and its slider takes the focus instead, so the arrow
    // keys move it straight away.
    $effect(() => {
        if (asking === 'scale') sliderEl?.focus();
        else if (asking || askingPlugin) cancelEl?.focus();
    });

    /**
     * Cordon is the one button whose label is the cluster's answer rather than
     * ours: offering to cordon a node that is already cordoned is a button that
     * does nothing.
     */
    function labelOf(action: Action): string {
        if (action.id === 'cordon') return facts.cordoned ? 'Uncordon' : 'Cordon';
        // Suspend and Pause follow the object the same way: a suspended job
        // offers to resume, not to be suspended again.
        if (action.id === 'suspend') return facts.suspended ? 'Resume' : 'Suspend';
        if (action.id === 'pause') return facts.paused ? 'Resume rollout' : 'Pause rollout';
        return action.label;
    }

    /** The icon follows the label, for the two whose label changes meaning. */
    function iconOf(action: Action): string {
        if (action.id === 'suspend' && facts.suspended) return 'play';
        if (action.id === 'pause' && facts.paused) return 'play';
        return action.icon;
    }

    function choose(action: Action): void {
        if (action.id === 'edit') {
            workspace.openEditor(object);
            return;
        }
        if (action.id === 'values') {
            workspace.openHelmValues(object);
            return;
        }
        if (action.form === 'revision') {
            asking = action.id;
            // The revisions come from the drawer's read of the release, which
            // is normally already done by the time anyone reaches this button.
            // Normally is not always -- the bar and the drawer mount together
            // and this is one click away -- so a picker with nothing in it asks
            // for itself rather than claiming the release has no history.
            if (revisions.length === 0) void helm.load(object);
            return;
        }
        if (action.id === 'logs') {
            workspace.openLogs(object);
            return;
        }
        if (action.id === 'shell') {
            workspace.openShell(object);
            return;
        }
        if (action.id === 'nodepods') {
            workspace.showPodsOnNode(object.contextId, object.name);
            return;
        }
        if (action.form === 'history') {
            asking = action.id;
            void loadHistory();
            return;
        }
        if (action.form === 'ports') {
            asking = action.id;
            void loadPorts();
            return;
        }
        if (action.form === 'immediate') {
            void perform(action.id);
            return;
        }
        if (action.form === 'number') openScale();
        if (action.id === 'drain') resetDrain();
        asking = action.id;
    }

    /**
     * Reads the workload's revisions for the rollback picker, defaulting to the
     * one before the current -- which is what "roll back" means when nobody
     * says otherwise, and what `kubectl rollout undo` does.
     */
    async function loadHistory(): Promise<void> {
        loadingHistory = true;
        historyError = '';
        const ref = { ...object };
        try {
            const got = await actions.history(ref);
            if (ref.name !== object.name || ref.kind !== object.kind) return;
            history = got;
            undoTo = got.find((r) => !r.current)?.revision ?? null;
        } catch (err) {
            historyError = err instanceof Error ? err.message : String(err);
        } finally {
            loadingHistory = false;
        }
    }

    /** How one revision reads in the workload's picker. */
    function historyLabel(entry: RolloutRevision): string {
        const parts = [`Revision ${entry.revision}`];
        if (entry.images.length > 0) parts.push(entry.images.join(', '));
        if (entry.age) parts.push(`${entry.age} ago`);
        return parts.join(' · ');
    }

    function resetDrain(): void {
        drainFlags = { deleteEmptyDirData: false, force: false, disableEviction: false, podSelector: '' };
        gracePeriod = null;
        drainTimeout = null;
    }

    /** The drain form as the backend takes it. */
    function drainOptions(): DrainOptions {
        return {
            ...drainFlags,
            podSelector: drainFlags.podSelector.trim(),
            gracePeriodSeconds: gracePeriod,
            timeoutSeconds: drainTimeout ?? 0,
        };
    }

    /**
     * Opens the drain question again with the boxes ticked that would move
     * what the last drain left behind. Each refusal names the option that
     * would have moved its pod, so "why was this left" is one click from "move
     * it anyway" -- ticked rather than run, because both options destroy
     * something, and the question still has to be answered.
     */
    function drainAgain(): void {
        const needed = new Set(drain?.refused.map((r) => r.option));
        resetDrain();
        drainFlags.deleteEmptyDirData = needed.has('deleteEmptyDirData');
        drainFlags.force = needed.has('force');
        asking = 'drain';
    }

    /**
     * Runs one action and says how it went.
     *
     * The API server's own words are what a failure reports: a denied action
     * says which verb on which resource was refused, which is more use than
     * anything this component could write.
     */
    async function perform(id: ActionId, value = 0): Promise<void> {
        busy = true;
        try {
            switch (id) {
                case 'delete':
                    await actions.remove(object);
                    notices.inform(`${subject} deleted`);
                    // Nothing is left to describe.
                    detail.close();
                    return;
                case 'scale':
                    await actions.scale(object, value);
                    notices.inform(`${subject} scaled to ${value}`);
                    break;
                case 'restart':
                    await actions.restart(object);
                    notices.inform(`${subject} restarting`);
                    break;
                case 'cordon':
                    await actions.cordon(object, !facts.cordoned);
                    notices.inform(`${subject} ${facts.cordoned ? 'cordoned' : 'uncordoned'}`);
                    break;
                case 'suspend':
                    await actions.suspend(object, !facts.suspended);
                    notices.inform(`${subject} ${facts.suspended ? 'suspended' : 'resumed'}`);
                    break;
                case 'pause':
                    await actions.pause(object, !facts.paused);
                    notices.inform(`${subject} rollout ${facts.paused ? 'paused' : 'resumed'}`);
                    break;
                case 'trigger': {
                    const job = await actions.trigger(object);
                    notices.inform(`${subject} started job ${job}`);
                    break;
                }
                case 'evict':
                    await actions.evict(object);
                    notices.inform(`${subject} evicted`);
                    // The pod is on its way out, exactly as after a delete.
                    detail.close();
                    return;
                case 'approve':
                case 'deny':
                    await actions.answer(object, id === 'approve');
                    notices.inform(`${subject} ${id === 'approve' ? 'approved' : 'denied'}`);
                    break;
                case 'undo':
                    await actions.undo(object, value);
                    notices.inform(`${subject} rolling back to revision ${value}`);
                    break;
                case 'drain':
                    await actions.drain(object, drainOptions());
                    break;
                case 'vmstart':
                case 'vmstop':
                case 'vmrestart':
                case 'vmpause':
                case 'vmunpause':
                case 'vmsoftreboot':
                case 'vmmigrate': {
                    // The backend's own name for the operation, which is the
                    // action id without the prefix the UI needs to keep its
                    // ids apart from a workload's Restart.
                    const op = id.slice('vm'.length);
                    await actions.vmOperation(object, op);
                    notices.inform(`${subject} ${VM_DONE[id]}`);
                    break;
                }
                case 'rollback':
                    await helm.rollback(object, value);
                    notices.inform(`${object.name} rolled back to revision ${value}`);
                    break;
                case 'uninstall':
                    await helm.uninstall(object, keepHistory);
                    notices.inform(`${object.name} uninstalled`);
                    // Nothing is left to describe, exactly as after a delete.
                    detail.close();
                    return;
            }
            asking = null;
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
            asking = null;
        } finally {
            busy = false;
        }
    }

    /**
     * Reads the ports this object could be forwarded from.
     *
     * A failure is shown in the form rather than swallowed: "this service
     * selects no pods" is the answer to why there is nothing to choose, and it
     * is more use than an empty list.
     */
    async function loadPorts(): Promise<void> {
        loadingPorts = true;
        portsError = '';
        try {
            const found = await forwards.ports(object);
            ports = found;
            // The first port is the one almost always wanted, and a form that
            // opens on a chosen value is one field shorter to fill in.
            remotePort = found[0]?.port ?? null;
            localPort = null;
            openBrowser = true;
        } catch (err) {
            portsError = err instanceof Error ? err.message : String(err);
        } finally {
            loadingPorts = false;
        }
    }

    /**
     * Opens the forward the form describes.
     *
     * The local port is left empty by default, which means "any free one" --
     * the port is normally reached by the link the app puts beside it, and
     * choosing one by hand only matters when something else already expects it.
     */
    async function forward(): Promise<void> {
        const remote = remotePort ?? 0;
        // Empty means "any free port", which is the whole reason this field is
        // allowed to be empty.
        const local = localPort ?? 0;
        if (remote <= 0 || remote > 65535) {
            notices.fail(`${remote} is not a port`);
            return;
        }
        if (local < 0 || local > 65535) {
            notices.fail(`${local} is not a port`);
            return;
        }

        busy = true;
        try {
            const opened = await forwards.start(object, remote, local, openBrowser);
            notices.inform(
                `Forwarding localhost:${opened.localPort} to ${object.name} on ${remote}`,
            );
            asking = null;
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            busy = false;
        }
    }

    /** How one port reads in the picker: its number, name and where it lands. */
    function portLabel(port: PortOption): string {
        const parts = [String(port.port)];
        if (port.name) parts.push(port.name);
        if (port.target && port.target !== String(port.port)) parts.push(`→ ${port.target}`);
        if (port.protocol && port.protocol !== 'TCP') parts.push(port.protocol);
        return parts.join(' · ');
    }

    /**
     * The question each asked-for action puts.
     *
     * Every branch names its own action. There is deliberately no "and
     * otherwise Delete" here: an action added to the catalogue with
     * form: 'confirm' and no question written for it would inherit that
     * default and ask to delete something it was never going to delete,
     * which is the worst way for this component to be wrong. An unwritten
     * question asks in the button's own words instead.
     */
    function question(id: ActionId): string {
        switch (id) {
            case 'delete':
                return `Delete ${subject}?`;
            case 'drain':
                return `Drain ${subject}? Everything running on it will be moved.`;
            // One release is many objects, which is what makes this worth
            // spelling out rather than asking "Uninstall X?".
            case 'uninstall':
                return `Uninstall ${object.name}? Everything the release installed will be removed.`;
            // An eviction asks the disruption budgets, which is the whole
            // difference from Delete and the thing worth saying.
            case 'evict':
                return `Evict ${subject}? It is stopped and replaced by whatever manages it, unless a disruption budget refuses.`;
            case 'approve':
                return `Approve ${subject}? The signer will issue a certificate the cluster trusts, and this cannot be taken back.`;
            case 'deny':
                return `Deny ${subject}? No certificate will be issued, and this cannot be taken back.`;

            // The machine ones say what happens to the guest, because that is
            // the part that is not obvious from the button. A virtual machine
            // is not a pod: nothing reschedules it, and whatever is running
            // inside was not written to be killed and replaced.
            case 'vmstop':
                return `Stop ${machine}? The guest is powered off — anything running inside it stops.`;
            case 'vmrestart':
                return `Restart ${machine}? The guest is powered off and started again, without being asked to shut down first.`;
            case 'vmsoftreboot':
                return `Reboot ${machine}? The guest is asked to shut itself down and start again, which needs the guest agent.`;
            case 'vmmigrate':
                return `Migrate ${machine} to another node? The guest keeps running while it moves.`;
        }
        // Nothing reaches here today. If something does, it asks for itself.
        const action = available.find((a) => a.id === id);
        return `${action?.label ?? id} ${subject}?`;
    }

    /** How one revision reads in the rollback picker. */
    function revisionLabel(entry: (typeof revisions)[number]): string {
        const parts = [`Revision ${entry.revision}`, entry.chart];
        if (entry.description) parts.push(entry.description);
        return parts.join(' · ');
    }

    let asked = $derived(available.find((a) => a.id === asking) ?? null);
</script>

<svelte:document
    onkeydown={(e) => {
        if (e.key !== 'Escape') return;
        if (asking) asking = null;
        if (askingPlugin) askingPlugin = null;
    }}
/>

{#if available.length > 0 || offered.length > 0}
    <div class="bar" class:stacked={asked?.id === 'drain' || asked?.form === 'number'}>
        {#if askingPlugin}
            {@const a = askingPlugin}
            <p class="question">{a.confirm}</p>
            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (askingPlugin = null)}>Cancel</button>
                <button class="go" class:danger={a.tone === 'danger'} disabled={busy} onclick={() => runPlugin(a)}>
                    {a.label}
                </button>
            </div>
        {:else if asked && asked.form === 'confirm'}
            <p class="question">{question(asked.id)}</p>
            {#if asked.id === 'uninstall'}
                <!-- Keeping the history leaves the release listed as
                     "uninstalled" and still rollable-back, which is the only
                     way an uninstall is undoable at all. -->
                <label class="check">
                    <input type="checkbox" bind:checked={keepHistory} />
                    Keep the history, so it can be rolled back
                </label>
            {/if}
            {#if asked.id === 'drain'}
                <!-- kubectl drain's flags. The three boxes each give up a
                     protection the defaults keep, so each says which; ticking
                     any of them turns the answer red. -->
                <div class="options">
                    <label class="check">
                        <input type="checkbox" bind:checked={drainFlags.deleteEmptyDirData} />
                        Delete emptyDir data
                    </label>
                    <label class="check">
                        <input type="checkbox" bind:checked={drainFlags.force} />
                        Force — delete pods nothing manages
                    </label>
                    <label class="check">
                        <input type="checkbox" bind:checked={drainFlags.disableEviction} />
                        Delete instead of evict — skips disruption budgets
                    </label>
                    <label class="field">
                        Grace period (s)
                        <input type="number" min="0" step="1" placeholder="pod's own" bind:value={gracePeriod} />
                    </label>
                    <label class="field">
                        Timeout (s)
                        <input type="number" min="0" step="1" placeholder="none" bind:value={drainTimeout} />
                    </label>
                    <label class="field">
                        Pod selector
                        <input
                            type="text"
                            placeholder="app=web"
                            spellcheck="false"
                            autocomplete="off"
                            bind:value={drainFlags.podSelector}
                        />
                    </label>
                </div>
            {/if}
            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (asking = null)}>Cancel</button>
                <button
                    class="go"
                    class:danger={asked.tone === 'danger' || (asked.id === 'drain' && drainRisky)}
                    disabled={busy}
                    onclick={() => perform(asked.id)}
                >
                    {asked.label}
                </button>
            </div>
        {:else if asked && asked.form === 'ports'}
            {#if loadingPorts}
                <p class="question">Reading {subject}'s ports…</p>
            {:else if portsError}
                <p class="question failed">{portsError}</p>
            {:else if ports.length === 0}
                <p class="question">
                    {subject} declares no ports. You can still forward one by typing it.
                </p>
            {/if}

            {#if !loadingPorts && !portsError}
                <label class="field">
                    Port
                    {#if ports.length > 0}
                        <select bind:value={remotePort}>
                            {#each ports as port (port.port + port.name)}
                                <option value={port.port}>{portLabel(port)}</option>
                            {/each}
                        </select>
                    {:else}
                        <input type="number" min="1" max="65535" bind:value={remotePort} />
                    {/if}
                </label>

                <label class="field">
                    Local
                    <input
                        type="number"
                        min="0"
                        max="65535"
                        placeholder="any free port"
                        bind:value={localPort}
                    />
                </label>

                <label class="check">
                    <input type="checkbox" bind:checked={openBrowser} />
                    Open a browser
                </label>
            {/if}

            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (asking = null)}>Cancel</button>
                <button
                    class="go"
                    disabled={busy || loadingPorts || !remotePort || remotePort <= 0}
                    onclick={() => forward()}
                >
                    Forward
                </button>
            </div>
        {:else if asked && asked.form === 'revision'}
            {#if revisions.length < 2}
                <p class="question">
                    {object.name} has only the revision it is on — there is nothing behind it to
                    go back to.
                </p>
            {:else}
                <label class="field">
                    Roll back to
                    <select bind:value={revision}>
                        {#each revisions.filter((r) => !r.current) as entry (entry.revision)}
                            <option value={entry.revision}>{revisionLabel(entry)}</option>
                        {/each}
                    </select>
                </label>
            {/if}

            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (asking = null)}>Cancel</button>
                <button
                    class="go"
                    disabled={busy || revision === null}
                    onclick={() => perform('rollback', revision ?? 0)}
                >
                    Rollback
                </button>
            </div>
        {:else if asked && asked.form === 'history'}
            {#if loadingHistory}
                <p class="question">Reading {subject}'s history…</p>
            {:else if historyError}
                <p class="question failed">{historyError}</p>
            {:else if history.length < 2}
                <p class="question">
                    {subject} has only the revision it is on — there is nothing behind it to go back to.
                </p>
            {:else}
                <label class="field">
                    Roll back to
                    <select bind:value={undoTo}>
                        {#each history.filter((r) => !r.current) as entry (entry.revision)}
                            <option value={entry.revision}>{historyLabel(entry)}</option>
                        {/each}
                    </select>
                </label>
                {#if undoChoice?.changeCause}
                    <span class="cause" title="The change cause recorded for this revision">{undoChoice.changeCause}</span>
                {/if}
            {/if}

            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (asking = null)}>Cancel</button>
                <button
                    class="go"
                    disabled={busy || loadingHistory || undoTo === null}
                    onclick={() => perform('undo', undoTo ?? 0)}
                >
                    Rollback
                </button>
            </div>
        {:else if asked && asked.form === 'number'}
            <!-- Drag, step or type: all three move the one count. The tick
                 under the track marks what is running now, so how far the
                 count has been moved from it can be seen as well as read. -->
            <div class="scale-head">
                <span class="scale-title">Scale {subject}</span>
                <span
                    class="scale-change"
                    class:up={scaleChange > 0}
                    class:down={scaleChange < 0}
                    class:stops={wanted === 0 && facts.replicas > 0}
                    aria-live="polite"
                >
                    {#if wanted === null}
                        Not a replica count
                    {:else if scaleChange === 0}
                        {facts.replicas} running
                    {:else}
                        {facts.replicas} → {wanted} ({scaleChange > 0 ? '+' : '−'}{Math.abs(scaleChange)}){wanted === 0
                            ? ' — stops every pod'
                            : ''}
                    {/if}
                </span>
            </div>
            <div class="scale-row">
                <button
                    class="step"
                    aria-label="One fewer replica"
                    title="One fewer"
                    disabled={busy || shownReplicas <= 0}
                    onclick={() => stepReplicas(-1)}
                >
                    <Icon name="minus" size={12} />
                </button>
                <div class="scalebar" style:--now={facts.replicas / sliderMax} style:--at={shownReplicas / sliderMax}>
                    <input
                        bind:this={sliderEl}
                        class="slider"
                        type="range"
                        min="0"
                        max={sliderMax}
                        step="1"
                        value={shownReplicas}
                        aria-label="Replica count"
                        aria-valuetext="{shownReplicas} replicas"
                        disabled={busy}
                        oninput={(e) => (replicas = e.currentTarget.valueAsNumber)}
                        onkeydown={applyOnEnter}
                    />
                    <span class="now" title="{facts.replicas} running now" aria-hidden="true"></span>
                    <span class="ends" aria-hidden="true"><span>0</span><span>{sliderMax}</span></span>
                </div>
                <button
                    class="step"
                    aria-label="One more replica"
                    title="One more"
                    disabled={busy}
                    onclick={() => stepReplicas(1)}
                >
                    <Icon name="plus" size={12} />
                </button>
                <label class="count">
                    Replicas
                    <input type="number" min="0" step="1" disabled={busy} bind:value={replicas} onkeydown={applyOnEnter} />
                </label>
            </div>
            <div class="answers">
                <button bind:this={cancelEl} class="plain" onclick={() => (asking = null)}>Cancel</button>
                <button class="go" disabled={!canScale} onclick={() => perform('scale', wanted ?? 0)}>Apply</button>
            </div>
        {:else}
            {#each available.filter((a) => a.tone !== 'danger') as action (action.id)}
                {@render builtinButton(action)}
            {/each}
            <!-- A plugin's buttons after the app's own, and before the one that
                 cannot be undone, each saying which plugin it is from. -->
            {#if offered.length > 0 && available.some((a) => a.tone !== 'danger')}
                <span class="sep" aria-hidden="true"></span>
            {/if}
            {#each offered as action (action.pluginId + '/' + action.id)}
                <button
                    class:danger={action.tone === 'danger'}
                    disabled={busy}
                    title="{action.label} — from the {action.pluginName} plugin"
                    onclick={() => choosePlugin(action)}
                >
                    <Icon name={action.icon} size={13} />
                    {action.label}
                </button>
            {/each}
            {#each available.filter((a) => a.tone === 'danger') as action (action.id)}
                {@render builtinButton(action)}
            {/each}
        {/if}
    </div>

    {#if drain}
        <!-- A drain outlives the click that started it, so it reports under the
             bar rather than in a notice that would have gone by the time the
             first pod is evicted. -->
        <div class="drain" class:failed={drain.error !== ''}>
            <div class="line">
                {#if drain.error}
                    <span class="what">Drain failed: {drain.error}</span>
                {:else if drain.done}
                    <span class="what">Drained — {drain.evicted} of {drain.total} pods moved</span>
                {:else if drain.phase === 'evicting'}
                    <span class="what">Draining — {drain.evicted} of {drain.total} pods moved</span>
                {:else}
                    <span class="what">Draining — {drain.phase}</span>
                {/if}

                {#if !drain.done}
                    <button class="plain" onclick={() => actions.cancelDrain(object)}>Stop</button>
                {/if}
            </div>

            {#if drain.total > 0 && !drain.error}
                <div class="track" role="progressbar" aria-valuenow={drain.evicted} aria-valuemin={0} aria-valuemax={drain.total}>
                    <div class="fill" style:width="{(drain.evicted / drain.total) * 100}%"></div>
                </div>
            {/if}

            {#if drain.refused.length > 0}
                <ul class="refused">
                    {#each drain.refused as refusal (refusal.pod.namespace + refusal.pod.name)}
                        <li>
                            <strong>{refusal.pod.namespace}/{refusal.pod.name}</strong>
                            — left behind: {refusal.reason}
                        </li>
                    {/each}
                </ul>
                {#if drain.done && drain.refused.some((r) => r.option)}
                    <button class="again" onclick={drainAgain}>Drain these too…</button>
                {/if}
            {/if}
        </div>
    {/if}
{/if}

{#snippet builtinButton(action: Action)}
    <button
        class:danger={action.tone === 'danger'}
        class:last={action.tone === 'danger'}
        disabled={busy || (action.needsHelm === true && helmMissing)}
        title={action.needsHelm === true && helmMissing ? helm.tool.reason : labelOf(action)}
        onclick={() => choose(action)}
    >
        <Icon name={iconOf(action)} size={13} />
        {labelOf(action)}
    </button>
{/snippet}

<style>
    .sep {
        width: 1px;
        height: 16px;
        margin: 0 2px;
        background: var(--border);
        flex: 0 0 auto;
    }

    /* Wraps rather than overflowing: a Deployment offers nine buttons, and a
       narrow pane has room for five. */
    .bar {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 6px;
        padding: 8px 12px;
        border-bottom: 1px solid var(--border);
        flex: 0 0 auto;
        min-height: 40px;
    }

    button {
        display: flex;
        align-items: center;
        gap: 6px;
        height: 24px;
        padding: 0 10px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 12px;
        color: var(--text);
        white-space: nowrap;
    }

    button:hover:not(:disabled) {
        background: var(--bg-hover);
    }

    button:disabled {
        opacity: 0.5;
    }

    /* The one that cannot be undone is pushed to the far end and coloured
       apart, so it is never the button next to the one you meant. */
    .last {
        margin-left: auto;
    }

    .danger {
        color: var(--error);
    }

    .danger:hover:not(:disabled) {
        background: color-mix(in srgb, var(--error) 16%, transparent);
    }

    .plain {
        background: none;
        box-shadow: none;
        color: var(--text-dim);
    }

    .question {
        margin: 0;
        font-size: 12px;
        color: var(--text);
        min-width: 0;
        overflow-wrap: anywhere;
    }

    .answers {
        display: flex;
        gap: 6px;
        margin-left: auto;
        flex: 0 0 auto;
    }

    .field {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 12px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .field select,
    .field input {
        height: 24px;
        padding: 0 6px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font: inherit;
        font-size: 12px;
        max-width: 170px;
    }

    .cause {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .check {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 12px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .question.failed {
        color: var(--error);
    }

    /* The drain question carries a form, which does not fit the bar's one
       line: the question, the options under it, the answers under those. */
    .bar.stacked {
        flex-wrap: wrap;
        row-gap: 8px;
    }

    .bar.stacked .question {
        flex: 1 1 100%;
    }

    .options {
        flex: 1 1 100%;
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px 16px;
    }

    .options .field input[type='number'] {
        width: 90px;
    }

    .again {
        margin-top: 8px;
    }

    /* ----- the scale form: a heading, the slider row, the answers ----- */

    .scale-head {
        flex: 1 1 100%;
        display: flex;
        align-items: baseline;
        gap: 8px;
        font-size: 12px;
    }

    .scale-title {
        min-width: 0;
        color: var(--text);
        overflow-wrap: anywhere;
    }

    .scale-change {
        margin-left: auto;
        color: var(--text-dim);
        font-variant-numeric: tabular-nums;
        white-space: nowrap;
    }

    .scale-change.up {
        color: var(--ok);
    }

    .scale-change.down {
        color: var(--warn);
    }

    .scale-change.stops {
        color: var(--error);
    }

    /* Room underneath for the 0 and max labels, which hang below the track
       so the track itself lines up with the buttons either side of it. */
    .scale-row {
        flex: 1 1 100%;
        display: flex;
        align-items: center;
        gap: 8px;
        padding-bottom: 10px;
    }

    .step {
        flex: 0 0 auto;
        width: 24px;
        padding: 0;
        justify-content: center;
    }

    .scalebar {
        --thumb: 14px;
        position: relative;
        flex: 1 1 auto;
        min-width: 96px;
    }

    /* Where along the track a count sits: the thumb's centre, which travels
       half a thumb in from either end rather than the whole width. */
    .slider,
    .now {
        --along: calc(var(--thumb) / 2 + (100% - var(--thumb)) * var(--at));
    }

    .slider {
        display: block;
        width: 100%;
        height: 18px;
        margin: 0;
        background: transparent;
        appearance: none;
        -webkit-appearance: none;
        cursor: pointer;
    }

    .slider:disabled {
        cursor: default;
        opacity: 0.5;
    }

    .slider:focus-visible {
        outline: none;
    }

    .slider::-webkit-slider-runnable-track {
        height: 4px;
        border-radius: 2px;
        background: linear-gradient(to right, var(--accent) var(--along), var(--bg-active) 0);
    }

    .slider::-webkit-slider-thumb {
        appearance: none;
        -webkit-appearance: none;
        width: var(--thumb);
        height: var(--thumb);
        margin-top: calc((4px - var(--thumb)) / 2);
        border-radius: 50%;
        background: var(--bg-raised);
        box-shadow:
            0 0 0 1.5px var(--accent),
            0 1px 2px rgb(0 0 0 / 0.25);
        cursor: grab;
    }

    .slider:active::-webkit-slider-thumb {
        cursor: grabbing;
    }

    .slider:focus-visible::-webkit-slider-thumb {
        box-shadow:
            0 0 0 1.5px var(--accent),
            0 0 0 4px color-mix(in srgb, var(--accent) 30%, transparent);
    }

    .slider::-moz-range-track {
        height: 4px;
        border-radius: 2px;
        background: var(--bg-active);
    }

    .slider::-moz-range-progress {
        height: 4px;
        border-radius: 2px;
        background: var(--accent);
    }

    .slider::-moz-range-thumb {
        width: var(--thumb);
        height: var(--thumb);
        border: none;
        border-radius: 50%;
        background: var(--bg-raised);
        box-shadow: 0 0 0 1.5px var(--accent);
    }

    /* What is running now: a tick under the track, hidden by the thumb until
       the count is moved away from it. */
    .now {
        --at: var(--now);
        position: absolute;
        top: 12px;
        left: var(--along);
        width: 2px;
        height: 5px;
        margin-left: -1px;
        border-radius: 1px;
        background: var(--text-dim);
        pointer-events: none;
    }

    .ends {
        position: absolute;
        top: 19px;
        left: 0;
        right: 0;
        display: flex;
        justify-content: space-between;
        font-size: 10px;
        line-height: 1;
        color: var(--text-faint);
        font-variant-numeric: tabular-nums;
        pointer-events: none;
    }

    .count {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 0 0 auto;
        font-size: 12px;
        color: var(--text-dim);
    }

    .count input {
        width: 64px;
        height: 24px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
        font: inherit;
        font-size: 12px;
        font-variant-numeric: tabular-nums;
    }

    .drain {
        padding: 8px 12px 10px;
        border-bottom: 1px solid var(--border);
        background: var(--bg);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .line {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .what {
        flex: 1 1 auto;
        min-width: 0;
        overflow-wrap: anywhere;
    }

    .failed .what {
        color: var(--error);
    }

    .track {
        margin-top: 6px;
        height: 3px;
        border-radius: 2px;
        background: var(--bg-active);
        overflow: hidden;
    }

    .fill {
        height: 100%;
        background: var(--ok);
        transition: width 180ms ease;
    }

    .refused {
        margin: 8px 0 0;
        padding: 0 0 0 14px;
        display: flex;
        flex-direction: column;
        gap: 3px;
    }

    .refused li {
        color: var(--warn);
    }

    .refused strong {
        font-weight: 600;
        color: var(--text);
    }
</style>
