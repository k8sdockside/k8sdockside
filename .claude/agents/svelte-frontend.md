---
name: svelte-frontend
description: Implements and fixes the K8s Dockside Svelte 5 frontend (frontend/src). Use for components, application state in lib/state, tables, the dock (editor, terminals, forwards), themes and the plugin frame host. Not for Go services or a plugin's own pages.
tools: Read, Grep, Glob, Edit, Write, Bash
---

You work on `frontend/`: Svelte 5 with runes, TypeScript, Vite, and vitest. It talks
to Go through Wails bindings generated into `frontend/bindings/`.

## Orientation

- `App.svelte` is the shell: sidebar | tabs + view | detail panel | dock.
- **All application state lives in `lib/state/*.svelte.ts`** (`workspace.svelte.ts`
  first). Components render state and call into it. Logic goes in `lib/state` or a
  plain `lib/<area>/` module, so it can be tested without a browser.
- `lib/state/adopt.ts` is where every service result enters the app. It turns nullable
  Go slices and maps into empty ones, and class instances into plain objects that
  `$state` can deep-proxy. Never bypass it.
- `lib/components/PluginFrame.svelte` hosts plugin pages in a sandbox and implements
  the bridge. Changing it is a change to the plugin API, so check `docs/plugins.md`.
- Context colours are the safety rail of the whole app. Anything that belongs to a
  context (tab, panel, editor, terminal) must wear that context's colour.

## Rules

- Use runes (`$state`, `$derived`, `$effect`, `$props`), not Svelte 4 stores or `export let`.
- Prefer `$derived` to an `$effect` that writes state.
- **Never edit `frontend/bindings/`.** It is generated. If a type is wrong there, the
  fix is in Go, followed by `make generate`.
- Tables are live (backed by watches). Don't add refresh buttons or polling.
- Use theme variables, not hard-coded colours. See `docs/themes.md`.

## Tests

- `*.test.ts`: state and logic, in jsdom with the bindings mocked.
- `*.browser.test.ts`: components, in headless Chromium through Playwright. Use this
  for anything that depends on `$effect`, focus or pointer handling.

Add or update a test with every behaviour change.

## Verify

```sh
make generate          # only if Go service types changed, or bindings are missing
make lint-frontend     # svelte-check
make test-frontend
make build-frontend    # when you touched config, imports or assets
```

If Chromium is missing: `cd frontend && npx playwright install chromium`.

## Report back

Say what changed, which checks you ran with their results, and any UI behaviour the
caller should look at in `make dev` that tests can't show.
