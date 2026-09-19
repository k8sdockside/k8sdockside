// The boundary between the budget bindings and the view, matching the other
// adopt modules: nullable everywhere a Go slice could be nil, resolved once
// here.
//
// The bar geometry lives here too rather than in the component, because
// choosing the denominator is a judgement rather than a layout detail and it is
// the part worth testing.

import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

export interface BudgetAmount {
    label: string;
    unit: string;
    capacity: number;
    allocatable: number;
    requested: number;
    limits: number;
    used: number;
    hasCapacity: boolean;
    hasDemand: boolean;
    hasUsed: boolean;
}

export interface BudgetUsage {
    /** `metrics-server`, `prometheus`, or empty when neither answered. */
    source: string;
    error: string;
}

export interface Budget {
    scope: { kind: string; name: string };
    amounts: BudgetAmount[];
    usage: BudgetUsage;
    error: string;
}

/**
 * How much of a scope's time went into waiting rather than running.
 *
 * Read from the kubelets rather than from a metrics stack, so it answers on a
 * cluster with nothing installed — see internal/kube/cpudelay.go.
 */
export interface CPUDelay {
    /** `kubelet`, or empty when nothing answered. */
    source: string;
    error: string;
    /** Share of CFS periods that ended with the cgroup stopped, 0-1. */
    throttled: number;
    /** Seconds of throttling per second. */
    stalled: number;
    /** Whether anything in scope has a CPU limit at all. */
    limited: boolean;
    /** PSI: share of time some task was waiting on CPU, 0-1. */
    pressure: number;
    hasPressure: boolean;
    nodes: number;
    sampled: number;
    /** The first reading of a counter, with nothing yet to compare it to. */
    waiting: boolean;
}

export function adoptDelay(delay: kube.CPUDelay): CPUDelay {
    return {
        source: delay.source ?? '',
        error: delay.error ?? '',
        throttled: delay.throttled ?? 0,
        stalled: delay.stalled ?? 0,
        limited: delay.limited ?? false,
        pressure: delay.pressure ?? 0,
        hasPressure: delay.hasPressure ?? false,
        nodes: delay.nodes ?? 0,
        sampled: delay.sampled ?? 0,
        waiting: delay.waiting ?? false,
    };
}

/**
 * How a delay reading should read: the sentence under the used bar, and whether
 * it is worth colouring as a warning.
 *
 * Throttling is a share, so the thresholds are shares. A container clipped in
 * one period in a hundred is doing fine; one clipped in a quarter of them is
 * spending real time stopped, and that is the point at which somebody should
 * look at its limit.
 */
export function delayTone(delay: CPUDelay): 'ok' | 'warn' | 'bad' {
    if (delay.throttled >= 0.25) return 'bad';
    if (delay.throttled >= 0.05) return 'warn';
    return 'ok';
}

export function adoptBudget(budget: kube.Budget): Budget {
    return {
        scope: { kind: budget.scope?.kind ?? '', name: budget.scope?.name ?? '' },
        amounts: (budget.amounts ?? []).map((a) => ({
            label: a.label ?? '',
            unit: a.unit ?? '',
            capacity: a.capacity ?? 0,
            allocatable: a.allocatable ?? 0,
            requested: a.requested ?? 0,
            limits: a.limits ?? 0,
            used: a.used ?? 0,
            hasCapacity: a.hasCapacity ?? false,
            hasDemand: a.hasDemand ?? false,
            hasUsed: a.hasUsed ?? false,
        })),
        usage: { source: budget.usage?.source ?? '', error: budget.usage?.error ?? '' },
        error: budget.error ?? '',
    };
}

/** One measured quantity, and how much of the ceiling it takes up. */
export interface Bar {
    label: string;
    value: number;
    /** 0-100, clamped. */
    percent: number;
    /** The value is past the ceiling: the bar is full and the number is not. */
    overcommitted: boolean;
    /**
     * The bar is scaled against the other bars rather than against a ceiling,
     * because there is no ceiling. The proportions are still true of each
     * other; they are not a fraction of any capacity.
     */
    relative: boolean;
    /** Whether to draw a track at all. False for a lone bar with no ceiling. */
    track: boolean;
}

/**
 * ceilingOf is the number the bars are drawn against.
 *
 * Allocatable rather than capacity: capacity is the hardware, allocatable is
 * what the scheduler may actually hand out, and the difference is already spoken
 * for by the kubelet. Drawing against capacity would show room that does not
 * exist. Capacity is the fallback only for the odd cluster that reports one and
 * not the other.
 */
export function ceilingOf(a: BudgetAmount): number {
    if (!a.hasCapacity) return 0;
    return a.allocatable > 0 ? a.allocatable : a.capacity;
}

/**
 * barsFor turns one amount into the bars to draw for it.
 *
 * Bars are left out rather than drawn empty wherever the number does not exist:
 * requests on a pod count, usage with no metrics source.
 *
 * With no ceiling -- a namespace with no ResourceQuota -- there is nothing to
 * be a fraction of, so the bars are scaled against the largest of themselves
 * instead and marked relative. That keeps requested-against-limits-against-used
 * readable, which is the comparison worth having, without implying a percentage
 * of a capacity the namespace does not own. A single such bar gets no track at
 * all: scaled against itself it would always be full, and a full bar that means
 * nothing is worse than no bar.
 */
export function barsFor(a: BudgetAmount): Bar[] {
    const ceiling = ceilingOf(a);

    const values: { label: string; value: number }[] = [];
    if (a.hasDemand) {
        values.push({ label: 'Requested', value: a.requested });
        values.push({ label: 'Limits', value: a.limits });
    }
    if (a.hasUsed) {
        values.push({ label: 'Used', value: a.used });
    }

    if (ceiling > 0) {
        return values.map(({ label, value }) => ({
            label,
            value,
            percent: Math.min(100, (value / ceiling) * 100),
            overcommitted: value > ceiling,
            relative: false,
            track: true,
        }));
    }

    const largest = Math.max(...values.map((v) => v.value), 0);
    const track = values.length > 1;
    return values.map(({ label, value }) => ({
        label,
        value,
        percent: track && largest > 0 ? (value / largest) * 100 : 0,
        overcommitted: false,
        relative: true,
        track,
    }));
}
