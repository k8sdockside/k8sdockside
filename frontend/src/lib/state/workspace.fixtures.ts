// Shared by the workspace.*.test.ts files. Import it dynamically, after the
// bindings are mocked, so the workspace it hands back talks to the stubs.
import { workspace } from './workspace.svelte';

export {
    workspace,
    isSettingsTab,
    isAppTab,
    resourceTabId,
    tabIdFor,
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    clustersTab,
} from './workspace.svelte';
export { clusters } from './health.svelte';
export { DEFAULT_PANE_SIZE } from './panes';
export { columnKeys, MAX_COLUMN_WIDTH, MIN_COLUMN_WIDTH } from '../columns';
export { labelFor, iconFor, SETTINGS, HELP, KUBERNETES } from '../catalogue';
export { changes } from './changes.svelte';
export { views } from './views';
export { notices } from './notices.svelte';
export { session } from './session.svelte';
export {
    ResourceService,
    KubeconfigService,
    SettingsService,
    ThemeService,
    PluginService,
    MetricsService,
    TerminalService,
} from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';

export const PROD = '/home/u/.kube/prod::admin@prod';
export const STAGING = '/home/u/.kube/staging::admin@staging';

/** One saved collection tab, as the settings file holds it. */
export function resource(contextId: string, kind: string, namespaces: string[] = []) {
    return { type: 'resource', contextId, kind, namespace: '', name: '', namespaces };
}

/** One saved editor tab, as the settings file holds it. A document tab is a
 *  view onto one object and carries no namespace filter. */
export function document(contextId: string, name: string, namespace = 'default', kind = 'pods') {
    return { type: 'edit', contextId, kind, namespace, name, namespaces: [] };
}

/** Opens tabs in order and returns their ids. */
export function open(...pairs: [string, string][]): string[] {
    for (const [contextId, kind] of pairs) {
        workspace.openTab(contextId, kind);
    }
    return workspace.tabs.map((t) => t.id);
}
