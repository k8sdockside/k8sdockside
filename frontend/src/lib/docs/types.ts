// The content model for the documentation pages: Help and the Kubernetes
// primer.
//
// A page is sections; a section is blocks; a block is one of a handful of
// shapes. The text lives in plain TypeScript files as data, so editing what a
// page says never means touching markup, and the one component that draws a
// page can be tested with three lines of fixture rather than the real thing.
//
// Sentences may carry the two inline marks in ./inline.ts and nothing else.

/** What a button in an `actions` block does when pressed. */
export type Action =
    /** Opens a resource kind's list in the selected cluster. */
    | { kind: 'show'; resource: string; label: string }
    /** Opens Settings on one of its sections. */
    | { kind: 'settings'; section: string; label: string }
    /** Opens the other documentation page. */
    | { kind: 'page'; page: 'help' | 'kubernetes'; label: string };

/** One thing worth reading elsewhere. */
export interface Link {
    label: string;
    href: string;
    /** A few words on what is there. */
    note?: string;
}

/** One entry in a glossary. */
export interface Term {
    term: string;
    meaning: string;
    /** A kind the app can list, offered as "show me" beside the meaning. */
    resource?: string;
    /** The official page on it. */
    href?: string;
}

/**
 * The version of the app something is written for: the desktop app, reading
 * the kubeconfigs on the user's own machine, or the web version, run on a
 * server behind a sign-in. Content marked with neither is true of both.
 */
export type Mode = 'desktop' | 'web';

export type Block = (
    | { type: 'p'; text: string }
    | { type: 'h3'; text: string }
    | { type: 'list'; items: string[] }
    | { type: 'steps'; items: string[] }
    | { type: 'code'; text: string; caption?: string }
    | { type: 'table'; head: string[]; rows: string[][] }
    | { type: 'terms'; terms: Term[] }
    | { type: 'links'; links: Link[] }
    | { type: 'actions'; actions: Action[] }
    /** A quiet aside: a caveat, or where to look next. */
    | { type: 'note'; text: string }
) & {
    /** Shown in one version of the app only, and left out of the other. See ./mode.ts. */
    only?: Mode;
};

export interface Section {
    /** Lowercase, dashes; the rail's anchor. */
    id: string;
    label: string;
    icon: string;
    /** Shown in one version of the app only, and left out of the other. See ./mode.ts. */
    only?: Mode;
    /** One line under the heading, saying what the section covers. */
    lede?: string;
    blocks: Block[];
}

export interface Page {
    title: string;
    /** One sentence under the title. */
    lede: string;
    sections: Section[];
}
