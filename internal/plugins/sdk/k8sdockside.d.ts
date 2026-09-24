/*
 * k8sdockside.d.ts -- types for the K8s Dockside plugin bridge.
 *
 * Describes the bridge of K8s Dockside >= 0.0.15 (protocol
 * "k8sdockside/plugin@1"): the `window.k8sdockside` object a plugin's own
 * pages get from
 *
 *     <script src="/plugin-ui/_sdk/k8sdockside.js"></script>
 *
 * The copy kept in step with the bridge is the one beside it in the K8s
 * Dockside repository, internal/plugins/sdk/k8sdockside.d.ts. It is
 * self-contained on purpose -- no imports, no exports, only global
 * declarations -- so the file can be copied as it is into any plugin written
 * in TypeScript. Put it where your tsconfig's "include" sees it (or
 * `/// <reference path="k8sdockside.d.ts" />` it) and both `k8sdockside` and
 * `window.k8sdockside` are typed everywhere.
 *
 * How the bridge works, in short: every call posts a message to the app, which
 * answers it -- or rejects the promise with an Error carrying a sentence --
 * from outside the page's sandboxed frame. The app answers only for kinds the
 * plugin declares (its `requires`, `views`, `cards`, `sections`, `actions`
 * and `ui.kinds`) and never for Secrets. Everything that comes back has been
 * through JSON: plain objects, arrays, strings, numbers, booleans and null.
 * A patch or an action is shown to the user in a dialog the page cannot
 * reach, and happens only if they say yes.
 *
 * The page itself has no network: fetch, XHR and websockets are refused by
 * its Content-Security-Policy, and it must be a classic script, not
 * `type="module"`.
 */

declare namespace K8sDockside {
    // ----- naming things ---------------------------------------------------

    /**
     * A kind the app can open: a built-in name -- `pods`, `deployments`,
     * `statefulsets`, `daemonsets`, `replicasets`, `jobs`, `cronjobs`,
     * `services`, `ingresses`, `configmaps`, `namespaces`, `nodes`, `events`,
     * ... -- or `crd:<plural>.<group>` for a custom resource, e.g.
     * `crd:applications.argoproj.io`. It must be one the plugin declares.
     * `secrets` is never readable, whatever is declared.
     */
    type Kind = string;

    /** One object. `namespace` is `''` (or left out) for a cluster-scoped kind. */
    interface ObjectRef {
        kind: Kind;
        namespace?: string;
        name: string;
    }

    /** An object, or -- with no `name` -- a kind's own tab. */
    interface OpenRef {
        kind: Kind;
        namespace?: string;
        name?: string;
    }

    /** What `list` reads. */
    interface ListQuery {
        kind: Kind;
        /** One namespace; `''` or left out for every namespace. */
        namespace?: string;
        /** A label selector in the usual `a=b,c in (d,e),!f` syntax. */
        selector?: string;
    }

    /** What `watch` polls: a list query, and how often. */
    interface WatchQuery extends ListQuery {
        /** Milliseconds between polls. Default 5000; anything under 1000 is raised to 1000. */
        interval?: number;
    }

    // ----- what comes back from the cluster ---------------------------------

    interface OwnerReference {
        apiVersion: string;
        kind: string;
        name: string;
        uid: string;
        controller?: boolean;
        blockOwnerDeletion?: boolean;
    }

    interface ObjectMeta {
        name: string;
        namespace?: string;
        uid?: string;
        resourceVersion?: string;
        generation?: number;
        creationTimestamp?: string;
        deletionTimestamp?: string;
        labels?: Record<string, string>;
        /**
         * The object's annotations -- less `kubectl.kubernetes.io/last-applied-configuration`,
         * which the app strips, as it does `managedFields`.
         */
        annotations?: Record<string, string>;
        ownerReferences?: OwnerReference[];
        [field: string]: unknown;
    }

    /**
     * A Kubernetes object, whole -- `spec`, `status` and all -- as the API
     * server returned it, less `metadata.managedFields` and the last-applied
     * annotation. Describe the fields you read by extending it:
     *
     *     interface Pod extends K8sDockside.KubeObject { spec?: PodSpec; status?: PodStatus }
     *     const pods = await k8sdockside.list<Pod>({ kind: 'pods' });
     */
    interface KubeObject {
        apiVersion?: string;
        kind?: string;
        metadata: ObjectMeta;
        spec?: unknown;
        status?: unknown;
        [field: string]: unknown;
    }

    // ----- what the page is looking at --------------------------------------

    /** The object a section is drawn for. */
    interface SectionObject {
        kind: Kind;
        /** `''` for a cluster-scoped object. */
        namespace: string;
        name: string;
    }

    /**
     * The app's theme: every colour token the app has on its root, and whether
     * it is a light or a dark one. The SDK has already written the tokens onto
     * the page's `:root` as custom properties (`--bg`, `--bg-panel`,
     * `--bg-raised`, `--bg-hover`, `--border`, `--border-soft`, `--text`,
     * `--text-dim`, `--text-faint`, `--accent`, `--accent-text`, `--ok`,
     * `--warn`, `--error`, `--chart-1` ... `--chart-8`, `--chart-grid`, ...)
     * and set `color-scheme` and `data-theme-base` to match.
     */
    interface Theme {
        /** The theme's id, e.g. `k8sdockside-dark`. May be `''`. */
        id: string;
        base: 'light' | 'dark';
        /** Token name (without the leading `--`) -> CSS value. */
        tokens: Record<string, string>;
    }

    /** An action the plugin declares in its manifest, as `ready()` lists them. */
    interface DeclaredAction {
        id: string;
        label: string;
        /** The kind whose action bar it is on. */
        kind: Kind;
    }

    /** A link from the manifest's `links`. Always `http(s)`; open it with `openUrl`. */
    interface PluginLink {
        label: string;
        url: string;
    }

    /** What the manifest says about the plugin itself. */
    interface PluginInfo {
        id: string;
        name: string;
        /** The manifest's `version`; `''` when it does not say. */
        version: string;
        /** The manifest's `docs` link; `''` when it has none. */
        docs: string;
        links: PluginLink[];
        /**
         * Who wrote the plugin, and where to find them; `''` when the
         * manifest does not say. The app already credits them under the
         * plugin's own overview -- these are for a page that wants to say so
         * itself. Optional: an app older than the one that added them leaves
         * them out.
         */
        author?: string;
        authorUrl?: string;
    }

    /**
     * How the user wants dates and times written -- one choice for the app
     * and every plugin, made in Settings -> Dates and times. Write them with
     * `format` rather than reading this yourself.
     */
    interface DateTimeSettings {
        /** `system` follows the locale; `24h` and `12h` override it. */
        clock: 'system' | '24h' | '12h';
        /** `system`, `iso` (2026-09-24), `dmy` (24.09.2026), `mdy` (09/24/2026) or `long` (24 Sep 2026). */
        dates: 'system' | 'iso' | 'dmy' | 'mdy' | 'long';
        /** `local` is the user's machine; `utc` is UTC, marked as such. */
        zone: 'local' | 'utc';
        /** Whether a moment in a table reads as how long ago (`relative`) or as the moment (`absolute`). */
        ages: 'relative' | 'absolute';
    }

    /** A moment: a Date, an RFC3339 string, or milliseconds since the epoch. */
    type When = Date | string | number;

    /** Writes dates and times as the user chose. Every method answers `''` for something that is not a date. */
    interface Format {
        /** The settings in force. */
        settings(): DateTimeSettings;
        /** A date: `2026-09-24`, `24.09.2026`, ... */
        date(when: When): string;
        /** A day without its year, for an axis: `09-24`, `24 Sep`, ... */
        day(when: When): string;
        /** A time of day. */
        time(when: When, opts?: { seconds?: boolean }): string;
        /** A date and a time, followed by `UTC` when that is the zone. */
        dateTime(when: When, opts?: { seconds?: boolean }): string;
        /**
         * How long ago, as the app's tables write it: `45s`, `2m5s`, `15m`,
         * `3h20m`, `12h`, `3d4h`, `12d`. Seconds for the first ten minutes, so
         * redraw every second while something that young is on screen. `now`
         * defaults to the current time.
         */
        age(when: When, now?: number): string;
        /**
         * A moment the way the app's tables show one: its age, or the moment
         * itself when the user chose that, with the other form as `title`.
         */
        moment(when: When): { text: string; title: string };
    }

    /** What `ready()` resolves with. */
    interface Context {
        /** The plugin's id from its manifest. */
        pluginId: string;
        /**
         * The view this page is a tab for: a custom view's id, or `'overview'`
         * for the plugin's own overview. `''` for a section.
         */
        viewId: string;
        /** The section this page is drawn as, in an object's detail view. `''` for a tab. */
        sectionId: string;
        /** The object a section is drawn for; `null` on a tab. */
        object: SectionObject | null;
        /** The kubeconfig context the tab belongs to -- the only cluster the page can see. */
        contextId: string;
        /** The same context as the app writes it for a person. */
        contextName: string;
        /** Every kind the page may read, as the app worked it out from the manifest. Never `secrets`. */
        readable: Kind[];
        /** Whether the manifest says `"ui": { "write": true }` -- whether `patch` can be asked for at all. */
        write: boolean;
        /**
         * Whether the manifest says `"ui": { "registries": true }` -- whether
         * `registry.lookup` can be asked at all. Absent on an app older than
         * 0.0.25.
         */
        registries?: boolean;
        /**
         * The in-cluster services the manifest lets the page call with
         * `services.get`. Absent on an app older than 0.0.27.
         */
        services?: DeclaredService[];
        /** Every action the plugin declares, whether or not it is offered on anything right now. */
        actions: DeclaredAction[];
        /**
         * What the manifest says about the plugin -- its name, version, docs
         * and links -- for an overview of its own to link to what it is
         * about. Optional: an app older than the one that added it leaves it
         * out, so read it as `ctx.plugin?.links ?? []`.
         */
        plugin?: PluginInfo;
        /** The theme at the moment the page loaded. See `on('theme')` for changes. */
        theme: Theme;
        /**
         * How dates and times are written, at the moment the page loaded. See
         * `on('datetime')` for changes. Absent on an app older than 0.1.10.
         */
        datetime?: DateTimeSettings;
    }

    // ----- actions ------------------------------------------------------------

    /** One of the plugin's actions offered on an object right now, as the app words it. */
    interface OfferedAction {
        pluginId: string;
        pluginName: string;
        id: string;
        label: string;
        /** An icon name from the app's set, `puzzle` when the manifest names none. */
        icon: string;
        /** `'danger'` or `''`. */
        tone: string;
        /** The manifest's question with `{name}` and `{namespace}` filled in; `''` when it has none. */
        confirm: string;
        /** The manifest's notice for once it has worked, filled in the same way. */
        done: string;
    }

    /**
     * Which object an action is asked about. For `actions()`, naming one needs
     * its `kind` too; for `run()`, the kind is always the action's own.
     */
    interface ActionTarget {
        kind?: Kind;
        namespace?: string;
        name: string;
    }

    interface RunResult {
        /** The name of the object a `create` request made; `''` for the other request types. */
        created: string;
    }

    // ----- the generated overview's ingredients ------------------------------

    /** One of the manifest's `requires`, checked against this cluster. */
    interface Presence {
        kind: Kind;
        label: string;
        optional: boolean;
        /** Whether this cluster serves the kind. */
        served: boolean;
        /** Set when the app could not find out -- not the same as finding it absent. */
        error: string;
    }

    /** One slice of a grouped card: a value the field took and how many objects had it. */
    interface Bucket {
        /** `''` means the field was absent on those objects (drawn as "no status yet"). */
        value: string;
        count: number;
        /** The manifest's tone for the value: `'ok'`, `'warn'`, `'error'`, `'info'`, or `''`. */
        tone: string;
    }

    /** One of the manifest's `cards`, counted. */
    interface CardResult {
        label: string;
        kind: Kind;
        total: number;
        /** Whether the card divides its count (has a `groupBy`). */
        grouped: boolean;
        /** Worst first, not largest first. */
        buckets: Bucket[];
        /** Why it has no number, if it has none. */
        error: string;
    }

    /** What `summary()` resolves with: what the generated overview is made of. */
    interface Summary {
        pluginId: string;
        /** Every non-optional requirement is served. Meaningless when `checked` is false. */
        installed: boolean;
        /** Whether the cluster could be asked at all. False is "could not tell", not "not installed". */
        checked: boolean;
        requirements: Presence[];
        cards: CardResult[];
        /** A failure that stopped the whole summary. */
        error: string;
    }

    // ----- charts ---------------------------------------------------------------

    interface ChartsQuery {
        /** How far back, in minutes. Default 60; kept between 5 and 10080 (a week). */
        minutes?: number;
    }

    interface ChartPoint {
        /** Unix seconds. */
        t: number;
        v: number;
    }

    interface ChartSeries {
        /** From the chart's `legend` label, or the whole label set when it names none. */
        name: string;
        /** A sample Prometheus could not compute is left out, which draws as a gap. */
        points: ChartPoint[];
    }

    /** How a chart's values are written. `percent` is a fraction: 0.87 is 87%. */
    type ChartUnit = '' | 'cores' | 'bytes' | 'bytes/s' | 'percent' | 'ops/s' | 'seconds' | 'count';

    /** One of the plugin's `"attach": "overview"` charts, with its data. */
    interface Chart {
        pluginId: string;
        pluginName: string;
        id: string;
        label: string;
        unit: ChartUnit | string;
        description: string;
        series: ChartSeries[];
        /** Why this chart is empty, if it is. One failing query does not empty the others. */
        error: string;
    }

    /** Where the app found the cluster's Prometheus. */
    interface MetricsEndpoint {
        namespace: string;
        service: string;
        port: string;
        /** A prefix before the API's paths -- `/select/0/prometheus` for a VictoriaMetrics vmselect; `''` otherwise. */
        path: string;
        /** Set instead of namespace/service/port when the user configured an address. */
        url: string;
        source: string;
    }

    interface MetricsSource {
        endpoint: MetricsEndpoint;
        /** The override the user typed in the cluster's settings, if any. */
        configured: string;
        /** Whether there is a Prometheus to ask at all. */
        available: boolean;
        /** Why the app could not look, as opposed to having looked and found nothing. */
        error: string;
        /** The endpoint written for a person: `monitoring/prometheus-operated:9090` or the URL. */
        describe: string;
    }

    /** What `charts()` resolves with. */
    interface ChartsPanel {
        /** Whether the plugin declares any overview charts. */
        attached: boolean;
        source: MetricsSource;
        charts: Chart[];
        /** The window actually used, in minutes. */
        range: number;
    }

    // ----- registries -----------------------------------------------------------

    /** What `registry.lookup` is asked. */
    interface RegistryLookupQuery {
        /** An image reference as a pod spec writes it: `nginx:1.27`, `ghcr.io/org/app:v1@sha256:…`. A pod in the cluster must run it. */
        image: string;
        /**
         * Ask the registry again rather than use the app's recent answer,
         * which it keeps for half an hour. An answer under a minute old is
         * used anyway.
         */
        refresh?: boolean;
    }

    /**
     * How the registry answered:
     *
     * - `ok`          -- it listed the tags
     * - `auth`        -- it wants credentials: the app asks anonymously, so a private image cannot be checked
     * - `missing`     -- it does not know the repository
     * - `limited`     -- it is rate-limiting this address; try again later
     * - `unreachable` -- it could not be reached: DNS, TLS, a timeout, or a registry that only speaks plain http
     * - `error`       -- anything else; `error` says what
     */
    type RegistryStatus = 'ok' | 'auth' | 'missing' | 'limited' | 'unreachable' | 'error';

    /** What `registry.lookup` resolves with. */
    interface RegistryLookup {
        /** The reference as asked. */
        image: string;
        /** The registry host, with Docker Hub's aliases folded into `docker.io`. */
        registry: string;
        /** The path in the registry, `library/` included for Docker Hub's official images. */
        repository: string;
        /** The tag the reference names: `latest` when none is written, `''` when only a digest is. */
        tag: string;
        /** Every tag the registry lists, in its order -- not sorted by version. */
        tags: string[];
        /** The registry had more than the 10000 tags the app reads. */
        truncated: boolean;
        /**
         * What `tag` points at in the registry now: for a multi-platform image
         * the index, which is also what a pod's `imageID` records when it was
         * pulled by tag. `''` when unknown.
         */
        digest: string;
        /** When the registry was asked, RFC 3339. */
        checkedAt: string;
        status: RegistryStatus;
        /** What went wrong, in words; `''` when `status` is `ok`. */
        error: string;
    }

    interface Registry {
        /**
         * Asks the image's registry which tags it has, and what the image's
         * tag points at now. Needs `"ui": { "registries": true }`. Rejects
         * when the plugin does not declare that, when the reference is not
         * one, or when no pod in this cluster runs the image; a registry that
         * fails resolves with a `status` other than `ok` instead.
         */
        lookup(query: RegistryLookupQuery): Promise<RegistryLookup>;
    }

    // ----- services in the cluster ---------------------------------------------

    /** One of the services the manifest declares in `"ui": { "services": [...] }`. */
    interface DeclaredService {
        id: string;
        label: string;
        /** The path prefixes the page may request. */
        paths: string[];
    }

    /** A query parameter's value; a list sends the parameter once per item. */
    type QueryValue = string | number | boolean | Array<string | number | boolean>;

    /** What `services.get` and `services.json` are asked. */
    interface ServiceRequest {
        /** The id of a service the manifest declares. */
        service: string;
        /**
         * The path to request, under one of the service's declared prefixes:
         * absolute and already clean -- no `..`, `//`, `%`, `?` or `#`. Put
         * values in `query`, where they are encoded for you.
         */
        path: string;
        query?: Record<string, QueryValue>;
    }

    /** What the service answered, as it answered it. */
    interface ServiceAnswer {
        /** The Service that answered, as `namespace/name:port`. */
        service: string;
        /** The service's own HTTP status: a 404 here is the service's, not the app's. */
        status: number;
        contentType: string;
        /** The answer as text, at most 8 MiB. */
        body: string;
    }

    interface Services {
        /**
         * Makes a GET request to one of the plugin's declared services,
         * through the API server's service proxy, and resolves with whatever
         * it answered. Rejects when the service or path is not declared, when
         * the Service cannot be found or reached, when the proxy is not
         * allowed (it needs `services/proxy`), and after 15 seconds.
         */
        get(request: ServiceRequest): Promise<ServiceAnswer>;
        /**
         * `get`, then the body parsed as JSON. Rejects on a status outside
         * 200-299, with the service's own words when it gave any.
         */
        json<T = unknown>(request: ServiceRequest): Promise<T>;
    }

    // ----- writing --------------------------------------------------------------

    interface PatchRequest {
        kind: Kind;
        namespace?: string;
        name: string;
        /**
         * A JSON merge patch (RFC 7386): an object, sent as JSON, or text. A
         * `null` value removes a field; a list replaces the whole list.
         */
        patch: object | string;
    }

    interface CreateRequest {
        kind: Kind;
        /** Where it is created, whatever the object's own metadata says. `''` for a cluster-scoped kind. */
        namespace?: string;
        /**
         * The whole object -- `apiVersion`, `kind`, `metadata.name` (or
         * `generateName`) and the rest -- as an object, sent as JSON, or JSON
         * text.
         */
        object: object | string;
    }

    interface CreateResult {
        /** The name the object was given -- the only way to know it for a `generateName`. */
        name: string;
    }

    // ----- pushes from the app -----------------------------------------------------

    /** The events `on` can listen for, and what each listener is called with. */
    interface Events {
        /** The user changed theme. The new tokens are already on `:root` when this is called. */
        theme: Theme;
        /** The user changed how dates and times are written. `format` already follows it. */
        datetime: DateTimeSettings;
    }

    /** Stops what returned it. */
    type Unsubscribe = () => void;

    // ----- remembering ---------------------------------------------------------

    /**
     * What a page keeps between sessions, for its plugin on its tab's cluster.
     * Values are anything JSON can hold, at most 16 KiB each and 64 keys per
     * plugin and cluster. Kept by the app in its settings, never in the cluster.
     */
    interface PluginStorage {
        /** The value kept under `key`, or null when there is none. */
        get<T = unknown>(key: string): Promise<T | null>;
        /** Keeps `value` under `key`. Rejects with the reason when a limit is hit. */
        set(key: string, value: unknown): Promise<null>;
        /** Forgets what is kept under `key`. */
        remove(key: string): Promise<null>;
        /** Every key kept, sorted. */
        keys(): Promise<string[]>;
    }

    // ----- the bridge -------------------------------------------------------------

    interface Bridge {
        /**
         * Resolves once the app has answered, with what this page is looking
         * at. Always the same promise; call it as often as is convenient. The
         * theme is applied before it resolves, and a section's height is
         * followed from then on.
         */
        ready(): Promise<Context>;

        /**
         * The object a section is drawn for, read live -- `get` on
         * `ready().object`. Rejects on a page that is a tab.
         */
        object<T extends KubeObject = KubeObject>(): Promise<T>;

        /**
         * Which of this plugin's actions are offered on an object right now:
         * the section's own object, or the one named (with its `kind`).
         * Actions from other plugins are left out. For drawing a button group
         * with only the buttons that apply.
         */
        actions(ref?: ActionTarget): Promise<OfferedAction[]>;

        /**
         * Runs one of the actions the plugin declares in its manifest, on the
         * section's object or the one named (`{ namespace, name }`; the kind
         * is the action's). The app asks the user first, every time, whatever
         * the manifest says about confirming; for a `delete` action they have
         * to type the object's name as well. Rejects with "the action was
         * declined" if they say no.
         */
        run(actionId: string, ref?: ActionTarget): Promise<RunResult>;

        /**
         * Sets a section's height, in pixels, by hand. The SDK already
         * measures the page and follows it, so this is for a page that lays
         * itself out in a way a ResizeObserver misses. Kept between 60 and
         * 4000; ignored on a tab, which fills its pane.
         */
        resize(height: number): Promise<null>;

        /**
         * Objects of one kind, whole. Rejects for a kind the plugin does not
         * declare, and for one this cluster does not serve.
         */
        list<T extends KubeObject = KubeObject>(query: ListQuery): Promise<T[]>;

        /** One object, read live. */
        get<T extends KubeObject = KubeObject>(ref: ObjectRef): Promise<T>;

        /**
         * Polls `list` every `query.interval` ms (default 5000, at least 1000)
         * and calls `onItems` with each answer, the first straight away. A
         * failed poll calls `onError` instead and polling carries on. Returns
         * a function that stops it. Polling pauses while the window is hidden
         * and resumes, with a read straight away, when it is shown (0.1.10
         * and newer).
         */
        watch<T extends KubeObject = KubeObject>(
            query: WatchQuery,
            onItems: (items: T[]) => void,
            onError?: (err: Error) => void,
        ): Unsubscribe;

        /** The names of this cluster's namespaces. */
        namespaces(): Promise<string[]>;

        /**
         * What the generated overview is made of, for this plugin and this
         * cluster: which of the manifest's `requires` the cluster serves, and
         * its `cards`, counted. For an overview of the plugin's own, so it can
         * still say first whether the solution is in this cluster at all.
         */
        summary(): Promise<Summary>;

        /**
         * This plugin's `"attach": "overview"` charts, from the cluster's
         * Prometheus -- never another plugin's. `source.available` false
         * means no Prometheus was found; `attached` false means the manifest
         * has no overview charts.
         */
        charts(query?: ChartsQuery): Promise<ChartsPanel>;

        /**
         * Asks to merge-patch one object. Needs `"ui": { "write": true }` and
         * a kind the plugin declares. The user sees the patch, the object and
         * the cluster, and it is applied only if they press Apply change.
         * Rejects with "the change was declined" if they do not, and with
         * "another change is already waiting for an answer" while an earlier
         * one is still on screen.
         */
        patch(request: PatchRequest): Promise<null>;

        /**
         * Asks to create one object. Needs `"ui": { "write": true }` and a kind
         * the plugin declares; Secrets, RBAC, admission webhooks and CRDs are
         * refused whatever is declared. The user sees the object, the namespace
         * and the cluster, and it is created only if they press Create. Rejects
         * with "the creation was declined" if they do not.
         */
        create(request: CreateRequest): Promise<CreateResult>;

        /** Opens an object in the app's details panel, or -- with no `name` -- the kind's own tab. */
        open(ref: OpenRef): Promise<null>;

        /** Opens another of this plugin's views by id, or `'overview'`. Rejects for an id it does not have. */
        openView(viewId: string): Promise<null>;

        /** Opens an object in the app's YAML editor. */
        edit(ref: ObjectRef): Promise<null>;

        /** Opens an object's logs in the app -- a pod's, or anything else the log view accepts. */
        logs(ref: ObjectRef): Promise<null>;

        /**
         * Opens an `http(s)` address in the user's browser. Anything else is
         * refused by the app with a notice (the promise still resolves).
         */
        openUrl(url: string): Promise<null>;

        /**
         * Writes dates and times as the user chose, the same way the app
         * does. Absent on an app older than 0.1.10: check for it, and fall
         * back to `toLocaleString` without it.
         */
        format?: Format;

        /** Listens for pushes from the app. Returns a function that stops listening. */
        on<E extends keyof Events>(event: E, listener: (data: Events[E]) => void): Unsubscribe;

        /**
         * What the page keeps between sessions, for this plugin on this
         * cluster. Absent on an app older than 0.0.19, which is why it is
         * optional: check for it, and fall back to the URL hash without it.
         */
        storage?: PluginStorage;

        /**
         * Image registries, asked by the app on the page's behalf. Absent on
         * an app older than 0.0.25: check for it.
         */
        registry?: Registry;

        /**
         * The in-cluster services the manifest declares, called by the app on
         * the page's behalf. Absent on an app older than 0.0.27: check for it.
         */
        services?: Services;
    }
}

/** The bridge. Defined by /plugin-ui/_sdk/k8sdockside.js, which must load before the page's own script. */
declare var k8sdockside: K8sDockside.Bridge;
