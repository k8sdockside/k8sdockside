// The boundary between the plugin bindings and the rest of the app, matching
// state/adopt.ts and theme/adopt.ts: the generated types are nullable wherever
// a Go slice could be nil, and resolving that once here keeps `?? []` out of
// the sidebar and the overview.

import type * as bindings from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/plugins/models.js';
import type { KnownPlugin, Plugin, PluginCatalogue, PluginLink, PluginSummary } from './types';

function adoptLinks(links: bindings.Link[] | null | undefined): PluginLink[] {
    return (links ?? []).map((l) => ({ label: l.label || l.url, url: l.url }));
}

export function adoptPlugin(plugin: bindings.Plugin): Plugin {
    return {
        id: plugin.id,
        name: plugin.name,
        tagline: plugin.tagline ?? '',
        icon: plugin.icon || 'puzzle',
        author: plugin.author ?? '',
        authorUrl: plugin.authorUrl ?? '',
        official: plugin.official ?? false,
        docs: plugin.docs ?? '',
        links: adoptLinks(plugin.links),
        version: plugin.version ?? '',
        minAppVersion: plugin.minAppVersion ?? '',
        description: plugin.description ?? '',
        requires: (plugin.requires ?? []).map((req) => ({
            kind: req.kind,
            label: req.label || req.kind,
            optional: req.optional ?? false,
        })),
        views: (plugin.views ?? []).map((view) => ({
            id: view.id,
            label: view.label,
            icon: view.icon || 'puzzle',
            type: view.type ?? 'table',
            kind: view.kind ?? '',
            namespace: view.namespace ?? '',
            selector: view.selector ?? '',
            entry: view.entry ?? '',
            focus: view.focus ? { kind: view.focus.kind, hash: view.focus.hash ?? '' } : null,
        })),
        ui: plugin.ui
            ? { readable: [...(plugin.ui.readable ?? [])], write: plugin.ui.write ?? false }
            : null,
        actions: (plugin.actions ?? []).map((a) => ({ id: a.id, label: a.label, kind: a.kind })),
        sections: (plugin.sections ?? []).map((s) => ({
            id: s.id,
            label: s.label,
            kind: s.kind,
            entry: s.entry || 'index.html',
            height: s.height || 240,
        })),
        overview: plugin.overview ? { entry: plugin.overview.entry || 'index.html' } : null,
        origin: plugin.origin,
        pack: plugin.pack,
        repo: plugin.repo ?? '',
        disabled: plugin.disabled ?? false,
    };
}

export function adoptPluginCatalogue(catalogue: bindings.Catalogue): PluginCatalogue {
    return {
        plugins: (catalogue.plugins ?? []).map(adoptPlugin),
        dir: catalogue.dir ?? '',
        folders: [...(catalogue.folders ?? [])],
        problems: (catalogue.problems ?? []).map((p) => ({ path: p.path, message: p.message })),
    };
}

export function adoptPluginSummary(summary: bindings.Summary): PluginSummary {
    return {
        pluginId: summary.pluginId,
        installed: summary.installed,
        checked: summary.checked,
        requirements: (summary.requirements ?? []).map((req) => ({
            kind: req.kind,
            label: req.label || req.kind,
            optional: req.optional,
            served: req.served,
            error: req.error,
        })),
        cards: (summary.cards ?? []).map((card) => ({
            label: card.label,
            kind: card.kind,
            total: card.total,
            grouped: card.grouped,
            buckets: (card.buckets ?? []).map((b) => ({ value: b.value, count: b.count, tone: b.tone })),
            error: card.error,
        })),
        error: summary.error,
    };
}

export function adoptKnownPlugin(known: bindings.KnownOffer): KnownPlugin {
    return {
        id: known.id,
        name: known.name,
        tagline: known.tagline ?? '',
        icon: known.icon || 'puzzle',
        description: known.description ?? '',
        repo: known.repo,
        detect: [...(known.detect ?? [])],
        links: adoptLinks(known.links),
        author: known.author ?? '',
        authorUrl: known.authorUrl ?? '',
        official: known.official ?? false,
        installed: known.installed,
    };
}
