// Who a plugin is from, as the app says it: its author, and whether it ships
// with the app, is an official plugin installed from its own repository, or
// comes from someone else. Drawn by the app, never by the plugin, so a plugin
// can neither leave its author out nor call itself official.

import type { KnownPlugin, Plugin } from './types';

export type Standing = 'builtin' | 'official' | 'community';

export const STANDING: Readonly<Record<Standing, { label: string; title: string }>> = {
    builtin: { label: 'Built in', title: 'Ships with K8s Dockside' },
    official: {
        label: 'Official',
        title: 'Kept alongside K8s Dockside by its author, and installed from its own repository',
    },
    community: {
        label: 'Community',
        title: 'Written by someone else. It is checked when it loads and its pages run sandboxed, but the K8s Dockside project has not reviewed it',
    },
};

export function standingOf(plugin: Pick<Plugin, 'origin' | 'official'>): Standing {
    if (plugin.origin === 'builtin') return 'builtin';
    return plugin.official ? 'official' : 'community';
}

export function knownStanding(known: Pick<KnownPlugin, 'official'>): Standing {
    return known.official ? 'official' : 'community';
}

/** The name to credit: the manifest's, or the project's own for a built-in that does not say. */
export function authorOf(plugin: Pick<Plugin, 'origin' | 'author'>): string {
    return plugin.author || (plugin.origin === 'builtin' ? 'K8s Dockside' : '');
}
