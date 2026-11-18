#!/usr/bin/env node
// Fails on high and critical npm advisories, except the ones accepted in
// audit-allow.json.
//
// `npm audit --audit-level=high` has no way to set one advisory aside, and
// sometimes one has to be: an advisory can cover every published version of
// a package (GHSA-fc5q-39xc-vjpf covered all of picomatch when it was added
// here), and then the only alternatives are a gate that stays red until
// upstream ships a fix -- training everyone to ignore it -- or one that stops
// looking at the whole tree.
//
// So the exceptions are written down, one advisory at a time, each with the
// package it is in, why it is acceptable, and the day it stops being
// accepted. An entry past that day fails the gate again, so an exception
// cannot quietly outlive its reason. Everything not on the list is gated
// exactly as before: high and critical fail, dev dependencies included.
//
// Judging advisories rather than packages is what keeps this narrow. npm
// reports a package as vulnerable both for an advisory of its own and for
// depending on one (vite "via" picomatch); only the first kind names an
// advisory, so setting one aside clears the packages that only depend on it
// and nothing else.
//
// Usage: node scripts/audit-gate.mjs [audit.json]
//   With a file, judges that `npm audit --json` report; without one, runs the
//   audit itself. Run from frontend/. Exits 1 when the gate fails, 2 when there
//   is no report to judge.

import { execFile } from "node:child_process";
import { appendFileSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

const GATED = new Set(["high", "critical"]);
const ID = /GHSA(-[23456789cfghjmpqrvwx]{4}){3}$/;
const DAY = /^\d{4}-\d{2}-\d{2}$/;

/** The GHSA id at the end of an advisory's url, or "" for one without. */
export function advisoryId(url) {
  return ID.exec(url ?? "")?.[0] ?? "";
}

/**
 * Checks the allow list's own shape. A mistake in it is reported rather than
 * guessed past: an entry the gate cannot read would otherwise accept nothing
 * and fail with a confusing message, or -- worse -- be read as accepting more
 * than it says.
 */
export function checkAllowList(entries) {
  if (!Array.isArray(entries)) return ["audit-allow.json should be a list of entries"];
  const problems = [];
  const seen = new Set();
  entries.forEach((entry, i) => {
    const at = `audit-allow.json entry ${i + 1}`;
    if (!entry || typeof entry !== "object") return problems.push(`${at} is not an object`);
    if (!ID.test(entry.id ?? "") || entry.id.length !== 19) problems.push(`${at}: "id" should be a GHSA id, like GHSA-fc5q-39xc-vjpf`);
    if (typeof entry.package !== "string" || !entry.package) problems.push(`${at}: "package" should name the package the advisory is in`);
    if (typeof entry.reason !== "string" || entry.reason.trim().length < 20) problems.push(`${at}: "reason" should say why it is acceptable, in a sentence`);
    if (!DAY.test(entry.until ?? "") || Number.isNaN(Date.parse(`${entry.until}T00:00:00Z`))) problems.push(`${at}: "until" should be a day, YYYY-MM-DD`);
    if (seen.has(entry.id)) problems.push(`${at}: ${entry.id} is listed twice`);
    seen.add(entry.id);
  });
  return problems;
}

/**
 * Sorts every advisory in an `npm audit --json` report (version 2) into the
 * ones that fail the gate and the ones accepted, and says which entries of
 * the allow list are past their day or no longer needed.
 *
 * `today` is a YYYY-MM-DD day; an entry is accepted through the end of its
 * `until` day.
 */
export function judge(report, allow, today) {
  if (report?.auditReportVersion !== 2 || typeof report.vulnerabilities !== "object") {
    throw new Error("not an npm audit --json report (auditReportVersion 2)");
  }
  const byId = new Map(allow.map((entry) => [entry.id, entry]));

  // The same advisory is listed under each package range it was found in;
  // judge each one once.
  const advisories = new Map();
  for (const pkg of Object.values(report.vulnerabilities)) {
    for (const via of pkg.via ?? []) {
      if (typeof via !== "object" || via === null) continue; // a package name: it depends on one
      const id = advisoryId(via.url) || `npm-${via.source}`;
      if (!advisories.has(id)) {
        advisories.set(id, { id, package: via.name, severity: via.severity, title: via.title, url: via.url });
      }
    }
  }

  const blocking = [];
  const accepted = [];
  const expired = [];
  // Below high is reported, not gated -- counted so a run that passes still
  // says they are there.
  const notGated = {};
  for (const advisory of advisories.values()) {
    if (!GATED.has(advisory.severity)) {
      notGated[advisory.severity] = (notGated[advisory.severity] ?? 0) + 1;
      continue;
    }
    const entry = byId.get(advisory.id);
    if (!entry) {
      blocking.push(advisory);
    } else if (entry.until < today) {
      expired.push({ ...advisory, entry });
    } else {
      accepted.push({ ...advisory, entry });
    }
  }
  // An entry for an advisory the audit no longer reports -- fixed upstream,
  // or the package gone from the tree -- is said out loud so it gets removed,
  // but does not fail anything.
  const unused = allow.filter((entry) => !advisories.has(entry.id));

  return { blocking, accepted, expired, unused, notGated, ok: blocking.length === 0 && expired.length === 0 };
}

/** The verdict as lines for a person. */
export function describe(verdict) {
  const lines = [];
  for (const a of verdict.accepted) {
    lines.push(`accepted  ${a.severity}  ${a.id}  ${a.package}: ${a.title} (until ${a.entry.until})`);
  }
  for (const a of verdict.expired) {
    lines.push(
      `EXPIRED   ${a.severity}  ${a.id}  ${a.package}: accepted until ${a.entry.until}. ` +
        `Check for a fixed release; then remove the entry, or renew it with a new reason.`,
    );
  }
  for (const a of verdict.blocking) {
    lines.push(`BLOCKING  ${a.severity}  ${a.id}  ${a.package}: ${a.title}  ${a.url ?? ""}`.trimEnd());
  }
  for (const entry of verdict.unused) {
    lines.push(`unused    ${entry.id}  ${entry.package}: no longer reported; remove it from audit-allow.json`);
  }
  const below = Object.entries(verdict.notGated ?? {}).map(([severity, n]) => `${n} ${severity}`);
  if (below.length) lines.push(`not gated ${below.join(", ")} -- run \`npm audit\` in frontend/ for the details`);
  if (verdict.ok) {
    lines.push(`npm audit gate: passed (${verdict.accepted.length} accepted)`);
  } else {
    lines.push(`npm audit gate: ${verdict.blocking.length} blocking, ${verdict.expired.length} expired`);
  }
  return lines;
}

async function report(file) {
  if (file) return JSON.parse(readFileSync(file, "utf8"));
  // npm audit exits non-zero whenever it finds anything, which is not a
  // failure to produce the report: read stdout either way.
  const out = await promisify(execFile)("npm", ["audit", "--json"], { maxBuffer: 64 * 1024 * 1024 }).catch(
    (err) => err,
  );
  return JSON.parse(out.stdout ?? "");
}

async function main() {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const allow = JSON.parse(readFileSync(path.join(here, "..", "audit-allow.json"), "utf8"));
  const problems = checkAllowList(allow);
  if (problems.length) {
    for (const problem of problems) console.error(problem);
    process.exit(1);
  }

  let verdict;
  try {
    verdict = judge(await report(process.argv[2]), allow, new Date().toISOString().slice(0, 10));
  } catch (err) {
    // No report, or npm's own error object (the registry did not answer):
    // nothing was judged, which is not the same as nothing being found.
    console.error(`npm audit gate: no report to judge (${err.message})`);
    process.exit(2);
  }
  const lines = describe(verdict);
  for (const line of lines) (verdict.ok ? console.log : console.error)(line);

  if (process.env.GITHUB_ACTIONS === "true") {
    for (const a of verdict.blocking) console.log(`::error::${a.severity} advisory ${a.id} in ${a.package}: ${a.title}`);
    for (const a of verdict.expired) console.log(`::error::${a.id} in ${a.package} was accepted until ${a.entry.until}`);
    for (const e of verdict.unused) console.log(`::warning::${e.id} in audit-allow.json is no longer reported; remove it`);
    if (process.env.GITHUB_STEP_SUMMARY) {
      appendFileSync(process.env.GITHUB_STEP_SUMMARY, ["", "### Gate", "", "```", ...lines, "```", ""].join("\n"));
    }
  }
  process.exit(verdict.ok ? 0 : 1);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
