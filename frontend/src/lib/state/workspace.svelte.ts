// The whole application state lives here: which kubeconfig files were found,
// what the user renamed and coloured each context, which views are open and in
// which pane, and what the slide-in detail panel is showing.
//
// It is one object rather than several stores because nearly every action
// touches more than one of those things -- opening a tab selects a context,
// closing one may change the active tab, dragging tabs persists settings -- and
// splitting them would only move the coordination somewhere less obvious.
//
// Tabs live in panes. There used to be two separate models here -- a strip of
// resource tabs along the top and a dock of documents at the foot, each with its
// own open/close/reorder/restore -- and which of the two a view was decided
// where on screen it could ever appear. They are one model now, and where a view
// sits is the user's choice: see ./panes.ts for the vocabulary.
//
// The class is one object, but not one file: it is written as a chain of
// layers in ./workspace/, each a part of the state you can read on its own,
// each extending the one before it --
//
//     base          every $state and $derived, and the machinery that saves them
//     sources       loading, kubeconfig files and folders, connecting and letting go
//     contexts      names, colours, table columns, selection
//     tabs          opening, closing and moving tabs between panes
//     layout        restoring and saving the panes, the describe tab, folding, zoom
//     plugins       custom resource definitions and the plugin catalogue
//     preferences   themes and the rest of the preferences
//
// -- and Workspace below is the last of them. A layer may call what a later one
// defines; the base declares those methods abstract, so the compiler still
// checks every call. What the rest of the app sees is unchanged: the one
// `workspace`, with every method and field it had.

import { WorkspacePreferences } from './workspace/preferences.svelte';

// The pane model is the app's vocabulary for "what is open and where", and it
// is re-exported here so that a component reaching for the store gets the types
// that go with it from the same place.
export {
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    PANE_IDS,
    PANE_LABELS,
    clustersTab,
    defaultPaneFor,
    detailsTab,
    isHorizontal,
    isDocumentView,
    isPaneId,
    isTabView,
    iconForView,
    resourceTabId,
    tabIdFor,
    MIN_PANE_SIZE,
    PANE_HEADROOM,
} from './panes';
export type { PaneId, PaneState, Tab, TabTarget, TabView } from './panes';
export { beginTabDrag, currentTabDrag, endTabDrag } from './tabdrag.svelte';
export type { TabDrag } from './tabdrag.svelte';
// Reachability moved to its own store; re-exported so the components drawing an
// indicator import the type from beside the thing they are drawing.
export type { Health, HealthStatus } from './health.svelte';
export type { Notice } from './notices.svelte';
export type { DetailTarget } from './detail.svelte';
export {
    PLUGIN_RECHECK_MS,
    isAppTab,
    isSettingsTab,
    type CustomApiGroup,
    type CustomKinds,
    type DockTab,
    type Reveal,
} from './workspace/helpers';

class Workspace extends WorkspacePreferences {}

export const workspace = new Workspace();
