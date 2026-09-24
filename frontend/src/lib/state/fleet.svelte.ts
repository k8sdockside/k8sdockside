// Every connected cluster's health, read on a timer, and what has got worse.
//
// One poller feeds four things: the fleet view, the mark beside each cluster
// in the sidebar, the alerts on the bell, and the desktop notifications. They
// share it so that a cluster is asked once a minute rather than once per
// thing that wants to know, and so that the sidebar and the fleet view can
// never disagree about the same cluster.
//
// Only the clusters connected in this window are watched. Connecting can run
// an exec credential plugin -- a browser login, a hardware key -- and a window
// that did that to twenty clusters on its own, once a minute, would be one
// nobody kept open. The fleet view offers to check the rest when asked.
//
// Its own store rather than a corner of the workspace, like the health store
// it reports to: it reads no tabs or panes, and what it needs from the
// workspace -- which clusters, what they are called, where alerts go -- is
// handed to it at start rather than reached for.

import { Events } from '@wailsio/runtime';
import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
// From its own file rather than the services index: it is only called when an
// alert is posted, and the tests that stand in for the index list what each of
// them uses -- none of which is this.
import * as NotifyService from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/notifyservice.js';
import { adoptCredentials, adoptHealth, adoptPodTrouble, type ClusterHealth, type Credentials } from './adopt';
import { clusters } from './health.svelte';
import {
    credentialAlerts,
    healthAlerts,
    troubleOf,
    type AlertAction,
    type AlertDraft,
    type AlertItem,
    type Tone,
    type Trouble,
} from '../fleet/alerts';

/** How often the watched clusters are read. */
export const FLEET_POLL_MS = 60_000;

/**
 * How often a cluster's credentials are read again. Certificates are dated in
 * days, so the minute the health moves at would be a waste -- and reading the
 * API server's certificate is a TLS handshake of its own.
 */
const CREDENTIALS_EVERY_MS = 6 * 60 * 60 * 1000;

/** How many alerts the bell keeps. The oldest go first. */
const ALERTS_KEPT = 50;

/** What the fleet knows about one cluster. */
export interface FleetEntry {
    health: ClusterHealth | null;
    credentials: Credentials | null;
    /** When the health was last read, as a timestamp; null until it has been. */
    checkedAt: number | null;
    /** When the credentials were last read. */
    credentialsAt: number | null;
    checking: boolean;
}

/** One alert on the bell. */
export interface ClusterAlert {
    id: string;
    contextId: string;
    tone: Tone;
    title: string;
    body: string;
    at: number;
    read: boolean;
    /** Every object it is about, for its details. */
    items: AlertItem[];
    /** Where its details send somebody next. */
    action: AlertAction;
}

/** What the store needs from the rest of the app. See start. */
export interface FleetSource {
    /** The clusters to read on the timer: those connected in this window. */
    watched(): string[];
    /** Every cluster there is, for "check all". */
    all(): string[];
    /** The name a cluster is shown by, for the notifications. */
    nameOf(contextId: string): string;
    /**
     * Where alerts go -- the bell and the system's notifications, the bell
     * only, or nowhere -- and until when they are snoozed (a timestamp, 0 for
     * not snoozed).
     */
    alerts(): { mode: 'system' | 'bell' | 'off'; snoozedUntil: number };
    /** Opens a cluster, for a clicked notification. */
    open(contextId: string): void;
}

const EMPTY: FleetEntry = { health: null, credentials: null, checkedAt: null, credentialsAt: null, checking: false };

/**
 * Why a call failed, never empty: an empty error is what "healthy" looks
 * like to everything that reads a ClusterHealth.
 */
function message(err: unknown): string {
    const text = err instanceof Error ? err.message : String(err);
    return text || 'The cluster did not answer.';
}

/** The health of a cluster that did not answer: the reason and nothing else. */
function unreachable(contextId: string, error: string): ClusterHealth {
    return {
        contextId,
        nodesReady: 0,
        nodesTotal: 0,
        notReadyNodes: [],
        podsRunning: 0,
        podsTotal: 0,
        pods: adoptPodTrouble(null),
        warnings: 0,
        warningReasons: [],
        error,
    };
}

class Fleet {
    private entries = $state<Record<string, FleetEntry>>({});
    alerts = $state<ClusterAlert[]>([]);
    /**
     * The alert somebody asked to see -- by clicking its notification -- and
     * a count that moves on every ask, so asking for the same one twice still
     * opens it. The bell watches this.
     */
    revealed = $state<{ id: string; nonce: number } | null>(null);
    /** How many alerts have not been seen. */
    unread = $derived(this.alerts.filter((a) => !a.read).length);

    private source: FleetSource | null = null;
    /**
     * Credential alerts already raised this session, by context and step: a
     * certificate a week from expiry is said once, not once every six hours.
     */
    private raised = new Set<string>();
    private asked = false;
    private sequence = 0;
    /**
     * Bumped per cluster by forget, so a reading that set out before the
     * cluster was disconnected does not bring it back when it lands.
     */
    private generations = new Map<string, number>();

    /** What is known about one cluster. */
    of(contextId: string): FleetEntry {
        return this.entries[contextId] ?? EMPTY;
    }

    /** The mark a cluster gets, or null when there is nothing to say. */
    trouble(contextId: string): Trouble | null {
        const entry = this.of(contextId);
        return troubleOf(entry.health, entry.credentials);
    }

    /**
     * Starts reading the watched clusters, now and on the timer. Returns the
     * stop. Called once, by the app shell, with what the store needs from the
     * workspace.
     */
    start(source: FleetSource): () => void {
        this.source = source;
        // A clicked notification shows that alert's details. One the bell no
        // longer has -- cleared, or from before a restart -- opens its
        // cluster's dashboard instead, which is where it came from.
        const off = Events.On('notify:open', (event: { data: { contextId?: string; alertId?: string } }) => {
            const { contextId = '', alertId = '' } = event.data ?? {};
            if (!this.reveal(alertId) && contextId) this.source?.open(contextId);
        });
        void this.tick();
        const timer = setInterval(() => void this.tick(), FLEET_POLL_MS);
        return () => {
            clearInterval(timer);
            off?.();
            this.source = null;
        };
    }

    /** Reads every watched cluster once. */
    async tick(): Promise<void> {
        const ids = this.source?.watched() ?? [];
        await Promise.all(ids.map((id) => this.check(id)));
    }

    /**
     * Reads every cluster there is, connected or not -- which connects the
     * ones that were not, with whatever that takes. Only ever done because
     * somebody pressed the button that says so.
     */
    async checkAll(): Promise<void> {
        const ids = this.source?.all() ?? [];
        await Promise.all(ids.map((id) => this.check(id, { credentials: true })));
    }

    /**
     * Reads one cluster, and raises whatever got worse since the last time.
     * credentials forces the credentials to be read again even when they
     * were read recently.
     */
    async check(contextId: string, { credentials = false } = {}): Promise<void> {
        const before = this.of(contextId);
        if (before.checking) return;
        this.entries[contextId] = { ...before, checking: true };
        const generation = this.generations.get(contextId) ?? 0;
        const current = () => (this.generations.get(contextId) ?? 0) === generation;
        // What the sidebar said as this set out. Anything reported while the
        // reading was on its way -- a dashboard that loaded, a probe -- is
        // newer evidence than this, and is left standing.
        const said = clusters.of(contextId);

        let health: ClusterHealth;
        try {
            health = adoptHealth(await ResourceService.Health(contextId));
        } catch (err) {
            health = unreachable(contextId, message(err));
        }
        if (!current()) return;
        // What this found is the same evidence a dashboard's load is, so it
        // settles the sidebar's connection mark the same way.
        if (clusters.of(contextId) === said) {
            if (health.error) clusters.report(contextId, 'error', health.error);
            else clusters.report(contextId, 'connected');
        }

        const drafts: AlertDraft[] = healthAlerts(before.health, health);

        let creds = before.credentials;
        let credentialsAt = before.credentialsAt;
        const due = credentials || credentialsAt === null || Date.now() - credentialsAt > CREDENTIALS_EVERY_MS;
        if (due) {
            try {
                creds = adoptCredentials(await ResourceService.Credentials(contextId, !health.error));
                if (!current()) return;
                credentialsAt = Date.now();
                for (const draft of credentialAlerts(creds)) {
                    const key = `${contextId}#${draft.key}`;
                    if (this.raised.has(key)) continue;
                    this.raised.add(key);
                    drafts.push(draft);
                }
            } catch {
                // A kubeconfig that cannot be read says so on the connection
                // already; the expiry column staying empty is enough here.
            }
        }

        this.entries[contextId] = {
            health,
            credentials: creds,
            checkedAt: Date.now(),
            credentialsAt,
            checking: false,
        };

        for (const draft of drafts) this.raise(contextId, draft);
    }

    /** Forgets a cluster, as disconnecting it does. */
    forget(contextId: string): void {
        this.generations.set(contextId, (this.generations.get(contextId) ?? 0) + 1);
        const { [contextId]: _, ...rest } = this.entries;
        this.entries = rest;
    }

    /** Shows one alert's details, marked read. False when there is no such alert. */
    reveal(id: string): boolean {
        if (!id || !this.alerts.some((a) => a.id === id)) return false;
        this.markRead(id);
        this.revealed = { id, nonce: (this.revealed?.nonce ?? 0) + 1 };
        return true;
    }

    markRead(id: string): void {
        this.alerts = this.alerts.map((a) => (a.id === id && !a.read ? { ...a, read: true } : a));
    }

    markAllRead(): void {
        this.alerts = this.alerts.map((a) => (a.read ? a : { ...a, read: true }));
    }

    dismiss(id: string): void {
        this.alerts = this.alerts.filter((a) => a.id !== id);
    }

    clear(): void {
        this.alerts = [];
    }

    /**
     * Keeps one alert on the bell, and posts it to the system when the user
     * wants that and has not snoozed it.
     *
     * Off, nothing is kept: the sidebar's marks and the fleet view still say
     * what is wrong, and that is what somebody who switched alerts off asked
     * for. Snoozed, the alert is still kept -- so nothing is lost while the
     * user was not being told -- but it is kept read, which keeps the bell's
     * dot quiet, and nothing is posted.
     */
    private raise(contextId: string, draft: AlertDraft): void {
        const want = this.source?.alerts() ?? { mode: 'system', snoozedUntil: 0 };
        if (want.mode === 'off') return;
        const snoozed = want.snoozedUntil > Date.now();
        const alert: ClusterAlert = {
            id: `alert-${++this.sequence}`,
            contextId,
            tone: draft.tone,
            title: draft.title,
            body: draft.body,
            at: Date.now(),
            read: snoozed,
            items: draft.items ?? [],
            action: draft.action ?? { kind: 'dashboard', label: 'Open the dashboard' },
        };
        this.alerts = [alert, ...this.alerts].slice(0, ALERTS_KEPT);
        if (want.mode === 'system' && !snoozed) void this.post(contextId, draft, alert.id);
    }

    /**
     * Posts one alert as a system notification. Failures are swallowed: the
     * alert is on the bell whatever happens here, and the settings page says
     * why the system would not take it.
     */
    private async post(contextId: string, draft: AlertDraft, alertId: string): Promise<void> {
        try {
            if (!this.asked) {
                // Asked the first time there is something to say, rather than
                // at launch: the question means more beside its first answer.
                this.asked = true;
                const status = await NotifyService.Status();
                if (status.available && !status.authorized) await NotifyService.RequestPermission();
            }
            await NotifyService.Send(
                `${contextId}#${draft.key}`,
                this.source?.nameOf(contextId) ?? contextId,
                draft.title,
                draft.body,
                contextId,
                alertId,
            );
        } catch {
            // See above.
        }
    }
}

export const fleet = new Fleet();
