import { describe, expect, it } from 'vitest';
import { authorOf, knownStanding, standingOf } from './credit';

describe('credit', () => {
    it('calls a plugin official only when the loader says so, whatever it is called', () => {
        expect(standingOf({ origin: 'builtin', official: false })).toBe('builtin');
        expect(standingOf({ origin: '/plugins/cert-manager/plugin.json', official: true })).toBe('official');
        expect(standingOf({ origin: '/plugins/cert-manager/plugin.json', official: false })).toBe('community');
        expect(standingOf({ origin: '/plugins/acme.json' })).toBe('community');
        expect(knownStanding({ official: false })).toBe('community');
    });

    it('credits the project for a built-in that names no author', () => {
        expect(authorOf({ origin: 'builtin', author: '' })).toBe('K8s Dockside');
        expect(authorOf({ origin: '/plugins/acme.json', author: '' })).toBe('');
        expect(authorOf({ origin: '/plugins/acme.json', author: 'Acme Inc' })).toBe('Acme Inc');
    });
});
