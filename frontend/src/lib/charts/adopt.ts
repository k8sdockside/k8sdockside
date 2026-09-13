// The boundary between the metrics bindings and the charts, matching the other
// adopt modules: nullable everywhere a Go slice could be nil, resolved once here.

import type * as bindings from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services/models.js';

export interface ChartSeries {
    name: string;
    points: { t: number; v: number }[];
}

export interface ChartData {
    pluginId: string;
    pluginName: string;
    id: string;
    label: string;
    unit: string;
    description: string;
    series: ChartSeries[];
    error: string;
}

export interface MetricsSource {
    endpoint: { namespace: string; service: string; port: string; path: string; url: string; source: string };
    configured: string;
    available: boolean;
    error: string;
    /** The endpoint written for a person, matching metrics.Endpoint.Describe. */
    describe: string;
}

export function adoptSource(source: bindings.Source): MetricsSource {
    const endpoint = {
        namespace: source.endpoint?.namespace ?? '',
        service: source.endpoint?.service ?? '',
        port: source.endpoint?.port ?? '',
        // Set for a service serving the API below its root, as a VictoriaMetrics vmselect does.
        path: source.endpoint?.path ?? '',
        url: source.endpoint?.url ?? '',
        source: source.endpoint?.source ?? '',
    };
    return {
        endpoint,
        configured: source.configured ?? '',
        available: source.available ?? false,
        error: source.error ?? '',
        describe: endpoint.url || (endpoint.service ? `${endpoint.namespace}/${endpoint.service}:${endpoint.port}${endpoint.path}` : ''),
    };
}

export interface MetricsPanelData {
    /** Whether any installed plugin draws on this surface at all. */
    attached: boolean;
    source: MetricsSource;
    charts: ChartData[];
    range: number;
}

export function adoptPanel(panel: bindings.Panel): MetricsPanelData {
    return {
        attached: panel.attached,
        source: adoptSource(panel.source),
        charts: (panel.charts ?? []).map((chart) => ({
            pluginId: chart.pluginId,
            pluginName: chart.pluginName,
            id: chart.id,
            label: chart.label,
            unit: chart.unit ?? '',
            description: chart.description ?? '',
            series: (chart.series ?? []).map((s) => ({
                name: s.name ?? '',
                points: (s.points ?? []).map((p) => ({ t: p.t, v: p.v })),
            })),
            error: chart.error ?? '',
        })),
        range: panel.range,
    };
}
