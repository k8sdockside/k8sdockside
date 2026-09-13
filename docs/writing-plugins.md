# Writing a plugin

This is the path from an empty folder to a plugin other people install from the
app. It points into [the plugin reference](plugins.md) for the details; read
this first, and that when you need a field or a call spelled out.

A plugin gives something that runs *in a cluster* — an operator, a product, a
set of conventions — a place of its own in K8s Dockside's sidebar. It never
needs a fork of the app, and nothing in it is built on the user's machine.

- [Pick a route](#pick-a-route)
- [1. The manifest](#1-the-manifest)
- [2. Try it in the app](#2-try-it-in-the-app)
- [3. Pages of your own, in plain JavaScript](#3-pages-of-your-own-in-plain-javascript)
- [4. The bridge: the app's API](#4-the-bridge-the-apps-api)
- [5. TypeScript, or a framework](#5-typescript-or-a-framework)
- [6. What a page may and may not do](#6-what-a-page-may-and-may-not-do)
- [Credit](#credit)
- [Publish it](#publish-it)
- [Get listed in the app](#get-listed-in-the-app)
- [Examples to copy](#examples-to-copy)

## Pick a route

| Route | What you write | Good for |
| --- | --- | --- |
| **JSON only** | one `plugin.json` | Giving an operator's custom resources their own rows in the sidebar, live counts on an overview, Prometheus charts, buttons on objects. No code, so nothing to review and nothing to break. |
| **JSON + pages in plain JavaScript** | `plugin.json` and a `ui/` folder of HTML and JS | Anything better drawn than listed: a map, a timeline, a score, a form that creates objects. |
| **JSON + pages in TypeScript or a framework** | the same, built from `src/` into `ui/` | The same, when the pages grow: types for every bridge call, tests, Svelte, Preact, Vue, Lit… |

Every route starts with the manifest, and a plugin can move from one to the next
without starting again.

## 1. The manifest

**Settings → Plugins → Write a starter plugin** drops a working file in the
plugins folder. Or start from this:

```json
{
    "$schema": "https://raw.githubusercontent.com/rogerwesterbo/k8sdockside/main/docs/plugin.schema.json",
    "id": "acme",
    "name": "Acme Mesh",
    "version": "1.0.0",
    "minAppVersion": "0.0.15",
    "tagline": "service mesh",
    "icon": "share",
    "author": "Acme Inc",
    "links": [{ "label": "acme.io", "url": "https://acme.io" }],
    "description": "One or two sentences on what this is and what to look at first.",
    "requires": [{ "kind": "crd:meshes.acme.io", "label": "Meshes" }],
    "views": [{ "id": "meshes", "label": "Meshes", "icon": "share", "kind": "crd:meshes.acme.io" }],
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

- **`id`** is permanent: it is in every tab a user opens on the plugin.
- **Kinds** are the app's names — `pods`, `deployments`, `nodes`, … — or
  `crd:<plural>.<group>` for a custom resource. `requires` is what the overview
  checks the cluster for; mark the ones that may be missing `"optional": true`.
- **`$schema`** makes your editor check every field as you type.
- Every field is in [the reference](plugins.md#writing-one); charts, buttons on
  objects and panels in detail views each have a section of their own there.

## 2. Try it in the app

1. Put the file — or its folder — in the plugins folder: **Settings → Plugins**
   shows where it is, with a button to open it. A folder elsewhere works too:
   **Watch another folder**.
2. Press **Reload**. The plugin appears under the cluster's **Plugins** in the
   sidebar, with its overview first.
3. Anything wrong is listed under **Settings → Plugins → Would not load**, one
   reason per line — a misspelt field comes with the one it was probably meant
   to be.

The same checks run outside the app, which is what CI uses:

```sh
go run github.com/rogerwesterbo/k8sdockside/cmd/plugincheck@main .
```

A page you change is picked up when you reopen its tab; the manifest when you
press **Reload**.

## 3. Pages of your own, in plain JavaScript

Declare a view of type `custom` (or an overview of your own) and put its page in
`ui/` beside the manifest:

```
acme/
├── plugin.json
└── ui/
    ├── index.html
    └── app.js
```

```json
{
    "id": "acme",
    "name": "Acme Mesh",
    "requires": [{ "kind": "crd:meshes.acme.io" }],
    "ui": { "kinds": ["pods"] },
    "overview": { "entry": "index.html" },
    "views": [{ "id": "meshes", "label": "Meshes", "kind": "crd:meshes.acme.io" }]
}
```

```html
<!doctype html>
<html>
    <head>
        <meta charset="utf-8" />
        <!-- The bridge. The app serves it, always at this path. -->
        <script src="/plugin-ui/_sdk/k8sdockside.js"></script>
    </head>
    <body>
        <h1 id="title">Meshes</h1>
        <ul id="list"></ul>
        <script src="app.js"></script>
    </body>
</html>
```

```js
// app.js -- a classic script, not type="module".
k8sdockside.ready().then(async (ctx) => {
    const meshes = await k8sdockside.list({ kind: 'crd:meshes.acme.io' });
    const list = document.getElementById('list');
    for (const mesh of meshes) {
        const item = document.createElement('li');
        // Cluster data goes on the page as text, never as HTML.
        item.textContent = `${mesh.metadata.namespace}/${mesh.metadata.name}`;
        item.onclick = () => k8sdockside.open({ kind: 'crd:meshes.acme.io', namespace: mesh.metadata.namespace, name: mesh.metadata.name });
        list.appendChild(item);
    }
});
```

The page gets the app's colours as CSS variables — `var(--bg)`, `var(--text)`,
`var(--accent)`, `var(--ok)`, `var(--warn)`, `var(--error)`, `var(--chart-1)` …
— and they follow the user's theme, so it looks like the app without trying.

A page can also be a **panel in an object's detail view** (`sections`), with the
object handed to it — see [Panels on an object](plugins.md#panels-on-an-object).

## 4. The bridge: the app's API

Everything a page learns about the cluster, and everything it changes, goes
through `window.k8sdockside`. Each call is a promise; a failure rejects with an
`Error` carrying a sentence you can show as it is.

| For | Calls |
| --- | --- |
| Where the page is | `ready()` — the cluster, the view, the kinds it may read, whether it may write, the theme, and what the manifest says about the plugin |
| Reading | `list({ kind, namespace?, selector? })`, `get({ kind, namespace, name })`, `watch(query, onItems)`, `namespaces()`, `object()` in a panel |
| The overview | `summary()` — whether the cluster has what `requires` names; `charts({ minutes })` — the plugin's Prometheus charts as numbers |
| Changing | `patch({ kind, namespace, name, patch })`, `create({ kind, namespace, object })`, `run(actionId, ref)` — the user sees each one and confirms it |
| Moving around the app | `open(ref)`, `openView(id)`, `edit(ref)`, `logs(ref)`, `openUrl(url)` |
| Staying in step | `on('theme', fn)`, `resize(height)` in a panel |
| Remembering | `storage.get(key)`, `storage.set(key, value)`, `storage.remove(key)`, `storage.keys()` — kept by the app per plugin and per cluster, across restarts (0.0.19 and newer; check it exists) |

Every call, with what it takes and returns, is in
[Views of its own](plugins.md#views-of-its-own) and
[Panels on an object](plugins.md#panels-on-an-object). The TypeScript
declarations — the most precise description there is — are
[`internal/plugins/sdk/k8sdockside.d.ts`](../internal/plugins/sdk/k8sdockside.d.ts);
they are useful from plain JavaScript too, as your editor's hints.

## 5. TypeScript, or a framework

Copy [k8sdockside-example-plugin-typescript](https://github.com/rogerwesterbo/k8sdockside-example-plugin-typescript):
an overview, a view and a panel over core kinds, built with esbuild, with
tests, a build that checks `ui/` is up to date, and CI that runs `plugincheck`.
Its `src/k8sdockside.d.ts` types `k8sdockside` everywhere; refresh it from
[the app's copy](../internal/plugins/sdk/k8sdockside.d.ts) when the bridge
grows.

Any tool that ends up as static files works — Svelte, Preact, Vue, Lit, plain
DOM. Three things differ from building an ordinary web page:

1. **Include the bridge** before your own script:
   `<script src="/plugin-ui/_sdk/k8sdockside.js"></script>`.
2. **Bundle to a classic script**, not an ES module: the page's frame has an
   opaque origin, where a module is a cross-origin load. With esbuild,
   `--bundle --format=iife`. With Vite, `build.rollupOptions.output` set to
   `{ format: 'iife', inlineDynamicImports: true }`, `modulePreload: false`,
   `base: './'`, and `type="module"` / `crossorigin` taken out of the built HTML.
3. **No network**: bundle fonts and images, or inline them.

**Commit what the build produces.** Installing clones the repository and reads
`plugin.json` and `ui/` as they are — nothing is built on the user's machine.

## 6. What a page may and may not do

The page runs in `<iframe sandbox="allow-scripts">` with a
Content-Security-Policy that repeats it. It:

- reads **only the kinds the plugin declares** (`requires`, `views`, `cards`,
  `ui.kinds` …), and **never Secrets**;
- changes nothing without the user seeing the change and pressing **Apply**,
  and only when the manifest says `"ui": { "write": true }`;
- sees **only the cluster of its tab**;
- has **no network**: `fetch`, XHR and websockets are refused.

The plugin's card in Settings says how many kinds its pages read and whether
they may ask to change them, before anyone opens one. Keep `ui.kinds` to what
you use: it is the first thing a careful user reads.

## Credit

Put your name on it:

```json
"author": "Acme Inc",
"authorUrl": "https://acme.example",
"links": [
    { "label": "Source", "url": "https://github.com/acme/k8sdockside-mesh" },
    { "label": "acme.io", "url": "https://acme.io" }
]
```

The app credits the author wherever it shows the plugin: on its card in
**Settings → Plugins**, on the generated overview, and on a line it draws
itself under an overview page of the plugin's own — outside the page's frame,
so it is there however the page is drawn. The name links to `authorUrl`.
`author` is at most 80 characters; `authorUrl` is `http(s)` only, needs
`author`, and is read by K8s Dockside 0.0.19 and newer — set `minAppVersion` to
`0.0.19` if you use it, so an older app says it is too old rather than
refusing an unknown field.

Next to the author, a badge says where the plugin stands:

| Badge | Means |
| --- | --- |
| **Built in** | Ships with the app. |
| **Official** | Kept alongside the app by its author, and installed from the repository the app's list names. The app checks the repository an installed plugin was cloned from, not its id, so no plugin can take the badge by taking a name. |
| **Community** | Everyone else — which is most plugins, and is not a warning. It is checked when it loads and its pages are sandboxed like every other plugin's; it simply has not been reviewed by the project. |

A pack's `author` and `authorUrl` are given to the plugins in it that name no
author of their own.

## Publish it

A plugin in a repository of its own installs with **Settings → Plugins → From a
repository**, and updates with the card's **Update from repository** button.

1. **Layout**: `plugin.json` at the root, `ui/` beside it. `package.json`,
   `tsconfig.json` and the like at the root are skipped, not read as plugins.
2. **CI**: run the app's checks on every push. With GitHub Actions:

   ```yaml
   name: check
   on: [push, pull_request]
   permissions:
       contents: read
   jobs:
       plugin:
           runs-on: ubuntu-latest
           steps:
               - uses: actions/checkout@v7
               - uses: actions/setup-go@v7
                 with:
                     go-version: stable
               - run: go run github.com/rogerwesterbo/k8sdockside/cmd/plugincheck@main .
   ```

   A TypeScript plugin also type-checks, tests and checks `ui/` matches a fresh
   build first — the example's `npm run check`.
3. **Versions**: bump `version` when you change the plugin; set
   `minAppVersion` to the first app release with everything you use, and the
   check tells an older app's user to update rather than listing field errors.
   `plugincheck -app 0.0.15 .` checks as that release would.
4. **Say what it reads**: the README is where someone decides whether to trust
   it. Name the kinds it reads, whether it writes, and what it writes.

Anyone can install it from its address now. Getting it onto the app's own list
is the next step.

## Get listed in the app

**Settings → Plugins → Available** offers the plugins in
[`internal/plugins/known.json`](../internal/plugins/known.json) with one
**Install** button each, and the sidebar suggests one for any cluster running
what it is about. The list is compiled into the app — it asks no server what
exists — so getting on it is a pull request to this repository adding an entry:

```json
{
    "id": "acme",
    "name": "Acme Mesh",
    "tagline": "service mesh",
    "icon": "share",
    "description": "What it shows, in two sentences: what to look at first, and what it adds to objects.",
    "repo": "https://github.com/acme/k8sdockside-mesh.git",
    "author": "Acme Inc",
    "authorUrl": "https://acme.example",
    "detect": ["crd:meshes.acme.io"],
    "links": [
        { "label": "acme.io", "url": "https://acme.io" },
        { "label": "Source", "url": "https://github.com/acme/k8sdockside-mesh" }
    ]
}
```

| Field | |
| --- | --- |
| `id` | The plugin's own id, exactly as in its `plugin.json`. |
| `repo` | A public `https://` git address; installing clones it. |
| `author`, `authorUrl` | Required and optional, as in the manifest. Everyone on the list is credited. |
| `detect` | Kinds whose presence means the product runs in a cluster, so the sidebar can suggest the plugin there. Leave it out for a plugin that works on any cluster. |
| `links` | At least one: what it is about, and its source. |
| `official` | Leave it out. It is for plugins kept alongside the app by its author. |

What the pull request needs:

- [ ] The repository is public, and `plugincheck` passes in its CI.
- [ ] Its README says what it shows, what it reads and whether it writes.
- [ ] It does something for a real product or a common need, and is not a
      duplicate of a built-in or of another plugin on the list.
- [ ] The entry has a tagline, a description, an author and a link — the
      app's own test, `go test ./internal/plugins/`, fails without them.
- [ ] A row for it in the table under
      [The plugins the app knows of](plugins.md#the-plugins-the-app-knows-of).

It reaches users in the next release of the app. From then on your plugin is
yours: updates are pushed to your repository and reach users through **Update
from repository**, with no change needed here unless its address moves.

## Examples to copy

| Plugin | Shows how to |
| --- | --- |
| [k8sdockside-example-plugin-typescript](https://github.com/rogerwesterbo/k8sdockside-example-plugin-typescript) | TypeScript with esbuild, tests and CI: an overview, a view and a panel over core kinds, a patch. The one to copy. |
| [k8sdockside-optimization](https://github.com/rogerwesterbo/k8sdockside-optimization) | TypeScript: rules with tests, a score, Prometheus data through `charts()`, `create` and `patch`. |
| [k8sdockside-metallb](https://github.com/rogerwesterbo/k8sdockside-metallb) | Plain JavaScript: an overview of its own, views and panels. |
| [k8sdockside-certmanager](https://github.com/rogerwesterbo/k8sdockside-certmanager) | Plain JavaScript: panels on built-in kinds (Ingresses, Gateways), charts on objects. |
| [`internal/plugins/builtin/`](../internal/plugins/builtin/) | The built-ins, in the same format: `flux.json` is JSON only; `argocd.json` has actions, a board and a panel; `prometheus.json` has every kind of chart. |
