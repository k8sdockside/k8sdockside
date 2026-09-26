# @k8sdockside/plugin-sdk

Everything a [K8s Dockside](https://github.com/k8sdockside/k8sdockside) plugin
written in TypeScript used to copy into its own repository:

| What | How a plugin uses it |
| --- | --- |
| Types for the bridge, `window.k8sdockside` | `"extends": "@k8sdockside/plugin-sdk/tsconfig.json"` |
| The build: `src/` into `ui/` with esbuild | `k8sdockside-plugin build \| watch \| check` |
| DOM helpers that put cluster data on the page as text, never HTML | `import { el, button, replace, byId, svg } from '@k8sdockside/plugin-sdk/dom'` |

The bridge itself is not in this package. The app serves it to every plugin
page at `/plugin-ui/_sdk/k8sdockside.js`, and a plugin is still installed by
cloning its repository and serving the `ui/` committed there. This package is
only needed to *build* a plugin, never to run one.

## Use it

```sh
npm install --save-dev @k8sdockside/plugin-sdk typescript
```

`tsconfig.json`:

```jsonc
{
    "extends": "@k8sdockside/plugin-sdk/tsconfig.json",
    "include": ["src"]
}
```

`package.json`:

```json
"scripts": {
    "build": "k8sdockside-plugin build",
    "watch": "k8sdockside-plugin watch",
    "typecheck": "tsc --noEmit",
    "check": "npm run typecheck && k8sdockside-plugin check"
}
```

The build expects the layout every TypeScript plugin already has:

```
src/pages/<page>.ts    bundled to ui/<page>.js (a classic script, not a module)
src/pages/<page>.html  copied to ui/
src/styles/*.css       copied to ui/
src/assets/*           copied to ui/ (the plugin's logo, above all)
```

`check` builds in memory and fails if `ui/` differs. Run it in CI: installing a
plugin does not build anything, so a stale `ui/` is what users would get.

## Versions

The package follows semver. A new call on the bridge is a minor version; the
types say which app version added it, and plugins should check it exists.
Only a change that breaks existing plugin source is a major version, and the
bridge protocol, `k8sdockside/plugin@1`, is what 1.x describes.

A plugin that depends on `^1.0.0` gets new types with `npm update`.

## Working on it

The types are not edited here. Their one copy is
`internal/plugins/sdk/k8sdockside.d.ts` in the app repository, beside the
bridge they describe; `npm run sync` (and `npm pack` or `npm publish`) copies
it in.

```sh
cd packages/plugin-sdk
npm install
npm test
```

To try an unpublished change in a plugin:

```sh
cd packages/plugin-sdk && npm pack             # makes k8sdockside-plugin-sdk-<version>.tgz
cd ../../../gpu && npm install --no-save ../k8sdockside/packages/plugin-sdk/k8sdockside-plugin-sdk-*.tgz
npm run check
```

To release: bump `version` in `package.json`, commit, and push a tag
`plugin-sdk-v<version>`. The `plugin-sdk` workflow tests and publishes it.
