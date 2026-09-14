// The Help page: what K8s Dockside does and how to use it, plus how to write
// and install a theme or a plugin. Kept in step with README.md and the two
// guides under docs/; where a section here stops short, it links to the guide.

import { DASHBOARD } from '../catalogue';
import type { Page } from './types';

const GUIDES = 'https://github.com/rogerwesterbo/k8sdockside/blob/main/docs';

export const HELP: Page = {
    title: 'Help',
    lede: 'A desktop workspace for the Kubernetes clusters in your kubeconfig files.',
    sections: [
        {
            id: 'start',
            label: 'Getting started',
            icon: 'rocket',
            lede: 'From launch to the first table in five steps.',
            blocks: [
                {
                    type: 'steps',
                    items: [
                        '**Launch it.** The app reads `~/.kube/config`, everything in `$KUBECONFIG`, and anything else under `~/.kube` that parses as a kubeconfig. Nothing connects to a cluster yet.',
                        '**Add anything it missed.** The `+` button in the sidebar adds a file; the folder button watches a whole folder, which is scanned by content rather than by file name and rescanned on **Sync**.',
                        '**Name and colour your contexts.** Select one, then use the panel at the foot of the sidebar. The colour follows the context onto every tab, panel and dock entry, which is what keeps a window of several clusters readable.',
                        '**Open a view.** Unfold a context and pick a kind. It opens as a tab in that context’s colour, and the live watch starts there.',
                        '**Click a row.** The details panel opens with the object’s report. From there: **Edit** for the live YAML, **Logs**, **Shell**, **Forward**, and the actions the kind allows.',
                    ],
                },
                {
                    type: 'note',
                    text: 'The app only reads your kubeconfig files. Aliases, colours, hidden files and removed contexts are kept in its own settings file, and a kubeconfig is never written.',
                },
                {
                    type: 'actions',
                    actions: [
                        { kind: 'settings', section: 'sources', label: 'Kubeconfig sources' },
                        { kind: 'page', page: 'kubernetes', label: 'New to Kubernetes? Read the primer' },
                    ],
                },
            ],
        },
        {
            id: 'search',
            label: 'Search',
            icon: 'search',
            lede: 'Find an object by name in every cluster at once, whatever its kind.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        '**The box in the title bar** searches as you type. **Cmd/Ctrl+K** puts you in it from anywhere; the arrow keys move through the results, **Enter** opens one and **Esc** closes the panel.',
                        '**Every kind the cluster serves** is searched — built-in and custom resources alike — except Events, which are named after the objects they are about and would bury them. Ask for them with `kind:events`.',
                        '**Words** must all appear in the name. `*` is a wildcard that pins the ends (`api-*-worker`), and a slash matches the namespace too (`kube-system/core`).',
                        '**Filters** narrow it: `kind:po,svc` by name, plural or short name; `ns:default` to one namespace, `ns:a,b` to several; `label:app=web` by label selector, which the cluster answers.',
                        '**Where it looks.** By default, the contexts already open: selected, showing a tab, or answering in the sidebar. **All contexts** reaches the rest as well, connecting to each one — which runs its credential plugin, if it has one.',
                        '**As it goes.** Results arrive cluster by cluster, and the panel shows which clusters are still being searched and how far each has got. **Stop** calls the search off and keeps what it found.',
                        '**Opening a hit** opens its kind’s list narrowed to its name, with its report in the details panel. Where an enabled plugin shows the kind, the hit also offers that view — the Argo CD application board opens with the Application selected.',
                        '**Only names, namespaces and labels are read.** A search never fetches what a Secret or a ConfigMap holds.',
                    ],
                },
            ],
        },
        {
            id: 'sidebar',
            label: 'The sidebar',
            icon: 'layers',
            lede: 'Every context on this machine, and under each one everything the cluster can show.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        '**Contexts** are listed one per row, flat by default. Turn on **Show kubeconfig names** under Settings → Appearance to group them under their files.',
                        '**The dot at the right** says whether the cluster answered: green for connected, red with a reason when it did not. Nothing is drawn until the cluster has actually been asked.',
                        '**current** marks the context the kubeconfig itself points at.',
                        '**Sections** — Cluster, Workloads, Network and the rest — fold and unfold. Folding one applies to every context; hold **Alt** to fold it for this context only, and a section set differently from the rest wears a small mark.',
                        '**Plugins** lists the installed plugins for every cluster and says in the margin which of them this cluster does not appear to have.',
                        '**Custom Resource Definitions** reads the cluster’s CRDs on first unfold and lists them by API group. Any of them opens as a table with the columns `kubectl get` would print.',
                        '**Removing and hiding.** The close button on a context row removes that one context from k8sdockside; the close button on a file heading hides the whole file. Neither writes to your kubeconfig, and a rescan brings neither back — the next scan finds them and hides them again. Both are listed under **Hidden** at the foot of the sidebar, each with a button to bring it back.',
                        '**Sorting.** The sort button in the sidebar header steps through A → Z, Z → A and the order the kubeconfigs themselves give. It sorts on the name shown, so a context you renamed sorts under its new name. The same choice is in Settings → Appearance.',
                        '**Filter** appears above the list once there are more than a handful of contexts.',
                    ],
                },
                {
                    type: 'p',
                    text: 'The panel at the foot of the sidebar edits the selected context: its display name, its colour, and where its metrics come from. Reset puts the kubeconfig name and the default colour back.',
                },
            ],
        },
        {
            id: 'tabs',
            label: 'Tabs and panels',
            icon: 'copies',
            lede: 'Views open as tabs; tabs live in panels; the layout is yours and is remembered.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        'A new tab opens beside the one you were looking at, in its context’s colour. Opening a view that is already open focuses it instead.',
                        '**Drag a tab** to reorder it, or into another panel to keep two views side by side.',
                        'There are three panels: the main one, the one on the left holding the cluster tree, and the dock along the bottom. The **View** menu in the title bar shows and hides them, and can move the tree elsewhere.',
                        'The **details panel** opens where you last put it. Its tabs — Describe, YAML, Events and so on — are the same for every kind.',
                        'A tab with unsaved changes wears a dot instead of its close button.',
                        'Open tabs, the dock and the layout are restored next launch. Logs and shells are not, since they are connections rather than state.',
                    ],
                },
                {
                    type: 'table',
                    head: ['Shortcut', 'Does'],
                    rows: [
                        ['`⌘,` / `Ctrl+,`', 'Open settings'],
                        ['`F1`', 'Open this help'],
                        ['`⌘B` / `Ctrl+B`', 'Show or hide the cluster tree'],
                        ['`⌘+` / `⌘-` / `⌘0`', 'Zoom in, out, and back to 100%'],
                        ['`⌘S` / `Ctrl+S`', 'Save the YAML being edited, or apply the YAML patch being written'],
                        ['`Esc`', 'Close a menu or the details panel'],
                    ],
                },
            ],
        },
        {
            id: 'objects',
            label: 'Working with objects',
            icon: 'box',
            lede: 'Tables are live. Rows open a report. The report carries the actions.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        '**Tables** are backed by a watch, not polled, so there is no refresh button: a change in the cluster appears as it happens. The **Namespace** picker ticks any number of namespaces — nothing ticked is the whole cluster — and filters the cache, so it repaints instantly. It grows a filter box once there are more than eight, and a namespace deleted while ticked stays listed until it is unticked.',
                        '**Filter** in a table’s header matches any cell.',
                        '**Select** rows with the checkboxes — or ⌘-click them, and shift-click for a range — and a bar offers to patch or delete them together. The header checkbox takes every row the filter shows. Delete asks first and names the count; anything the cluster refuses, for either, stays ticked with the reason.',
                        '**Patch** a selection: set or remove a label, an annotation, or any field by dotted path — `spec.replicas`, say — on every ticked object at once. It is one JSON merge patch per object, shown before it is sent. A field value that parses as JSON is sent as such and anything else as text; a list in the patch replaces the whole list. The **YAML** mode takes a whole merge patch written the way the object reads — a value sets a field, `null` removes it — for several fields at once, names the line of anything it cannot read, and `⌘S` applies it.',
                        '**Nodes** list what `kubectl get nodes -o wide` prints — roles, version, internal and external IP, OS image, kernel and container runtime — and then capacity beside allocatable, which is what the scheduler may actually hand out.',
                        '**Describe** is the same report `kubectl describe` prints, with recent events at the end.',
                        '**Edit** opens the object’s YAML in the dock with syntax checking. Save with `⌘S`. A save against an object somebody else changed in the meantime is refused, with the API server’s own reason, rather than forced.',
                        '**Actions** depend on the kind: scale and restart for workloads; cordon, uncordon and drain for nodes; delete for anything. Each asks first.',
                        '**Logs** stream per container, with the container picker above the output.',
                        '**Shell** opens a terminal in a container, in the dock or in your own terminal emulator. On a node it does what `kubectl debug node` does: a privileged pod, removed when the terminal closes.',
                        '**Forward** opens a port forward to a pod or a service, resolving the service port to the pod port for you. Forwards are listed under Network → Port Forwards, where they can be stopped, and are remembered between sessions as requests.',
                        '**Secrets** are redacted before they enter the cache; tables show key counts only.',
                    ],
                },
                {
                    type: 'actions',
                    actions: [
                        { kind: 'show', resource: 'pods', label: 'Open the pods of the selected cluster' },
                    ],
                },
            ],
        },
        {
            id: 'dashboards',
            label: 'Dashboards and metrics',
            icon: 'dashboard',
            lede: 'What the cluster is made of, what it has been doing, and what went wrong lately.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        'The **Dashboard** at the top of every context counts nodes, pods, deployments and namespaces — each tile opens its list — shows capacity against requests, limits and live usage, and ends with recent events. An event opens its own report.',
                        '**Resources** reads capacity, allocatable, requests and limits from the API server, so it works on a cluster with no monitoring at all. Live usage comes from metrics-server when there is one, or from Prometheus through a plugin.',
                        '**Charts** come from the cluster’s Prometheus, which is found automatically and reached through the API server’s service proxy: no port forward and no second credential. When discovery finds the wrong one, or none, set the endpoint under the context’s settings at the foot of the sidebar as `namespace/service:port` or an `http(s)://` address.',
                        'The **time range** above a set of charts applies to all of them, so they can be compared.',
                        'A chart with no data at all usually means nothing is scraping that metric. Its description, behind the ⓘ, says what it needs.',
                        'Each plugin’s **Overview** tells you whether the cluster has what the plugin describes, counts what it manages as rings, and charts what its own metrics say.',
                    ],
                },
                {
                    type: 'actions',
                    actions: [{ kind: 'show', resource: DASHBOARD, label: 'Open the dashboard of the selected cluster' }],
                },
            ],
        },
        {
            id: 'helm',
            label: 'Helm',
            icon: 'helm',
            lede: 'Releases read from Helm’s own records, and the helm binary for anything that changes them.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        '**Helm → Releases** lists every release in the cluster by reading the release Secrets, so it needs no `helm` on this machine to look.',
                        'A release’s report shows its chart, values, notes, the objects it created, and its revision history. The values sit in the same viewer the editor uses, read-only: **Find**, or `⌘F` once the text has focus, searches them with the matches marked, and the gutter folds a block away.',
                        '**Upgrade**, **Rollback** and **Uninstall** run the `helm` binary. Which one, and whether it was found, is under Settings → Helm.',
                    ],
                },
                { type: 'actions', actions: [{ kind: 'settings', section: 'helm', label: 'Helm settings' }] },
            ],
        },
        {
            id: 'plugins',
            label: 'Plugins',
            icon: 'puzzle',
            lede: 'A plugin gives something installed in your clusters a place of its own in the sidebar.',
            blocks: [
                {
                    type: 'p',
                    text: 'Argo CD, Flux and Prometheus ship built in. Each unfolds into its own views instead of scattering custom resources through the definitions tree, and each has an overview that says whether this cluster actually has it. A plugin is a JSON file naming kinds the app already knows how to show, and it cannot choose where a query goes. One installed from a folder may also bring views of its own: a page drawn in a sandboxed frame, which reads only the kinds the plugin declares and never changes anything without asking you first.',
                },
                {
                    type: 'note',
                    text: 'A plugin is installed on **this machine**. What it describes is installed in **a cluster**. The sidebar lists every plugin for every cluster and marks the ones a cluster does not have as *not installed*, rather than hiding them.',
                },
                { type: 'h3', text: 'Installing one' },
                {
                    type: 'p',
                    text: 'Put the `.json` file in the plugins folder. Settings → Plugins shows the exact path for your machine and has a button to open it. Files are read from that folder and one level into any subfolder, so an unzipped pack works as it is. You can also watch folders elsewhere. Plugins are read at launch and whenever you press **Reload**.',
                },
                {
                    type: 'table',
                    head: ['Platform', 'Folder'],
                    rows: [
                        ['Linux, macOS', '`$XDG_CONFIG_HOME/k8sdockside/plugins/`, falling back to `~/.config/k8sdockside/plugins/`'],
                        ['Windows', '`%AppData%\\k8sdockside\\plugins\\`'],
                    ],
                },
                { type: 'h3', text: 'Writing one' },
                {
                    type: 'p',
                    text: 'Start with **Write a starter plugin** under Settings → Plugins, which drops a working file to edit a line at a time. The shape:',
                },
                {
                    type: 'code',
                    text: `{
    "id": "acme",
    "name": "Acme Mesh",
    "tagline": "service mesh",
    "icon": "share",
    "docs": "https://example.com/acme",
    "description": "One or two sentences on what this is and what to look at first.",
    "requires": [
        { "kind": "crd:meshes.acme.io", "label": "Meshes" },
        { "kind": "crd:sidecars.acme.io", "label": "Sidecars", "optional": true }
    ],
    "views": [
        { "id": "meshes", "label": "Meshes", "icon": "share", "kind": "crd:meshes.acme.io" },
        {
            "id": "control-plane",
            "label": "Control plane",
            "icon": "server",
            "kind": "deployments",
            "namespace": "acme-system",
            "selector": "app.kubernetes.io/part-of=acme"
        }
    ],
    "cards": [
        {
            "label": "Meshes",
            "kind": "crd:meshes.acme.io",
            "groupBy": "status.conditions[Ready]",
            "tones": { "True": "ok", "False": "error", "Unknown": "warn" }
        }
    ],
    "charts": [
        {
            "id": "requests",
            "label": "Requests",
            "attach": "overview",
            "unit": "ops/s",
            "legend": "code",
            "query": "sum by (code) (rate(acme_requests_total[5m]))"
        }
    ]
}`,
                    caption: 'A plugin needs at least one of `views`, `cards` or `charts`.',
                },
                {
                    type: 'table',
                    head: ['Field', 'What it is'],
                    rows: [
                        ['`id`', 'Lowercase letters, digits and dashes. It is part of every tab’s identity, so renaming it later loses those tabs from a saved session.'],
                        ['`name`, `tagline`, `description`, `docs`, `author`', 'What the sidebar and the overview say about it. `docs` must be an `http(s)` link.'],
                        ['`icon`', 'One of the app’s icon names, such as `rocket`, `share`, `server`, `gauge`, `puzzle`. Defaults to `puzzle`.'],
                        ['`requires`', 'The kinds the overview checks the cluster for. Mark one `optional` when the thing works without it.'],
                        ['`views`', 'The rows under the plugin. Each lists a kind: a built-in name such as `deployments`, or `crd:<plural>.<group>` for a custom resource. `namespace` pins the view; `selector` narrows it by labels.'],
                        ['`cards`', 'Live counts on the overview. `groupBy` divides the count by a field: a dotted path such as `status.health.status`, or `status.conditions[Ready]` for a condition. `tones` colours the values.'],
                        ['`charts`', 'PromQL drawn from the cluster’s Prometheus. `attach` is a kind, `dashboard` or `overview`. A chart on a kind may use `$namespace`, `$name` and `$node`.'],
                    ],
                },
                {
                    type: 'p',
                    text: 'A file with a `plugins` array is a pack. A plugin whose `id` matches a built-in one replaces it, which is how you change the Prometheus queries for a monitoring stack that names its metrics differently. Nothing about a plugin is fatal: a file that will not load is named with the reason under **Would not load**, and the rest still load.',
                },
                {
                    type: 'actions',
                    actions: [{ kind: 'settings', section: 'plugins', label: 'Plugin settings: folder, starter file, reload' }],
                },
                {
                    type: 'links',
                    links: [
                        { label: 'The full plugin guide', href: `${GUIDES}/plugins.md`, note: 'every field, the chart rules, and the three built-ins as worked examples' },
                        { label: 'The built-in plugins', href: 'https://github.com/rogerwesterbo/k8sdockside/tree/main/internal/plugins/builtin', note: 'in exactly the format above' },
                    ],
                },
            ],
        },
        {
            id: 'themes',
            label: 'Themes',
            icon: 'display',
            lede: 'Every colour the app draws comes from a theme, and a theme is a JSON file of colours.',
            blocks: [
                {
                    type: 'p',
                    text: 'Thirteen ship with the app. A theme is colours and nothing else: it cannot ship CSS, change a layout or run code, so installing one you found is about as risky as a wallpaper, and one written today still works after the app grows a screen its author never saw.',
                },
                { type: 'h3', text: 'Installing one' },
                {
                    type: 'p',
                    text: 'Put the `.json` file in the themes folder. Settings → Themes shows the exact path and opens it. Like plugins, themes are read from the folder and one level into subfolders, from any extra folder you watch, at launch and on **Reload**.',
                },
                {
                    type: 'table',
                    head: ['Platform', 'Folder'],
                    rows: [
                        ['Linux, macOS', '`$XDG_CONFIG_HOME/k8sdockside/themes/`, falling back to `~/.config/k8sdockside/themes/`'],
                        ['Windows', '`%AppData%\\k8sdockside\\themes\\`'],
                    ],
                },
                { type: 'h3', text: 'Writing one' },
                {
                    type: 'p',
                    text: '**Write a starter theme** under Settings → Themes drops a complete file with every colour filled in. Change one, press Reload, look at it. The minimum is an `id`, a `name` and a `base`; every colour is optional and inherits from the built-in theme matching your base:',
                },
                {
                    type: 'code',
                    text: `{
    "id": "acme-neon",
    "name": "Acme Neon",
    "tagline": "loud dark · neon pink",
    "base": "dark",
    "author": "you",
    "tokens": {
        "bg": "#0b0b12",
        "bg-sidebar": "#10101a",
        "accent": "#ff3d9a",
        "accent-text": "#1a0010"
    }
}`,
                    caption: 'A real, complete theme. `base` is `dark` or `light`, and decides which built-in fills in what you leave out.',
                },
                {
                    type: 'table',
                    head: ['Token', 'What it colours'],
                    rows: [
                        ['`bg`, `bg-sidebar`, `bg-panel`, `bg-raised`', 'The window; the sidebar, settings rail and status bar; panels on the window; controls standing off a surface.'],
                        ['`bg-hover`, `bg-active`', 'Laid over a surface on hover, and when selected. Usually translucent so they work everywhere.'],
                        ['`border`, `border-soft`', 'The structural edge of a panel, and the much fainter line between rows.'],
                        ['`text`, `text-dim`, `text-faint`', 'Body text; secondary text; small print such as counts and paths.'],
                        ['`accent`, `accent-text`', 'Selection, focus rings, links and primary buttons, and the text drawn on top of them. Change one, change both.'],
                        ['`ok`, `warn`, `error`', 'Healthy, not-quite, and failed. These mean something; do not reuse them for decoration.'],
                        ['`scrollbar`, `scrollbar-hover`', 'The scrollbar thumb, at rest and under the pointer.'],
                        ['`chart-1` … `chart-8`, `chart-grid`', 'The series colours in a chart, in order, and its gridlines. The order is chosen to stay apart under colour-blindness; restep them, but do not shuffle them.'],
                    ],
                },
                {
                    type: 'p',
                    text: 'Colours are hex, the CSS colour functions, or `transparent`. Named colours such as `red` are refused. The app measures every text colour against every surface and flags anything under 4.5:1 on the theme’s card; it is advice, and the theme still loads. A theme whose `id` matches a built-in replaces it, and a file with a `themes` array is a pack.',
                },
                {
                    type: 'actions',
                    actions: [{ kind: 'settings', section: 'themes', label: 'Theme settings: folder, starter file, every colour' }],
                },
                {
                    type: 'links',
                    links: [
                        { label: 'The full theme guide', href: `${GUIDES}/themes.md`, note: 'every token with what it is drawn on, and the contrast rules' },
                        { label: 'The built-in themes', href: 'https://github.com/rogerwesterbo/k8sdockside/tree/main/internal/themes/builtin', note: 'the same shape yours is; any of them is a starting point' },
                    ],
                },
            ],
        },
        {
            id: 'app',
            label: 'Updates and settings',
            icon: 'settings',
            lede: 'Where the app keeps things, and the one place it reaches out to.',
            blocks: [
                {
                    type: 'list',
                    items: [
                        '**Settings** live in one JSON file: `$XDG_CONFIG_HOME/k8sdockside/settings.json` on Linux and macOS, `%AppData%\\k8sdockside\\settings.json` on Windows. The path is shown in the status bar and under Settings → About, with a button to open it. Deleting it is how you start over. Your themes and plugins sit in folders beside it.',
                        '**Updates.** The bell in the title bar says when a newer release is out, and offers the release page and the download for the way this build was installed. It is one request to GitHub shortly after launch and every six hours, carrying nothing but the app’s name and version. Switch it off under Settings → Behaviour; the check-now button under About works either way.',
                        '**Nothing else leaves the machine.** Clusters are dialled only when you open something on them, and Prometheus is reached through the API server you are already talking to. A cluster’s client is kept for two minutes after the last tab or poll on it, so switching tabs does not rebuild it, and a kind the cluster does not have is looked for again at most once a minute.',
                        '**Credentials.** Client certificates, tokens and `exec` credential plugins all work, because the app uses the same client library `kubectl` does.',
                    ],
                },
                {
                    type: 'actions',
                    actions: [
                        { kind: 'settings', section: 'about', label: 'About and updates' },
                        { kind: 'settings', section: 'behaviour', label: 'Behaviour' },
                    ],
                },
                {
                    type: 'links',
                    links: [
                        { label: 'K8s Dockside on GitHub', href: 'https://github.com/rogerwesterbo/k8sdockside', note: 'source, releases, and where to report a problem' },
                        { label: 'Verifying a download', href: 'https://github.com/rogerwesterbo/k8sdockside/blob/main/SECURITY.md#verifying-a-download' },
                    ],
                },
            ],
        },
    ],
};
