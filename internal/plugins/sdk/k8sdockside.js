/*
 * The k8sdockside plugin bridge, for a plugin's own views.
 *
 * Include it from a view's HTML before your own script:
 *
 *     <script src="/plugin-ui/_sdk/k8sdockside.js"></script>
 *
 * It defines `window.k8sdockside`. Every call is a message to the app, which
 * answers only for the kinds the plugin declares (its requires, views, cards
 * and ui.kinds) and never for Secrets. A patch is shown to the user first and
 * applied only if they say yes.
 *
 * The page runs in a sandboxed frame with no network access of its own: fetch,
 * XHR and websockets are refused by its Content-Security-Policy. Bundle what
 * you need into your own files.
 *
 * k8sdockside.d.ts beside this file types all of it for a plugin written in
 * TypeScript; change the two together.
 */
(function () {
    'use strict';

    var PROTOCOL = 'k8sdockside/plugin@1';
    var pending = new Map();
    var listeners = {};
    var nextId = 1;

    function call(method, params) {
        return new Promise(function (resolve, reject) {
            var id = nextId++;
            pending.set(id, { resolve: resolve, reject: reject });
            window.parent.postMessage({ protocol: PROTOCOL, id: id, method: method, params: params || {} }, '*');
        });
    }

    function emit(event, data) {
        (listeners[event] || []).forEach(function (fn) {
            try {
                fn(data);
            } catch (err) {
                console.error('k8sdockside: a "' + event + '" listener failed', err);
            }
        });
    }

    /* Light or dark from the first paint, before the app has answered: the
       frame's address says which. WebKit keeps the scrollbars it drew first,
       so a page that only turned dark once the theme arrived kept white ones. */
    (function () {
        var scheme = /[?&]scheme=(light|dark)\b/.exec(location.search);
        if (scheme) document.documentElement.style.colorScheme = scheme[1];
    })();

    /* The app's theme, as the same custom properties the app's own components
       use: var(--bg), var(--text), var(--accent), var(--ok) and the rest. */
    function applyTheme(theme) {
        if (!theme) return;
        var root = document.documentElement;
        var tokens = theme.tokens || {};
        Object.keys(tokens).forEach(function (name) {
            root.style.setProperty('--' + name, tokens[name]);
        });
        root.style.colorScheme = theme.base === 'light' ? 'light' : 'dark';
        root.dataset.themeBase = theme.base === 'light' ? 'light' : 'dark';
    }

    window.addEventListener('message', function (event) {
        if (event.source !== window.parent) return;
        var msg = event.data;
        if (!msg || msg.protocol !== PROTOCOL) return;

        if (typeof msg.id === 'number') {
            var waiting = pending.get(msg.id);
            if (!waiting) return;
            pending.delete(msg.id);
            if (msg.error !== undefined) waiting.reject(new Error(msg.error));
            else waiting.resolve(msg.result);
            return;
        }
        if (typeof msg.event === 'string') {
            if (msg.event === 'theme') applyTheme(msg.data);
            emit(msg.event, msg.data);
        }
    });

    /* A section sits in a scrolling detail panel, where a frame cannot size
       itself to its content; so the page's height is measured here and told to
       the app whenever it changes. A tab fills its pane and ignores it. */
    function followHeight() {
        var last = 0;
        function measure() {
            var h = Math.ceil(document.documentElement.scrollHeight);
            if (h !== last) {
                last = h;
                call('resize', { height: h });
            }
        }
        if (typeof ResizeObserver === 'function') {
            new ResizeObserver(measure).observe(document.documentElement);
        }
        window.addEventListener('load', measure);
        measure();
    }

    var ready = call('hello').then(function (context) {
        applyTheme(context.theme);
        if (context.sectionId) {
            // The frame's own height is the page's height; a page that sets
            // height: 100% on html/body would otherwise measure itself forever.
            document.documentElement.style.height = 'auto';
            if (document.body) followHeight();
            else document.addEventListener('DOMContentLoaded', followHeight);
        }
        return context;
    });

    window.k8sdockside = {
        /**
         * Resolves once the app has answered, with what this page is looking at:
         * { pluginId, viewId, sectionId, object, contextId, contextName,
         *   readable, write, registries, services, actions, plugin, theme }.
         *
         * `object` is { kind, namespace, name } for a section in an object's
         * detail view, and null for a view that is a tab of its own.
         *
         * `plugin` is what the manifest says about the plugin itself:
         * { id, name, version, docs, links: [{ label, url }], author, authorUrl } -- for a page
         * that is the plugin's own overview to link to what it is about. Open
         * a link with openUrl.
         */
        ready: function () {
            return ready;
        },

        /** The object a section is drawn for, read live. Rejects on a tab. */
        object: function () {
            return ready.then(function (ctx) {
                if (!ctx.object) throw new Error('this page is not drawn for an object');
                return call('get', ctx.object);
            });
        },

        /**
         * Which of this plugin's actions are offered on an object right now --
         * the section's own object unless one is named as { kind, namespace,
         * name } -- as [{ pluginId, pluginName, id, label, icon, tone, confirm,
         * done }]. For drawing a button group with only the buttons that apply.
         */
        actions: function (ref) {
            return call('actions', ref || {});
        },

        /**
         * Runs one of the actions this plugin declares in its manifest, on the
         * section's object or the one named ({ namespace, name }). The app asks
         * the user first, every time. Resolves with { created } -- the name of an
         * object a create request made -- and rejects if they decline.
         */
        run: function (actionId, ref) {
            return call('action', Object.assign({}, ref || {}, { id: actionId }));
        },

        /** Sets a section's height by hand, for a page that lays itself out oddly. */
        resize: function (height) {
            return call('resize', { height: height });
        },

        /** Objects of one kind: { kind, namespace?, selector? } -> object[]. */
        list: function (query) {
            return call('list', query);
        },

        /** One object: { kind, namespace, name } -> object. */
        get: function (ref) {
            return call('get', ref);
        },

        /**
         * Polls `list` and calls back with the objects whenever asked, and once
         * straight away. Returns a function that stops it.
         * { kind, namespace?, selector?, interval? (ms, default 5000) }
         */
        watch: function (query, callback, onError) {
            var stopped = false;
            var timer = null;
            var interval = Math.max(1000, (query && query.interval) || 5000);
            function tick() {
                call('list', query)
                    .then(function (items) {
                        if (!stopped) callback(items);
                    })
                    .catch(function (err) {
                        if (!stopped && onError) onError(err);
                    })
                    .then(function () {
                        if (!stopped) timer = setTimeout(tick, interval);
                    });
            }
            tick();
            return function () {
                stopped = true;
                if (timer) clearTimeout(timer);
            };
        },

        /** The namespaces of this cluster. */
        namespaces: function () {
            return call('namespaces');
        },

        /**
         * What the generated overview is made of, for this plugin and this
         * cluster: { installed, checked, requirements: [{ kind, label,
         * optional, served, error }], cards: [{ label, kind, total, grouped,
         * buckets: [{ value, count, tone }], error }], error }. For a page
         * that is the plugin's own overview, so it can still say first whether
         * the solution is in this cluster at all. `checked` false means the
         * cluster could not be asked, which is not the same as "not installed".
         */
        summary: function () {
            return call('summary');
        },

        /**
         * This plugin's overview charts, from the cluster's Prometheus, for a
         * page that is the plugin's own overview to draw its own way:
         * { minutes? (default 60) } -> { attached, range, source: { available,
         * error, describe }, charts: [{ id, label, unit, description, error,
         * series: [{ name, points: [{ t (unix seconds), v }] }] }] }.
         * `source.available` false means no Prometheus was found.
         */
        charts: function (query) {
            return call('charts', query || {});
        },

        /**
         * Asks to merge-patch one object: { kind, namespace, name, patch }.
         * `patch` is an object or JSON/YAML text. Needs "ui": { "write": true },
         * and the user sees the patch and confirms it first. Rejects if they
         * decline.
         */
        patch: function (request) {
            var params = Object.assign({}, request);
            if (typeof params.patch !== 'string') params.patch = JSON.stringify(params.patch, null, 2);
            return call('patch', params);
        },

        /**
         * Asks to create one object: { kind, namespace, object }. `object` is
         * the whole object -- apiVersion, kind, metadata.name and the rest --
         * as an object or JSON text; it lands in `namespace` whatever its own
         * metadata says. Needs "ui": { "write": true }, and the user sees the
         * object and confirms it first. Resolves with { name }; rejects if
         * they decline.
         */
        create: function (request) {
            var params = Object.assign({}, request);
            if (typeof params.object !== 'string') params.object = JSON.stringify(params.object, null, 2);
            return call('create', params);
        },

        /** Opens an object in the details panel, or a kind's own tab if no name is given. */
        open: function (ref) {
            return call('open', ref);
        },

        /** Opens another of this plugin's views by id, or 'overview'. */
        openView: function (viewId) {
            return call('openView', { viewId: viewId });
        },

        /** Opens an object in the YAML editor. */
        edit: function (ref) {
            return call('edit', ref);
        },

        /** Opens the logs of a pod (or anything else the log view accepts). */
        logs: function (ref) {
            return call('logs', ref);
        },

        /** Opens an http(s) address in the user's browser. */
        openUrl: function (url) {
            return call('openUrl', { url: url });
        },

        /**
         * What the page keeps between sessions, for this plugin on this
         * cluster: a folded section, a filter, a choice. Values are anything
         * JSON can hold -- at most 16 KiB each and 64 keys. The app keeps them
         * in its own settings, not in the cluster: nobody else sees them.
         */
        storage: {
            /** The value kept under `key`, or null. */
            get: function (key) {
                return call('storage.get', { key: key }).then(function (raw) {
                    if (raw === null || raw === undefined) return null;
                    try {
                        return JSON.parse(raw);
                    } catch (err) {
                        return null;
                    }
                });
            },
            /** Keeps `value` under `key`; undefined forgets it. */
            set: function (key, value) {
                return call('storage.set', { key: key, value: value === undefined ? '' : JSON.stringify(value) });
            },
            /** Forgets what is kept under `key`. */
            remove: function (key) {
                return call('storage.set', { key: key, value: '' });
            },
            /** Every key kept, sorted. */
            keys: function () {
                return call('storage.keys');
            },
        },

        /**
         * Image registries, asked by the app: the page itself has no network.
         * Needs "ui": { "registries": true }.
         */
        registry: {
            /**
             * What the image's registry says about it: { image, refresh? } ->
             * { image, registry, repository, tag, tags, truncated, digest,
             *   checkedAt, status, error }. `status` is ok, auth, missing,
             * limited, unreachable or error; only a request the app refuses
             * -- an image no pod in the cluster runs, say -- rejects.
             */
            lookup: function (query) {
                return call('registry.lookup', query || {});
            },
        },

        /**
         * Services in the cluster, called by the app through the API server's
         * service proxy. Needs "ui": { "services": [...] } declaring each one
         * and the paths the page may request. GET only.
         */
        services: {
            /**
             * { service, path, query? } -> { service, status, contentType, body }.
             * `service` is a declared id; `path` sits under one of its
             * prefixes. The status is the service's own.
             */
            get: function (request) {
                return call('services.get', request || {});
            },
            /** get, then the body as JSON; rejects on a status outside 200-299. */
            json: function (request) {
                return call('services.get', request || {}).then(function (answer) {
                    if (answer.status < 200 || answer.status > 299) {
                        var said = String(answer.body || '').trim().slice(0, 300);
                        throw new Error(answer.service + ' answered ' + answer.status + (said ? ': ' + said : ''));
                    }
                    try {
                        return JSON.parse(answer.body);
                    } catch (err) {
                        throw new Error(answer.service + ' did not answer with JSON (' + (answer.contentType || 'no content type') + ')');
                    }
                });
            },
        },

        /** Listens for pushes from the app. Events: 'theme'. Returns an unsubscribe function. */
        on: function (event, fn) {
            (listeners[event] = listeners[event] || []).push(fn);
            return function () {
                listeners[event] = (listeners[event] || []).filter(function (f) {
                    return f !== fn;
                });
            };
        },
    };
})();
