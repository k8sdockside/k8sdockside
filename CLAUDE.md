# K8s Dockside

A Kubernetes workspace built twice from one codebase: a **desktop app** (Wails v3,
Go backend + Svelte 5 frontend) and a **server mode** (`-tags server`, an HTTP
server behind its own sign-in, shipped as an image and a Helm chart).

Read before changing anything non-trivial:
- [docs/architecture.md](docs/architecture.md): how cluster data flows, and the repo layout
- [docs/development.md](docs/development.md): build, test, CI, release
- [docs/plugins.md](docs/plugins.md) and [docs/writing-plugins.md](docs/writing-plugins.md): the plugin format
- [docs/server-mode.md](docs/server-mode.md) and [SECURITY.md](SECURITY.md): the server's security model

## Layout in one breath

- `main.go` / `main_server.go` / `window.go`: entry points, split by the `server` build tag
- `internal/services/`: the services the frontend calls (wired in `services.go`)
- `internal/kube/`: kubeconfig parsing, dynamic client, informers, exec/port-forward
- `internal/plugins/`: plugin manifest, loader, strict decoding, built-in plugins
- `cmd/plugincheck/`: the loader's checks, run outside the app
- `packages/plugin-sdk/`: `@k8sdockside/plugin-sdk` (types, build tool, DOM helpers)
- `frontend/src/`: Svelte 5 app; all state is in `lib/state/*.svelte.ts`
- `charts/k8sdockside/`: the Helm chart for server mode
- `build/`: the Wails platform scaffold. Mostly generated, so don't lint or refactor it.

## Commands

```sh
make build-go          # compile Go only (fast)
make build-go-server   # compile with -tags server
make test              # Go unit tests
make vet && make lint  # vet + golangci-lint (default AND server tags)
make fmt               # gofmt
make generate          # regenerate frontend/bindings from the Go services
make lint-frontend     # svelte-check
make test-frontend     # vitest: *.test.ts in jsdom, *.browser.test.ts in Chromium
make helm-lint         # chart lint with every ci/*-values.yaml
make precheck          # everything CI runs. Run it before saying "done" on a large change.
```

Run a single Go test with `go test ./internal/<pkg>/ -run <Name>`. The live
cluster tests (`-run Live`) skip unless `K8SDOCKSIDE_TEST_KUBECONFIG` and
`K8SDOCKSIDE_TEST_CONTEXT` are set, so never point them at a cluster on your own
initiative.

## Rules that are easy to break

- **Build both tags.** Code that compiles without `-tags server` can still fail with it, and the reverse. Touching anything near `main*.go`, `window*.go` or services means running `make build-go` and `make build-go-server`.
- **`frontend/bindings/` is generated and gitignored.** Never edit it. After changing a service method or any type it mentions, run `make generate`. Every service result enters the app through `frontend/src/lib/state/adopt.ts`, which handles nullable slices/maps and class instances that `$state` would not deep-proxy.
- **Everything goes through the dynamic client** (`unstructured`). Don't add typed clients for one kind.
- **Secrets are redacted before they are cached.** Only the editor reads a Secret live. Never add a path that puts Secret values into an informer cache, a table, a log or a plugin page.
- **The kubeconfig is read, never written.** Local terminals get a temporary single-context kubeconfig (0600) instead.
- **No telemetry, and no network calls the user didn't ask for.** See [docs/network-and-privacy.md](docs/network-and-privacy.md). A new outbound request needs a line there.
- **Plugins are strict.** Unknown manifest fields are errors, not warnings. If you change the manifest format, update `internal/plugins`, `docs/plugin.schema.json`, `docs/plugins.md`, `cmd/plugincheck` and the SDK types together.
- **Server mode defaults to read-only**, and everyone acts with the pod's credentials. Chart defaults must stay safe.

## Style

- Go: gofmt, golangci-lint clean, standard `testing` with `httptest` fakes, no assertion libraries. Comments explain *why*, in full sentences.
- Frontend: Svelte 5 runes (`$state`, `$derived`, `$effect`), TypeScript, logic in `lib/state` rather than in components.
- Docs and comments use British spelling (colour, serialise) and plain, specific prose: say what happens and why, with no marketing tone.
- Commits use conventional prefixes (`feat:`, `fix:`, `docs:` …).

## Plugin repositories

The plugins (talos, cilium, cnpg, …) are **separate git repositories** in sibling
folders (`../<name>`), not part of this repo. The example to copy is
`../example-plugin`. To check one against the local loader:

```sh
go run ./cmd/plugincheck ../<plugin>
```
