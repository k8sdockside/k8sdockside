# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security vulnerability.**

Use GitHub's private reporting instead: **Security → Report a vulnerability** on
[this repository](https://github.com/k8sdockside/k8sdockside/security/advisories/new).
That opens a private advisory only the maintainers can see.

Include what you found, how to reproduce it, what an attacker gets out of it,
and a suggested fix if you have one.

You can expect an acknowledgement within a few days, and to be credited in the
advisory unless you would rather not be.

## Supported versions

Fixes go onto the latest release. This project has not reached 1.0, so there is
no back-porting to older tags.

| Version | Supported |
| ------- | --------- |
| Latest release | ✅ |
| Anything older | ❌ |

## What this app does with your credentials

K8s Dockside is a desktop application. It has no telemetry, no account, and no
server of the project's own that it reports to. (An optional web version runs in
your own cluster: see [docs/server-mode.md](docs/server-mode.md).) Every
connection the app makes is listed in
[docs/network-and-privacy.md](docs/network-and-privacy.md).

- **Kubeconfigs are read, never written.** Context aliases and colours are
  stored in the app's own settings file; your kubeconfig files are not modified.
- **Credentials stay in `clientcmd`.** Client certificates, bearer tokens and
  `exec` credential plugins are handled by the standard Kubernetes client
  libraries. The app does not copy secrets out of them or persist them.
- **A cluster is contacted when you use it.** Contexts are listed from disk. At
  launch the app checks that the selected context's cluster answers, and
  reopens the tabs you left open (*Settings → Behaviour → Restore tabs at
  launch*). Beyond that, a connection opens when you open a view, and closes a
  couple of minutes after the last tab using it does. Port forwards remembered
  from a previous session are stored as *requests*, not as live connections.
- **The update check is the only automatic request beyond your clusters.**
  Shortly after launch, and
  every six hours after, the app asks GitHub's public releases API which
  release is newest: one unauthenticated `GET` of
  `api.github.com/repos/k8sdockside/k8sdockside/releases/latest`, carrying
  nothing but the app's name and version in its `User-Agent`. No cluster,
  kubeconfig or settings data is sent. It can be switched off under
  *Settings → Behaviour*; the button under *About* checks only when pressed.
  The page it offers to open is built from the release tag, never taken from
  the response. Everything else that leaves the machine happens only when you
  ask for it: installing or updating a plugin, a Helm upgrade, the optional
  image-inventory plugin's registry lookups, or a Prometheus address you typed
  in.
- **Secrets are redacted before caching.** An informer holds a whole collection
  in memory, so Secret values are stripped on the way in and the tables show key
  counts only. The YAML editor is the deliberate exception: it reads the object
  live, because an editor opened on a redacted copy would write the redaction
  back over the secret.
- **Themes and plugin manifests are data, not code.** Both formats are JSON — a
  theme is colours, a plugin names resource kinds and PromQL the app already
  knows how to render. Neither can ship CSS or execute anything.
- **A plugin's own views are code, and fenced as code.** A plugin installed from
  a folder may ship HTML pages ("custom views"). They run in an
  `<iframe sandbox="allow-scripts">` with an opaque origin, under a
  Content-Security-Policy that forbids network access and limits what they load
  to their own folder; the asset server refuses the app's runtime to any request
  from a sandboxed frame. A view reads the cluster only through a message bridge
  the app answers, only for the kinds its plugin declares (never Secrets), and
  any patch it asks for is shown to the user and applied only on confirmation.
  A view can still send what it is allowed to read elsewhere by navigating its
  frame, so install views only from people you would trust with read access to
  the kinds they declare. See [docs/plugins.md](docs/plugins.md#what-a-view-can-and-cannot-do).
- **A plugin's object actions are data.** A button a plugin puts on an object
  names one request in its manifest — a merge patch, a subresource call under
  the object's own API group, or a create in the object's namespace — and the
  backend reads that request from the manifest; the webview only says which
  action on which object. No action may write Secrets, ServiceAccounts, RBAC,
  admission webhooks or CRDs. A plugin's own page asking to run one always
  gets a confirmation from the app first.
- **Installing a plugin from a repository runs `git clone`** into the plugins
  folder, for `https://`, `ssh://` and `git@host:path` addresses only, with
  prompts disabled. The plugins offered under *Settings → Plugins → Available*
  are a list compiled into the app, each with a fixed `https://` repository;
  the frontend names the plugin, never the address, and nothing is fetched to
  find out what exists. Suggestions in the sidebar only open Settings.
- **Plugin files are read strictly.** A member the manifest has no field for,
  an icon the app does not draw, a link that is not `http(s)`, or a page that is
  not in the plugin's own folder refuses the plugin, with the reason, rather
  than being ignored.

The app is only as privileged as the kubeconfig you point it at. Treat a
context's RBAC as the boundary: **Shell** on a node, for example, creates the
same privileged debug pod `kubectl debug` would, and will only work if you are
already allowed to do that.

## Build and release security

- **Every push is scanned.** CI runs `gosec` and `govulncheck` over the Go code
  and `npm audit` over the frontend tree, and fails on findings.
- **Weekly deep scan.** A scheduled workflow re-runs those in reporting mode,
  adds a Trivy filesystem scan for committed secrets and misconfiguration, and
  files results under **Security → Code scanning**.
- **Dependencies are updated automatically** by Dependabot across the Go, npm
  and GitHub Actions trees.
- **Releases are built only from a tag**, entirely in GitHub Actions, and ship
  an SPDX SBOM and a `checksums.txt` of every asset.
- **Binaries are `-trimpath` and stripped**, and the desktop build uses the
  production build tags.
- **`checksums.txt` is signed with cosign, keylessly.** There is no private key
  to steal: the release job exchanges its short-lived GitHub OIDC token for a
  Sigstore certificate that names the workflow, repository and tag that did the
  signing, and the signature is logged in Rekor.
- **Every asset carries SLSA build provenance**, recorded in GitHub's
  attestation store, tying each file to the workflow run and commit that
  produced it.

## Verifying a download

Checksums:

```sh
sha256sum -c --ignore-missing checksums.txt
```

The signature on that checksum file — one signature covering every asset,
because `checksums.txt` names them all:

```sh
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/k8sdockside/k8sdockside/\.github/workflows/release\.yml@refs/tags/' \
  checksums.txt
```

Build provenance for an individual file:

```sh
gh attestation verify <file> --repo k8sdockside/k8sdockside
```

### What this does not cover

Sigstore signatures and provenance attestations prove **where a file came
from**. They are not operating-system code signing, and neither macOS nor
Windows consults them:

- macOS builds are ad-hoc signed, not notarised — Gatekeeper will warn.
- Windows installers are unsigned — SmartScreen will warn.
- Linux packages are unsigned.

Fixing those needs an Apple Developer ID with notarisation and an Authenticode
certificate, which are paid and identity-bound. The Wails scaffold already has
the `darwin:sign:notarize`, `windows:sign:installer` and `linux:sign:packages`
tasks waiting for credentials.
