// The bridge's storage: what a plugin's own pages keep between sessions, per
// plugin and per context, saved by the app in its settings file. Reads come
// from the settings already in memory; writes go through Go, which holds the
// limits, and come back as the settings as saved.

import { PluginService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import { adoptSettings } from '../state/adopt';
import { workspace } from '../state/workspace.svelte';

/** Everything one plugin keeps on one context. */
export function pluginState(pluginId: string, contextId: string): Record<string, string> {
    return workspace.settings.pluginState?.[pluginId]?.[contextId] ?? {};
}

/**
 * Writes run one after another, so two quick folds cannot land out of order
 * and leave the older settings in memory.
 */
let queue: Promise<unknown> = Promise.resolve();

/** Keeps one value, or forgets it when `value` is empty. Rejects with Go's reason when a limit is hit. */
export function setPluginState(pluginId: string, contextId: string, key: string, value: string): Promise<void> {
    const write = queue.then(async () => {
        workspace.settings = adoptSettings(await PluginService.SetState(pluginId, contextId, key, value));
    });
    queue = write.catch(() => {});
    return write;
}
