#!/usr/bin/env node
// k8sdockside-plugin -- builds a plugin's src/ into ui/, the folder the app
// serves the plugin's pages from. Run it from the plugin's root.
//
//   k8sdockside-plugin build   build once
//   k8sdockside-plugin watch   rebuild on every change under src/
//   k8sdockside-plugin check   build in memory and fail if ui/ differs
//
// (`--watch` and `--check` work too, as they did for scripts/build.mjs.)
//
// Every src/pages/<page>.ts is bundled on its own into ui/<page>.js as a
// classic script (an IIFE, not an ES module: the page runs in a sandboxed
// frame with an opaque origin, where a module would be a cross-origin load).
// The HTML next to it and the stylesheets in src/styles are copied as they
// are. Nothing else ends up in ui/, and a file there that the build did not
// write is removed, so ui/ is always exactly what src/ says.
//
// No dependencies beyond esbuild and Node's own modules.

import { readdir, readFile, writeFile, mkdir, rm, watch as watchDir } from 'node:fs/promises';
import path from 'node:path';
import * as esbuild from 'esbuild';

const ROOT = process.cwd();
const PAGES = path.join(ROOT, 'src', 'pages');
const STYLES = path.join(ROOT, 'src', 'styles');
// Files the app or the pages load by name rather than import: the plugin's
// own mark, above all. Optional -- a plugin with none simply has no folder.
const ASSETS = path.join(ROOT, 'src', 'assets');
const OUT = path.join(ROOT, 'ui');

// macOS draws the pages in WKWebView (Safari's engine), Windows in WebView2
// (Chromium) and Linux in WebKitGTK. Anything these understand is fine.
const TARGET = ['safari16', 'chrome114', 'edge114', 'firefox115'];

const BANNER = '// Built by k8sdockside-plugin from src/ -- edit the TypeScript there, not this file.';

const USAGE = 'usage: k8sdockside-plugin [build|watch|check]';

function parseMode(args) {
    const words = args.map((arg) => arg.replace(/^--/, ''));
    if (words.includes('help') || words.includes('-h')) {
        console.log(USAGE);
        process.exit(0);
    }
    const known = words.filter((word) => ['build', 'watch', 'check'].includes(word));
    const unknown = words.filter((word) => !known.includes(word));
    if (unknown.length || known.length > 1) {
        console.error(USAGE);
        process.exit(2);
    }
    return known[0] ?? 'build';
}

const mode = parseMode(process.argv.slice(2));

async function entryPoints() {
    const names = await readdir(PAGES).catch(() => {
        console.error(`no src/pages in ${ROOT} -- run k8sdockside-plugin from the plugin's root`);
        process.exit(2);
    });
    return names
        .filter((name) => name.endsWith('.ts') && !name.endsWith('.test.ts') && !name.endsWith('.d.ts'))
        .sort()
        .map((name) => path.join(PAGES, name));
}

/** The static files: each page's HTML, the stylesheets, and the assets. */
async function statics() {
    const out = new Map();
    for (const [dir, ext] of [
        [PAGES, '.html'],
        [STYLES, '.css'],
        // Whatever is in assets, whatever it is named: the manifest's `logo`
        // may be an .svg, a .png or a .webp, and ui/ is checked against this
        // list, so anything left out of it is reported as unexpected.
        [ASSETS, ''],
    ]) {
        for (const name of (await names(dir)).sort()) {
            if (name.endsWith(ext)) out.set(name, await readFile(path.join(dir, name)));
        }
    }
    return out;
}

/** The files in a folder, or none when the folder is not there. */
async function names(dir) {
    try {
        return await readdir(dir);
    } catch {
        return [];
    }
}

async function options() {
    return {
        absWorkingDir: ROOT,
        entryPoints: await entryPoints(),
        outdir: OUT,
        bundle: true,
        format: 'iife',
        platform: 'browser',
        target: TARGET,
        charset: 'utf8',
        legalComments: 'none',
        banner: { js: BANNER },
        // Written by this script rather than by esbuild, so --check can
        // compare instead of overwrite.
        write: false,
        logLevel: 'warning',
    };
}

/** Everything ui/ should hold, as file name -> bytes. */
async function expected(outputFiles) {
    const files = await statics();
    for (const file of outputFiles) {
        files.set(path.relative(OUT, file.path).split(path.sep).join('/'), Buffer.from(file.contents));
    }
    return files;
}

async function onDisk() {
    const files = new Map();
    let names = [];
    try {
        names = await readdir(OUT, { recursive: true, withFileTypes: true });
    } catch {
        return files;
    }
    for (const entry of names) {
        if (!entry.isFile()) continue;
        const full = path.join(entry.parentPath ?? entry.path, entry.name);
        files.set(path.relative(OUT, full).split(path.sep).join('/'), await readFile(full));
    }
    return files;
}

async function writeAll(files) {
    await mkdir(OUT, { recursive: true });
    const have = await onDisk();
    for (const name of have.keys()) {
        if (!files.has(name)) await rm(path.join(OUT, name));
    }
    let written = 0;
    for (const [name, bytes] of files) {
        const old = have.get(name);
        if (old && old.equals(bytes)) continue;
        await mkdir(path.dirname(path.join(OUT, name)), { recursive: true });
        await writeFile(path.join(OUT, name), bytes);
        written++;
    }
    return written;
}

function stamp() {
    return new Date().toLocaleTimeString();
}

async function buildOnce() {
    const result = await esbuild.build(await options());
    return expected(result.outputFiles);
}

if (mode === 'build') {
    const files = await buildOnce();
    const written = await writeAll(files);
    console.log(`ui/: ${files.size} files, ${written} changed`);
} else if (mode === 'check') {
    // A fresh build, compared with what is in ui/ -- which in CI is what was
    // committed. The app installs a plugin by cloning it, so a src/ change
    // whose ui/ was not rebuilt and committed never reaches anyone.
    const want = await buildOnce();
    const have = await onDisk();
    const problems = [];
    for (const [name, bytes] of want) {
        const got = have.get(name);
        if (!got) problems.push(`missing   ui/${name}`);
        else if (!got.equals(bytes)) problems.push(`stale     ui/${name}`);
    }
    for (const name of have.keys()) {
        if (!want.has(name)) problems.push(`unexpected ui/${name}`);
    }
    if (problems.length) {
        console.error('ui/ does not match a fresh build of src/:\n  ' + problems.join('\n  '));
        console.error('\nRun `npm run build` and commit ui/ -- installing from a repository does not build anything.');
        process.exit(1);
    }
    console.log(`ui/ is up to date (${want.size} files match a fresh build)`);
} else {
    const ctx = await esbuild.context({
        ...(await options()),
        plugins: [
            {
                name: 'write-ui',
                setup(build) {
                    build.onEnd(async (result) => {
                        if (result.errors.length) {
                            console.error(`[${stamp()}] build failed; ui/ left as it was`);
                            return;
                        }
                        const written = await writeAll(await expected(result.outputFiles ?? []));
                        console.log(`[${stamp()}] ui/ rebuilt, ${written} changed -- reopen the tab to see it`);
                    });
                },
            },
        ],
    });
    await ctx.watch();
    console.log('Watching src/ -- Ctrl+C to stop.');
    // HTML, CSS and the assets are not part of esbuild's graph, so a change to
    // them asks for a rebuild by hand. A new page's .ts needs a restart of the
    // watch, and so does a first assets folder.
    let timer = null;
    for (const dir of [PAGES, STYLES, ASSETS]) {
        (async () => {
            for await (const event of watchDir(dir)) {
                if (dir !== ASSETS && !/\.(html|css)$/.test(event.filename ?? '')) continue;
                clearTimeout(timer);
                timer = setTimeout(() => ctx.rebuild().catch(() => {}), 60);
            }
        })();
    }
}
