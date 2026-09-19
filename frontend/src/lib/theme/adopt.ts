// The boundary between the theme bindings and the rest of the app, matching
// state/adopt.ts: the generated types are nullable everywhere a Go slice or map
// could be nil, and resolving that once here keeps `?? []` out of the gallery.

import type * as bindings from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/themes/models.js';
import type { Theme, ThemeCatalogue, ThemeToken } from './apply';

export function adoptTheme(theme: bindings.Theme): Theme {
    return {
        id: theme.id,
        name: theme.name,
        tagline: theme.tagline ?? '',
        base: theme.base === 'light' ? 'light' : 'dark',
        author: theme.author ?? '',
        tokens: strings(theme.tokens),
        resolved: strings(theme.resolved),
        origin: theme.origin,
        pack: theme.pack,
        warnings: [...(theme.warnings ?? [])],
    };
}

export function adoptCatalogue(catalogue: bindings.Catalogue): ThemeCatalogue {
    return {
        themes: (catalogue.themes ?? []).map(adoptTheme),
        dir: catalogue.dir ?? '',
        folders: [...(catalogue.folders ?? [])],
        problems: (catalogue.problems ?? []).map((p) => ({ path: p.path, message: p.message })),
    };
}

export function adoptTokens(tokens: bindings.Token[] | null): ThemeToken[] {
    return (tokens ?? []).map((t) => ({ name: t.name, help: t.help }));
}

/**
 * A Go `map[string]string` arrives with optional values, because the generated
 * type cannot say that a present key always has one. Dropping the undefined
 * entries here means the rest of the app gets the map it expects.
 */
function strings(map: { [_ in string]?: string } | null): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [key, value] of Object.entries(map ?? {})) {
        if (typeof value === 'string') out[key] = value;
    }
    return out;
}

/** The catalogue before anything has loaded: the app's own colours, no more. */
export function emptyCatalogue(): ThemeCatalogue {
    return { themes: [], dir: '', folders: [], problems: [] };
}
