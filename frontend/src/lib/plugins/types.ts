// What a solution plugin is, as the app works with it.
//
// A plugin teaches k8sdockside about something installed in a cluster -- Argo
// CD, Flux, Prometheus -- and gives it a place of its own in the sidebar. Like
// a theme it is a JSON file and nothing else: it names kinds the app already
// knows how to list and says how to arrange and summarise them, so installing
// someone else's is as safe as installing their theme.
//
// The one distinction worth holding on to is that a plugin is installed on
// *this machine*, while the solution it describes is installed in a *cluster*.
// Those come apart constantly, and everything below that talks about "detected"
// or "installed" is about the second.

/** One entry under a plugin in the sidebar, and one tab when opened. */
export interface PluginViewSpec {
    id: string;
    label: string;
    icon: string;
    /** `table`, or `custom` for one of the plugin's own pages. */
    type: string;
    /** The kind this view lists: a built-in name, or a `crd:` custom resource. Empty on a custom view. */
    kind: string;
    /** Fixed for this view; the tab's own namespace filter is not offered. */
    namespace: string;
    selector: string;
    /**
     * The file a custom view opens, relative to the plugin's UI folder.
     * Optional only so hand-built fixtures need not spell it out; adopt always
     * sets it.
     */
    entry?: string;
    /**
     * Custom views only: the kind the page can be opened on, and what goes
     * after the # in its address to say which object -- `{namespace}` and
     * `{name}` filled in. Null (or absent) for a view that cannot be.
     */
    focus?: { kind: string; hash: string } | null;
}

/**
 * What a plugin's own views may do. Present only for a plugin that ships some;
 * see PluginFrame for how it is enforced.
 */
export interface PluginUI {
    /** Every kind the views may read -- worked out by the Go loader. Never `secrets`. */
    readable: string[];
    /** Whether the views may ask to patch those kinds. Each patch is confirmed by the user. */
    write: boolean;
    /** Whether the views may ask registries about the images the cluster runs. */
    registries: boolean;
    /** The in-cluster Services the views may make GET requests to. */
    services: PluginServiceAccess[];
}

/**
 * One Service a plugin's views may call, as the settings card and the frame
 * need it. Where requests go and which paths are allowed are checked in Go.
 */
export interface PluginServiceAccess {
    id: string;
    label: string;
    /** Where it is, as `namespace/name:port` or by its selector. */
    where: string;
    paths: string[];
}

/**
 * A button a plugin puts on the action bar of objects of a kind. Only what the
 * app needs to know before asking the backend: the request it makes is read
 * from the manifest in Go, never sent from here.
 */
export interface PluginActionSpec {
    id: string;
    label: string;
    kind: string;
    /** The request's type -- `patch`, `subresource`, `create` or `delete` -- so a delete can be asked about as one. */
    type?: string;
    /** `danger` or empty. */
    tone?: string;
    /** The manifest's question, with `{name}` and `{namespace}` not yet filled in. */
    confirm?: string;
}

/** One of a plugin's own panels in the detail view of objects of a kind. */
export interface PluginSectionSpec {
    id: string;
    label: string;
    kind: string;
    /** The file it opens, relative to the plugin's ui folder. */
    entry: string;
    /** Pixels, until the page measures itself. */
    height: number;
}

/** A plugin action offered on one object right now, as the backend words it. */
export interface OfferedAction {
    pluginId: string;
    pluginName: string;
    id: string;
    label: string;
    icon: string;
    tone: string;
    /** The question to ask first, with the object's name in it. Empty runs on click. */
    confirm: string;
    /** The notice once it has worked. */
    done: string;
    /** The request's type: `patch`, `subresource`, `create` or `delete`. */
    type?: string;
}

/** One place a plugin points its reader at: the product's site, its source. */
export interface PluginLink {
    label: string;
    url: string;
}

/** A kind the plugin needs the cluster to serve. */
export interface PluginRequirement {
    kind: string;
    label: string;
    optional: boolean;
    /**
     * With a selector, the requirement is for the objects rather than the
     * kind: the cluster has to hold something matching it. Only the backend
     * can answer that -- see workspace.probeCluster -- and only a plugin whose
     * product defines no custom resources needs it.
     */
    namespace?: string;
    selector?: string;
}

export interface Plugin {
    id: string;
    name: string;
    tagline: string;
    /**
     * What the plugin is about, in one word: `storage`, `networking`, ... The
     * Go loader fills in `other` for a manifest that names none, so what the
     * app receives is always one of CATEGORIES -- see
     * lib/plugins/categories.ts. Optional here only so hand-built fixtures
     * need not spell it out; absent reads as `other`.
     */
    category?: string;
    icon: string;
    /**
     * The plugin's own mark, a file in its ui folder, served at
     * `/plugin-ui/<id>/<logo>`. Empty for a plugin that ships none, which
     * falls back to a bundled mark and then to `icon`; see PluginMark.
     */
    logo?: string;
    author: string;
    /** Where to find the author, http(s) only. Optional so fixtures need not spell it out. */
    authorUrl?: string;
    /**
     * Installed from the repository of an official entry on the known list,
     * as the loader checked it -- a manifest cannot say so about itself.
     */
    official?: boolean;
    docs: string;
    /** What the plugin is about, http(s) only. Optional so fixtures need not spell it out. */
    links?: PluginLink[];
    /** The plugin's own version, empty if it does not say. */
    version?: string;
    /** The oldest release of the app it works with, empty if it does not say. */
    minAppVersion?: string;
    description: string;
    requires: PluginRequirement[];
    views: PluginViewSpec[];
    /** Null (or absent) unless the plugin ships views of its own. */
    ui?: PluginUI | null;
    /** Buttons on objects' action bars. Optional so fixtures need not spell it out. */
    actions?: PluginActionSpec[];
    /** Panels in objects' detail views. Optional so fixtures need not spell it out. */
    sections?: PluginSectionSpec[];
    /**
     * A landing page of the plugin's own, opened in place of the generated
     * overview. Null (or absent) for the generated one.
     */
    overview?: { entry: string } | null;
    /** `builtin`, or the path of the file it was read from. */
    origin: string;
    /** The collection it arrived in, empty for one that came on its own. */
    pack: string;
    /** The git checkout it was read from, empty unless it was installed from a repository. */
    repo?: string;
    /**
     * Switched off in Settings. A disabled plugin is still in the catalogue --
     * that is where it gets switched back on -- but nothing offers it: no
     * sidebar rows, no charts, no overview. See `workspace.enabledPlugins`.
     */
    disabled: boolean;
}

/**
 * A plugin kept in a repository of its own that the app knows of, offered in
 * Settings with one button and suggested in the sidebar for a cluster running
 * what it is about.
 */
export interface KnownPlugin {
    id: string;
    name: string;
    tagline: string;
    /** What it is about, as on an installed plugin. Absent reads as `other`. */
    category?: string;
    icon: string;
    description: string;
    /** What installing it clones. */
    repo: string;
    /** Kinds whose presence in a cluster gives the product away. Empty: never suggested. */
    detect: string[];
    /**
     * Whether the app can recognise this one by what it runs, for a product
     * that defines no custom resources -- see PluginService.Probe. Only used
     * to know that an answer is coming, so a row can wait for it instead of
     * guessing.
     */
    probed?: boolean;
    links: PluginLink[];
    /** Who wrote it, and where to find them. Optional so fixtures need not spell it out. */
    author?: string;
    authorUrl?: string;
    official: boolean;
    /** A plugin with this id is already in the catalogue. */
    installed: boolean;
}

/** Everything installed on this machine, and what would not load. */
export interface PluginCatalogue {
    plugins: Plugin[];
    dir: string;
    folders: string[];
    problems: { path: string; message: string }[];
}

/** One of a plugin's requirements, checked against a cluster. */
export interface Presence {
    kind: string;
    label: string;
    optional: boolean;
    /** What it looked for, when the requirement asks for objects. */
    selector?: string;
    served: boolean;
    /** Set when we could not find out, as opposed to finding out it is absent. */
    error: string;
}

/** One slice of a card: a value the grouped field took, and how many had it. */
export interface Bucket {
    /** Empty means the field was absent on those objects. */
    value: string;
    count: number;
    tone: string;
}

/** One live tile on the overview. */
export interface CardResult {
    label: string;
    kind: string;
    total: number;
    buckets: Bucket[];
    /** Whether this card divides its count at all. */
    grouped: boolean;
    /** Why it has no number, if it has none. */
    error: string;
}

/** A plugin's overview for one context. */
export interface PluginSummary {
    pluginId: string;
    /** Every required kind is served by this cluster. */
    installed: boolean;
    /** Whether we managed to ask; false leaves `installed` meaningless. */
    checked: boolean;
    requirements: Presence[];
    cards: CardResult[];
    error: string;
}

/** What a `plugin:` tab kind actually means. */
export interface ResolvedView {
    kind: string;
    namespace: string;
    selector: string;
    pluginId: string;
    pluginName: string;
    viewId: string;
    label: string;
    icon: string;
    overview: boolean;
    custom: boolean;
    entry: string;
}

/** The catalogue before anything has loaded. */
export function emptyPluginCatalogue(): PluginCatalogue {
    return { plugins: [], dir: '', folders: [], problems: [] };
}
