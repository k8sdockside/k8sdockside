// Copies the bridge's types into the package.
//
// The one copy kept in step with the bridge is
// internal/plugins/sdk/k8sdockside.d.ts, beside the k8sdockside.js the app
// embeds and serves -- a change to the bridge and to its types lands in the
// same commit there. This package ships a copy made at pack time, so there is
// nothing to keep in step by hand and the copy here is not committed.

import { copyFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const SOURCE = path.resolve(HERE, '..', '..', 'internal', 'plugins', 'sdk', 'k8sdockside.d.ts');

await copyFile(SOURCE, path.join(HERE, 'k8sdockside.d.ts'));
console.log('k8sdockside.d.ts copied from internal/plugins/sdk');
