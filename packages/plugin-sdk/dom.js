// Small DOM helpers for plugin pages.
//
// The rule these exist to enforce: cluster data goes on the page as text,
// never as HTML. Nothing here takes a string of markup except `svg()`, which
// must only ever be handed the plugin's own icon constants. A page that builds
// its rows with `el` cannot have an injection bug through an object's name or
// an operator's error message, which is where cluster data is genuinely
// hostile.

/** An element, with attributes and children. Strings become text nodes. */
export function el(tag, attrs = {}, ...children) {
    const node = document.createElement(tag);
    for (const [name, value] of Object.entries(attrs)) {
        if (value === undefined || value === false) continue;
        if (name === 'class') node.className = String(value);
        else if (name === 'text') node.textContent = String(value);
        else node.setAttribute(name, String(value));
    }
    append(node, children);
    return node;
}

/** A <button> with a click handler. */
export function button(label, onClick, attrs = {}) {
    const node = el('button', { type: 'button', ...attrs }, label);
    node.addEventListener('click', onClick);
    return node;
}

/** Replaces everything in a node. */
export function replace(parent, ...children) {
    parent.replaceChildren();
    append(parent, children);
}

/** The element with an id, or a thrown error -- a missing id is the page's bug, not the user's. */
export function byId(id) {
    const node = document.getElementById(id);
    if (!node) throw new Error(`the page has no #${id}`);
    return node;
}

/**
 * An inline SVG icon from markup. The one helper that takes markup: hand it
 * only constants from the plugin's own source, never anything from the cluster.
 */
export function svg(markup, className = 'icon') {
    const holder = document.createElement('span');
    holder.innerHTML = markup;
    const node = holder.firstElementChild;
    if (!node) throw new Error('svg() was given no element');
    node.setAttribute('class', className);
    return node;
}

function append(parent, children) {
    for (const child of children) {
        if (child === null || child === undefined || child === false) continue;
        parent.append(child);
    }
}
