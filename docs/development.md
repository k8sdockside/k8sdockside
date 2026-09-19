# Development

Building, testing and releasing k8sdockside. For how it talks to a cluster, see
[architecture.md](architecture.md).

## Requirements

- Go 1.27+ and Node 20+
- The [Wails v3 CLI](https://v3.wails.io/getting-started/installation/), at the
  version this module depends on:
  ```sh
  make install-wails
  ```
- **Linux:** GTK 4 and WebKitGTK 6 development packages. Without them neither
  the app nor the `wails3` CLI will compile:
  - Arch: `sudo pacman -S gtk4 webkitgtk-6.0`
  - Debian/Ubuntu: `sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev`
- **macOS:** Xcode command line tools.
- **Windows:** a C toolchain (the Go MSVC/mingw setup Wails documents), and
  NSIS if you want to build the installer.

## Everyday targets

```sh
make dev             # hot-reloading desktop app
make build           # production desktop build into bin/
make build-go        # compile the Go packages only, no frontend bundle
make test            # Go unit tests with a coverage report
make test-frontend   # Svelte tests (vitest)
make lint            # golangci-lint
make lint-frontend   # svelte-check
make audit           # gosec + govulncheck + npm audit
make precheck        # everything CI checks, in one go -- run before you push
make help            # every target
```

`make precheck` runs the formatting and `go mod tidy` checks, vet, lint, both
Go builds, the Go tests, the bindings, the frontend's type check, tests and
bundle, the Helm chart checks and the security scans, and stops at the first
one that fails. It needs the same tools as CI: the `wails3` CLI, the frontend's
`node_modules`, Playwright's Chromium, and helm.

There is also a server mode that runs the app as an HTTP server behind its own
sign-in, with no native GUI dependencies — what the container image and the Helm
chart ship, and handy for poking at the backend:

```sh
make build-go-server # compile check with -tags server, no frontend bundle
make run-server      # build and serve on http://127.0.0.1:8080, state in bin/server-data
```

Everything about it — the image, the chart, OAuth — is in
[server-mode.md](server-mode.md).

## Tests

### The frontend tests

`make test-frontend` runs two vitest projects. Application state (`*.test.ts`)
runs in jsdom with the Wails bindings mocked — it is plain logic over runes and
does not need a browser. Components (`*.browser.test.ts`) run in headless
Chromium through Playwright, because `$effect`, focus and pointer handling only
behave as they do in the app when there is a real browser under them.

The browser project needs Chromium once:

```sh
cd frontend && npx playwright install chromium
```

### Running the Go tests against a real cluster

Most of the package is tested against literals. The live tests are opt-in,
because they need a cluster, and skip silently without the environment:

```sh
K8SDOCKSIDE_TEST_KUBECONFIG=~/kubeconfig/example.config \
K8SDOCKSIDE_TEST_CONTEXT=admin@example \
  go test ./internal/kube/ -run Live -v
```

The exec and port-forward checks need something to open, and skip without it.
Both go through a protocol upgrade that no unit test can stand in for:

```sh
K8SDOCKSIDE_TEST_KUBECONFIG=~/.kube/config \
K8SDOCKSIDE_TEST_CONTEXT=admin@example \
K8SDOCKSIDE_TEST_POD=argocd/argocd-redis-5b965dbf67-wnk4x \
K8SDOCKSIDE_TEST_SERVICE=argocd/argocd-server \
  go test ./internal/kube/ -run Live -v
```

## Continuous integration

| Workflow | Runs on | What it does |
|---|---|---|
| [`ci.yml`](../.github/workflows/ci.yml) | every branch push, and PRs into `main` | Go build (desktop and server mode), test, vet, gofmt check and golangci-lint; bindings generation, svelte-check, vitest and the frontend bundle; gosec, govulncheck and `npm audit --audit-level=high`; `helm lint` and `helm template` of the chart |
| [`security.yml`](../.github/workflows/security.yml) | Mondays 03:00 UTC, manual, and dependency changes on `main` | The same Go scanners in reporting mode with SARIF filed under **Security → Code scanning**, a Trivy filesystem scan for secrets and misconfiguration, and a full npm audit |
| [`release.yml`](../.github/workflows/release.yml) | tags matching `v*`, and manual | Builds and packages every platform, then publishes the GitHub release |
| [`server-image.yml`](../.github/workflows/server-image.yml) | tags matching `v*`, and manual | Builds the server-mode image for linux/amd64 and linux/arm64 and pushes it to `ghcr.io/k8sdockside/k8sdockside`, then pushes the Helm chart to `oci://ghcr.io/k8sdockside/helm` |

CI only runs the jobs a change can affect, via `dorny/paths-filter`. Note that
the frontend filter includes `**/*.go`: `frontend/bindings/` is generated from
the Go services and is not committed, so a service signature change breaks the
frontend without touching a file under `frontend/`.

The toolchain setup is shared by all three workflows in
[`.github/actions/setup-build`](../.github/actions/setup-build/action.yml),
which installs the Linux GUI headers, Go from `go.mod`, Node, and the `wails3`
CLI pinned to the version `go.mod` requires. Pinning matters: a CLI newer than
the library generates bindings the app cannot compile against.

## Cutting a release

Releases are driven entirely by the tag. Either route works and both run
[`release.yml`](../.github/workflows/release.yml) exactly once — publishing a
release on the website creates the tag, which fires the same push event.

**From git:**

```sh
git tag v1.2.3
git push origin v1.2.3
```

**From github.com:** *Releases → Draft a new release*, create the tag `v1.2.3`
against `main`, write whatever notes you want and publish. The workflow fills in
the assets and appends the install instructions to your notes.

Tags must be `vMAJOR.MINOR.PATCH` with an optional pre-release suffix. A suffix
(`v1.2.3-rc.1`) marks the GitHub release as a pre-release, and is stripped from
the version stamped into the packaging metadata, because NSIS and
`CFBundleVersion` accept dotted numbers only.

`workflow_dispatch` rebuilds an existing tag, for when a release job failed on
something outside the code.

### What a release produces

| Platform | Assets |
|---|---|
| Linux (amd64, arm64) | `.AppImage`, `.deb`, `.rpm`, `.pkg.tar.zst`, and a `.tar.gz` of the bare binary |
| macOS (Intel, Apple Silicon) | `.dmg`, and a `.zip` of the `.app` bundle |
| Windows (amd64) | NSIS `-installer.exe`, and a `.zip` of the bare `.exe` |
| All | `checksums.txt`, its cosign signature (`.sig` + `.pem`), and an SPDX SBOM |

Both macOS architectures build on the Apple Silicon runner: the macOS SDK is
universal and clang cross-targets `x86_64` natively, which is what Wails' own
`darwin:build:universal` task relies on. That avoids depending on the Intel
runner images continuing to exist.

### Version stamping

The repository carries a hardcoded `0.0.1` in its packaging metadata.
[`.github/scripts/stamp-version.sh`](../.github/scripts/stamp-version.sh)
rewrites it from the tag in the release job's own checkout, and never commits
the result. It touches `build/config.yml`, `build/windows/info.json`,
`build/windows/nsis/wails_tools.nsh`, `build/darwin/Info.plist`,
`build/linux/nfpm/nfpm.yaml` and `internal/services/settingsservice.go`.

That last one is the version the *running app* shows — in the About dialog
under the app menu, and in **Settings → About**. It is stamped into the source
rather than injected with `-ldflags -X`, because the Wails build tasks hardcode
their own `-ldflags` and expose no hook to add to them: overriding `BUILD_FLAGS`
on the command line is silently ignored, since Task's task-level `vars` outrank
CLI vars. A build from a working tree leaves it empty and the app reports
"development build".

The full tag reaches the app and the deb/rpm packages, so `v1.2.3-rc.1` reads as
a pre-release. `Info.plist` and NSIS get `1.2.3`, because both accept dotted
numbers only.

Run it locally to see what it does:

```sh
.github/scripts/stamp-version.sh 1.2.3
git diff        # then: git checkout -- build/
```

### Signing

Two different things go by that name, and only one of them is in place.

**Supply-chain signing — done.** The release job signs `checksums.txt` with
cosign using keyless OIDC (no stored key; the certificate names the workflow,
repo and tag), and attests SLSA build provenance for every asset. Verification
commands are in [SECURITY.md](../SECURITY.md#verifying-a-download). Both need
`id-token: write`, and provenance also needs `attestations: write` — already
set on the `release` job.

**OS code signing — not done.** Gatekeeper and SmartScreen do not look at
Sigstore, so:

- The macOS `.app` is **ad-hoc** signed. Gatekeeper will refuse it on first
  launch until the user right-clicks → **Open**, or clears the quarantine
  attribute. Proper signing needs an Apple Developer ID and notarisation
  credentials; the Wails scaffold already has `darwin:sign` and
  `darwin:sign:notarize` tasks waiting for them.
- The Windows installer is **unsigned**, so SmartScreen warns once. An
  Authenticode certificate would plug into the existing `windows:sign` and
  `windows:sign:installer` tasks.
- Linux packages are **unsigned**. `linux:sign:packages` signs the `.deb` and
  `.rpm` with a PGP key.

Wiring any of these up means adding the credentials as repository secrets and
calling the matching task in `release.yml`. They are paid and identity-bound,
which is the only reason they are not already there.
