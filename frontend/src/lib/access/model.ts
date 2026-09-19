// The access overview's model: RBAC as the frontend reasons about it.
//
// The backend hands over plain lists -- roles, bindings, service accounts, and
// who the caller is (internal/kube/access.go). Everything the overview says
// about them is worked out here, as pure functions, so it can be tested without
// a cluster: which role a binding points at, what a rule allows in words, how
// risky a role is, and who can do a given thing.
//
// The matching follows the RBAC authorizer's own rules (see ResourceMatches and
// friends in k8s.io/kubernetes/pkg/apis/rbac/v1/evaluation_helpers.go), so an
// answer here is the answer the API server would give -- for RBAC. Other
// authorizers (the Node authorizer, a webhook) are invisible from here, and the
// view says so.

import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

// ----- shapes --------------------------------------------------------------

export interface Rule {
    verbs: string[];
    apiGroups: string[];
    resources: string[];
    resourceNames: string[];
    nonResourceURLs: string[];
}

export interface Role {
    kind: string; // Role | ClusterRole
    name: string;
    namespace: string;
    rules: Rule[];
    aggregated: boolean;
    default: boolean;
}

export interface Subject {
    kind: string; // User | Group | ServiceAccount
    name: string;
    namespace: string;
}

export interface Binding {
    kind: string; // RoleBinding | ClusterRoleBinding
    name: string;
    namespace: string;
    roleKind: string;
    roleName: string;
    subjects: Subject[];
    default: boolean;
}

export interface Identity {
    username: string;
    groups: string[];
    error: string;
}

export interface Access {
    roles: Role[];
    bindings: Binding[];
    serviceAccounts: { name: string; namespace: string }[];
    me: Identity;
    unreadable: string[];
    error: string;
}

export interface MyRules {
    namespace: string;
    rules: Rule[];
    incomplete: boolean;
    evaluationError: string;
    error: string;
}

// ----- adopting the bindings -----------------------------------------------

function adoptRule(r: kube.AccessRule): Rule {
    return {
        verbs: [...(r.verbs ?? [])],
        apiGroups: [...(r.apiGroups ?? [])],
        resources: [...(r.resources ?? [])],
        resourceNames: [...(r.resourceNames ?? [])],
        nonResourceURLs: [...(r.nonResourceURLs ?? [])],
    };
}

/** The access payload with every nullable slice resolved. */
export function adoptAccess(raw: kube.AccessModel): Access {
    return {
        roles: (raw.roles ?? []).map((r) => ({
            kind: r.kind,
            name: r.name,
            namespace: r.namespace ?? '',
            rules: (r.rules ?? []).map(adoptRule),
            aggregated: r.aggregated,
            default: r.default,
        })),
        bindings: (raw.bindings ?? []).map((b) => ({
            kind: b.kind,
            name: b.name,
            namespace: b.namespace ?? '',
            roleKind: b.roleKind,
            roleName: b.roleName,
            subjects: (b.subjects ?? []).map((s) => ({ kind: s.kind, name: s.name, namespace: s.namespace ?? '' })),
            default: b.default,
        })),
        serviceAccounts: (raw.serviceAccounts ?? []).map((a) => ({ name: a.name, namespace: a.namespace })),
        me: {
            username: raw.me?.username ?? '',
            groups: [...(raw.me?.groups ?? [])],
            error: raw.me?.error ?? '',
        },
        unreadable: [...(raw.unreadable ?? [])],
        error: raw.error ?? '',
    };
}

export function adoptMyRules(raw: kube.MyRules): MyRules {
    return {
        namespace: raw.namespace,
        rules: (raw.rules ?? []).map(adoptRule),
        incomplete: raw.incomplete,
        evaluationError: raw.evaluationError ?? '',
        error: raw.error ?? '',
    };
}

// ----- keys ----------------------------------------------------------------

export function roleKey(role: { kind: string; namespace: string; name: string }): string {
    return `${role.kind}/${role.kind === 'ClusterRole' ? '' : role.namespace}/${role.name}`;
}

/** The key of the role a binding names. A RoleBinding's Role lives in its own namespace. */
export function roleKeyOf(binding: Binding): string {
    return roleKey({ kind: binding.roleKind, namespace: binding.namespace, name: binding.roleName });
}

export function bindingKey(binding: { kind: string; namespace: string; name: string }): string {
    return `${binding.kind}/${binding.namespace}/${binding.name}`;
}

export function subjectKey(subject: Subject): string {
    return `${subject.kind}/${subject.namespace}/${subject.name}`;
}

// ----- who is who ------------------------------------------------------------

/** The username the API server gives a service account. */
export function serviceAccountUser(namespace: string, name: string): string {
    return `system:serviceaccount:${namespace}:${name}`;
}

/**
 * The groups a subject is in without any binding saying so.
 *
 * A service account is in three groups by construction, and a binding to any
 * of them reaches it; leaving them out would make a service account look
 * powerless when `system:authenticated` has been handed half the cluster. A
 * user's groups come from wherever they authenticated and cannot be known from
 * here -- except for the caller, whose groups the cluster told us.
 */
export function impliedGroups(subject: Subject, me?: Identity): string[] {
    if (subject.kind === 'ServiceAccount') {
        return ['system:serviceaccounts', `system:serviceaccounts:${subject.namespace}`, 'system:authenticated'];
    }
    if (subject.kind === 'User') {
        if (me && subject.name === me.username) return me.groups;
        return ['system:authenticated'];
    }
    return [];
}

/** Whether a subject is the caller, or a group the caller is in. */
export function isMe(subject: Subject, me: Identity): boolean {
    if (!me.username) return false;
    if (subject.kind === 'User') return subject.name === me.username;
    if (subject.kind === 'Group') return me.groups.includes(subject.name);
    if (subject.kind === 'ServiceAccount') return serviceAccountUser(subject.namespace, subject.name) === me.username;
    return false;
}

const KNOWN_GROUPS: Record<string, string> = {
    'system:masters':
        'Superusers. Members skip RBAC entirely -- every request is allowed, whatever the roles say.',
    'system:authenticated': 'Everyone who has signed in, every service account included.',
    'system:unauthenticated': 'Anonymous requests: anyone who can reach the API server without credentials.',
    'system:serviceaccounts': 'Every service account in every namespace.',
    'system:nodes': 'The kubelets, one per node. Mostly governed by the Node authorizer rather than RBAC.',
    'system:bootstrappers': 'Nodes joining the cluster, before they have an identity of their own.',
    'system:monitoring': 'Monitoring systems allowed to read health and metrics endpoints.',
};

const KNOWN_USERS: Record<string, string> = {
    'system:anonymous': 'Anonymous requests: anyone who can reach the API server without credentials.',
    'system:kube-scheduler': 'The scheduler, which places pods on nodes.',
    'system:kube-controller-manager': 'The controller manager, which runs the built-in controllers.',
    'system:kube-proxy': 'kube-proxy, which programs Service routing on every node.',
    'system:apiserver': 'The API server itself.',
};

/** A sentence about a well-known subject, or '' for one we know nothing about. */
export function explainSubject(subject: Subject): string {
    if (subject.kind === 'Group') {
        if (KNOWN_GROUPS[subject.name]) return KNOWN_GROUPS[subject.name];
        if (subject.name.startsWith('system:serviceaccounts:')) {
            return `Every service account in the namespace ${subject.name.slice('system:serviceaccounts:'.length)}.`;
        }
        return 'A group. Who is in it is decided by how people sign in (certificates, OIDC, a cloud IAM), not by the cluster.';
    }
    if (subject.kind === 'User') {
        if (KNOWN_USERS[subject.name]) return KNOWN_USERS[subject.name];
        if (subject.name.startsWith('system:node:')) return 'The kubelet of one node.';
        if (subject.name.startsWith('system:serviceaccount:')) return 'A service account, named as a user.';
        return 'A person or tool, as named by its credentials. Kubernetes keeps no list of users.';
    }
    if (subject.kind === 'ServiceAccount') {
        return 'An identity for workloads. Pods run as a service account and get whatever it is granted.';
    }
    return '';
}

const KNOWN_ROLES: Record<string, string> = {
    'cluster-admin':
        'Everything, everywhere. Bound with a RoleBinding it is instead everything inside that one namespace.',
    admin: 'Full control inside a namespace, its roles and bindings included -- but not the namespace itself or its quota.',
    edit: 'Read and write most objects in a namespace, Secrets included. Cannot change roles or bindings.',
    view: 'Read most objects in a namespace. Not Secrets, and not roles or bindings.',
};

export function explainRole(role: { kind: string; name: string }): string {
    if (role.kind === 'ClusterRole' && KNOWN_ROLES[role.name]) return KNOWN_ROLES[role.name];
    return '';
}

/**
 * Whether an object is the cluster's own plumbing rather than something a
 * person set up: the API server's bootstrap roles, kubeadm's, and the like.
 * They are most of what a fresh cluster holds, and the overview hides them
 * unless asked -- but not the four user-facing roles above, which are what
 * people actually bind.
 */
export function isSystem(obj: { name: string; namespace: string; default: boolean }): boolean {
    if (obj.name.startsWith('system:') || obj.name.startsWith('kubeadm:')) return true;
    return obj.default && obj.namespace.startsWith('kube-');
}

// ----- words -----------------------------------------------------------------

/** The verbs the matrix gives a column each, in the order people think of them. */
export const MATRIX_VERBS = ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete', 'deletecollection'];

/** What each verb means, for the column tooltips and the legend. */
export const VERB_HELP: Record<string, string> = {
    get: 'Read one object by name',
    list: 'Read every object of the kind',
    watch: 'Follow changes as they happen',
    create: 'Make new objects',
    update: 'Replace existing objects',
    patch: 'Change parts of existing objects',
    delete: 'Remove one object',
    deletecollection: 'Remove many objects at once',
    escalate: 'Grant more than you have yourself, by editing roles',
    bind: 'Bind roles you do not hold yourself',
    impersonate: 'Act as another user, group or service account',
    use: 'Use a policy object (e.g. a PodSecurityPolicy)',
    approve: 'Approve certificate signing requests',
    sign: 'Sign certificate signing requests',
    '*': 'Every verb',
};

const READ = ['get', 'list', 'watch'];
const WRITE = ['create', 'update', 'patch'];
const REMOVE = ['delete', 'deletecollection'];

/** "read", "read and change", "full control" -- a verb list in words. */
export function describeVerbs(verbs: string[]): string {
    if (verbs.includes('*')) return 'full control of';
    const parts: string[] = [];
    const has = (list: string[]) => list.filter((v) => verbs.includes(v));
    const read = has(READ);
    if (read.length === READ.length) parts.push('read');
    else if (read.length) parts.push(read.join('/'));
    const write = has(WRITE);
    if (write.length === WRITE.length) parts.push('create and change');
    else if (write.length) parts.push(write.join('/'));
    const remove = has(REMOVE);
    if (remove.length) parts.push('delete');
    const other = verbs.filter((v) => !READ.includes(v) && !WRITE.includes(v) && !REMOVE.includes(v));
    parts.push(...other);
    if (parts.length <= 1) return parts[0] ?? 'nothing on';
    return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`;
}

function describeGroups(groups: string[]): string {
    if (groups.includes('*')) return 'in every API group';
    const named = groups.map((g) => (g === '' ? 'core' : g));
    return named.length === 1 && named[0] === 'core' ? '' : `(${named.join(', ')})`;
}

/** One rule as a sentence: "Can read pods, services" or "Can do anything to anything". */
export function describeRule(rule: Rule): string {
    if (rule.nonResourceURLs.length) {
        return `Can ${describeVerbs(rule.verbs)} the URLs ${rule.nonResourceURLs.join(', ')}`;
    }
    const allVerbs = rule.verbs.includes('*');
    const allResources = rule.resources.includes('*');
    const allGroups = rule.apiGroups.includes('*');
    if (allVerbs && allResources && allGroups) return 'Can do anything to anything';

    const what = allResources ? 'every resource' : rule.resources.join(', ');
    const groups = describeGroups(rule.apiGroups);
    const names = rule.resourceNames.length ? ` -- only those named ${rule.resourceNames.join(', ')}` : '';
    return `Can ${describeVerbs(rule.verbs)} ${what}${groups ? ' ' + groups : ''}${names}`;
}

// ----- risk ------------------------------------------------------------------

export type RiskLevel = 'none' | 'low' | 'high' | 'critical';

export interface Risk {
    level: RiskLevel;
    reasons: string[];
}

const LEVELS: RiskLevel[] = ['none', 'low', 'high', 'critical'];

function worse(a: RiskLevel, b: RiskLevel): RiskLevel {
    return LEVELS.indexOf(a) >= LEVELS.indexOf(b) ? a : b;
}

function any(list: string[], wanted: string[]): boolean {
    return list.includes('*') || wanted.some((w) => list.includes(w));
}

/**
 * How much a set of rules hands over, and why.
 *
 * Not a security audit -- a quick reading for a person trying to see which of
 * forty roles deserve a closer look. "Critical" is what amounts to owning the
 * scope it applies in; "high" is what leaks credentials or lets somebody run
 * code; "low" is any other write. Reads are "none".
 */
export function assessRules(rules: Rule[]): Risk {
    let level: RiskLevel = 'none';
    const reasons = new Set<string>();
    const flag = (at: RiskLevel, why: string) => {
        level = worse(level, at);
        reasons.add(why);
    };

    for (const rule of rules) {
        if (rule.nonResourceURLs.length) continue;
        const v = rule.verbs;
        const r = rule.resources;
        const core = rule.apiGroups.includes('') || rule.apiGroups.includes('*');
        const rbac = rule.apiGroups.includes('rbac.authorization.k8s.io') || rule.apiGroups.includes('*');

        if (v.includes('*') && r.includes('*') && rule.apiGroups.includes('*')) {
            flag('critical', 'Full control of everything (the same as cluster-admin in its scope)');
            continue;
        }
        if (any(v, ['escalate', 'bind']) && rbac) flag('critical', 'Can grant itself more permissions (escalate/bind)');
        if (any(v, ['impersonate'])) flag('critical', 'Can act as other users or groups (impersonate)');
        if (rbac && any(v, WRITE) && any(r, ['roles', 'clusterroles', 'rolebindings', 'clusterrolebindings'])) {
            flag('high', 'Can change roles or bindings');
        }
        if (core && any(v, ['get', 'list', 'watch']) && any(r, ['secrets'])) {
            flag('high', 'Can read Secrets (passwords, tokens, keys)');
        }
        if (core && any(v, ['create', 'get']) && any(r, ['pods/exec', 'pods/attach'])) {
            flag('high', 'Can run commands inside containers');
        }
        if (core && any(v, ['create']) && any(r, ['pods'])) {
            flag('high', 'Can start pods, and so run anything as any service account in the namespace');
        }
        if (core && any(v, ['create']) && any(r, ['serviceaccounts/token'])) {
            flag('high', 'Can mint service account tokens');
        }
        if (core && any(v, ['get', 'create']) && any(r, ['nodes/proxy'])) {
            flag('high', 'Can reach the kubelet API on nodes');
        }
        if (v.includes('*')) flag('high', `Every verb on ${r.includes('*') ? 'every resource' : r.join(', ')}`);
        if (any(v, [...WRITE, ...REMOVE])) flag('low', 'Can change or delete objects');
    }
    return { level, reasons: [...reasons] };
}

// ----- the permission matrix -------------------------------------------------

/** One cell: allowed, allowed only for some named objects, or not at all. */
export type Cell = 'yes' | 'some' | '';

export interface MatrixRow {
    /** "pods", "pods/log", "*" or a URL. */
    resource: string;
    /** The API group, '' for core, '*' for all. Empty for a URL row. */
    group: string;
    url: boolean;
    cells: Record<string, Cell>;
    /** Verbs outside MATRIX_VERBS (escalate, bind, use...). */
    other: string[];
    /** Object names the "some" cells are limited to. */
    names: string[];
}

/**
 * Rules turned into a resource-by-verb table, one row per resource and group.
 *
 * Several rules often touch the same resource; a cell takes the most any of
 * them allows, so the table reads as "what can I do to pods" rather than as a
 * copy of the YAML.
 */
export function matrix(rules: Rule[]): MatrixRow[] {
    const rows = new Map<string, MatrixRow>();
    const row = (resource: string, group: string, url: boolean): MatrixRow => {
        const key = `${url ? 'url' : 'res'}|${group}|${resource}`;
        let found = rows.get(key);
        if (!found) {
            found = { resource, group, url, cells: {}, other: [], names: [] };
            rows.set(key, found);
        }
        return found;
    };

    for (const rule of rules) {
        const limited = rule.resourceNames.length > 0;
        const targets: [string, string, boolean][] = rule.nonResourceURLs.length
            ? rule.nonResourceURLs.map((u) => [u, '', true])
            : rule.apiGroups.flatMap((g) => rule.resources.map((r): [string, string, boolean] => [r, g, false]));

        for (const [resource, group, url] of targets) {
            const target = row(resource, group, url);
            const verbs = rule.verbs.includes('*') ? [...MATRIX_VERBS, '*'] : rule.verbs;
            for (const verb of verbs) {
                if (MATRIX_VERBS.includes(verb)) {
                    const was = target.cells[verb] ?? '';
                    target.cells[verb] = was === 'yes' || !limited ? 'yes' : 'some';
                } else if (!target.other.includes(verb)) {
                    target.other.push(verb);
                }
            }
            if (limited) {
                for (const n of rule.resourceNames) if (!target.names.includes(n)) target.names.push(n);
            }
        }
    }

    return [...rows.values()].sort((a, b) => {
        if (a.url !== b.url) return a.url ? 1 : -1;
        // Wildcards first: they say the most.
        if ((a.resource === '*') !== (b.resource === '*')) return a.resource === '*' ? -1 : 1;
        if (a.group !== b.group) return a.group < b.group ? -1 : 1;
        return a.resource < b.resource ? -1 : 1;
    });
}

// ----- lookups ---------------------------------------------------------------

export interface Index {
    roles: Map<string, Role>;
    /** Bindings by the key of the role they name. */
    bindingsByRole: Map<string, Binding[]>;
    /** Bindings by the key of each subject they name. */
    bindingsBySubject: Map<string, Binding[]>;
    /** Group name -> bindings naming that group. */
    bindingsByGroup: Map<string, Binding[]>;
    accounts: Set<string>;
}

export function index(access: Access): Index {
    const roles = new Map(access.roles.map((r) => [roleKey(r), r]));
    const bindingsByRole = new Map<string, Binding[]>();
    const bindingsBySubject = new Map<string, Binding[]>();
    const bindingsByGroup = new Map<string, Binding[]>();
    const push = <K>(m: Map<K, Binding[]>, k: K, b: Binding) => {
        const list = m.get(k);
        if (list) {
            if (!list.includes(b)) list.push(b);
        } else m.set(k, [b]);
    };
    for (const b of access.bindings) {
        push(bindingsByRole, roleKeyOf(b), b);
        for (const s of b.subjects) {
            push(bindingsBySubject, subjectKey(s), b);
            if (s.kind === 'Group') push(bindingsByGroup, s.name, b);
        }
    }
    const accounts = new Set(access.serviceAccounts.map((a) => `${a.namespace}/${a.name}`));
    return { roles, bindingsByRole, bindingsBySubject, bindingsByGroup, accounts };
}

/** Where a binding grants: "everywhere" or one namespace. */
export function scopeOf(binding: Binding): string {
    return binding.kind === 'ClusterRoleBinding' ? '' : binding.namespace;
}

export function describeScope(binding: Binding): string {
    return binding.kind === 'ClusterRoleBinding' ? 'in every namespace, and cluster-wide' : `in namespace ${binding.namespace}`;
}

/** One route by which a subject holds a role. */
export interface Grant {
    binding: Binding;
    role: Role | null;
    /** '' for cluster-wide, else the one namespace it applies in. */
    scope: string;
    /** The group the grant reaches the subject through, if not directly. */
    viaGroup: string;
}

/**
 * Every route by which a subject holds a role: its own bindings, and the
 * bindings of the groups it is in by construction (see impliedGroups).
 */
export function grantsFor(access: Access, idx: Index, subject: Subject): Grant[] {
    const out: Grant[] = [];
    const seen = new Set<string>();
    const add = (b: Binding, viaGroup: string) => {
        const key = bindingKey(b);
        if (seen.has(key)) return;
        seen.add(key);
        out.push({ binding: b, role: idx.roles.get(roleKeyOf(b)) ?? null, scope: scopeOf(b), viaGroup });
    };
    for (const b of idx.bindingsBySubject.get(subjectKey(subject)) ?? []) add(b, '');
    for (const g of impliedGroups(subject, access.me)) {
        for (const b of idx.bindingsByGroup.get(g) ?? []) add(b, g);
    }
    return out.sort((a, b) => (a.scope === b.scope ? 0 : a.scope === '' ? -1 : b.scope === '' ? 1 : a.scope < b.scope ? -1 : 1));
}

// ----- who can ----------------------------------------------------------------

export interface Question {
    verb: string;
    /** "pods", "deployments.apps", "pods/exec", "httproutes.gateway.networking.k8s.io". */
    resource: string;
    /** '' for any namespace. */
    namespace: string;
}

export interface ParsedResource {
    resource: string;
    subresource: string;
    group: string;
}

/** Reads kubectl's `resource[.group][/subresource]` form. */
export function parseResource(text: string): ParsedResource {
    const trimmed = text.trim().toLowerCase();
    const slash = trimmed.indexOf('/');
    const head = slash < 0 ? trimmed : trimmed.slice(0, slash);
    const subresource = slash < 0 ? '' : trimmed.slice(slash + 1);
    const dot = head.indexOf('.');
    return dot < 0
        ? { resource: head, subresource, group: '' }
        : { resource: head.slice(0, dot), subresource, group: head.slice(dot + 1) };
}

/** Whether a rule allows a verb on a resource -- the authorizer's own matching. */
export function ruleAllows(rule: Rule, verb: string, target: ParsedResource): boolean {
    if (rule.nonResourceURLs.length) return false;
    if (!rule.verbs.includes('*') && !rule.verbs.includes(verb)) return false;
    if (!rule.apiGroups.includes('*') && !rule.apiGroups.includes(target.group)) return false;

    const combined = target.subresource ? `${target.resource}/${target.subresource}` : target.resource;
    return rule.resources.some(
        (r) => r === '*' || r === combined || (target.subresource !== '' && r === `*/${target.subresource}`),
    );
}

export interface Answer {
    subject: Subject;
    /** The routes by which it may, each with the rule that matched. */
    routes: { binding: Binding; role: Role; rule: Rule; scope: string }[];
    /** True when every matching rule is limited to named objects. */
    onlyNamed: boolean;
}

/**
 * Who may do something, and by which binding and role.
 *
 * A question about one namespace counts the cluster-wide bindings and that
 * namespace's own; a question about any namespace counts every binding, and
 * each route says where it applies.
 */
export function whoCan(access: Access, idx: Index, q: Question): Answer[] {
    const target = parseResource(q.resource);
    if (!target.resource || !q.verb) return [];

    const answers = new Map<string, Answer>();
    for (const binding of access.bindings) {
        if (binding.kind === 'RoleBinding' && q.namespace && binding.namespace !== q.namespace) continue;
        const role = idx.roles.get(roleKeyOf(binding));
        if (!role) continue;
        const matching = role.rules.filter((r) => ruleAllows(r, q.verb, target));
        if (!matching.length) continue;

        for (const subject of binding.subjects) {
            const key = subjectKey(subject);
            let answer = answers.get(key);
            if (!answer) {
                answer = { subject, routes: [], onlyNamed: true };
                answers.set(key, answer);
            }
            for (const rule of matching) {
                answer.routes.push({ binding, role, rule, scope: scopeOf(binding) });
                if (!rule.resourceNames.length) answer.onlyNamed = false;
            }
        }
    }

    const order = (s: Subject) => ['Group', 'User', 'ServiceAccount'].indexOf(s.kind);
    return [...answers.values()].sort(
        (a, b) =>
            order(a.subject) - order(b.subject) ||
            a.subject.namespace.localeCompare(b.subject.namespace) ||
            a.subject.name.localeCompare(b.subject.name),
    );
}

/** Questions people actually ask, for one-click answers. */
export const COMMON_QUESTIONS: { label: string; verb: string; resource: string }[] = [
    { label: 'read Secrets', verb: 'get', resource: 'secrets' },
    { label: 'exec into pods', verb: 'create', resource: 'pods/exec' },
    { label: 'delete pods', verb: 'delete', resource: 'pods' },
    { label: 'create deployments', verb: 'create', resource: 'deployments.apps' },
    { label: 'change role bindings', verb: 'create', resource: 'rolebindings.rbac.authorization.k8s.io' },
    { label: 'impersonate users', verb: 'impersonate', resource: 'users' },
    { label: 'drain nodes', verb: 'create', resource: 'pods/eviction' },
];

/** Resources worth offering in the question's picker: the common ones and every one a rule names. */
export function knownResources(access: Access): string[] {
    const out = new Set<string>([
        'pods',
        'pods/exec',
        'pods/log',
        'pods/portforward',
        'pods/eviction',
        'secrets',
        'configmaps',
        'services',
        'serviceaccounts',
        'serviceaccounts/token',
        'namespaces',
        'nodes',
        'persistentvolumeclaims',
        'events',
        'deployments.apps',
        'statefulsets.apps',
        'daemonsets.apps',
        'jobs.batch',
        'cronjobs.batch',
        'ingresses.networking.k8s.io',
        'roles.rbac.authorization.k8s.io',
        'rolebindings.rbac.authorization.k8s.io',
        'clusterroles.rbac.authorization.k8s.io',
        'clusterrolebindings.rbac.authorization.k8s.io',
    ]);
    for (const role of access.roles) {
        for (const rule of role.rules) {
            for (const g of rule.apiGroups) {
                if (g === '*') continue;
                for (const r of rule.resources) {
                    if (r === '*' || r.startsWith('*/')) continue;
                    const [head, sub] = r.split('/');
                    out.add(`${head}${g ? '.' + g : ''}${sub ? '/' + sub : ''}`);
                }
            }
        }
    }
    return [...out].sort();
}

// ----- the graph --------------------------------------------------------------

export type Column = 'subject' | 'binding' | 'role';

export interface GraphNode {
    id: string;
    column: Column;
    /** The Kubernetes kind: User, Group, ServiceAccount, RoleBinding... */
    kind: string;
    name: string;
    namespace: string;
    system: boolean;
    me: boolean;
    risk: RiskLevel;
    /** A binding naming a role, or a service account, that does not exist. */
    missing: boolean;
    /** Matched the search, when there is one. */
    match: boolean;
}

export interface GraphEdge {
    from: string;
    to: string;
}

export interface Graph {
    subjects: GraphNode[];
    bindings: GraphNode[];
    roles: GraphNode[];
    edges: GraphEdge[];
}

export interface GraphFilter {
    /** '' for every namespace. */
    namespace: string;
    query: string;
    showSystem: boolean;
    /** Roles no binding names -- defined, but granting nothing to anyone. */
    showUnbound: boolean;
    /** Only the chains that reach the caller. */
    onlyMe: boolean;
    /** Leave out the cluster-wide bindings when a namespace is chosen. */
    hideClusterWide: boolean;
    /** Only the bindings whose role is high or critical risk. */
    riskyOnly: boolean;
}

export const DEFAULT_FILTER: GraphFilter = {
    namespace: '',
    query: '',
    showSystem: false,
    showUnbound: false,
    onlyMe: false,
    hideClusterWide: false,
    riskyOnly: false,
};

/**
 * The three-column graph: subjects, the bindings that name them, the roles
 * those bindings grant.
 *
 * Layered rather than force-directed because RBAC's shape is always exactly
 * this, subject -> binding -> role, and a layout that keeps it lets the eye
 * read across a row instead of untangling a hairball. The ordering is a couple
 * of barycentre passes, which is enough to keep most edges short.
 */
export function buildGraph(access: Access, idx: Index, filter: GraphFilter): Graph {
    const query = filter.query.trim().toLowerCase();
    const hit = (s: string) => query !== '' && s.toLowerCase().includes(query);

    const risks = new Map<string, RiskLevel>();
    const riskOf = (key: string): RiskLevel => {
        let level = risks.get(key);
        if (level === undefined) {
            const role = idx.roles.get(key);
            level = role ? assessRules(role.rules).level : 'none';
            risks.set(key, level);
        }
        return level;
    };

    const bindings = access.bindings.filter((b) => {
        if (!filter.showSystem && isSystem(b)) return false;
        if (filter.riskyOnly && LEVELS.indexOf(riskOf(roleKeyOf(b))) < LEVELS.indexOf('high')) return false;
        if (filter.namespace) {
            if (b.kind === 'RoleBinding' && b.namespace !== filter.namespace) return false;
            if (b.kind === 'ClusterRoleBinding' && filter.hideClusterWide) return false;
        }
        if (filter.onlyMe && !b.subjects.some((s) => isMe(s, access.me))) return false;
        if (query && !hit(b.name) && !hit(b.roleName) && !b.subjects.some((s) => hit(s.name))) return false;
        return true;
    });

    const subjects = new Map<string, GraphNode>();
    const roles = new Map<string, GraphNode>();
    const bindingNodes: GraphNode[] = [];
    const edges: GraphEdge[] = [];
    const drawn = new Set<string>();

    const roleNode = (key: string, kind: string, namespace: string, name: string): GraphNode => {
        let node = roles.get(key);
        if (!node) {
            const role = idx.roles.get(key);
            node = {
                id: 'r:' + key,
                column: 'role',
                kind,
                name,
                namespace: kind === 'ClusterRole' ? '' : namespace,
                system: role ? isSystem(role) : false,
                me: false,
                risk: riskOf(key),
                missing: !role,
                match: hit(name),
            };
            roles.set(key, node);
        }
        return node;
    };

    for (const b of bindings) {
        const id = 'b:' + bindingKey(b);
        const rKey = roleKeyOf(b);
        const role = roleNode(rKey, b.roleKind, b.namespace, b.roleName);
        const node: GraphNode = {
            id,
            column: 'binding',
            kind: b.kind,
            name: b.name,
            namespace: b.namespace,
            system: isSystem(b),
            me: false,
            risk: role.risk,
            missing: role.missing,
            match: hit(b.name),
        };
        bindingNodes.push(node);
        edges.push({ from: id, to: role.id });

        for (const s of b.subjects) {
            const sKey = subjectKey(s);
            let sNode = subjects.get(sKey);
            if (!sNode) {
                const me = isMe(s, access.me);
                sNode = {
                    id: 's:' + sKey,
                    column: 'subject',
                    kind: s.kind,
                    name: s.name,
                    namespace: s.namespace,
                    system: s.name.startsWith('system:'),
                    me,
                    risk: 'none',
                    missing:
                        s.kind === 'ServiceAccount' &&
                        access.serviceAccounts.length > 0 &&
                        !idx.accounts.has(`${s.namespace}/${s.name}`),
                    match: hit(s.name),
                };
                subjects.set(sKey, sNode);
            }
            if (sNode.risk !== role.risk) sNode.risk = worse(sNode.risk, role.risk);
            if (sNode.me) node.me = role.me = true;
            // A binding can name the same subject twice; the map draws one edge.
            const edge = `${sNode.id}>${id}`;
            if (!drawn.has(edge)) {
                drawn.add(edge);
                edges.push({ from: sNode.id, to: id });
            }
        }
    }

    if (filter.showUnbound && !filter.onlyMe && !filter.riskyOnly) {
        for (const role of access.roles) {
            const key = roleKey(role);
            if (roles.has(key) || idx.bindingsByRole.has(key)) continue;
            if (!filter.showSystem && isSystem(role)) continue;
            if (filter.namespace && role.kind === 'Role' && role.namespace !== filter.namespace) continue;
            if (query && !hit(role.name)) continue;
            roleNode(key, role.kind, role.namespace, role.name);
        }
    }

    return order({ subjects: [...subjects.values()], bindings: bindingNodes, roles: [...roles.values()], edges });
}

/** Reorders each column towards the average position of its neighbours. */
function order(graph: Graph): Graph {
    const pos = new Map<string, number>();
    const place = (nodes: GraphNode[]) => nodes.forEach((n, i) => pos.set(n.id, i));
    const neighbours = new Map<string, string[]>();
    for (const e of graph.edges) {
        (neighbours.get(e.from) ?? neighbours.set(e.from, []).get(e.from)!).push(e.to);
        (neighbours.get(e.to) ?? neighbours.set(e.to, []).get(e.to)!).push(e.from);
    }
    const centre = (n: GraphNode, fallback: number) => {
        const ps = (neighbours.get(n.id) ?? []).map((id) => pos.get(id)).filter((p): p is number => p !== undefined);
        return ps.length ? ps.reduce((a, b) => a + b, 0) / ps.length : fallback;
    };
    const sortBy = (nodes: GraphNode[]) => {
        const keyed = nodes.map((n, i) => ({ n, k: centre(n, i) }));
        keyed.sort((a, b) => a.k - b.k);
        return keyed.map((x) => x.n);
    };

    // Seeded by risk, then name, so that where the barycentre passes have no
    // opinion the dangerous roles gather at the top.
    let roles = [...graph.roles].sort(
        (a, b) => LEVELS.indexOf(b.risk) - LEVELS.indexOf(a.risk) || a.name.localeCompare(b.name),
    );
    place(roles);
    let bindings = sortBy(graph.bindings);
    place(bindings);
    let subjects = sortBy(graph.subjects);
    place(subjects);
    bindings = sortBy(bindings);
    place(bindings);
    roles = sortBy(roles);
    place(roles);
    subjects = sortBy(subjects);

    return { ...graph, subjects, bindings, roles };
}

/** Every node reachable from one, across the three columns, in both directions. */
export function connected(graph: Graph, id: string): Set<string> {
    const out = new Set<string>([id]);
    const forward = new Map<string, string[]>();
    const backward = new Map<string, string[]>();
    for (const e of graph.edges) {
        (forward.get(e.from) ?? forward.set(e.from, []).get(e.from)!).push(e.to);
        (backward.get(e.to) ?? backward.set(e.to, []).get(e.to)!).push(e.from);
    }
    // Follow edges one way only from the start, so selecting a role lights its
    // bindings and their subjects -- not every other role those subjects hold.
    const walk = (from: string, edges: Map<string, string[]>) => {
        const stack = [from];
        while (stack.length) {
            const next = stack.pop()!;
            for (const n of edges.get(next) ?? []) {
                if (!out.has(n)) {
                    out.add(n);
                    stack.push(n);
                }
            }
        }
    };
    walk(id, forward);
    walk(id, backward);
    return out;
}
