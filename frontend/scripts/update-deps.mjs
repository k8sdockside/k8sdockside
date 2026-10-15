#!/usr/bin/env node
// Bumps package.json to the newest versions that are at least `min-release-age`
// days old (see .npmrc). `ncu` and `npm outdated` both pick the `latest` dist-tag,
// which is often younger than that, and npm then refuses to install it.
//
// A bump is also held back when another direct dependency declares a peer range
// it would break (svelte-check pinning typescript, say), since npm would refuse
// the install with ERESOLVE.
//
// Bumped packages are dropped from package-lock.json and node_modules so the
// following `npm install` resolves them afresh: otherwise npm keeps the locked
// peer group (vitest 4 next to @vitest/browser 4, ...) and fails on the bump.
//
// Usage: node scripts/update-deps.mjs [patch|minor|latest]   (default: minor)
// Run `npm install` afterwards.

import { execFile, execFileSync } from "node:child_process";
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { promisify } from "node:util";

const run = promisify(execFile);
const target = process.argv[2] ?? "minor";
if (!["patch", "minor", "latest"].includes(target)) {
  console.error(`unknown target "${target}" (want patch, minor or latest)`);
  process.exit(2);
}

const days = Number(execFileSync("npm", ["config", "get", "min-release-age"], { encoding: "utf8" }).trim()) || 0;
const cutoff = Date.now() - days * 24 * 60 * 60 * 1000;

// --- semver -----------------------------------------------------------------

const SEMVER = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/;

function parse(v) {
  const m = SEMVER.exec(v);
  if (!m) return null;
  return { major: +m[1], minor: +m[2], patch: +m[3], pre: m[4] ? m[4].split(".") : [] };
}

function compare(a, b) {
  for (const k of ["major", "minor", "patch"]) if (a[k] !== b[k]) return a[k] - b[k];
  if (!a.pre.length || !b.pre.length) return b.pre.length - a.pre.length;
  for (let i = 0; i < Math.max(a.pre.length, b.pre.length); i++) {
    const x = a.pre[i];
    const y = b.pre[i];
    if (x === undefined) return -1;
    if (y === undefined) return 1;
    const nx = /^\d+$/.test(x);
    const ny = /^\d+$/.test(y);
    if (nx && ny && +x !== +y) return +x - +y;
    if (nx !== ny) return nx ? -1 : 1;
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

const sameTuple = (a, b) => a.major === b.major && a.minor === b.minor && a.patch === b.patch;

// Partial versions ("5", "5.1", "5.x") as [major, minor, patch] with null for wildcards.
function partial(s) {
  const m = /^v?(\d+|[xX*])(?:\.(\d+|[xX*]))?(?:\.(\d+|[xX*]))?(?:-([0-9A-Za-z.-]+))?$/.exec(s);
  if (!m) return null;
  const n = (p) => (p === undefined || /[xX*]/.test(p) ? null : +p);
  return { parts: [n(m[1]), n(m[2]), n(m[3])], pre: m[4] ? m[4].split(".") : [] };
}

const ver = (maj, min = 0, pat = 0, pre = []) => ({ major: maj, minor: min, patch: pat, pre });

// One comparator set ("^5.0.0", ">=1.2 <2", "1.x", "1.0.0 - 2.0.0") as [op, version] pairs.
function comparators(set) {
  const hyphen = /^(\S+)\s+-\s+(\S+)$/.exec(set.trim());
  if (hyphen) return [...comparators(`>=${hyphen[1]}`), ...comparators(`<=${hyphen[2]}`)];
  const out = [];
  for (const tok of set.trim().split(/\s+/).filter(Boolean)) {
    const m = /^(\^|~>?|>=|<=|>|<|=)?\s*(.*)$/.exec(tok);
    const op = m[1] ?? "";
    const p = partial(m[2]);
    if (!p) return null;
    const [a, b, c] = p.parts;
    if (a === null) continue; // "*" or "x"
    if (op === "^") {
      out.push([">=", ver(a, b ?? 0, c ?? 0, p.pre)]);
      if (a > 0 || b === null) out.push(["<", ver(a + 1)]);
      else if (b > 0 || c === null) out.push(["<", ver(0, b + 1)]);
      else out.push(["<", ver(0, 0, c + 1)]);
    } else if (op.startsWith("~")) {
      out.push([">=", ver(a, b ?? 0, c ?? 0, p.pre)]);
      out.push(["<", b === null ? ver(a + 1) : ver(a, b + 1)]);
    } else if (op === "" || op === "=") {
      if (c !== null) out.push(["=", ver(a, b, c, p.pre)]);
      else {
        out.push([">=", ver(a, b ?? 0)]);
        out.push(["<", b === null ? ver(a + 1) : ver(a, b + 1)]);
      }
    } else if (op === ">") {
      out.push(c !== null ? [">", ver(a, b, c, p.pre)] : [">=", b === null ? ver(a + 1) : ver(a, b + 1)]);
    } else if (op === "<=") {
      out.push(c !== null ? ["<=", ver(a, b, c, p.pre)] : ["<", b === null ? ver(a + 1) : ver(a, b + 1)]);
    } else {
      out.push([op, ver(a, b ?? 0, c ?? 0, p.pre)]);
    }
  }
  return out;
}

const test = { "=": (d) => d === 0, ">": (d) => d > 0, ">=": (d) => d >= 0, "<": (d) => d < 0, "<=": (d) => d <= 0 };

// Ranges we cannot read are treated as satisfied, so they never block a bump.
function satisfies(v, range) {
  return range.split("||").some((set) => {
    const cs = comparators(set);
    if (!cs) return true;
    // Prereleases count only against a comparator on the same version tuple, as in npm.
    if (v.pre.length && !cs.some(([, c]) => c.pre.length && sameTuple(c, v))) return false;
    return cs.every(([op, c]) => test[op](compare(v, c)));
  });
}

// --- registry ---------------------------------------------------------------

async function view(args) {
  const { stdout } = await run("npm", ["view", ...args, "--json"], { maxBuffer: 64 << 20 });
  if (!stdout.trim()) return {};
  const data = JSON.parse(stdout);
  // Newer npm wraps `view --json` output in an array.
  return Array.isArray(data) ? Object.assign({}, ...data) : data;
}

const peerCache = new Map();
function peersOf(name, version) {
  const key = `${name}@${version}`;
  if (!peerCache.has(key)) peerCache.set(key, view([key, "peerDependencies"]).catch(() => ({})));
  return peerCache.get(key);
}

// --- main -------------------------------------------------------------------

function allowed(cur, cand) {
  if (compare(cand, cur) <= 0) return false;
  // Prereleases only when already on that prerelease line (e.g. 3.0.0-beta.N).
  if (cand.pre.length && !(cur.pre.length && sameTuple(cur, cand))) return false;
  if (target === "patch") return cand.major === cur.major && cand.minor === cur.minor;
  if (target === "minor") return cand.major === cur.major;
  return true;
}

const pkgPath = new URL("../package.json", import.meta.url);
const raw = readFileSync(pkgPath, "utf8");
const pkg = JSON.parse(raw);

const deps = [];
for (const section of ["dependencies", "devDependencies"]) {
  for (const [name, spec] of Object.entries(pkg[section] ?? {})) {
    const m = /^([~^]?)(.+)$/.exec(spec);
    const cur = m && parse(m[2]);
    if (!cur) continue; // ranges, tags, file:, git: etc. are left alone
    deps.push({ section, name, prefix: m[1], current: m[2], cur, candidates: [] });
  }
}

await Promise.all(
  deps.map(async (dep) => {
    let times;
    try {
      times = await view([dep.name, "time"]);
    } catch (err) {
      console.error(`skipping ${dep.name}: ${err.message.split("\n")[0]}`);
      return;
    }
    for (const [version, when] of Object.entries(times)) {
      const cand = parse(version);
      if (cand && Date.parse(when) <= cutoff && allowed(dep.cur, cand)) dep.candidates.push({ version, v: cand });
    }
    dep.candidates.sort((a, b) => compare(b.v, a.v)); // newest first
  }),
);

// Every dependency starts at its newest candidate (or stays put); then any pick
// that breaks a peer range declared by another dependency's pick steps down,
// until nothing changes.
const chosen = new Map(deps.map((d) => [d.name, d.candidates[0] ?? { version: d.current, v: d.cur }]));
const held = new Map();
for (let round = 0; round < 20; round++) {
  const peers = await Promise.all(deps.map(async (d) => [d.name, await peersOf(d.name, chosen.get(d.name).version)]));
  let changed = false;
  for (const dep of deps) {
    const ranges = peers.filter(([owner, p]) => owner !== dep.name && p[dep.name]).map(([owner, p]) => [owner, p[dep.name]]);
    if (ranges.every(([, r]) => satisfies(chosen.get(dep.name).v, r))) continue;
    const ok = [...dep.candidates, { version: dep.current, v: dep.cur }].find((c) => ranges.every(([, r]) => satisfies(c.v, r)));
    const next = ok ?? { version: dep.current, v: dep.cur };
    if (next.version === chosen.get(dep.name).version) continue;
    held.set(dep.name, `${dep.candidates[0].version} (peer ${ranges.map(([o, r]) => `${o} wants ${r}`).join(", ")})`);
    chosen.set(dep.name, next);
    changed = true;
  }
  if (!changed) break;
}

const changes = [];
const bumped = [];
for (const dep of deps) {
  const { version } = chosen.get(dep.name);
  if (version === dep.current) continue;
  pkg[dep.section][dep.name] = dep.prefix + version;
  changes.push(`  ${dep.name.padEnd(36)} ${dep.current} → ${version}`);
  bumped.push(dep.name);
}

if (held.size) {
  console.log("Held back by peer dependencies:");
  for (const [name, why] of [...held].sort()) console.log(`  ${name.padEnd(36)} ${why}`);
}
if (!changes.length) {
  console.log(`All dependencies are up to date (target=${target}, min-release-age=${days}d).`);
  process.exit(0);
}
console.log(`Upgrading (target=${target}, min-release-age=${days}d):\n${changes.sort().join("\n")}`);
const indent = /^(\s+)"/m.exec(raw)?.[1] ?? "  ";
writeFileSync(pkgPath, JSON.stringify(pkg, null, indent) + (raw.endsWith("\n") ? "\n" : ""));

const lockPath = new URL("../package-lock.json", import.meta.url);
if (existsSync(lockPath)) {
  const lockRaw = readFileSync(lockPath, "utf8");
  const lock = JSON.parse(lockRaw);
  for (const name of bumped) {
    delete lock.packages?.[`node_modules/${name}`];
    rmSync(new URL(`../node_modules/${name}`, import.meta.url), { recursive: true, force: true });
  }
  writeFileSync(lockPath, JSON.stringify(lock, null, 2) + (lockRaw.endsWith("\n") ? "\n" : ""));
}
