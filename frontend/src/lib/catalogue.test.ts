import { describe, expect, test } from 'vitest';
import {
    DASHBOARD,
    DASHBOARD_ITEM,
    DEFAULT_COLLAPSED_GROUPS,
    NAV_GROUPS,
    PLUGIN_OVERVIEW,
    PLUGINS_GROUP,
    groupForKind,
    iconFor,
    isPluginOverview,
    labelFor,
    parsePluginKind,
    pluginKindFor,
    registerPluginViews,
    singularFor,
} from './catalogue';
import { PATHS } from './components/Icon.svelte';

const ITEMS = NAV_GROUPS.flatMap((group) => group.items);
const KINDS = ITEMS.map((item) => item.kind);

describe('the resource catalogue', () => {
    test('never lists the same kind twice', () => {
        expect(KINDS).toHaveLength(new Set(KINDS).size);
    });

    // An icon name with no matching path renders an empty <svg>: the row still
    // lays out, so a typo here is invisible until someone looks closely.
    test('every nav item names an icon that exists', () => {
        const missing = ITEMS.filter((item) => !(item.icon in PATHS)).map((item) => `${item.kind}:${item.icon}`);
        expect(missing).toEqual([]);
    });

    // Plugins is the one exception, and deliberately so: its rows are the
    // plugins installed on this machine, which this list cannot know about. It
    // is still a group so that it folds and is remembered folded with the rest.
    test('every group has a label, and items unless its rows come from elsewhere', () => {
        for (const group of NAV_GROUPS) {
            expect(group.label).not.toBe('');
            if (group.label === PLUGINS_GROUP) {
                expect(group.items).toHaveLength(0);
                continue;
            }
            expect(group.items.length).toBeGreaterThan(0);
        }
    });
});

describe('the kinds added beyond the original set', () => {
    const ADDED: [string, string][] = [
        ['replicasets', 'Replica Sets'],
        ['replicationcontrollers', 'Replication Controllers'],
        ['horizontalpodautoscalers', 'Horizontal Pod Autoscalers'],
        ['resourcequotas', 'Resource Quotas'],
        ['limitranges', 'Limit Ranges'],
        ['leases', 'Leases'],
        ['poddisruptionbudgets', 'Pod Disruption Budgets'],
        ['priorityclasses', 'Priority Classes'],
        ['runtimeclasses', 'Runtime Classes'],
        ['mutatingwebhookconfigurations', 'Mutating Webhooks'],
        ['validatingwebhookconfigurations', 'Validating Webhooks'],
        ['mutatingadmissionpolicies', 'Mutating Admission Policies'],
        ['mutatingadmissionpolicybindings', 'Mutating Policy Bindings'],
        ['validatingadmissionpolicies', 'Validating Admission Policies'],
        ['validatingadmissionpolicybindings', 'Validating Policy Bindings'],
        ['listenersets', 'Listener Sets'],
        ['tlsroutes', 'TLS Routes'],
        ['tcproutes', 'TCP Routes'],
        ['udproutes', 'UDP Routes'],
        ['backendtlspolicies', 'Backend TLS Policies'],
    ];

    test.each(ADDED)('%s is offered in the sidebar as "%s"', (kind, label) => {
        expect(KINDS).toContain(kind);
        expect(labelFor(kind)).toBe(label);
        expect(iconFor(kind)).not.toBe('box');
    });

    test('the scheduling and admission groups exist', () => {
        const labels = NAV_GROUPS.map((g) => g.label);
        expect(labels).toContain('Scheduling');
        expect(labels).toContain('Admission');
    });

    // The detail panel titles one row with this, so "Leases" must not become
    // "Leafe" or similar through the plural-stripping.
    test('singular forms read correctly for the new kinds', () => {
        expect(singularFor('leases')).toBe('Lease');
        expect(singularFor('replicasets')).toBe('Replica Set');
        expect(singularFor('priorityclasses')).toBe('Priority Class');
        expect(singularFor('backendtlspolicies')).toBe('Backend TLS Policy');
        expect(singularFor('networkpolicies')).toBe('Network Policy');
        expect(singularFor('listenersets')).toBe('Listener Set');
    });

    test('the Gateway API kinds sit together in their own section', () => {
        for (const kind of ['listenersets', 'tlsroutes', 'tcproutes', 'udproutes', 'backendtlspolicies']) {
            expect(groupForKind(kind)).toBe('Gateway API');
        }
    });
});

// Everything else Kubernetes serves, so that no built-in kind needs a CRD-style
// detour through the definitions tree to be looked at. The list is the Go
// side's too -- see TestEveryKindKubernetesServesIsOffered.
describe('the rest of what Kubernetes serves', () => {
    const REST: [string, string, string][] = [
        ['leasecandidates', 'Lease Candidates', 'Cluster'],
        ['controllerrevisions', 'Controller Revisions', 'Workloads'],
        ['podtemplates', 'Pod Templates', 'Workloads'],
        ['servicecidrs', 'Service CIDRs', 'Network'],
        ['ipaddresses', 'IP Addresses', 'Network'],
        ['volumeattributesclasses', 'Volume Attributes Classes', 'Storage'],
        ['volumeattachments', 'Volume Attachments', 'Storage'],
        ['csidrivers', 'CSI Drivers', 'Storage'],
        ['csinodes', 'CSI Nodes', 'Storage'],
        ['csistoragecapacities', 'CSI Storage Capacities', 'Storage'],
        ['certificatesigningrequests', 'Certificate Signing Requests', 'Access'],
        ['podcertificaterequests', 'Pod Certificate Requests', 'Access'],
        ['clustertrustbundles', 'Cluster Trust Bundles', 'Access'],
        ['workloads', 'Scheduling Workloads', 'Scheduling'],
        ['podgroups', 'Pod Groups', 'Scheduling'],
        ['compositepodgroups', 'Composite Pod Groups', 'Scheduling'],
        ['evictionrequests', 'Eviction Requests', 'Scheduling'],
        ['evictions', 'Evictions', 'Scheduling'],
        ['deviceclasses', 'Device Classes', 'Dynamic Resources'],
        ['resourceclaims', 'Resource Claims', 'Dynamic Resources'],
        ['resourceclaimtemplates', 'Resource Claim Templates', 'Dynamic Resources'],
        ['resourceslices', 'Resource Slices', 'Dynamic Resources'],
        ['devicetaintrules', 'Device Taint Rules', 'Dynamic Resources'],
        ['resourcepoolstatusrequests', 'Pool Status Requests', 'Dynamic Resources'],
        ['apiservices', 'API Services', 'API Server'],
        ['flowschemas', 'Flow Schemas', 'API Server'],
        ['prioritylevelconfigurations', 'Priority Levels', 'API Server'],
        ['storageversions', 'Storage Versions', 'API Server'],
        ['storageversionmigrations', 'Storage Version Migrations', 'API Server'],
    ];

    test.each(REST)('%s is offered as "%s" under %s', (kind, label, group) => {
        expect(KINDS).toContain(kind);
        expect(labelFor(kind)).toBe(label);
        expect(groupForKind(kind)).toBe(group);
        expect(iconFor(kind)).not.toBe('box');
    });

    test('their singular forms read correctly', () => {
        expect(singularFor('ipaddresses')).toBe('IP Address');
        expect(singularFor('csistoragecapacities')).toBe('CSI Storage Capacity');
        expect(singularFor('volumeattributesclasses')).toBe('Volume Attributes Class');
        expect(singularFor('deviceclasses')).toBe('Device Class');
        expect(singularFor('prioritylevelconfigurations')).toBe('Priority Level');
        expect(singularFor('certificatesigningrequests')).toBe('Certificate Signing Request');
    });
});

describe('groupForKind', () => {
    test('names the section a resource is listed under', () => {
        expect(groupForKind('pods')).toBe('Workloads');
        expect(groupForKind('mutatingwebhookconfigurations')).toBe('Admission');
        expect(groupForKind('priorityclasses')).toBe('Scheduling');
    });

    // The dashboard is not in a section any more: it sits at the top of the
    // tree on its own, so there is never a section to unfold to reach it.
    test('the dashboard belongs to no section', () => {
        expect(groupForKind(DASHBOARD)).toBeNull();
    });

    // Custom resources are opened from the definitions table and have no nav
    // entry, so there is no section to speak of.
    test('a custom resource belongs to no section', () => {
        expect(groupForKind('crd:certificates.cert-manager.io')).toBeNull();
    });

    test('an unknown kind belongs to no section', () => {
        expect(groupForKind('nonsense')).toBeNull();
    });

    test('every kind the sidebar offers can be traced back to its section', () => {
        for (const group of NAV_GROUPS) {
            for (const item of group.items) {
                expect(groupForKind(item.kind)).toBe(group.label);
            }
        }
    });
});

describe('the dashboard entry', () => {
    test('is offered on its own rather than inside a section', () => {
        expect(DASHBOARD_ITEM.kind).toBe(DASHBOARD);
        expect(NAV_GROUPS.flatMap((g) => g.items).map((i) => i.kind)).not.toContain(DASHBOARD);
    });

    test('still resolves its label and icon like any other kind', () => {
        expect(labelFor(DASHBOARD)).toBe('Dashboard');
        expect(iconFor(DASHBOARD)).toBe('dashboard');
    });

    test('there is no Overview section left', () => {
        expect(NAV_GROUPS.map((g) => g.label)).not.toContain('Overview');
    });
});

describe('the default folding', () => {
    // Every section starts shut. Expanding a context then shows the dashboard
    // and a short list of headings rather than fifty rows.
    test('folds every section', () => {
        expect([...DEFAULT_COLLAPSED_GROUPS].sort()).toEqual(NAV_GROUPS.map((g) => g.label).sort());
    });

    test('follows the sections rather than a list kept in step by hand', () => {
        expect(DEFAULT_COLLAPSED_GROUPS).toHaveLength(NAV_GROUPS.length);
    });
});


// A plugin view is a tab kind like any other, which is the whole point: the tab
// bar, the persisted session and the reveal machinery all handle it without
// knowing that plugins exist.
describe('plugin view kinds', () => {
    test('round-trip through the kind string', () => {
        const kind = pluginKindFor('argocd', 'applications');
        expect(kind).toBe('plugin:argocd/applications');
        expect(parsePluginKind(kind)).toEqual({ pluginId: 'argocd', viewId: 'applications' });
    });

    test.each(['pods', 'crd:applications.argoproj.io', 'plugin:', 'plugin:argocd', 'plugin:/applications', 'plugin:argocd/'])(
        'refuses %s',
        (kind) => {
            expect(parsePluginKind(kind)).toBeNull();
        },
    );

    test('the overview is told apart from a listing', () => {
        expect(isPluginOverview(pluginKindFor('argocd', PLUGIN_OVERVIEW))).toBe(true);
        expect(isPluginOverview(pluginKindFor('argocd', 'applications'))).toBe(false);
        expect(isPluginOverview('pods')).toBe(false);
    });
});

// A `crd:` kind carries its own name; a plugin view's name lives in a file, so
// it has to be registered before anything can title a tab with it.
describe('registered plugin views', () => {
    test('take their label and icon from the registry', () => {
        registerPluginViews([
            { kind: 'plugin:argocd/applications', label: 'Applications', icon: 'rocket' },
        ]);

        expect(labelFor('plugin:argocd/applications')).toBe('Applications');
        expect(iconFor('plugin:argocd/applications')).toBe('rocket');
    });

    // What a tab restored before its plugin has loaded shows for an instant,
    // and what one whose plugin was uninstalled shows for good. The whole
    // `plugin:x/y` string would be worse than the view's own name.
    test('fall back to the view id when the plugin is not loaded', () => {
        registerPluginViews([]);

        expect(labelFor('plugin:argocd/applications')).toBe('applications');
        expect(iconFor('plugin:argocd/applications')).toBe('puzzle');
    });

    test('registering replaces what was there rather than adding to it', () => {
        registerPluginViews([{ kind: 'plugin:a/one', label: 'One', icon: 'box' }]);
        registerPluginViews([{ kind: 'plugin:b/two', label: 'Two', icon: 'box' }]);

        expect(labelFor('plugin:a/one')).toBe('one');
        expect(labelFor('plugin:b/two')).toBe('Two');
    });
});
