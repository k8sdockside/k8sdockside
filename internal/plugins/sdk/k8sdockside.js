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
            if (msg.event === 'datetime') applyDateTime(msg.data);
            emit(msg.event, msg.data);
        }
    });

    /* The app's own shortcuts, passed up to it. A key pressed while the page
       has focus goes to the page's document, never the app's window, so
       zooming with Cmd/Ctrl +, - or 0 -- and the app's other shortcuts --
       did nothing once the page had been clicked. Only these keys are passed
       on; the app checks the list again. Keep in step with onZoomKey in
       frontend/src/App.svelte. */
    function appShortcut(event) {
        if (event.key === 'F1') return true;
        if (!event.metaKey && !event.ctrlKey) return false;
        switch (event.key) {
            case '+':
            case '=':
            case '-':
            case '_':
            case '0':
            case ',':
            case 'b':
            case 'B':
                return true;
        }
        return event.code === 'NumpadAdd' || event.code === 'NumpadSubtract';
    }

    window.addEventListener(
        'keydown',
        function (event) {
            if (event.defaultPrevented || !appShortcut(event)) return;
            event.preventDefault();
            call('shortcut', {
                key: event.key,
                code: event.code,
                metaKey: event.metaKey,
                ctrlKey: event.ctrlKey,
                shiftKey: event.shiftKey,
                altKey: event.altKey,
            }).catch(function () {
                // An app older than the shortcut request does not know it.
            });
        },
        false,
    );

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

    /* How the user wants dates and times written, and the helpers that write
       them so. The same rules as the app's own frontend/src/lib/datetime.svelte.ts,
       which this must be kept in step with: a plugin that formats through
       these writes a time exactly as the app beside it does. An app older
       than 0.1.10 sends none, and the defaults -- the locale's own -- apply. */
    var dateTime = { clock: 'system', dates: 'system', zone: 'local', ages: 'relative' };

    function applyDateTime(next) {
        if (!next) return;
        dateTime = {
            clock: next.clock || 'system',
            dates: next.dates || 'system',
            zone: next.zone || 'local',
            ages: next.ages || 'relative',
        };
    }

    function toDate(when) {
        var d = when instanceof Date ? when : new Date(when);
        return isNaN(d.getTime()) ? null : d;
    }

    function tz() {
        return dateTime.zone === 'utc' ? 'UTC' : undefined;
    }

    function ymd(d) {
        var out = { y: '', m: '', d: '' };
        new Intl.DateTimeFormat('en-CA', { timeZone: tz(), year: 'numeric', month: '2-digit', day: '2-digit' })
            .formatToParts(d)
            .forEach(function (p) {
                if (p.type === 'year') out.y = p.value;
                if (p.type === 'month') out.m = p.value;
                if (p.type === 'day') out.d = p.value;
            });
        return out;
    }

    var format = {
        /** The settings in force: { clock, dates, zone, ages }. */
        settings: function () {
            return { clock: dateTime.clock, dates: dateTime.dates, zone: dateTime.zone, ages: dateTime.ages };
        },
        /** A date, as the user chose to see dates written. */
        date: function (when) {
            var d = toDate(when);
            if (!d) return '';
            var p = ymd(d);
            switch (dateTime.dates) {
                case 'iso':
                    return p.y + '-' + p.m + '-' + p.d;
                case 'dmy':
                    return p.d + '.' + p.m + '.' + p.y;
                case 'mdy':
                    return p.m + '/' + p.d + '/' + p.y;
                case 'long':
                    return d.toLocaleDateString(undefined, { timeZone: tz(), day: 'numeric', month: 'short', year: 'numeric' });
                default:
                    return d.toLocaleDateString(undefined, { timeZone: tz() });
            }
        },
        /** A day without its year, for an axis. */
        day: function (when) {
            var d = toDate(when);
            if (!d) return '';
            var p = ymd(d);
            switch (dateTime.dates) {
                case 'iso':
                    return p.m + '-' + p.d;
                case 'dmy':
                    return p.d + '.' + p.m;
                case 'mdy':
                    return p.m + '/' + p.d;
                default:
                    return d.toLocaleDateString(undefined, { timeZone: tz(), day: 'numeric', month: 'short' });
            }
        },
        /** A time of day; { seconds: true } adds the seconds. */
        time: function (when, opts) {
            var d = toDate(when);
            if (!d) return '';
            var o = { timeZone: tz(), hour: '2-digit', minute: '2-digit' };
            if (opts && opts.seconds) o.second = '2-digit';
            if (dateTime.clock === '24h') o.hourCycle = 'h23';
            if (dateTime.clock === '12h') o.hourCycle = 'h12';
            return d.toLocaleTimeString(undefined, o);
        },
        /** A date and a time, marked UTC when that is the zone. */
        dateTime: function (when, opts) {
            var d = toDate(when);
            if (!d) return '';
            var text = format.date(d) + ' ' + format.time(d, opts);
            return dateTime.zone === 'utc' ? text + ' UTC' : text;
        },
        /**
         * How long ago, as the app's tables write it: "45s", "2m5s", "15m",
         * "3h20m", "12h", "3d4h", "12d". Seconds for the first ten minutes;
         * redraw every second while something that young is on screen.
         */
        age: function (when, now) {
            var d = toDate(when);
            if (!d) return '';
            var s = Math.max(0, Math.floor(((now === undefined ? Date.now() : now) - d.getTime()) / 1000));
            if (s < 60) return s + 's';
            if (s < 600) return s % 60 ? Math.floor(s / 60) + 'm' + (s % 60) + 's' : s / 60 + 'm';
            var minutes = Math.floor(s / 60);
            if (minutes < 60) return minutes + 'm';
            if (minutes < 60 * 48) {
                var h = Math.floor(minutes / 60);
                var m = minutes % 60;
                return h < 10 && m > 0 ? h + 'h' + m + 'm' : h + 'h';
            }
            var days = Math.floor(minutes / (60 * 24));
            var hours = Math.floor((minutes % (60 * 24)) / 60);
            return days < 10 && hours > 0 ? days + 'd' + hours + 'h' : days + 'd';
        },
        /**
         * A moment the way the app's tables show one: its age, or the moment
         * itself when the user chose that. Returns { text, title }, the title
         * being the other form, for a tooltip.
         */
        moment: function (when) {
            var d = toDate(when);
            if (!d) return { text: '', title: '' };
            var age = format.age(d);
            var exact = format.dateTime(d, { seconds: true });
            return dateTime.ages === 'absolute' ? { text: format.dateTime(d), title: age + ' ago' } : { text: age, title: exact };
        },
    };

    /* What is typed into the page, kept by the app while the page is away.

       The app rebuilds a tab's frame every time the tab is brought forward
       again, so a search typed into a plugin's page was gone when the reader
       came back to it. Every input, textarea and select with an id (or a
       name) is reported to the app as it changes, and handed back in hello;
       here it is put back, with the input and change events a person typing
       it would have fired, so the page's own handlers filter exactly as
       they did. A field the page draws later -- after its data has arrived --
       is filled in as it appears.

       Opt a field out with data-k8sdockside-keep="off" on it or on anything
       around it. Passwords and file pickers are never kept. Kept for the
       session only, and dropped when the tab is closed. */
    var KEEP_OFF = '[data-k8sdockside-keep="off"]';

    function fieldKey(el) {
        if (!el || el.nodeType !== 1) return '';
        var tag = el.tagName;
        if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT') return '';
        if (el.closest && el.closest(KEEP_OFF)) return '';
        var type = String(el.type || '').toLowerCase();
        if (tag === 'INPUT' && /^(password|file|hidden|submit|button|reset|image)$/.test(type)) return '';
        if (type === 'radio') return el.name ? 'radio:' + el.name : '';
        if (el.id) return '#' + el.id;
        if (el.name) return 'name:' + el.name;
        return '';
    }

    function keepFields(saved) {
        saved = saved && typeof saved === 'object' ? saved : {};
        var values = Object.assign({}, saved);
        var settled = {};

        function record(event) {
            var el = event.target;
            var key = fieldKey(el);
            if (!key) return;
            // Typed into by the reader: never put back over what they typed.
            if (event.isTrusted) settled[key] = true;
            if (el.type === 'radio') {
                if (!el.checked) return;
                values[key] = el.value;
            } else if (el.type === 'checkbox') {
                values[key] = !!el.checked;
            } else {
                values[key] = String(el.value);
            }
            call('fields.set', { fields: values }).catch(function () {});
        }
        document.addEventListener('input', record, true);
        document.addEventListener('change', record, true);

        var waiting = Object.keys(saved).length;
        if (!waiting) return;

        function fire(el) {
            el.dispatchEvent(new Event('input', { bubbles: true }));
            el.dispatchEvent(new Event('change', { bubbles: true }));
        }

        function restore(el) {
            var key = fieldKey(el);
            if (!key || settled[key] || !Object.prototype.hasOwnProperty.call(saved, key)) return;
            var want = saved[key];
            if (el.type === 'radio') {
                if (el.value !== want) return;
                settled[key] = true;
                waiting--;
                if (!el.checked) {
                    el.checked = true;
                    fire(el);
                }
                return;
            }
            if (el.type === 'checkbox') {
                settled[key] = true;
                waiting--;
                if (el.checked !== !!want) {
                    el.checked = !!want;
                    fire(el);
                }
                return;
            }
            if (el.value === want) {
                settled[key] = true;
                waiting--;
                return;
            }
            el.value = want;
            // A select whose option is not there yet takes no value; it is
            // tried again when its options are drawn.
            if (el.value !== want) return;
            settled[key] = true;
            waiting--;
            fire(el);
        }

        function sweep(node) {
            if (!node || node.nodeType !== 1) return;
            if (node.tagName === 'OPTION' && node.parentElement) {
                var select = node.closest('select');
                if (select) restore(select);
                return;
            }
            restore(node);
            if (node.querySelectorAll) {
                var found = node.querySelectorAll('input, textarea, select');
                for (var i = 0; i < found.length; i++) restore(found[i]);
            }
        }

        sweep(document.documentElement);
        if (waiting <= 0 || typeof MutationObserver !== 'function') return;
        var observer = new MutationObserver(function (records) {
            records.forEach(function (r) {
                for (var i = 0; i < r.addedNodes.length; i++) sweep(r.addedNodes[i]);
            });
            if (waiting <= 0) observer.disconnect();
        });
        observer.observe(document.documentElement, { childList: true, subtree: true });
        // A field that never comes back -- the page changed -- is not waited
        // for forever.
        setTimeout(function () {
            observer.disconnect();
        }, 30000);
    }

    /* After the page's own scripts have run, and after whatever they started
       from ready(): one task later than both, so the handlers the restored
       events are meant for are listening. */
    function whenLoaded(fn) {
        function later() {
            setTimeout(fn, 0);
        }
        if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', later);
        else later();
    }


    /* ---- ui.section: a block of a page that folds, as the app's own do ----
       The same heading, chevron and spacing as the details panel's sections
       (frontend/src/lib/components/PanelSection.svelte -- keep the two in
       step), and the folds kept in the same place: the app's settings, for
       every object on every cluster. */

    var folds = null; // the ids folded, once the app has said; null before
    var sections = []; // every section drawn, so the app's answer can reach them

    function applyFolds(list) {
        folds = {};
        (list || []).forEach(function (id) {
            folds[id] = true;
        });
        sections.forEach(function (sec) {
            if (!sec.touched) sec.set(isFolded(sec), false);
        });
    }

    /* A section that starts open is remembered when it is folded; one that
       starts folded (open: false) is remembered when it is opened, as
       "<id>#open". Either way, one that was never touched has no entry. */
    function memoryKey(sec) {
        return sec.startFolded ? sec.id + '#open' : sec.id;
    }

    function isFolded(sec) {
        var marked = !!(folds && folds[memoryKey(sec)]);
        return sec.startFolded ? !marked : marked;
    }

    var SECTION_CSS =
        '.k8sd-section{margin:0 0 12px}' +
        '.k8sd-section.k8sd-boxed{border:1px solid var(--border,#333);border-radius:var(--radius,6px);background:var(--bg-raised,transparent);padding:6px 12px 10px}' +
        '.k8sd-section.k8sd-boxed.k8sd-folded{padding-bottom:6px}' +
        '.k8sd-section-head{display:flex;align-items:center;gap:8px}' +
        '.k8sd-section-head h3{flex:1 1 auto;min-width:0;margin:0 0 6px;font-size:11px;font-weight:500;letter-spacing:.04em;text-transform:uppercase;color:var(--text-faint,#888);user-select:none}' +
        '.k8sd-folded .k8sd-section-head h3{margin-bottom:0}' +
        '.k8sd-fold{display:flex;align-items:center;gap:8px;width:100%;min-height:26px;margin:0 -6px;padding:3px 6px;box-sizing:content-box;border:none;border-radius:var(--radius-sm,4px);background:none;font:inherit;font-weight:600;color:var(--text-dim,#aaa);text-transform:inherit;letter-spacing:inherit;text-align:left;cursor:pointer}' +
        '.k8sd-fold:hover{background:var(--bg-hover,rgba(127,127,127,.12));color:var(--text,inherit)}' +
        '.k8sd-fold:focus-visible{outline:2px solid var(--accent,#4c8);outline-offset:1px}' +
        '.k8sd-chevron{flex:0 0 auto;display:flex;align-items:center;justify-content:center;width:20px;height:20px;border:1px solid var(--border,#333);border-radius:var(--radius-sm,4px);background:var(--bg-raised,transparent);color:var(--text-dim,#aaa);transition:transform 120ms ease}' +
        '.k8sd-fold:hover .k8sd-chevron{border-color:var(--accent,#4c8);color:var(--accent,#4c8)}' +
        '.k8sd-folded .k8sd-chevron{transform:rotate(-90deg)}' +
        '.k8sd-label{flex:1 1 auto;min-width:0;display:flex;align-items:baseline;gap:6px}' +
        '.k8sd-count,.k8sd-by{letter-spacing:0;text-transform:none;font-weight:400;font-variant-numeric:tabular-nums;color:var(--text-dim,#aaa)}' +
        '.k8sd-hint{flex:0 0 auto;font-size:11px;text-transform:none;letter-spacing:0;font-weight:400;color:var(--text-faint,#888)}' +
        '.k8sd-section-actions{display:flex;align-items:center;gap:6px;flex:0 0 auto;margin-bottom:6px}' +
        '.k8sd-folded .k8sd-section-actions,.k8sd-folded .k8sd-section-body{display:none}';

    function sectionStyles() {
        if (document.getElementById('k8sd-section-css')) return;
        var style = document.createElement('style');
        style.id = 'k8sd-section-css';
        style.textContent = SECTION_CSS;
        (document.head || document.documentElement).appendChild(style);
    }

    function svgChevron() {
        var ns = 'http://www.w3.org/2000/svg';
        var svg = document.createElementNS(ns, 'svg');
        svg.setAttribute('viewBox', '0 0 24 24');
        svg.setAttribute('width', '13');
        svg.setAttribute('height', '13');
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('stroke-width', '2');
        svg.setAttribute('stroke-linecap', 'round');
        svg.setAttribute('stroke-linejoin', 'round');
        var path = document.createElementNS(ns, 'path');
        path.setAttribute('d', 'M6 9.5l6 6 6-6');
        svg.appendChild(path);
        return svg;
    }

    function section(opts) {
        opts = opts || {};
        var id = String(opts.id || opts.title || '').slice(0, 120);
        if (!id) throw new Error('ui.section needs an id or a title');
        sectionStyles();

        var root = document.createElement('section');
        root.className = 'k8sd-section' + (opts.boxed ? ' k8sd-boxed' : '');
        root.dataset.section = id;

        var head = document.createElement('div');
        head.className = 'k8sd-section-head';
        var h3 = document.createElement('h3');
        var button = document.createElement('button');
        button.type = 'button';
        button.className = 'k8sd-fold';
        var chevron = document.createElement('span');
        chevron.className = 'k8sd-chevron';
        chevron.setAttribute('aria-hidden', 'true');
        chevron.appendChild(svgChevron());
        var label = document.createElement('span');
        label.className = 'k8sd-label';
        var title = document.createElement('span');
        var count = document.createElement('span');
        count.className = 'k8sd-count';
        var by = document.createElement('span');
        by.className = 'k8sd-by';
        label.append(title, count, by);
        var hint = document.createElement('span');
        hint.className = 'k8sd-hint';
        hint.textContent = 'Show';
        button.append(chevron, label, hint);
        h3.appendChild(button);
        var actions = document.createElement('div');
        actions.className = 'k8sd-section-actions';
        head.append(h3, actions);

        var body = document.createElement('div');
        body.className = 'k8sd-section-body';
        root.append(head, body);

        var what = opts.what || 'this section';
        var listeners = [];
        var sec = {
            id: id,
            root: root,
            startFolded: opts.open === false,
            touched: false,
            folded: false,
            set: function (folded, remember) {
                sec.folded = !!folded;
                root.classList.toggle('k8sd-folded', sec.folded);
                button.setAttribute('aria-expanded', String(!sec.folded));
                button.title = (sec.folded ? 'Show ' : 'Fold ') + what + (sec.folded ? ', for every object' : ' away, for every object');
                hint.hidden = !sec.folded;
                if (remember) {
                    sec.touched = true;
                    var marked = sec.startFolded ? !sec.folded : sec.folded;
                    if (folds) {
                        if (marked) folds[memoryKey(sec)] = true;
                        else delete folds[memoryKey(sec)];
                    }
                    call('section.fold', { id: memoryKey(sec), folded: marked }).catch(function () {});
                }
                listeners.forEach(function (fn) {
                    try {
                        fn(sec.folded);
                    } catch (err) {
                        console.error('k8sdockside: a ui.section listener failed', err);
                    }
                });
            },
        };
        // A page that redraws on every poll makes new sections each time:
        // only those still on the page are kept.
        if (sections.length > 32) {
            sections = sections.filter(function (other) {
                return other.root.isConnected;
            });
        }
        sections.push(sec);

        function setText(node, value) {
            node.textContent = value === undefined || value === null || value === '' ? '' : String(value);
            node.hidden = node.textContent === '';
        }
        setText(title, opts.title || id);
        setText(count, opts.count);
        setText(by, opts.by ? '· ' + opts.by : '');

        button.addEventListener('click', function () {
            sec.set(!sec.folded, true);
        });
        sec.set(folds ? isFolded(sec) : sec.startFolded, false);

        if (opts.parent) opts.parent.appendChild(root);

        return {
            /** The whole section, to put on the page. */
            root: root,
            /** Where its contents go. */
            body: body,
            /** Buttons on the heading's row, shown while it is open. */
            actions: actions,
            setTitle: function (value) {
                setText(title, value);
            },
            setCount: function (value) {
                setText(count, value);
            },
            isFolded: function () {
                return sec.folded;
            },
            setFolded: function (folded) {
                sec.set(folded, true);
            },
            /** Called with true when it folds and false when it opens. Returns an unsubscribe function. */
            onToggle: function (fn) {
                listeners.push(fn);
                return function () {
                    listeners = listeners.filter(function (f) {
                        return f !== fn;
                    });
                };
            },
        };
    }

    var ui = { section: section };

    var ready = call('hello').then(function (context) {
        whenLoaded(function () {
            keepFields(context.fields);
        });
        applyFolds(context.folded);
        applyTheme(context.theme);
        applyDateTime(context.datetime);
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
         *   readable, write, registries, releases, services, actions, plugin,
         *   theme, datetime }.
         *
         * `datetime` is how the user wants dates and times written:
         * { clock, dates, zone, ages }. Write them with `format` rather than
         * reading it yourself.
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
                // Paused while the window is hidden -- nobody sees the answer --
                // and resumed the moment it is shown.
                if (document.visibilityState === 'hidden') {
                    document.addEventListener('visibilitychange', function shown() {
                        if (document.visibilityState === 'hidden' || stopped) return;
                        document.removeEventListener('visibilitychange', shown);
                        tick();
                    });
                    return;
                }
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
         * The published releases of a public GitHub repository, asked for by
         * the app: the page itself has no network. Needs "ui": { "releases":
         * [{ "github": "owner/repo" }] } naming the repository.
         *
         * releases(repo, { refresh? }) -> [{ tag, name, prerelease, draft,
         * publishedAt, url }], newest first, never a draft. The list also
         * carries `repo`, `fetchedAt` (when GitHub last answered, RFC 3339)
         * and `stale` (true when GitHub could not be asked just now and this
         * is the last good list, `error` saying why). Rejects for a repository
         * the manifest does not name, when the user has switched release lists
         * off, or when GitHub cannot be reached and nothing is held: fall back
         * on what the plugin ships with.
         */
        releases: function (repo, opts) {
            var refresh = !!(opts && opts.refresh);
            return call('releases', { repo: String(repo || ''), refresh: refresh }).then(function (answer) {
                var list = ((answer && answer.releases) || []).map(function (r) {
                    return {
                        tag: r.tag,
                        name: r.name,
                        prerelease: !!r.prerelease,
                        draft: false,
                        publishedAt: r.publishedAt,
                        url: r.url,
                    };
                });
                list.repo = (answer && answer.repo) || String(repo || '');
                list.fetchedAt = (answer && answer.fetchedAt) || '';
                list.stale = !!(answer && answer.stale);
                list.error = (answer && answer.error) || '';
                return list;
            });
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

        /**
         * Command line tools on the user's machine, run by the app: the page
         * itself cannot start a process. Needs "ui": { "tools": [...] }
         * declaring the tool and exactly which commands. Desktop app only.
         */
        tools: {
            /** tool id -> { id, label, tool: { found, path, version, reason }, files: [{ id, label, path, source, exists }] }. */
            status: function (tool) {
                return call('tools.status', { tool: tool });
            },
            /** Asks the user, in a file dialog, for one of the tool's files for this cluster; resolves with the status. */
            chooseFile: function (tool, file) {
                return call('tools.chooseFile', { tool: tool, file: file });
            },
            /** Goes back to the manifest's default for one of the tool's files. */
            forgetFile: function (tool, file) {
                return call('tools.forgetFile', { tool: tool, file: file });
            },
            /**
             * Runs a "read" command: (tool, args) -> { stdout, stderr, code,
             * truncated }. A non-zero code resolves; a command the manifest
             * does not allow rejects.
             */
            exec: function (tool, args) {
                return call('tools.exec', { tool: tool, args: args || [] });
            },
            /** exec, then stdout as JSON; rejects with stderr when the code is not 0. */
            json: function (tool, args) {
                return call('tools.exec', { tool: tool, args: args || [] }).then(function (out) {
                    if (out.code !== 0) throw new Error(String(out.stderr || 'exit status ' + out.code).trim().slice(0, 500));
                    return JSON.parse(out.stdout);
                });
            },
            /**
             * Runs a command declared as an extraction -- a "read" entry
             * { command, extract } -- and resolves with only the fields it
             * names: [{ node, fields: { 'a.b': value }, error? }]. The app
             * parses the output itself; it never reaches the page. 0.1.33.
             */
            extract: function (tool, args) {
                return call('tools.extract', { tool: tool, args: args || [] });
            },
            /**
             * Asks to run a "run" command: { tool, args, defaults?, label?,
             * title?, danger?, confirm? }. The user sees the exact command
             * line; if they say yes it runs in the tool's console in the dock.
             * Rejects with "the command was declined" if they do not.
             */
            run: function (request) {
                return call('tools.run', request || {});
            },
            /** Opens the tool's console in the dock: { tool, defaults?, label? }. */
            console: function (request) {
                return call('tools.console', request || {});
            },
            /** Opens an "interactive" command in the user's own terminal: (tool, args). */
            external: function (tool, args) {
                return call('tools.external', { tool: tool, args: args || [] });
            },
        },

        /**
         * Writes dates and times the way the user chose to see them, in the
         * app and in every plugin: format.date(when), format.time(when,
         * { seconds }), format.dateTime(when, { seconds }), format.day(when),
         * format.age(when), format.moment(when) -> { text, title }, and
         * format.settings(). `when` is a Date, an RFC3339 string or
         * milliseconds. Follows the user's changes as they make them; listen
         * for 'datetime' to redraw.
         */
        format: format,

        /**
         * Pieces of the app's own interface, drawn the same way it draws
         * them. ui.section({ id, title, count?, by?, what?, open?, boxed?,
         * parent? }) is a block with a heading that folds it away, exactly
         * as the details panel's own sections do, remembered in the same
         * place. 0.1.36 and newer: check ui exists.
         */
        ui: ui,

        /**
         * What was typed into this page's fields when it was last on screen,
         * keyed '#id', 'name:name' or 'radio:name'. The SDK puts these back
         * by itself; this is for a page that builds a field long after it
         * loads and would rather set it from its own state.
         */
        fields: function () {
            return ready.then(function (ctx) {
                return Object.assign({}, ctx.fields || {});
            });
        },

        /** Listens for pushes from the app. Events: 'theme', 'datetime'. Returns an unsubscribe function. */
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
