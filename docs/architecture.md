# Architecture

How k8sdockside talks to a cluster, and why it is built the way it is. For
building and running the app, see [development.md](development.md).

## How the cluster data gets here

Resource listings are **live and watched**, not polled. Opening a tab starts a
`client-go` informer against the cluster; the backend pushes the whole current
table to the frontend whenever anything changes, so a rollout repaints as it
happens and nothing has a refresh button.

- **Everything goes through the dynamic client.** Built-in kinds, the Gateway
  API and arbitrary CRDs are all `unstructured` objects on one code path, which
  is what lets a custom resource open in a tab without anything being compiled
  in for it.
- **One watch per kind per context**, shared by every tab looking at it, closed
  when the last of them does. The namespace filter is applied to the informer's
  cache, so changing it repaints without reopening anything.
- **Columns are data.** Most are JSONPath expressions; the few that are computed
  rather than read (a pod's ready count, a workload's condition) have a Go
  function instead. Custom resources supply their own columns from the CRD's
  `additionalPrinterColumns` — the same source `kubectl get` uses, so the
  tables match the command line.
- **Secrets are redacted before they are cached.** An informer holds the whole
  collection in memory; the tables only ever show how many keys a secret has, so
  the values are dropped on the way in. The editor is the exception, and has to
  be: it reads the object live rather than from the cache, because an editor
  that opened on a redacted copy would save the redaction back over the secret.
- **Editing is a read, a `PUT` and nothing clever.** The document keeps its
  `resourceVersion`, so a save while a controller (or another person) has moved
  the object on is refused as a conflict rather than silently winning it. What
  comes back from the save is what the editor then holds — with the new
  version, and whatever defaulting and admission control did on the way in —
  so a second save is not arguing with an object that no longer exists.
  Changing `apiVersion`, `kind`, `metadata.name` or `metadata.namespace` is
  refused: the write goes to the URL of the object that was opened, so a rename
  there cannot mean what it looks like it means.
- **A shell and a forward are streams, not requests.** Both upgrade one HTTP
  request into a connection that stays open — over websockets where the API
  server speaks them, over SPDY where it or something in between does not, which
  is the same pair `kubectl` falls back through. Both ride the connection a tab
  already has. A shell tries each configured shell in turn and takes "no such
  executable" as "try the next one"; while it is doing that, what you type is
  held for whichever attempt is live, because a failed attempt's copier would
  otherwise still be holding the keyboard.
- **Node and dashboard figures are capacity and requests, not usage.** Live
  usage comes from the metrics API, a separate server that is not always
  installed.

Credentials come from `clientcmd`, so client certificates, tokens and `exec`
plugins all work. Note that the kubeconfig is read twice by design: the parser
in [`internal/kube/config.go`](../internal/kube/config.go) reads the little
needed to *list* contexts and is forgiving of files it cannot fully understand,
while `clientcmd` reads what is needed to *connect*.

## Layout

```
main.go                      the window, and the settings file it opens
internal/services/           the services the frontend calls, wired together in services.go
  kubeconfigservice.go       discovery, add/remove, the context cache
  settingsservice.go         aliases, colours, tab order, layout, and the version shown
  resourceservice.go         dashboard, resource tables, describe, editing
  budgetservice.go           what a cluster, node, namespace or pod has, has promised, and is using,
                             plus the CPU delay read straight from the kubelets
  actionservice.go           scale, restart, cordon, drain, delete, and the bulk forms of them
  logservice.go              log streams, per container
  helmservice.go             releases read from their Secrets, and the helm binary for the rest
  themeservice.go            the theme catalogue, and the folders it is read from
  pluginservice.go           the solution plugins, and each one's per-cluster overview
  metricsservice.go          finding a cluster's Prometheus, and drawing plugin charts
  terminalservice.go         shells: the sessions open, and the external terminals
  portforwardservice.go      the tunnels open, and the ones remembered from last time
  updateservice.go           whether a newer release exists, and whether the user has heard
  searchservice.go           finding objects by name across clusters, reported as they are found
internal/kube/               kubeconfig parsing, and the stubbed cluster data
internal/appconfig/          the settings file
internal/addons/             finding and merging add-on files, shared by the two below
internal/themes/             the theme format, loader and built-in palettes
internal/themes/builtin/     the thirteen themes, as JSON in the public format
internal/plugins/            the plugin format, loader and overview builder; object actions and
                             panels (actions.go); serving plugins' own views (ui.go) with the
                             bridge's client and its types (sdk/); installing from a repository
                             (git.go); strict reading and its messages (decode.go); the plugins
                             offered for installing (known.go, known.json)
internal/plugins/builtin/    Argo CD, Flux and Prometheus, in the public format; builtin/ui/ holds
                             the pages of those that have any
cmd/plugincheck/             the loader's checks, run on a plugin's folder outside the app
docs/plugin.schema.json      the plugin manifest as a JSON Schema, for editors
internal/metrics/            PromQL, Prometheus discovery, and reading its answers
internal/termapp/            the terminal emulators on this machine, and how to run one in it
internal/updates/            asking GitHub for the latest release, and ordering versions
frontend/src/
  App.svelte                 shell: sidebar | tabs + view | detail panel | dock
  lib/state/workspace.svelte.ts   all application state
  lib/state/editor.svelte.ts      the documents open in the dock's editor
  lib/state/terminals.svelte.ts   the shells open in the dock, and their xterm instances
  lib/state/forwards.svelte.ts    the port forwards, live and remembered
  lib/state/updates.svelte.ts     the newest release known, and whether it is still unread
  lib/state/tabdrag.svelte.ts     the tab in the air, while one is being dragged between panes
  lib/state/views.ts              how each open list is sorted and filtered, for as long as its tab is
  lib/state/search.svelte.ts      the search in the title bar: the query, its options, and what has turned up
  lib/search/                     ordering search hits, and the plugin views each one can be opened in
  lib/theme/apply.ts         writing a theme's colours onto the document
  lib/plugins/               the plugin catalogue as the sidebar and overview see it
  lib/components/PluginFrame.svelte   a plugin's own view or detail panel, sandboxed, and the bridge it talks through
  lib/charts/                the SVG line chart, and the panel that hosts them
  lib/budget/                the requested/limits/used bars, and the throttling reading under them
  lib/catalogue.ts           the resource kinds the sidebar offers
  lib/colors.ts              the context palette
  lib/components/            sidebar, tab bar, tables, detail panel, dock
```

## The generated bindings

`frontend/bindings/` is produced by `wails3 generate bindings` (`make
generate`). It is build output rather than source: it is gitignored, and CI
generates it before anything under `frontend/` is type-checked, tested or
bundled. Rerun it after changing a service's methods or any type they mention.

Two things about the generated types are worth knowing, and both are handled in
one place — `frontend/src/lib/state/adopt.ts`, which every service result passes
through on its way into the app:

- Every Go slice and map is typed as nullable, because a nil one serialises to
  JSON `null`.
- Depending on the generator flags, models arrive as *classes* rather than plain
  objects, and Svelte's `$state` does not deep-proxy class instances — so a
  nested write to one would never reach the UI.

`make generate` and the binding step inside `wails3 build` must be given the
same flags, or each will quietly undo the other's output.

## Where settings live

`$XDG_CONFIG_HOME/k8sdockside/settings.json`, falling back to
`~/.config/k8sdockside/settings.json`, on both macOS and Linux; Windows uses
`%AppData%`. The path is shown in the status bar. Themes and plugins you install
go in `themes/` and `plugins/` folders beside it — see [themes.md](themes.md)
and [plugins.md](plugins.md).

Releases up to and including the ones that used `os.UserConfigDir` wrote to
`~/Library/Application Support/k8sdockside/` on macOS. That file is moved to the
new location automatically on first launch, unless one is already there.
