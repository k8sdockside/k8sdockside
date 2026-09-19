// The KubeVirt detail payload, with the bindings' nulls resolved.
//
// The same job ../state/adopt.ts does for the rest: the generator types every
// Go slice and map as nullable, and resolving that once here is what keeps the
// component free of `?? []`.

import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

/** Another object a value leads to, which the panel offers as a link. */
export interface Ref {
    kind: string;
    namespace: string;
    name: string;
}

/** One labelled value, or one cell of a table. */
export interface Fact {
    label: string;
    value: string;
    ref: Ref | null;
    tone: string;
    note: string;
}

/** One titled block: a list of facts, or a small table. */
export interface Section {
    title: string;
    facts: Fact[];
    columns: string[];
    rows: Fact[][];
    empty: string;
}

export interface KubeVirtDetail {
    kind: string;
    sections: Section[];
    error: string;
}

function adoptFact(fact: kube.Fact | undefined): Fact {
    return {
        label: fact?.label ?? '',
        value: fact?.value ?? '',
        // A ref with no name cannot be opened, so it is not a link.
        ref: fact?.ref?.name ? { kind: fact.ref.kind ?? '', namespace: fact.ref.namespace ?? '', name: fact.ref.name } : null,
        tone: fact?.tone ?? '',
        note: fact?.note ?? '',
    };
}

export function adoptKubeVirtDetail(detail: kube.KubeVirtDetail): KubeVirtDetail {
    return {
        kind: detail?.kind ?? '',
        error: detail?.error ?? '',
        sections: (detail?.sections ?? []).map((section) => ({
            title: section?.title ?? '',
            facts: (section?.facts ?? []).map(adoptFact),
            columns: [...(section?.columns ?? [])],
            rows: (section?.rows ?? []).map((row) => (row ?? []).map(adoptFact)),
            empty: section?.empty ?? '',
        })),
    };
}
