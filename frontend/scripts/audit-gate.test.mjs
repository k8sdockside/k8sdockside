import { readFileSync } from 'node:fs';
import { describe, expect, test } from 'vitest';
import { advisoryId, checkAllowList, describe as describeVerdict, judge } from './audit-gate.mjs';

// A report shaped as `npm audit --json` writes it: picomatch with an advisory
// of its own, fdir and vite only depending on it, and an unrelated package
// with one advisory per severity given.
function report(extra = {}) {
    return {
        auditReportVersion: 2,
        vulnerabilities: {
            picomatch: {
                name: 'picomatch',
                severity: 'high',
                via: [
                    {
                        source: 1247710,
                        name: 'picomatch',
                        title: 'picomatch vulnerable to ReDoS',
                        url: 'https://github.com/advisories/GHSA-fc5q-39xc-vjpf',
                        severity: 'high',
                    },
                ],
            },
            fdir: { name: 'fdir', severity: 'high', via: ['picomatch'] },
            vite: { name: 'vite', severity: 'high', via: ['picomatch', 'fdir'] },
            ...extra,
        },
        metadata: {},
    };
}

function advisory(name, severity, id) {
    return {
        [name]: {
            name,
            severity,
            via: [{ source: 1, name, title: `${name} is bad`, url: `https://github.com/advisories/${id}`, severity }],
        },
    };
}

const PICOMATCH = {
    id: 'GHSA-fc5q-39xc-vjpf',
    package: 'picomatch',
    reason: 'Only build tooling uses it, with patterns we write.',
    until: '2027-01-31',
};

describe('judge', () => {
    test('a high advisory nobody accepted blocks, once however many packages depend on it', () => {
        const verdict = judge(report(), [], '2026-11-18');
        expect(verdict.ok).toBe(false);
        expect(verdict.blocking.map((a) => a.id)).toEqual(['GHSA-fc5q-39xc-vjpf']);
    });

    test('accepting it clears the packages that only depend on it', () => {
        const verdict = judge(report(), [PICOMATCH], '2026-11-18');
        expect(verdict.ok).toBe(true);
        expect(verdict.blocking).toEqual([]);
        expect(verdict.accepted.map((a) => a.id)).toEqual(['GHSA-fc5q-39xc-vjpf']);
    });

    test('accepting one advisory does not hide another', () => {
        const verdict = judge(report(advisory('lodash', 'critical', 'GHSA-jf85-cpcp-j695')), [PICOMATCH], '2026-11-18');
        expect(verdict.ok).toBe(false);
        expect(verdict.blocking.map((a) => a.id)).toEqual(['GHSA-jf85-cpcp-j695']);
    });

    test('only high and critical are gated, as before', () => {
        const verdict = judge(
            report({ ...advisory('a', 'moderate', 'GHSA-2222-3333-4444'), ...advisory('b', 'low', 'GHSA-5555-6666-7777') }),
            [PICOMATCH],
            '2026-11-18',
        );
        expect(verdict.ok).toBe(true);
        // ...but still said, so a passing run does not read as a clean tree.
        expect(verdict.notGated).toEqual({ moderate: 1, low: 1 });
        expect(describeVerdict(verdict).join('\n')).toContain('not gated 1 moderate, 1 low');
    });

    test('an entry holds through its last day, and fails the gate the day after', () => {
        expect(judge(report(), [PICOMATCH], '2027-01-31').ok).toBe(true);

        const after = judge(report(), [PICOMATCH], '2027-02-01');
        expect(after.ok).toBe(false);
        expect(after.expired.map((a) => a.id)).toEqual(['GHSA-fc5q-39xc-vjpf']);
        expect(describeVerdict(after).join('\n')).toContain('EXPIRED');
    });

    test('an entry for an advisory no longer reported is pointed out, without failing', () => {
        const clean = { auditReportVersion: 2, vulnerabilities: {}, metadata: {} };
        const verdict = judge(clean, [PICOMATCH], '2026-11-18');
        expect(verdict.ok).toBe(true);
        expect(verdict.unused.map((e) => e.id)).toEqual(['GHSA-fc5q-39xc-vjpf']);
        expect(describeVerdict(verdict).join('\n')).toContain('remove it from audit-allow.json');
    });

    test('refuses something that is not an audit report, rather than passing it', () => {
        expect(() => judge({ error: { summary: 'network' } }, [], '2026-11-18')).toThrow(/not an npm audit/);
        expect(() => judge(null, [], '2026-11-18')).toThrow(/not an npm audit/);
    });
});

describe('advisoryId', () => {
    test('reads the GHSA id off the end of an advisory url', () => {
        expect(advisoryId('https://github.com/advisories/GHSA-fc5q-39xc-vjpf')).toBe('GHSA-fc5q-39xc-vjpf');
        expect(advisoryId('https://example.com/whatever')).toBe('');
        expect(advisoryId(undefined)).toBe('');
    });
});

describe('checkAllowList', () => {
    test('the list in the repository is well formed', () => {
        const list = JSON.parse(readFileSync(new URL('../audit-allow.json', import.meta.url), 'utf8'));
        expect(checkAllowList(list)).toEqual([]);
    });

    test('names what is wrong with a bad entry', () => {
        const problems = checkAllowList([
            { id: 'CVE-2026-1', package: '', reason: 'meh', until: 'soon' },
            PICOMATCH,
            PICOMATCH,
        ]);
        expect(problems.join('\n')).toMatch(/entry 1: "id" should be a GHSA id/);
        expect(problems.join('\n')).toMatch(/entry 1: "package"/);
        expect(problems.join('\n')).toMatch(/entry 1: "reason"/);
        expect(problems.join('\n')).toMatch(/entry 1: "until"/);
        expect(problems.join('\n')).toMatch(/entry 3: GHSA-fc5q-39xc-vjpf is listed twice/);
    });

    test('a list that is not a list is refused', () => {
        expect(checkAllowList({})).toEqual(['audit-allow.json should be a list of entries']);
    });
});
