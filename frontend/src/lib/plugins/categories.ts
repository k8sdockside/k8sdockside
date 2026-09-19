// What a plugin is about, and how the settings view finds one.
//
// The categories themselves are the Go side's (internal/plugins/categories.go)
// and arrive on every plugin and every known offer; this file is what they are
// called in the interface, and the searching, filtering and ordering built on
// them. It is plain functions over plain objects rather than component state,
// because the list is drawn four times over -- available, built in, installed,
// watched -- and all four have to narrow the same way.
//
// The search deliberately matches more than the name: someone who remembers
// "the one for volumes" should find Longhorn by typing "volumes", and that
// word is in its description, not its name.

/** One category, as the interface words it. */
export interface Category {
    id: string;
    label: string;
    /** An icon from Icon.svelte's set. */
    icon: string;
    /** What belongs here, for the filter's tooltip. */
    note: string;
}

/**
 * Every category, in the order the filter offers them: the ones about a thing
 * in the cluster first, then the ones about how it is run, then Other.
 *
 * Kept in step with Categories in internal/plugins/categories.go by a test.
 */
export const CATEGORIES: readonly Category[] = [
    { id: 'storage', label: 'Storage', icon: 'database', note: 'Volumes, disks and the data on them' },
    // The datapath is its own category: a cluster has exactly one CNI, so
    // these are alternatives to each other, while what is under Networking
    // sits on top of whichever one was chosen.
    { id: 'cni', label: 'CNI', icon: 'share', note: 'The pod network itself: the CNI, its addresses and its policy' },
    { id: 'networking', label: 'Networking', icon: 'gateway', note: 'What sits on the pod network: addresses, load balancing, ingress and DNS' },
    { id: 'security', label: 'Security', icon: 'shield', note: 'Certificates, secrets, policy and who may do what' },
    { id: 'images', label: 'Images', icon: 'box', note: 'Container images and the registries they come from' },
    { id: 'observability', label: 'Observability', icon: 'gauge', note: 'Metrics, logs and what is actually running' },
    { id: 'delivery', label: 'Delivery', icon: 'rocket', note: 'GitOps and how workloads get into the cluster' },
    { id: 'virtualization', label: 'Virtualization', icon: 'monitor', note: 'Virtual machines alongside containers' },
    { id: 'cost', label: 'Cost & efficiency', icon: 'scale', note: 'What the cluster costs and what it wastes' },
    { id: 'platform', label: 'Platform', icon: 'layers', note: 'Clusters, machines and the platform itself' },
    { id: 'other', label: 'Other', icon: 'puzzle', note: 'Everything the categories above do not describe' },
];

const BY_ID = new Map(CATEGORIES.map((category) => [category.id, category]));

/** The category with an id, or Other -- which is what an absent one means too. */
export function categoryOf(id: string | undefined): Category {
    return BY_ID.get(id ?? '') ?? BY_ID.get('other')!;
}

/** Enough of a plugin -- installed or merely known -- to search and sort it. */
export interface Listed {
    id: string;
    name: string;
    tagline: string;
    category?: string;
    description: string;
    author?: string;
}

export type PluginSort = 'name' | 'category' | 'author';

export const PLUGIN_SORTS: { id: PluginSort; label: string }[] = [
    { id: 'name', label: 'Name' },
    { id: 'category', label: 'Category' },
    { id: 'author', label: 'Author' },
];

export interface PluginQuery {
    text: string;
    /** A category id, or '' for all of them. */
    category: string;
    sort: PluginSort;
}

export const ANY_PLUGIN: PluginQuery = { text: '', category: '', sort: 'name' };

/** Whether a query narrows anything at all. */
export function narrowed(query: PluginQuery): boolean {
    return query.text.trim() !== '' || query.category !== '';
}

/** Everything a plugin can be found by, lowercased once. */
function haystack(plugin: Listed): string {
    return [
        plugin.name,
        plugin.id,
        plugin.tagline,
        plugin.description,
        plugin.author ?? '',
        plugin.category ?? '',
        categoryOf(plugin.category).label,
    ]
        .join(' ')
        .toLowerCase();
}

export function matches(plugin: Listed, query: PluginQuery): boolean {
    if (query.category && (plugin.category || 'other') !== query.category) return false;
    const words = query.text.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (words.length === 0) return true;
    const hay = haystack(plugin);
    // Every word has to match something, so a second word narrows rather than
    // widens -- which is what everyone expects of a search box and almost no
    // search box does.
    return words.every((word) => hay.includes(word));
}

/** Where a category comes in the filter's order; an unknown one goes last. */
function categoryRank(id: string | undefined): number {
    const at = CATEGORIES.findIndex((category) => category.id === (id || 'other'));
    return at < 0 ? CATEGORIES.length : at;
}

export function arrange<T extends Listed>(plugins: T[], query: PluginQuery): T[] {
    const byName = (a: Listed, b: Listed) => a.name.localeCompare(b.name);
    const kept = plugins.filter((plugin) => matches(plugin, query));
    switch (query.sort) {
        case 'category':
            return kept.sort((a, b) => categoryRank(a.category) - categoryRank(b.category) || byName(a, b));
        case 'author':
            return kept.sort((a, b) => (a.author ?? '').localeCompare(b.author ?? '') || byName(a, b));
        default:
            return kept.sort(byName);
    }
}

/**
 * How many of these plugins are in each category, for the filter. Categories
 * nothing is in are left out: offering a filter that empties the page is worse
 * than not offering it.
 */
export function categoryCounts(plugins: Listed[]): { category: Category; count: number }[] {
    const tally = new Map<string, number>();
    for (const plugin of plugins) {
        const id = plugin.category || 'other';
        tally.set(id, (tally.get(id) ?? 0) + 1);
    }
    return CATEGORIES.filter((category) => (tally.get(category.id) ?? 0) > 0).map((category) => ({
        category,
        count: tally.get(category.id) ?? 0,
    }));
}
