// Types for dom.js: small DOM helpers that put cluster data on the page as
// text, never as HTML.

export type Attrs = Record<string, string | number | boolean | undefined>;
export type Child = Node | string | null | undefined | false;

/** An element, with attributes and children. Strings become text nodes. */
export declare function el<K extends keyof HTMLElementTagNameMap>(
    tag: K,
    attrs?: Attrs,
    ...children: Child[]
): HTMLElementTagNameMap[K];

/** A <button> with a click handler. */
export declare function button(label: string, onClick: () => void, attrs?: Attrs): HTMLButtonElement;

/** Replaces everything in a node. */
export declare function replace(parent: Element, ...children: Child[]): void;

/** The element with an id, or a thrown error -- a missing id is the page's bug, not the user's. */
export declare function byId<T extends HTMLElement = HTMLElement>(id: string): T;

/**
 * An inline SVG icon from markup. The one helper that takes markup: hand it
 * only constants from the plugin's own source, never anything from the cluster.
 */
export declare function svg(markup: string, className?: string): SVGElement;
