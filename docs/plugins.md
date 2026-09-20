# Plugins

A solution plugin gives something installed *in your clusters* — Argo CD, Flux,
Prometheus — a place of its own in the sidebar, instead of leaving its custom
resources scattered through the Custom Resource Definitions tree under group
names like `kustomize.toolkit.fluxcd.io`.

Three ship with the app: Argo CD, Flux and Prometheus. Others live in
repositories of their own and are a button away in **Settings → Plugins →
Available**: cert-manager, MetalLB, Cilium, Calico, KubeVirt, Vitistack, an
optimization advisor and an image inventory. The
sidebar suggests one for any cluster running what it is about. Anything else is
a JSON file you drop in a folder, or a repository you give the address of.

> **Writing one of your own?** Start with [Writing a plugin](writing-plugins.md),
> a step-by-step guide from an empty folder to a published plugin. This page is
> the reference it points into.

Most of a plugin is data and nothing else: it names resource kinds the app
already knows how to list and says how to arrange and summarise them. That part
cannot ship code or CSS, which is what makes installing it about as risky as
installing a stranger's wallpaper, and why it keeps working as the app grows.

A plugin may *also* ship [views of its own](#views-of-its-own) — an HTML page
drawn in a sandboxed frame, for when a solution is better read as something
other than rows: an Argo CD application's resource tree, a virtual machine's
console and state. Those are code, and are fenced accordingly, whether the
plugin came from a folder, a repository, or with the app.

Built-in and installed plugins are the same thing in two places. A built-in can
have pages, actions and panels exactly like a plugin from a repository — the
built-in Argo CD plugin has all three — and a plugin you install under a
built-in's id [replaces it](#replacing-a-built-in).

## The distinction everything here turns on

**A plugin is installed on your machine. The solution it describes is installed
in a cluster.** Those come apart constantly — you keep the Argo CD plugin and
open a cluster that has never heard of it — so the app never conflates them:

- The sidebar shows every installed plugin under **Plugins**, for every
  cluster, and marks the ones this cluster does not appear to have as *not
  installed*. It does not hide them: you installed the plugin, and a row
  vanishing without explanation is worse than a row that says why it is quiet.
- Each plugin's **Overview** is where that gets explained properly — which of
  the kinds it needs this cluster serves, and which it does not.
- Its other views still open. They report `this cluster does not serve
  applications.argoproj.io — the argoproj.io API is not installed`, which is the
  ordinary answer for an optional API, not a failure.

## Installing one

Put the `.json` file in the plugins folder:

| Platform | Folder |
| --- | --- |
| Linux, macOS | `$XDG_CONFIG_HOME/k8sdockside/plugins/`, falling back to `~/.config/k8sdockside/plugins/` |
| Windows | `%AppData%\k8sdockside\plugins\` |

**Settings → Plugins** shows the exact path, with a button to open it. Files are
read from that folder and one level into any subfolder, so a pack cloned or
unzipped into a directory of its own works as it is. You can also point the app
at folders elsewhere with **Watch another folder**. Plugins are read at launch
and whenever you press **Reload**.

This is the same folder layout, the same starter-file button and the same
"would not load" reporting as [themes](themes.md), on purpose.

### The plugins the app knows of

**Settings → Plugins → Available** lists plugins kept in repositories of their
own, each with a line on what it shows, links to what it is about, and an
**Install** button that clones it into the plugins folder (see
[Installing from a repository](#installing-from-a-repository)). A card says
which of your clusters run the product, where the sidebar has read their
definitions.

| Plugin | Category | By | Repository | Suggested for clusters serving |
| --- | --- | --- | --- | --- |
| cert-manager | Security | K8s Dockside | [k8sdockside/certmanager](https://github.com/k8sdockside/certmanager) | `crd:certificates.cert-manager.io` |
| Longhorn | Storage | K8s Dockside | [k8sdockside/longhorn](https://github.com/k8sdockside/longhorn) | `crd:volumes.longhorn.io` |
| Rook Ceph | Storage | K8s Dockside | [k8sdockside/rookceph](https://github.com/k8sdockside/rookceph) | `crd:cephclusters.ceph.rook.io` |
| MetalLB | Networking | K8s Dockside | [k8sdockside/metallb](https://github.com/k8sdockside/metallb) | `crd:ipaddresspools.metallb.io` |
| Cilium | CNI | K8s Dockside | [k8sdockside/cilium](https://github.com/k8sdockside/cilium) | `crd:ciliumnetworkpolicies.cilium.io` |
| Calico | CNI | K8s Dockside | [k8sdockside/calico](https://github.com/k8sdockside/calico) | `crd:ippools.crd.projectcalico.org` |
| Kube-OVN | CNI | K8s Dockside | [k8sdockside/kubeovn](https://github.com/k8sdockside/kubeovn) | `crd:subnets.kubeovn.io` |
| Flannel | CNI | K8s Dockside | [k8sdockside/flannel](https://github.com/k8sdockside/flannel) | its `kube-flannel` DaemonSet — it has no custom resources |
| KubeVirt | Virtualization | K8s Dockside | [k8sdockside/kubevirt](https://github.com/k8sdockside/kubevirt) | `crd:virtualmachines.kubevirt.io` |
| Vitistack | Platform | K8s Dockside | [k8sdockside/vitistack](https://github.com/k8sdockside/vitistack) | `crd:kubernetesclusters.vitistack.io` |
| Image inventory | Images | K8s Dockside | [k8sdockside/image-inventory](https://github.com/k8sdockside/image-inventory) | — works on any cluster |
| Optimization advisor | Cost & efficiency | K8s Dockside | [k8sdockside/optimization](https://github.com/k8sdockside/optimization) | — works on any cluster |

Every card credits its author, and says whether the plugin is **Official** —
kept alongside the app by its author — or from the **Community**. Yours can be
on this list: see [Get listed in the app](writing-plugins.md#get-listed-in-the-app).

The list is long enough now that reading all of it is the wrong way to find
one, so the top of the section has a search box, a row of category chips and an
order. They narrow every list at once — available, built in, installed and the
ones read from folders you watch — because "where is the one for storage" is
not a question about which folder a plugin happens to be in. The search matches
a plugin's name, id, tagline, description, author and category, and every word
has to match, so a second word narrows. A card's category tag is also a filter:
pressing it shows only that category, and pressing it again undoes that.

A product with no custom resources of its own cannot be recognised that way —
Flannel is a DaemonSet, a ConfigMap and nothing else — so such an entry names
the workload to look for instead, and the app asks the cluster for it once,
alongside the definitions. Only plugins that are not installed are asked about.

When a cluster serves one of those kinds, or runs that workload, and no plugin
with that id is installed, the cluster's **Plugins** section in the sidebar
shows a faint *get plugin* row that opens Settings on it. Nothing is cloned from the sidebar.
The cross on the row stops the suggestion for good; the card in Settings can
bring it back.

The list is compiled into the app (`internal/plugins/known.json`) rather than
fetched, so the app does not ask a server what exists every time it starts. A
plugin that is not on it installs exactly the same way, from its address.

## Writing one

Start with **Settings → Plugins → Write a starter plugin**, which drops a
working file you can edit a line at a time.

```json
{
    "$schema": "https://raw.githubusercontent.com/k8sdockside/k8sdockside/main/docs/plugin.schema.json",
    "id": "acme",
    "name": "Acme Mesh",
    "version": "1.0.0",
    "minAppVersion": "0.0.15",
    "tagline": "service mesh",
    "category": "networking",
    "icon": "share",
    "docs": "https://example.com/acme",
    "links": [
        { "label": "acme.io", "url": "https://acme.io" },
        { "label": "GitHub", "url": "https://github.com/acme/mesh" }
    ],
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
    ]
}
```

| Field | | |
| --- | --- | --- |
| `id` | required | Lowercase letters, digits and dashes. It appears in every tab's identity, so changing it later loses those tabs from a saved session. |
| `name` | required | What the sidebar calls it. |
| `tagline` | optional | One short line, shown under the name on the overview. |
| `category` | optional | What the plugin is about, in one word: `storage`, `cni`, `networking`, `security`, `images`, `observability`, `delivery`, `virtualization`, `cost`, `platform` or `other`. `cni` is the pod network itself — a cluster has one, and those plugins are alternatives to each other; `networking` is what sits on top of it. Settings groups, filters and searches by it. Left out, it is `other`. Needs K8s Dockside 0.1.1 or newer. |
| `description` | optional | A paragraph on the overview. Worth writing: it is where you say what to look at first. |
| `icon` | optional | See [icons](#icons). Defaults to `puzzle`. |
| `logo` | optional | The plugin's own mark, drawn in place of `icon` wherever the app names it. See [A mark of its own](#a-mark-of-its-own). Needs a `ui/` folder, and K8s Dockside 0.0.30 or newer. |
| `docs` | optional | A link on the overview. `http(s)` only. |
| `links` | optional | What the plugin is about — the product's own site, its source, its documentation: `[{ "label", "url" }]`, `http(s)` only, at most eight. The label defaults to the address's host. Shown on the plugin's card in Settings, in the generated overview's foot, and handed to a page of its own through `ready()`. |
| `version` | optional | The plugin's own version, `1.2.0` or `v1.2.0`. Shown on its card. |
| `minAppVersion` | optional | The oldest release of K8s Dockside the plugin works with. See [Versions](#versions). |
| `$schema` | optional | Where an editor finds [the schema](#checking-a-plugin). Ignored by the app. |
| `author` | optional | Who wrote it — you, or your company. Credited on the plugin's card in Settings, on its overview, and on a line the app draws under an overview page of its own. At most 80 characters. |
| `authorUrl` | optional | Where to find the author — a profile, a company site. `http(s)` only; the author's name links to it. Needs `author`, and K8s Dockside 0.0.19 or newer. See [Credit](writing-plugins.md#credit). |
| `requires` | optional | What the overview checks this cluster for: kinds, or — with a `selector` — the objects themselves. See [Requirements](#requirements). |
| `views` | required¹ | The rows under the plugin in the sidebar. |
| `cards` | optional¹ | The live counts on the overview. |
| `charts` | optional¹ | Time-series graphs from the cluster's Prometheus. See [Charts](#charts). |
| `actions` | optional¹ | Buttons on objects' action bars. See [Buttons on an object](#buttons-on-an-object). |
| `sections` | optional¹ | Panels of the plugin's own in objects' detail views. See [Panels on an object](#panels-on-an-object). |
| `overview` | optional¹ | A landing page of the plugin's own, in place of the generated one. See [An overview of its own](#an-overview-of-its-own). |

¹ A plugin needs at least one of `views`, `cards`, `charts`, `actions`,
`sections` or `overview` — otherwise there would be nothing to show.

### Kinds

Everywhere a `kind` appears it names something the app can already open:

- a built-in name — `pods`, `deployments`, `services`, `configmaps`, … (the
  sidebar's own kinds, listed in `frontend/src/lib/catalogue.ts`)
- `crd:<plural>.<group>` for a custom resource — `crd:applications.argoproj.io`

A kind that is neither is refused when the file is read, and named under **would
not load**, rather than becoming a sidebar row that opens onto an error.

### Views

A view is one row under the plugin and one tab when clicked.

| Field | | |
| --- | --- | --- |
| `id` | required | Lowercase letters, digits and dashes, unique within the plugin. It is what a restored tab is found by, so renaming one loses its place in a saved session. `overview` is reserved. |
| `label` | required | The row's text. |
| `kind` | required | What it lists. |
| `icon` | optional | Defaults to `puzzle`. |
| `namespace` | optional | Pins the view to one namespace. The tab's namespace picker is replaced with a note saying where you are — a view called "Argo CD's own workloads" is not answering a question about `kube-system`. |
| `selector` | optional | A label selector in the usual `a=b,c in (d,e)` syntax. It is what lets a plugin offer a view of a *built-in* kind — the Deployments that are Argo CD's — rather than only of custom resources nothing else owns. |

A malformed selector or namespace is caught when the file is read, not when the
tab is opened.

**Every plugin gets an Overview view whether or not it declares one**, always
first. It is where "is this even in this cluster?" is answered, which matters
most exactly when the CRDs are missing and every other row would open onto the
same error. The app draws it from the manifest — requirements, cards, charts,
views — so every plugin's looks alike; a plugin that ships pages of its own can
[draw its own instead](#an-overview-of-its-own).

### Requirements

`requires` is what the overview checks the cluster for, and what decides whether
the sidebar row reads as installed.

A plugin counts as installed when **every non-optional** requirement is served.
That is deliberately not "any of them": a cluster serving one Argo CD CRD out of
three is a broken install, not a working one, and saying "installed" would send
the reader looking in the wrong place.

Mark the extras `"optional": true`. Argo CD without ApplicationSets is still
Argo CD, and an absent optional requirement is shown greyed rather than as a red
cross.

| Field | | |
| --- | --- | --- |
| `kind` | required | A built-in name, or `crd:<plural>.<group>`. |
| `label` | optional | How the overview names it. Defaults to the kind. |
| `optional` | optional | Reported, but does not decide whether the plugin counts as installed. |
| `namespace`, `selector` | optional | Ask for *objects* rather than for the kind. Needs K8s Dockside 0.1.1 or newer. |

**A requirement usually names a kind, because that is enough.** A custom
resource is served only where the product that defines it is installed, so
`crd:applications.argoproj.io` being there *is* Argo CD being there, and the
answer comes free from definitions the sidebar has already read.

**A product that defines no custom resources needs more.** Flannel is a
DaemonSet, a ConfigMap and nothing else, so a manifest requiring `daemonsets`,
`nodes` and `configmaps` requires only kinds every cluster serves — and reads as
installed in every cluster, which helps nobody. Give such a requirement a
selector, and it is met only if the objects are actually there:

```json
{ "kind": "daemonsets", "label": "Flannel daemonset", "selector": "app=flannel" }
```

That costs one list per requirement against each cluster, asked once alongside
its definitions, so name only what identifies the product. A cluster that
cannot be asked leaves the row as it was rather than reading as "not
installed": not knowing and knowing it is absent are different answers.

### Cards

A card is one tile on the overview: how many of a kind there are, optionally
divided by one of their own fields. A divided count is drawn as a ring with the
total in the middle and one slice per value, coloured by its tone, with the
values listed beside it. A card's title opens the view that lists the same
kind, when the plugin has one.

| Field | | |
| --- | --- | --- |
| `label` | required | The tile's heading. |
| `kind` | required | What to count. |
| `groupBy` | optional | The field to divide by. Omit for a plain count. |
| `tones` | optional | Maps a field value to `ok`, `warn`, `error` or `info`. A value with no entry is drawn plainly, so only name the ones that mean something. |
| `namespace`, `selector` | optional | Narrow what is counted, exactly as on a view. |

**Field paths** come in two shapes, and only two:

```
status.health.status        a plain dotted path
status.conditions[Ready]    the status of the condition of that type
```

The second exists because conditions are the near-universal Kubernetes idiom and
a dotted path cannot reach into a list. It is not a query language: a plugin file
comes from outside the app, and "an address" is a much smaller thing to accept
than "an expression". A path that is not one of these two shapes is refused when
the file is read.

Objects whose field is absent are counted in their own bucket, shown as *no
status yet* — a resource that has not been reconciled has no status at all, and
that is worth seeing rather than rounding to zero.

Buckets are ordered **worst first**, not by count: one Degraded application among
forty Healthy ones is the whole reason the tile is on screen, and sorting by size
would bury it.

## Charts

A card counts what the API server can tell you. A **chart** draws what it cannot:
CPU over the last hour, memory climbing towards a limit, requests by response
code. Those come from the cluster's Prometheus.

```json
"charts": [
    {
        "id": "cpu",
        "label": "CPU",
        "attach": "pods",
        "unit": "cores",
        "legend": "container",
        "description": "Cores used per container, averaged over five minutes.",
        "query": "sum by (container) (rate(container_cpu_usage_seconds_total{namespace=\"$namespace\",pod=\"$name\",container!=\"\"}[5m]))"
    }
]
```

| Field | | |
| --- | --- | --- |
| `id` | required | Lowercase letters, digits and dashes, unique within the plugin. |
| `label` | required | The chart's title. |
| `attach` | required | Where it is drawn — see below. |
| `query` | required | PromQL. |
| `unit` | optional | `cores`, `bytes`, `bytes/s`, `percent`, `ops/s`, `seconds`, `count`, or omitted for a plain number. It decides only how values are written — 512 MiB rather than 536870912. `percent` wants a fraction: a query returning 0.87 is written as 87%, so a ratio goes in as it comes out of PromQL, not multiplied by a hundred. |
| `legend` | optional | The Prometheus label each series is named by. Omitted, a query returning several series names them by their whole label set. |
| `description` | optional | A sentence behind the ⓘ next to the title. Worth writing: what a query actually measures is rarely obvious from a word like "CPU". |

### Where a chart is drawn

`attach` takes one of:

| Value | Where it appears | Variables |
| --- | --- | --- |
| a kind — `pods`, `nodes`, `crd:…` | the detail panel of any object of that kind | `$namespace`, `$name`, `$node` |
| `dashboard` | the cluster's own dashboard tab | none |
| `overview` | the plugin's own overview — never another plugin's | none |

The three variables are the only things interpolable into a query, and a chart
attached to `dashboard` or `overview` may not use them — there is no object to
name, so the query would always come out empty. Both rules are checked when the
file is read.

`$node` and `$name` are the same value on a node's own charts, so either reads
correctly there.

### What is and is not allowed in a query

The query goes to Prometheus **as written**. That is a different thing from the
field paths a card uses: a field path is an expression *this app* evaluates, so
it is kept to two shapes it can evaluate safely, while PromQL is passed through
untouched to a server that exists to answer it. There is nothing to sandbox.

What *is* policed is the substitution. A value lands inside a label matcher, so
it is checked against what a Kubernetes name can contain before it goes in —
anything with a quote, a brace or a newline in it is refused rather than escaped.
A query referring to a variable that does not exist is refused when the file is
read, rather than left in for Prometheus to read the `$` as an operator.

A plugin also cannot choose *where* a query goes. The endpoint is always the one
resolved for the current context, so a plugin cannot point queries at somewhere
else.

### Where the data comes from

The app looks for a Prometheus in the cluster and reaches it **through the API
server's service proxy** — the same API server the kubeconfig already
authenticates against. That means no port-forward, no second credential, and it
works wherever the kubeconfig works, including through a bastion that only
exposes the API server. It needs the `services/proxy` permission, which most
read-only roles include.

Discovery looks for a Service labelled `app.kubernetes.io/name=prometheus` first,
then `app=prometheus`, then a short list of well-known names — and within each,
for a port named `web`, `http-web`, `http` or `api`, or any port numbered 9090.
The label is a much stronger signal than a name, which is why it is checked
first: a cluster easily has several things called prometheus-something.

A cluster with no Prometheus but a **VictoriaMetrics** is found too: a single
server (`app.kubernetes.io/name` `vmsingle` or `victoria-metrics-single`) is
queried as it is, and a cluster's `vmselect` under `/select/0/prometheus`, the
path it serves the Prometheus API on. Both answer PromQL, so every chart works
unchanged.

When that finds the wrong thing or nothing, set the address on the context
itself, in the sidebar's cluster settings panel:

```
monitoring/prometheus-operated:9090     through the API server's service proxy
https://thanos.example.com              straight at the address
```

The second form sends **no credentials** — it is an address somebody typed into a
settings field, and quietly presenting the cluster's credentials to it would be a
way to leak them somewhere the kubeconfig never pointed. A Prometheus behind auth
wants a proxy in front of it, or the service form.

### Reading a chart

- Every chart on a page shares one time-range control. That is deliberate: charts
  with different windows cannot be compared, and comparing them is why they are
  next to each other.
- Series colours are `--chart-1` … `--chart-8`, assigned in order and never
  cycled. The order is what keeps neighbouring lines apart under protanopia and
  deuteranopia, so a theme may restep those tokens but should not reorder them. A
  ninth series folds into the eighth rather than being given an invented hue.
- Values are written out — in the legend, at the line's end, and in the crosshair
  readout — rather than left to be read off the axis. Hover or focus the chart and
  arrow along it for the numbers at a moment.
- The y-axis always starts at zero. These are rates and sizes, where the distance
  from nothing is the thing being read.
- A sample Prometheus could not compute leaves a gap rather than a zero — those
  mean opposite things on a chart.

### Icons

Any of: `alert`, `bell`, `book`, `box`, `certificate`, `check`, `chevron-down`,
`chevron-left`, `chevron-right`, `chevron-up`, `chip`, `clock`, `close`,
`collapse-all`, `columns`, `copies`, `copy`, `dashboard`, `database`, `display`,
`dock-bottom`, `dock-left`, `dock-right`, `dot`, `download`, `drive`, `edit`,
`exit`, `expand-all`, `file`, `folder`, `folder-plus`, `forward`, `gateway`,
`gauge`, `globe`, `grant`, `graph`, `helm`, `help`, `history`, `info`,
`layers`, `link`, `lock`, `minus`,
`monitor`, `moon`, `pause`, `pin`, `play`, `plus`, `policy`, `power`,
`priority`, `puzzle`, `refresh`, `repeat`, `restore`, `rocket`, `route`,
`rows`, `save`, `scale`, `search`, `server`, `settings`, `share`, `shield`,
`sliders`, `sort-asc`, `sort-desc`, `sort-off`, `stop`, `sun`, `terminal`,
`tick`, `trash`, `type`, `undo`, `unlock`, `user`, `users`, `webhook`.

The same names go for views and actions. Any other name is refused when the
file is read, with the nearest real one suggested, rather than drawing an
empty square.

### A mark of its own

An icon from that list says what kind of thing a plugin is about. A plugin for
a real product can do better: `logo` points at a file in its `ui/` folder, and
the app draws that instead — in the sidebar, on the plugin's overview, and on
its card in Settings.

```json
"logo": "logo.svg"
```

It is served from the same place the plugin's pages are, at
`/plugin-ui/<id>/<logo>`, as an image rather than as a page: a logo never gets
the sandbox a view gets, and nothing in it runs. The file has to be there when
the manifest is read, or the plugin is refused rather than leaving a broken
image in the sidebar.

Worth knowing before you draw one:

- **It is shown at 14 px** in the sidebar, 16–18 px on a card and 22 px on the
  overview. A detailed mark turns to mush at the small end; the app's own
  plugins use each project's icon or glyph, never its horizontal lock-up, and a
  wordmark is wasted because the plugin's name is already beside it.
- **It is never stretched.** A drawing that is not square is fitted inside the
  box and centred, so a tall glyph simply draws narrow.
- **An SVG is parsed as XML**, not as HTML. Anything unbalanced does not draw
  at all.
- **It has to work on both themes.** The app does not put a backdrop behind it,
  so a mark drawn only in near-black or near-white disappears on one of them.

If your plugin is about someone else's product, the mark is that project's
trademark: use the one it publishes, unchanged, and only to say which product
your plugin is for. The plugins shipped alongside this app take theirs from the
CNCF's artwork repository, from the project's own repository, or from its
website, and each `logo.svg` names its source in a comment at the top.

## Views of its own

A view with `"type": "custom"` opens a page from a `ui/` folder beside the
plugin's file instead of a table. It can draw anything, and it reads the cluster
through a small bridge the app answers:

```
my-plugin/
├── plugin.json
└── ui/
    ├── index.html
    └── app.js
```

```json
{
    "id": "acme",
    "name": "Acme",
    "requires": [{ "kind": "crd:meshes.acme.io" }],
    "ui": { "kinds": ["pods"], "write": true },
    "views": [
        { "id": "map", "label": "Mesh map", "icon": "share", "type": "custom" },
        { "id": "meshes", "label": "Meshes", "kind": "crd:meshes.acme.io" }
    ]
}
```

| Field | | |
| --- | --- | --- |
| view `type` | | `custom` for a page of the plugin's own; `table`, the default, for a listing. |
| view `entry` | optional | The file it opens, relative to the ui folder. Defaults to `index.html`. One page can serve several views and tell them apart by `viewId`. A custom view takes no `kind`, `namespace` or `selector`. |
| view `focus` | optional | Lets the app open the view on one object: `{ "kind": "crd:applications.argoproj.io", "hash": "selected={namespace}/{name}" }`. Wherever the app has an object of that kind in hand — a search hit, today — it offers the view, and opens the page with `hash` after the `#` in its address, `{namespace}` and `{name}` filled in and URL-encoded. `hash` defaults to `namespace={namespace}&name={name}`. The page reads `location.hash` as it loads; a new focus reloads it. The kind becomes readable by the plugin's views. |
| `ui.dir` | optional | The folder, relative to the plugin's file. Defaults to `ui`. It may not leave the file's folder. |
| `ui.kinds` | optional | Kinds the views may read, beyond those the plugin already names in `requires`, `views` and `cards`. |
| `ui.write` | optional | Lets the views *ask* to merge-patch and create objects of those kinds. |
| `ui.registries` | optional | Lets the views ask the registries of images the cluster runs which tags they have, with `registry.lookup`. The app asks, not the page. K8s Dockside 0.0.25 and newer. |
| `ui.services` | optional | In-cluster Services the views may make GET requests to, with `services.get`. See [Calling a service in the cluster](#calling-a-service-in-the-cluster). K8s Dockside 0.0.27 and newer. |

A plugin with a custom view and no `ui` block gets the defaults: a `ui/` folder,
read-only. A built-in's pages are embedded in the app from
`internal/plugins/builtin/ui/<id>/` rather than read from a folder beside a file;
`ui.dir` means nothing for one. Everything else — the sandbox, the declared
kinds, the confirmation before a write — is the same.

The page includes the bridge, which the app serves, and uses it:

```html
<script src="/plugin-ui/_sdk/k8sdockside.js"></script>
<script>
    k8sdockside.ready().then(async (ctx) => {
        // ctx: { pluginId, viewId, contextId, contextName, readable, write, registries, plugin, theme }
        const meshes = await k8sdockside.list({ kind: 'crd:meshes.acme.io', namespace: '' });
        // ...draw them
    });
</script>
```

| Call | |
| --- | --- |
| `ready()` | Resolves with what the view is looking at, once the app has answered. `plugin` in it is `{ id, name, version, docs, links, author, authorUrl }`, what the manifest says about the plugin itself — for a page to link to what it is about. |
| `list({ kind, namespace?, selector? })` | Objects of a kind, whole — `status` and all. |
| `get({ kind, namespace, name })` | One object, read live. |
| `watch(query, onItems, onError?)` | Polls `list` (`query.interval`, default 5 s). Returns a stop function. |
| `namespaces()` | The cluster's namespace names. |
| `summary()` | What the generated overview is made of for this plugin and cluster: `{ installed, checked, requirements, cards, error }`. `checked` false means the cluster could not be asked — not the same as "not installed". |
| `charts({ minutes? })` | This plugin's `overview` charts from the cluster's Prometheus, as `{ attached, range, source: { available, error, describe }, charts: [{ id, label, unit, description, error, series: [{ name, points: [{ t, v }] }] }] }`. `t` is Unix seconds. Only ever this plugin's own. |
| `patch({ kind, namespace, name, patch })` | A merge patch. Needs `ui.write`; the user sees it and confirms first. Rejects if they decline. |
| `create({ kind, namespace?, object })` | Creates one object — the whole of it, `apiVersion`, `kind` and `metadata.name` included — in `namespace`, whatever its own metadata says. Needs `ui.write`; the user sees the object and confirms first. Secrets, ServiceAccounts, RBAC, admission webhooks and CRDs are refused whatever is declared, as they are for actions. Resolves with `{ name }`; rejects if they decline. |
| `open({ kind, namespace?, name? })` | The object in the details panel, or the kind's own tab. |
| `openView(viewId)` | Another of this plugin's views, or `overview`. |
| `edit(ref)`, `logs(ref)` | The YAML editor or the log view, in the app. |
| `openUrl(url)` | An `http(s)` address in the user's browser. |
| `registry.lookup({ image, refresh? })` | What the registry of an image the cluster runs says about it: `{ image, registry, repository, tag, tags, truncated, digest, checkedAt, status, error }` — every tag it lists (up to 10000, in its own order), and the digest the image's tag points at now, comparable with a pod's `imageID`. The app asks, anonymously and over https, and keeps the answer for half an hour; `refresh` asks again if the answer is over a minute old. `status` is `ok`, `auth` (a private image: not checked), `missing`, `limited` (rate-limited), `unreachable` or `error`, with `error` in words; it rejects only when the plugin does not declare `ui.registries`, the reference is not one, or no pod in the tab's cluster runs the image. K8s Dockside 0.0.25 and newer: check `k8sdockside.registry` exists. |
| `services.get({ service, path, query? })` | A GET request to one of the Services the manifest declares, made by the app through the API server's service proxy: `{ service, status, contentType, body }`, the status and body being the Service's own. `services.json(...)` does the same and parses the body, rejecting on a status outside 200–299. Rejects when the service or path is not declared, the Service cannot be found or reached, or the proxy is not allowed. K8s Dockside 0.0.27 and newer: check `k8sdockside.services` exists. |
| `on('theme', fn)` | Called when the user changes theme. |
| `storage.get(key)`, `storage.set(key, value)`, `storage.remove(key)`, `storage.keys()` | What the page keeps between sessions — a folded section, a filter — for this plugin on this tab's cluster. Values are anything JSON can hold, at most 16 KiB each and 64 keys per plugin and cluster. The app keeps them in its settings file, never in the cluster. K8s Dockside 0.0.19 and newer: check `k8sdockside.storage` exists, and keep state in the URL hash without it. |

The SDK sets the app's colour tokens on the page's `:root` — `var(--bg)`,
`var(--text)`, `var(--accent)`, `var(--ok)`, `var(--error)` and the rest — and
keeps them in step with the app, so a view looks like the app without trying.

Complete ones to read: [k8sdockside/metallb](https://github.com/k8sdockside/metallb)
and [k8sdockside/certmanager](https://github.com/k8sdockside/certmanager)
in plain script — views, panels and an overview of their own — and the built-in
Argo CD plugin's pages, in `internal/plugins/builtin/ui/argocd/`, which are the
same thing shipped with the app.

### Calling a service in the cluster

Some of what a product knows is not in the Kubernetes API: flow logs, a query
API, a status page, served by the product's own Service. A plugin can name
those Services, and its views ask the app to make GET requests to them:

```json
"ui": {
    "services": [
        {
            "id": "flows",
            "label": "Flow logs",
            "selector": "k8s-app=flow-api",
            "port": "http",
            "paths": ["/api/v1/flows"]
        }
    ]
}
```

```js
const flows = await k8sdockside.services.json({
    service: 'flows',
    path: '/api/v1/flows',
    query: { limit: 100, namespace: ['shop', 'data'] },
});
```

| Field | | |
| --- | --- | --- |
| `id` | required | What the views name the service by. |
| `label` | optional | How the plugin's card names it. Defaults to the service name. |
| `name`, `namespace` | | The Service by name, with its namespace. |
| `selector`, `namespace` | | Or by a label selector, for a product whose namespace or release name varies; `namespace` is then optional. The first Service found with the port is used, in namespace and name order. Give `name` or `selector`, not both. |
| `port` | required | The Service port, by name or number. |
| `scheme` | optional | `http`, the default, or `https`. The API server makes the connection. |
| `paths` | required | The path prefixes the views may request, each a whole number of segments: `/api` allows `/api/flows`, not `/apikeys`. |

The request goes through the API server's service proxy, with the
credentials the app already has — the same route the charts take to a
Prometheus — so it needs the `services/proxy` permission, which the built-in
`edit` and `admin` roles have and `view` does not. The connection to the
Service's pods is made by the API server, so a network policy covering them
has to let it in; one that does not makes the call time out. The view chooses
only which declared service and which path under its prefixes; a path is
taken only as it is, so `..`, `//`, `%`, `?` and `#` are refused, and
parameters go in `query`. Requests are GET only, answered within 15 seconds
and at most 8 MiB, and the body arrives as text. What the Service answers,
a 404 included, is returned as it is; what the API server refuses — the
permission, a missing Service, one with no ready pod — rejects with that said.

A plugin can declare at most eight services, and its card in **Settings →
Plugins** names them before any of its views is opened.

### Writing the pages in TypeScript, or with a framework

[k8sdockside/image-inventory](https://github.com/k8sdockside/image-inventory)
is a complete plugin written in TypeScript and built with esbuild — an overview,
a view and a panel over core kinds, so it works on any cluster — meant to be
copied as the start of your own. The bridge's types are
`internal/plugins/sdk/k8sdockside.d.ts`; copy that file into a project to type
`window.k8sdockside`.

Anything that ends up as static files works — TypeScript, Svelte, Preact, Vue,
Lit, plain DOM. Three things differ from building for a normal web page:

1. **Include the bridge** before your own script, from the path the app serves
   it at: `<script src="/plugin-ui/_sdk/k8sdockside.js"></script>`.
2. **Bundle to a classic script, not an ES module.** The view runs in a
   sandboxed frame with an opaque origin, so `<script type="module">` is a
   cross-origin load, which not every webview allows from the app's own scheme.
   With esbuild, `--bundle --format=iife`. With Vite, `build.rollupOptions.output`
   `{ format: 'iife', inlineDynamicImports: true }`, `modulePreload: false` and
   `base: './'`, and drop `type="module"` / `crossorigin` from the built HTML if
   Vite leaves them in. Vite builds one IIFE per page, so a plugin with several
   pages builds each on its own.
3. **No network.** `fetch`, XHR and websockets are refused by the page's
   Content-Security-Policy; everything about the cluster comes through
   `window.k8sdockside` — a service in the cluster through `services.get`.
   Bundle fonts and images, or inline them.

Commit what the build produces. Installing from a repository clones it and
reads `plugin.json` and the ui folder as they are; nothing is built on the
user's machine.

### What a view can and cannot do

The page runs in an `<iframe sandbox="allow-scripts">` — no same-origin, no
forms, no popups — and every file served from the ui folder carries a
Content-Security-Policy that repeats the sandbox, forbids `fetch`, XHR and
websockets outright, and loads scripts, styles and images only from the plugin's
own folder and the SDK. The app refuses its own runtime to any request from a
sandboxed frame. What the page learns about the cluster, it asks the app for.

- It reads **only the kinds the plugin declares**, checked in the frame and
  again in Go. **Secrets are never readable**, whatever is declared.
- It **never writes without you**: each patch or new object is shown in a
  dialog the page cannot reach, with the object and cluster it is for, and
  applied only on **Apply change** or **Create**.
- It sees only the cluster of the tab it is in.
- It reaches **only the services the plugin declares**, with GET requests the
  app makes, and only under the paths declared.
- **Settings → Plugins** lists, on the plugin's card, how many kinds its views
  read (hover for which), whether they may ask to change them, and which
  services they call.

What it *can* do is read the kinds it declares and, if it navigates its frame
somewhere else, send what it read there. That is the honest limit of running
someone's code: install views from people you would trust with read access to
those kinds.

Switching away from a custom view's tab unloads the page, the way it closes a
table's watch; keep anything worth keeping in the URL hash, or re-read it.

## An overview of its own

The generated overview is all a plugin that is only data can have, which is
why every one of them looks the same. A plugin that already ships pages can
replace it:

```json
"overview": { "entry": "overview.html" }
```

| Field | | |
| --- | --- | --- |
| `overview.entry` | optional | The file its Overview tab opens, relative to the ui folder. Defaults to `index.html`. |

The Overview row stays first under the plugin in the sidebar and opens the page
in the same sandboxed frame as a custom view, with the same bridge and the same
limits; `ready()` gives it `viewId: "overview"`. Two calls exist for it in
particular:

- `summary()` answers what the generated page leads with — which of the kinds
  in `requires` this cluster serves, and the manifest's card counts — so the
  page can still say first whether the solution is here at all. Say it: a page
  that shows an empty dashboard for a cluster without the CRDs is worse than
  the generated one.
- `charts({ minutes })` hands over the plugin's `overview` charts, for the page
  to draw its own way. The generated chart panel is not on screen, so without
  it those charts would go nowhere.

[k8sdockside/metallb](https://github.com/k8sdockside/metallb) has
one: a verdict and a sentence on what MetalLB is doing, the addresses in use as
a ring by pool, the path an address takes drawn as five stages that turn red
where it is broken, what needs attention, MetalLB's recent events, its charts,
and the plugin's links in its foot.

## Buttons on an object

`actions` put buttons on the action bar of every object of a kind — a virtual
machine's Start, Pause and Migrate, an application's Sync. They are data, like
the rest of the manifest, so a built-in plugin can have them too:

```json
"actions": [
    {
        "id": "pause",
        "label": "Pause",
        "icon": "pause",
        "kind": "crd:virtualmachines.kubevirt.io",
        "done": "{name} paused",
        "when": [{ "field": "status.printableStatus", "in": ["Running"] }],
        "request": {
            "type": "subresource",
            "apiGroup": "subresources.kubevirt.io",
            "version": "v1",
            "resource": "virtualmachineinstances",
            "subresource": "pause"
        }
    }
]
```

| Field | | |
| --- | --- | --- |
| `id`, `label`, `icon` | | As on a view. |
| `kind` | required | The objects the button appears on. |
| `tone` | optional | `danger` colours it apart. |
| `confirm` | optional | A question asked before it runs. `{name}` and `{namespace}` are filled in. Empty runs on the click. |
| `done` | optional | The notice once it has worked. |
| `when` | optional | Conditions on the object, all of which must hold: `{ "field", "in": [...] }`, `{ "field", "notIn": [...] }`, or just `{ "field" }` for "is set". The field is a path as in `cards`. `notIn` also holds when the field is absent. |
| `request` | required | What pressing it does — one of the three below. |

| `request.type` | |
| --- | --- |
| `patch` | Merge-patches the object with `patch`. |
| `subresource` | Calls `/apis/<apiGroup>/<version>/namespaces/<ns>/<resource>/<name>/<subresource>` with `method` (`PUT`, the default, or `POST`) and an optional JSON `body`. The kind must be a custom resource; `apiGroup` defaults to its group and may only be that group or one under it (`subresources.kubevirt.io` under `kubevirt.io`); `resource` defaults to its plural. |
| `create` | Creates `object` (with its `apiVersion` and `kind`), of the app kind `kind`, in the object's own namespace. `metadata.generateName` works; the name given is in the notice. |

Strings anywhere in `patch`, `body` and `object` may use `{name}` and
`{namespace}`.

What the button sends is read from the manifest by the backend: the webview only
says *which* action on *which* object, so a button does what the file says and
nothing else. The object is read again when the button is pressed, and an
action whose `when` no longer holds is refused. No action may write Secrets,
ServiceAccounts, RBAC, admission webhooks or CRDs, or call anything in those
API groups or the core group.

The bar reads each object's offered actions when it opens and every few seconds
after, so a stopped machine offers Start and, a moment after pressing it, Pause.
A plugin from outside the app that brings actions for a kind the app has its own
product buttons for — the app's own virtual machine buttons — takes over from
them rather than adding a second set.

## Panels on an object

`sections` put one of the plugin's own pages in the detail view of every object
of a kind, above the YAML report. They are custom views in all but where they
are drawn, so they need a `ui/` folder and the same bridge:

```json
"sections": [
    { "id": "machine", "label": "Machine", "kind": "crd:virtualmachines.kubevirt.io", "entry": "vm.html", "height": 260 }
]
```

`height` is where the frame starts; the SDK measures the page and resizes it to
fit. The section's `kind` is readable by the page without declaring it again.
The page gets these on top of the calls above:

| Call | |
| --- | --- |
| `ready()` | As before, with `object: { kind, namespace, name }` and `sectionId`. |
| `object()` | The object the section is drawn for, read live. |
| `actions(ref?)` | Which of this plugin's actions are offered on the object right now — for drawing a button group of its own. |
| `run(actionId, ref?)` | Runs one of the plugin's declared actions on the object. The app asks the user first, **every time**, since the click came from inside the page. |
| `resize(height)` | Sets the height by hand. |

The panel's page is reloaded when the detail view moves to another object.

[k8sdockside/kubevirt](https://github.com/k8sdockside/kubevirt)
has both: the seven lifecycle actions on the action bar, and a *Machine* panel
with the state, the node, the addresses, the conditions and recent migrations,
and a round icon button group drawn from `actions()`.

## Installing from a repository

A plugin can live in a repository of its own. Put `plugin.json` at the root and
its `ui/` folder beside it, and install it with **Settings → Plugins → From a
repository**: the app runs `git clone --depth 1` into the plugins folder, and
the card gets an **Update from repository** button that runs
`git pull --ff-only`. `https://`, `ssh://` and `git@host:owner/repo` addresses
are accepted; git never prompts, so a private repository needs credentials git
can find on its own. `package.json`, `tsconfig*.json` and the like at the root
are skipped rather than read as plugins.

Installing a plugin whose folder is already there as a clone of the same
repository updates that clone instead of refusing — which is what gets a
repository cloned before its plugin was pushed out of the way. A folder of that
name holding anything else is left alone, and a clone in the plugins folder
with no plugin file at its root is listed under **would not load**.

A clone that worked but holds nothing that loads is reported as a failure, with
the reasons — "installed" followed by nothing appearing would leave you with
nowhere to look. The folder is kept, so a plugin waiting on a newer app loads
once the app is updated, and one with a mistake in it can be fixed and updated
in place.

**Uninstall** on an installed plugin's card deletes what it came in: the folder
it was cloned or copied into, or the file, for one dropped straight into the
plugins folder. The app says exactly what that is — and which other plugins go
with it, the rest of a pack or of a folder holding several — and asks first. A
built-in cannot be uninstalled, only switched off, and a plugin read from a
folder you added with **Watch another folder** is never deleted: stop watching
the folder instead. A plugin both installed and in a watched folder — the one
you are writing, say — loads from the plugins folder, and the other copy is
listed under **would not load**; uninstalling the installed copy lets the other
one load.

A plugin read from a watched folder is listed apart, under **From folders you
watch**, with the folder it is in. It gets no **Update** and no **Uninstall** —
the folder is your checkout, to pull and to delete yourself — only a button to
stop watching the folder. A known plugin it shares an id with is still offered
under **Available**: installing it puts a copy in the plugins folder, which
then takes the id, and the watched copy is listed under **would not load**.

## Shipping several at once

A file with a `plugins` array is a pack, which is how a collection is
distributed as one file:

```json
{
    "name": "Acme Pack",
    "author": "Acme Inc",
    "authorUrl": "https://acme.example",
    "version": "1.0.0",
    "plugins": [ { "id": "acme", "...": "..." }, { "id": "acme-edge", "...": "..." } ]
}
```

The pack's `author` and `authorUrl` are given to every plugin in it that names
no author of its own.

## Replacing a built-in

A plugin whose `id` matches a built-in one takes its place — that is how you
retune the Flux plugin's view list without renaming it everywhere. Two *user*
plugins claiming the same id is a mistake: the first found wins, the other is
reported under **would not load**.

## Versions

`minAppVersion` is the oldest release of K8s Dockside a plugin works with:

```json
"minAppVersion": "0.0.15"
```

An older app refuses the plugin with that said — `plugin "metallb" needs K8s
Dockside 0.0.15 or newer, and this is 0.0.14 -- update the app to use it` —
before reading anything else in it, so a plugin using fields the older app has
never heard of is not reported field by field. A development build of the app
loads every plugin whatever it asks for. Leave it out and every app tries to
load the plugin; set it to the first release with everything the plugin uses.
0.0.15 is the first with pages' own overviews, links and this field itself.

`version` is the plugin's own, shown on its card. Neither is compared with
anything else yet.

## Checking a plugin

A plugin is checked when it is read, and the checks are strict about what is
put in:

- **The JSON**, with the line and column of a syntax error.
- **Every field name.** A member the manifest has no field for is refused, with
  the one it was probably meant to be: `unknown field "lable" in views[0]
  (line 12, column 9) -- did you mean "label"?`. A misspelling read leniently
  would be a plugin that loads and quietly does nothing.
- **Every field's type**: `views[0].label should be a string, not a number`.
- **Every kind** in `requires`, views, cards, charts, actions, sections and
  `ui.kinds` is one the app can open, and no page may read Secrets.
- **Ids** are lowercase letters, digits and dashes, and unique within the plugin.
- **Icons** are the app's; **links** and `docs` are `http(s)`; `version` and
  `minAppVersion` are versions.
- **Queries** parse as PromQL, units are known, and cluster-wide ones do not ask
  about one object.
- **Selectors** parse and **namespaces** are namespace names.
- **Actions**' requests are complete and do not write what no plugin may write.
- **Pages**: the ui folder exists, and every page a view, section or overview
  opens is in it and is an HTML file.

Everything wrong with a plugin is reported at once, one reason per line, under
**Settings → Plugins → Would not load**, and the status bar says so once a
session so nobody has to go looking.

The same checks run outside the app:

```
go run github.com/k8sdockside/k8sdockside/cmd/plugincheck@main .
```

reads the folder it is given exactly as the app reads it once cloned — the
`.json` files at its root — and prints what it found or why it would not load,
exiting non-zero on anything wrong. `-app 0.0.14` checks as that release would.
The repositories listed above run it in CI.

For an editor, `docs/plugin.schema.json` is a JSON Schema of the manifest; name
it in `$schema` and the editor checks field names, types, icons and versions as
you type. It cannot know which kinds a cluster serves or whether a query
parses; `plugincheck` does.

## When something is wrong

Nothing about a plugin is fatal. One unreadable file does not cost you the
plugins either side of it; one bad plugin inside a pack does not cost you the
rest of the pack; and anything refused is named with its reasons under **would
not load** rather than silently missing.

A tab left open on a plugin that has since been uninstalled says so, and keeps
your tab, rather than showing an empty table.

## The built-ins

`internal/plugins/builtin/*.json` are in exactly the format above and are worth
reading as worked examples:

- **`argocd.json`** — the whole format at once. Its pages are in
  `builtin/ui/argocd/`: its own [overview](#an-overview-of-its-own) (a verdict,
  every Application as a honeycomb cell coloured by health and rimmed when it
  has drifted from Git, what needs you with the button that fixes it, the
  deploy stream, Argo CD's own components and its charts), an *Application
  board* view (every application as a tile, filtered from a rail, one of them
  in a drawer with its resource tree, last sync and history), a panel in every
  Application's detail view, and Sync, Refresh and Hard refresh
  [on its action bar](#buttons-on-an-object). Also health and sync as two
  separate cards over the same kind, and a view of a *built-in* kind narrowed
  by a label selector.
- **`flux.json`** — thirteen views over five API groups, all summarised through
  the one `status.conditions[Ready]` path every Flux controller answers on.
- **`prometheus.json`** — the Prometheus Operator's CRDs, plus every chart in the
  app: CPU, memory and network per pod; CPU, memory and pod count per node; and
  cluster CPU, memory, pods by phase and API server request rate on the
  dashboard. Its pages are in `builtin/ui/prometheus/`: its own overview (a
  verdict, targets up as a ring, tiles with sparklines, what is firing now,
  every alert over the last hours as a timeline, scrape health per job, the
  servers, and which rule objects no Prometheus loads), and an *Alerts & rules*
  view — every alerting rule with its state, and a builder that writes new
  ones into a PrometheusRule labelled so a Prometheus's `ruleSelector` picks it
  up, through the bridge's `create` and `patch`. Its queries assume the metric and label names the
  kube-prometheus-stack sets up (cAdvisor with a `node` label,
  kube-state-metrics). If your monitoring is set up differently, copy the file
  into your own plugins folder under the same id and change the queries — a
  user plugin replaces a built-in of the same id.

None of them pins a namespace, on purpose: the official manifests set
`app.kubernetes.io/part-of` wherever they are installed, so a selector is robust
where a hardcoded `argocd` or `flux-system` would quietly show nothing for
anyone who installed elsewhere.

## How it works, briefly

A tab opened on a plugin view carries the kind `plugin:<pluginId>/<viewId>` —
the same trick `crd:<plural>.<group>` plays. A tab's kind is persisted,
reordered, restored and titled by machinery that never looks inside it, so a
plugin view becomes a tab without any of that learning a second shape. It is
resolved back into a real kind and its filters at the last moment, when the watch
is opened.

A custom view's tab carries the same kind; `Pane.svelte` sees the view's type
and hosts it in `PluginFrame.svelte` instead of a table.

- `internal/plugins/` — the format, the validator, the loader, the overview
  builder, the three built-ins, and `known.json`, the plugins offered for
  installing.
- `cmd/plugincheck/` — the loader's checks, run on a folder.
- `internal/plugins/ui.go` — serving custom views, their Content-Security-Policy,
  and the guard that refuses the runtime to them; `sdk/` is the bridge's client.
- `frontend/src/lib/components/PluginFrame.svelte` — the frame, and the bridge's
  host side: what a view may ask for, and the confirmation a patch waits on.
- `internal/addons/` — the file discovery both plugins and themes share.
- `internal/kube/tally.go` — counting objects by a field path.
- `pluginservice.go` — what the frontend calls.
