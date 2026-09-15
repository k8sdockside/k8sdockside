<div align="center">

<img src="frontend/public/k8s_dockside_logo_concept.png" alt="K8s Dockside" width="560">

**Every cluster in your kubeconfig, in one window.**

[![CI](https://github.com/rogerwesterbo/k8sdockside/actions/workflows/ci.yml/badge.svg)](https://github.com/rogerwesterbo/k8sdockside/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/rogerwesterbo/k8sdockside?sort=semver)](https://github.com/rogerwesterbo/k8sdockside/releases/latest)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey)

</div>

<!-- Screenshots go here. A dark-theme shot of the workspace with two coloured
     contexts open, and one of the YAML editor in the dock, would carry this
     README further than any paragraph below it. -->

## What it is

K8s Dockside is a desktop app for the Kubernetes clusters you already have
access to. It reads the kubeconfigs on your machine, lists every context in a
sidebar, and opens each resource view as a tab.

The point is knowing where you are. Every context gets a name and a colour you
choose, and its tabs are painted in it — so the window tells you at a glance
whether you are about to delete a pod in staging or in production. Nothing is
polled: tables are backed by live watches, so a rollout repaints as it happens.

It is a single native binary — no cluster-side agent, no browser tab, no
credentials leaving your machine. The same app also runs as a
[web app inside a cluster](#server-mode-kubernetes), for a team, behind a
sign-in of its own. Free and open source under Apache 2.0.

## Why you might want it

- **Many clusters, one window.** Not one terminal per context and a mental note
  about which is which.
- **Colour as a safety rail.** Production is red because you made it red, and
  every tab, dock and detail panel belonging to it stays red.
- **Live, not refreshed.** Informer-backed tables. No refresh button, because
  there is nothing to refresh.
- **Your CRDs are first-class.** Everything goes through the dynamic client, so
  a custom resource opens in a tab with the same columns `kubectl get` prints,
  read from the CRD itself.
- **It reads your kubeconfig, and only reads it.** Names and colours are stored
  in the app's own settings; your kubeconfig files are never modified. Nothing
  dials a cluster at launch. The one thing it does reach out to is GitHub's
  releases page, to tell you when a newer version is out — and that can be
  switched off.

## Screenshots

<!-- ![Cluster dashboard with metrics](./docs/images/cluster_dashboard.png "Cluster dashboard with metrics")

![Plugins in settings](./docs/images/settings_plugins.png "Plugins in settings")

![Kubernetes help pages](./docs/images/kubernetes_help.png "Kubernetes help pages") -->

### Cluster dashboard

<img src="./docs/images/cluster_dashboard.png" alt="Cluster dashboard" width="1000" >

### List pods, view pod metrics, follow pod log

<img src="./docs/images/list_pods_pod_metrics_pod_log.png" alt="List pods, view pod metrics, follow pod log" width="1000" >

### Activate plugins, or install your own

<img src="./docs/images/settings_plugins.png" alt="Activate plugins, or install you own" width="1000" >

[See more screenshots](./docs/images/)

## Install

Grab the build for your platform from the
[latest release](https://github.com/rogerwesterbo/k8sdockside/releases/latest).

### macOS

Download the `.dmg` — `darwin-arm64` for Apple Silicon, `darwin-amd64` for
Intel — and drag **K8s Dockside** into Applications.

The app is not yet notarised, so macOS will say it cannot be checked on first
launch. Right-click it and choose **Open**, or:

```sh
xattr -dr com.apple.quarantine "/Applications/k8sdockside.app"
```

### Windows

Run the `-installer.exe`. It is unsigned, so SmartScreen warns once —
**More info → Run anyway**. The `.zip` holds the bare `.exe` if you would rather
not install anything.

### Linux

Needs GTK 4 and WebKitGTK 6 at runtime. The packages declare that dependency;
the AppImage expects it present.

```sh
# Debian / Ubuntu
sudo apt install ./k8sdockside-<version>-linux-amd64.deb

# Fedora / RHEL
sudo dnf install ./k8sdockside-<version>-linux-amd64.rpm

# Arch
sudo pacman -U ./k8sdockside-<version>-linux-amd64.pkg.tar.zst

# Anywhere else
chmod +x k8sdockside-<version>-linux-amd64.AppImage
./k8sdockside-<version>-linux-amd64.AppImage
```

### Verifying what you downloaded

Every release ships `checksums.txt`, a cosign signature over it, and SLSA build
provenance for each asset:

```sh
sha256sum -c --ignore-missing checksums.txt
gh attestation verify <file> --repo rogerwesterbo/k8sdockside
```

The full recipe, including `cosign verify-blob`, is in
[SECURITY.md](SECURITY.md#verifying-a-download).

### From source

See [docs/development.md](docs/development.md).

## Getting started

1. **Launch it.** It finds `~/.kube/config`, everything in `$KUBECONFIG`, and
   anything else under `~/.kube` that parses as a kubeconfig. Nothing connects
   yet.
2. **Add anything it missed.** Point it at a file, or at a whole folder — a
   watched folder is scanned regardless of how the files in it are named, and
   rescanned on **Sync**.
3. **Name and colour your contexts.** Select one, then use the panel at the foot
   of the sidebar. This is the step worth doing properly; it is what makes the
   rest of the window readable.
4. **Open a view.** Pick a kind under a context and it opens as a tab in that
   context's colour. The watch starts here.
5. **Click a row.** The describe panel slides in. From there: **Edit** for live
   YAML, **Shell** for a terminal in the container, **Forward** for a port
   forward, **Logs** for the log stream.

## Features

**Clusters and contexts**
- Discovers kubeconfigs from `~/.kube/config`, `$KUBECONFIG` and `~/.kube`
- Watched folders, scanned by content rather than by filename
- Per-context alias and colour, carried through tabs, dock and panels
- Hide a discovered file, or forget one you added, and bring it back later
- Remove a single context from its row in the sidebar — the file and its
  other contexts stay, the kubeconfig is not written, and the context comes
  back the moment it is added again
- A file that fails to parse is listed with the reason, not silently dropped

**Resources**
- Live informer-backed tables — no polling, no refresh button
- One watch per kind per context, shared by every tab using it
- Namespace filtering, any number of namespaces at once, applied to the cache
  so it repaints instantly
- CRDs, the Gateway API and built-in kinds on one dynamic-client code path
- Columns read from the CRD's `additionalPrinterColumns`, so tables match
  `kubectl get`
- Nodes carry the `kubectl get nodes -o wide` columns — internal and external
  IP, OS image, kernel and container runtime — beside capacity and allocatable
- A dashboard of cluster capacity, requests and workload health
- Search in the title bar (`⌘K`): find an object by name in every kind every
  open cluster serves — or every context in your kubeconfigs — with results
  arriving cluster by cluster as they are found. Filters for kind, namespace
  and label; only metadata is read, never a Secret's values. A hit opens in its
  list and details panel, or in the plugin view that shows it — an Argo CD
  Application opens selected on the application board

**Working with objects**
- A describe panel you can dock right, bottom or left, and resize
- YAML editing in the dock: live document, syntax checking, `⌘S` to save
- Conflict-safe writes — a save against a moved object is refused, not forced,
  and the API server's own words come back
- Object actions: scale, restart, cordon, drain, delete
- Bulk delete: tick rows, or everything a filter matches, and delete them
  together — refusals come back by name, in the API server's words
- Bulk patch: set or remove a label, an annotation or a field by path on every
  selected object, or write a whole merge patch as YAML for several fields at
  once — either way shown as the JSON each object receives before it is sent
- Log streaming, per container

**Terminals and networking**
- A shell in any container, in the dock or in your own terminal emulator
- A shell on a *node*, via the same privileged pod `kubectl debug` would create,
  cleaned up when the terminal closes
- Port forwarding for pods and services, with the service-port-to-pod-port
  resolution done for you
- Forwards listed where you can stop them, and remembered between sessions as
  requests rather than live connections

**Extending it**
- Built-in **Plugins** for Argo CD, Flux and Prometheus — each unfolds into
  its own views instead of scattering custom resources through the definitions
  tree, with an overview of whether the cluster actually has it
- More a button away in *Settings → Plugins*:
  [cert-manager](https://github.com/rogerwesterbo/k8sdockside-certmanager),
  [MetalLB](https://github.com/rogerwesterbo/k8sdockside-metallb),
  [KubeVirt](https://github.com/rogerwesterbo/k8sdockside-kubevirt),
  [Vitistack](https://github.com/rogerwesterbo/k8sdockside-vitistack) and an
  [image inventory](https://github.com/rogerwesterbo/k8sdockside-example-plugin-typescript),
  each kept in its own repository, and suggested in the sidebar for a cluster
  running what it is about
- A plugin is a JSON file naming kinds the app already knows how to show, so
  supporting your own operator is a file, not a fork. One kept in its own
  folder or repository can also bring views of its own: HTML drawn in a
  sandboxed frame, reading the cluster through a narrow bridge — in plain
  JavaScript, TypeScript or any framework that builds to static files.
  **[Writing a plugin](docs/writing-plugins.md)** takes you from an empty
  folder to a published plugin, and to getting it listed in the app;
  [docs/plugins.md](docs/plugins.md) is the reference. Plugins are credited to
  their authors wherever the app shows them
- Graphs where the cluster can answer for them: Prometheus is found
  automatically and reached *through the API server*, no port-forward and no
  second credential. The queries live in the plugin file.
- 14 built-in themes, from `K8s Dockside Dark` through `Deep Sea` and
  `Lighthouse` to ports of Nord and Catppuccin Mocha. A theme is a JSON file of
  colours — it cannot ship CSS or run code —
  [docs/themes.md](docs/themes.md)

**Details that matter**
- A bell in the title bar says when a new release is out, and can be marked as
  read — one request to GitHub shortly after launch and every six hours, off
  under *Settings → Behaviour*, with a check-now button under *About*. It
  offers the release page, and the download for the way this build was
  installed (AppImage, `.deb`, `.rpm`, Arch package, `.dmg`, Windows
  installer or portable) when the release has one
- Secrets are redacted before they enter the informer cache; tables show key
  counts only
- Tabs and dock contents are restored next launch, and a tab with unsaved
  changes wears a dot instead of its close button
- Client certificates, tokens and `exec` credential plugins all work — it is
  `clientcmd` underneath
- A cluster's client outlives its last tab or poll by two minutes, so a tab
  switch or the dashboard's next refresh finds it warm instead of rebuilding
  it, and a kind the cluster lacks costs a rediscovery at most once a minute

## Where your things live

Settings go to `$XDG_CONFIG_HOME/k8sdockside/settings.json` (falling back to
`~/.config/...`) on macOS and Linux, and `%AppData%` on Windows. The exact path
is shown in the status bar. Themes and plugins you install sit in `themes/` and
`plugins/` folders beside it.

## Server mode (Kubernetes)

The same app also builds as a web server, to run inside a cluster and open in a
browser. It sits behind a sign-in of its own — local accounts, the first of
which becomes the admin, and GitHub, Google, Facebook, GitLab, Microsoft or any
OIDC provider — and reaches clusters through the pod's ServiceAccount, through
kubeconfig Secrets you mount, or through kubeconfigs an admin uploads.

```sh
helm install k8sdockside oci://ghcr.io/rogerwesterbo/helm/k8sdockside \
  --namespace k8sdockside --create-namespace
kubectl -n k8sdockside port-forward svc/k8sdockside 8080:80
```

The difference from the desktop app is whose credentials are used: everyone
who signs in acts with the pod's, so choose its RBAC with care — the chart
defaults to read-only. [docs/server-mode.md](docs/server-mode.md) covers OAuth
setup, persistence and the security model; the chart's options are in
[charts/k8sdockside](charts/k8sdockside/README.md).

## Documentation

In the app: **Help** (F1, or the question mark in the sidebar) is the guide to
the app itself, including how to write and install themes and plugins, and
**Kubernetes primer** under the View menu explains the cluster and its terms
for anyone new to it.

- [Architecture](docs/architecture.md) — how the cluster data gets here, and the
  code layout
- [Development](docs/development.md) — building, testing, and cutting a release
- [Server mode](docs/server-mode.md) — running it in Kubernetes as a web app,
  with sign-in, OAuth and the Helm chart
- [Themes](docs/themes.md) — the theme format
- [Writing a plugin](docs/writing-plugins.md) — step by step: JSON only, pages
  in plain JavaScript or TypeScript, publishing, credit, and getting listed
- [Plugin reference](docs/plugins.md) — every manifest field and every call of
  the bridge a plugin's pages talk to the app through

## Contributing

Issues and pull requests are welcome. CI runs build, test, lint and security
scans on every push. `make precheck` runs every one of those checks locally —
worth doing before you push — and `make audit` just the security scans.

Security issues: please see [SECURITY.md](SECURITY.md) rather than opening a
public issue.

## License

[Apache License 2.0](LICENSE).

Built with [Wails v3](https://v3.wails.io/) and [Svelte 5](https://svelte.dev/).
