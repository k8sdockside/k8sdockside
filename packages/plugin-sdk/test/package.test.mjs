// What the published package holds, and that its parts fit together.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const pkg = JSON.parse(await readFile(path.join(HERE, 'package.json'), 'utf8'));

test('the types are the app copy, byte for byte', async () => {
    const app = await readFile(path.join(HERE, '..', '..', 'internal', 'plugins', 'sdk', 'k8sdockside.d.ts'));
    const here = await readFile(path.join(HERE, 'k8sdockside.d.ts'));
    assert.ok(app.equals(here), 'run `npm run sync`');
});

test('every export points at a file the package ships', async () => {
    // npm ships package.json whatever "files" says.
    const shipped = (file) =>
        file === 'package.json' || pkg.files.some((entry) => file === entry || file.startsWith(entry));
    const targets = Object.values(pkg.exports).flatMap((value) =>
        typeof value === 'string' ? [value] : Object.values(value),
    );
    for (const target of [...targets, pkg.types, ...Object.values(pkg.bin)]) {
        const file = target.replace(/^\.\//, '');
        assert.ok(shipped(file), `${file} is exported but not in "files"`);
        await readFile(path.join(HERE, file)); // and exists
    }
});

test('dom.js and dom.d.ts declare the same functions', async () => {
    const js = await readFile(path.join(HERE, 'dom.js'), 'utf8');
    const dts = await readFile(path.join(HERE, 'dom.d.ts'), 'utf8');
    const names = (src, re) => [...src.matchAll(re)].map((m) => m[1]).sort();
    assert.deepEqual(
        names(js, /^export function (\w+)/gm),
        names(dts, /^export declare function (\w+)/gm),
    );
});
