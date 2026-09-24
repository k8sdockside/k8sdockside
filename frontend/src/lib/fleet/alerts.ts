// What is worth telling somebody about a cluster: the comparison of one
// reading of its health with the last, and how bad what it found is.
//
// Kept apart from the store that polls, because it is the part with rules in
// it -- which changes are news and which are noise -- and rules are what want
// testing without a timer or a cluster.

import type { ClusterHealth, Credentials } from '../state/adopt';
import { formatDate } from '../datetime.svelte';

export type Tone = 'error' | 'warn';

/** One object an alert is about, which its details list and open. */
export interface AlertItem {
    /** What the list says: a node's name, a pod's, with why when known. */
    label: string;
    /** A second line: the reason, the last termination, a namespace. */
    detail?: string;
    ref?: { kind: string; namespace: string; name: string };
}

/**
 * Where an alert's details send somebody to go further: the list the objects
 * are in, narrowed to them where that can be said.
 */
export type AlertAction =
    | { kind: 'dashboard'; label: string }
    | { kind: 'list'; label: string; list: string; query?: string };

/** One thing that has got worse on a cluster. */
export interface AlertDraft {
    /**
     * Names the alert across readings, so the same trouble found again later
     * replaces its notification rather than stacking another beside it.
     */
    key: string;
    tone: Tone;
    title: string;
    body: string;
    /** Every object it is about, not only the few the body names. */
    items?: AlertItem[];
    /** What to open to go further. The cluster's dashboard when absent. */
    action?: AlertAction;
}

/** How many days out a credential starts being worth mentioning. */
export const CREDENTIAL_WARN_DAYS = 30;
/** How many days out it becomes urgent. */
export const CREDENTIAL_URGENT_DAYS = 7;

function plural(n: number, one: string, many = `${one}s`): string {
    return `${n} ${n === 1 ? one : many}`;
}

/** The first few of a list, and how many more there are. */
function some(names: string[], shown = 3): string {
    if (names.length <= shown) return names.join(', ');
    return `${names.slice(0, shown).join(', ')} and ${names.length - shown} more`;
}

/**
 * What got worse between two readings of one cluster.
 *
 * The first reading of a cluster raises nothing: a window opened on fifteen
 * clusters would otherwise open with fifteen notifications about things that
 * were already true yesterday. What is already wrong shows on the sidebar and
 * in the fleet view; the alerts are for what changes while somebody is not
 * looking.
 */
export function healthAlerts(previous: ClusterHealth | null, next: ClusterHealth): AlertDraft[] {
    if (!previous) return [];
    const out: AlertDraft[] = [];

    if (next.error && !previous.error) {
        out.push({
            key: 'unreachable',
            tone: 'error',
            title: 'Cannot reach the cluster',
            body: next.error,
            action: { kind: 'dashboard', label: 'Open the dashboard' },
        });
        return out;
    }
    if (next.error) return out;

    const down = next.notReadyNodes.filter((n) => !previous.notReadyNodes.includes(n));
    if (down.length > 0) {
        out.push({
            key: `nodes:${down.join(',')}`,
            tone: 'error',
            title: down.length === 1 ? `Node ${down[0]} is not ready` : `${down.length} nodes are not ready`,
            body: `${some(down)} — ${next.nodesReady} of ${next.nodesTotal} nodes ready.`,
            items: down.map((name) => ({ label: name, detail: 'Not ready', ref: { kind: 'nodes', namespace: '', name } })),
            action: { kind: 'list', label: 'Open Nodes', list: 'nodes' },
        });
    }

    // A pod counts as news when it was not on the last reading's list in the
    // same trouble: one that moved from restarting to not starting is.
    const was = new Set(previous.pods.worst.map((p) => `${p.trouble}:${p.namespace}/${p.name}`));
    const freshPods = (trouble: string) =>
        next.pods.worst.filter((p) => p.trouble === trouble && !was.has(`${p.trouble}:${p.namespace}/${p.name}`));
    const fresh = (trouble: string) => freshPods(trouble).map((p) => p.name);
    // The pods in one kind of trouble, as the details list them: every one the
    // reading named, newest trouble first, with why.
    const podItems = (trouble: string): AlertItem[] =>
        next.pods.worst
            .filter((p) => p.trouble === trouble)
            .map((p) => ({
                label: p.name,
                detail: [p.namespace, p.reason, p.lastTermination, p.lastRestart ? `${p.lastRestart} ago` : '', p.message]
                    .filter(Boolean)
                    .join(' · '),
                ref: { kind: 'pods', namespace: p.namespace, name: p.name },
            }));

    if (next.pods.crashLooping > previous.pods.crashLooping) {
        const names = fresh('crashloop');
        out.push({
            key: 'crashlooping',
            tone: 'error',
            title: `${plural(next.pods.crashLooping, 'pod')} not starting`,
            body: names.length > 0 ? some(names) : `Up from ${previous.pods.crashLooping}.`,
            items: podItems('crashloop'),
            action: { kind: 'list', label: 'Open Pods', list: 'pods' },
        });
    }

    if (next.pods.evicted > previous.pods.evicted) {
        const more = next.pods.evicted - previous.pods.evicted;
        out.push({
            key: 'evicted',
            tone: 'warn',
            title: `${plural(more, 'pod')} evicted`,
            body: `${plural(next.pods.evicted, 'evicted pod')} left on the cluster.`,
            items: podItems('evicted'),
            action: { kind: 'list', label: 'Show the evicted pods', list: 'pods', query: 'Evicted' },
        });
    }

    if (next.pods.failed > previous.pods.failed) {
        const names = fresh('failed');
        out.push({
            key: 'failed',
            tone: 'warn',
            title: `${plural(next.pods.failed - previous.pods.failed, 'pod')} failed`,
            body: names.length > 0 ? some(names) : `${plural(next.pods.failed, 'failed pod')} in all.`,
            items: podItems('failed'),
            action: { kind: 'list', label: 'Open Pods', list: 'pods' },
        });
    }

    if (next.pods.restartedRecently > previous.pods.restartedRecently) {
        const names = fresh('restarting');
        out.push({
            key: 'restarting',
            tone: 'warn',
            title: `${plural(next.pods.restartedRecently, 'pod')} restarted in the last hour`,
            body: names.length > 0 ? some(names) : `Up from ${previous.pods.restartedRecently}.`,
            items: podItems('restarting'),
            action: { kind: 'list', label: 'Open Pods', list: 'pods' },
        });
    }

    return out;
}

/**
 * Credentials running out, each said once per step towards expiry -- a month
 * out, a week out, and gone -- rather than on every reading.
 *
 * Unlike the health alerts these are raised on the first reading too: a
 * certificate that expires on Friday is news whenever it is first noticed.
 */
export function credentialAlerts(credentials: Credentials): AlertDraft[] {
    const out: AlertDraft[] = [];
    for (const item of credentials.items) {
        if (!item.notAfter) continue;
        const step = credentialStep(item.daysLeft);
        if (!step) continue;
        const what = item.subject ? `${item.kind} (${item.subject})` : item.kind;
        out.push({
            key: `credential:${item.kind}:${item.subject}:${step}`,
            tone: step === 'month' ? 'warn' : 'error',
            title: item.daysLeft < 0 ? `${item.kind} has expired` : `${item.kind} expires ${inDays(item.daysLeft)}`,
            body: `${what} — valid until ${formatDate(item.notAfter)}.`,
            items: [{ label: what, detail: `Valid until ${formatDate(item.notAfter)} (${inDays(item.daysLeft)})` }],
            action: { kind: 'dashboard', label: 'Open the dashboard' },
        });
    }
    return out;
}

function credentialStep(daysLeft: number): 'gone' | 'week' | 'month' | null {
    if (daysLeft < 0) return 'gone';
    if (daysLeft <= CREDENTIAL_URGENT_DAYS) return 'week';
    if (daysLeft <= CREDENTIAL_WARN_DAYS) return 'month';
    return null;
}

/** "today", "tomorrow", "in 12 days", "12 days ago". */
export function inDays(days: number): string {
    if (days < -1) return `${-days} days ago`;
    if (days === -1) return 'yesterday';
    if (days === 0) return 'today';
    if (days === 1) return 'tomorrow';
    return `in ${days} days`;
}

/** How a credential's expiry reads, and how loudly. */
export function credentialTone(daysLeft: number, notAfter: string): Tone | null {
    if (!notAfter) return null;
    if (daysLeft <= CREDENTIAL_URGENT_DAYS) return 'error';
    if (daysLeft <= CREDENTIAL_WARN_DAYS) return 'warn';
    return null;
}

/** The mark a cluster gets in the sidebar and the fleet view, or null for none. */
export interface Trouble {
    tone: Tone;
    /** How many things are wrong, for the badge. */
    count: number;
    /** What they are, in a sentence, for its tooltip. */
    summary: string;
}

export function troubleOf(health: ClusterHealth | null, credentials: Credentials | null): Trouble | null {
    const parts: string[] = [];
    let tone: Tone | null = null;
    let count = 0;
    const raise = (t: Tone) => {
        if (tone !== 'error') tone = t;
    };

    if (health && !health.error) {
        const down = health.nodesTotal - health.nodesReady;
        if (down > 0) {
            parts.push(`${plural(down, 'node')} not ready`);
            count += down;
            raise('error');
        }
        if (health.pods.crashLooping > 0) {
            parts.push(`${plural(health.pods.crashLooping, 'pod')} not starting`);
            count += health.pods.crashLooping;
            raise('error');
        }
        if (health.pods.evicted > 0) {
            parts.push(`${plural(health.pods.evicted, 'evicted pod')}`);
            count += health.pods.evicted;
            raise('warn');
        }
        if (health.pods.failed > 0) {
            parts.push(`${plural(health.pods.failed, 'failed pod')}`);
            count += health.pods.failed;
            raise('warn');
        }
        if (health.pods.restartedRecently > 0) {
            parts.push(`${plural(health.pods.restartedRecently, 'pod')} restarted in the last hour`);
            count += health.pods.restartedRecently;
            raise('warn');
        }
    }

    for (const item of credentials?.items ?? []) {
        const t = credentialTone(item.daysLeft, item.notAfter);
        if (!t) continue;
        parts.push(item.daysLeft < 0 ? `${item.kind} expired` : `${item.kind} expires ${inDays(item.daysLeft)}`);
        count += 1;
        raise(t);
    }

    if (!tone) return null;
    return { tone, count, summary: parts.join(' · ') };
}
