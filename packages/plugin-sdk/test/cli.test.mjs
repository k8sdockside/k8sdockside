// Builds a throwaway plugin with the CLI and checks what lands in ui/.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtemp, mkdir, writeFile, readFile, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const CLI = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'bin', 'k8sdockside-plugin.mjs');

async function plugin() {
    const root = await mkdtemp(path.join(tmpdir(), 'k8sdockside-plugin-'));
    await mkdir(path.join(root, 'src', 'pages'), { recursive: true });
    await mkdir(path.join(root, 'src', 'styles'), { recursive: true });
    await writeFile(path.join(root, 'src', 'pages', 'main.ts'), "const n: number = 1;\ndocument.title = String(n);\n");
    await writeFile(path.join(root, 'src', 'pages', 'main.html'), '<!doctype html><script src="main.js"></script>\n');
    await writeFile(path.join(root, 'src', 'styles', 'main.css'), 'body { margin: 0; }\n');
    return root;
}

function run(root, ...args) {
    return spawnSync(process.execPath, [CLI, ...args], { cwd: root, encoding: 'utf8' });
}

test('build writes the pages, their HTML and the styles as a classic script', async (t) => {
    const root = await plugin();
    t.after(() => rm(root, { recursive: true, force: true }));

    execFileSync(process.execPath, [CLI, 'build'], { cwd: root });

    assert.deepEqual((await readdir(path.join(root, 'ui'))).sort(), ['main.css', 'main.html', 'main.js']);
    const js = await readFile(path.join(root, 'ui', 'main.js'), 'utf8');
    assert.match(js, /^\/\/ Built by k8sdockside-plugin from src\//);
    assert.match(js, /\(\(\) => \{/, 'an IIFE, not a module');
    assert.doesNotMatch(js, /\bexport\b|\bimport\b/);
});

test('check passes on a fresh build and fails on a stale or extra file', async (t) => {
    const root = await plugin();
    t.after(() => rm(root, { recursive: true, force: true }));

    assert.equal(run(root, 'check').status, 1, 'no ui/ yet');
    assert.equal(run(root, 'build').status, 0);
    assert.equal(run(root, '--check').status, 0, 'the old flag form still works');

    await writeFile(path.join(root, 'ui', 'stray.js'), '');
    const stray = run(root, 'check');
    assert.equal(stray.status, 1);
    assert.match(stray.stderr, /unexpected ui\/stray\.js/);

    assert.equal(run(root).status, 0, 'no argument builds, which removes the stray file');
    await writeFile(path.join(root, 'src', 'styles', 'main.css'), 'body { margin: 1px; }\n');
    assert.match(run(root, 'check').stderr, /stale {5}ui\/main\.css/);
});

test('an unknown command or a folder without src/pages is refused', async (t) => {
    const root = await plugin();
    t.after(() => rm(root, { recursive: true, force: true }));

    assert.equal(run(root, 'deploy').status, 2);
    assert.equal(run(tmpdir(), 'build').status, 2);
});
