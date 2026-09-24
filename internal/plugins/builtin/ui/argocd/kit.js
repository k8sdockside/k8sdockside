// The drawing kit the Argo CD pages share: icons, stacked bars, the honeycomb
// of applications, the tooltip, and the resource tree.
//
// Everything that came from the cluster is written with textContent, never
// innerHTML: the frame is sandboxed, but a page that let an application's name
// run as markup would be handing that name the bridge.
(function () {
    'use strict';

    var A = window.Argo;
    var SVG = 'http://www.w3.org/2000/svg';

    function el(tag, className, text) {
        var node = document.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined && text !== null) node.textContent = text;
        return node;
    }

    function add(parent) {
        for (var i = 1; i < arguments.length; i++) {
            var child = arguments[i];
            if (child === null || child === undefined || child === false) continue;
            parent.appendChild(typeof child === 'string' ? document.createTextNode(child) : child);
        }
        return parent;
    }

    function svg(tag, attrs) {
        var node = document.createElementNS(SVG, tag);
        Object.keys(attrs || {}).forEach(function (k) {
            node.setAttribute(k, attrs[k]);
        });
        return node;
    }

    // Single-stroke icons on a 24-unit grid.
    var ICONS = {
        logo: ['M12 3a7 7 0 0 1 7 7c0 3-2 5-2 7h-10c0-2-2-4-2-7a7 7 0 0 1 7-7z', 'M9 17v3', 'M12 17v4', 'M15 17v3', 'M9.5 10h.01', 'M14.5 10h.01'],
        app: ['M12 3l8 4.5v9L12 21l-8-4.5v-9z', 'M12 12l8-4.5', 'M12 12v9', 'M12 12L4 7.5'],
        git: ['M6 3v12', 'M18 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6z', 'M6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6z', 'M18 9a9 9 0 0 1-9 9'],
        sync: ['M20 11a8 8 0 0 0-14.9-3.5', 'M4 4v4h4', 'M4 13a8 8 0 0 0 14.9 3.5', 'M20 20v-4h-4'],
        refresh: ['M20 12a8 8 0 1 1-2.3-5.6', 'M20 4v4h-4'],
        heart: ['M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z'],
        cluster: ['M4 5h16v5H4z', 'M4 14h16v5H4z', 'M8 7.5h.01', 'M8 16.5h.01'],
        project: ['M3 7l2-3h5l2 3h9v12H3z'],
        alert: ['M12 3l10 18H2z', 'M12 10v4', 'M12 17.5h.01'],
        check: ['M4 12.5l5 5L20 6.5'],
        close: ['M6 6l12 12', 'M18 6L6 18'],
        arrow: ['M5 12h14', 'M13 6l6 6-6 6'],
        open: ['M14 4h6v6', 'M20 4l-9 9', 'M18 14v6H4V6h6'],
        edit: ['M4 20h4L19 9l-4-4L4 16z', 'M14 6l4 4'],
        clock: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 7v5l3 2'],
        search: ['M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14z', 'M20 20l-4-4'],
        pause: ['M8 5h3v14H8z', 'M13 5h3v14h-3z'],
        missing: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6', 'M12 17h.01'],
        cube: ['M12 3l8 4.5v9L12 21l-8-4.5v-9z'],
        link: ['M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1', 'M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1'],
        auto: ['M13 3L5 14h6l-1 7 8-11h-6z'],
        chart: ['M4 4v16h16', 'M8 15l3-4 3 2 5-6'],
        grid: ['M4 4h7v7H4z', 'M13 4h7v7h-7z', 'M4 13h7v7H4z', 'M13 13h7v7h-7z'],
        activity: ['M3 12h4l3-8 4 16 3-8h4'],
        image: ['M4 5h16v14H4z', 'M4 15l4-4 4 4 3-3 5 5'],
        filter: ['M4 5h16l-6 7v6l-4 2v-8z'],
        stack: ['M12 3l9 5-9 5-9-5z', 'M3 13l9 5 9-5'],
        plus: ['M12 5v14', 'M5 12h14'],
        sliders: ['M4 7h10', 'M18 7h2', 'M4 17h4', 'M12 17h8', 'M16 5v4', 'M8 15v4'],
    };

    function icon(name, className) {
        var node = svg('svg', { viewBox: '0 0 24 24', class: 'ico' + (className ? ' ' + className : ''), 'aria-hidden': 'true' });
        (ICONS[name] || ICONS.cube).forEach(function (d) {
            node.appendChild(svg('path', { d: d }));
        });
        return node;
    }

    var HEALTH_ICON = { Healthy: 'heart', Progressing: 'refresh', Degraded: 'alert', Suspended: 'pause', Missing: 'missing' };

    function healthChip(health) {
        return chip(health || 'Unknown', A.HEALTH_TONE[health] || 'muted', HEALTH_ICON[health] || 'missing');
    }

    function syncChip(sync) {
        return chip(sync === 'OutOfSync' ? 'Out of sync' : sync || 'Unknown', A.SYNC_TONE[sync] || 'muted', sync === 'Synced' ? 'check' : 'sync');
    }

    function chip(text, tone, iconName, title) {
        var node = el('span', 'chip' + (tone ? ' ' + tone : ''));
        if (iconName) node.appendChild(icon(iconName));
        node.appendChild(el('span', '', text));
        if (title) node.title = title;
        return node;
    }

    function button(text, className, iconName, onClick) {
        var node = el('button', className || '');
        node.type = 'button';
        if (iconName) node.appendChild(icon(iconName));
        if (text) node.appendChild(el('span', '', text));
        if (onClick) node.addEventListener('click', onClick);
        return node;
    }

    function link(text, onClick, title) {
        var node = el('button', 'link', text);
        node.type = 'button';
        if (title) node.title = title;
        node.addEventListener('click', onClick);
        return node;
    }

    // What the plugin is about, from its manifest's links: a row of links,
    // each opened in the user's browser. Null when the app did not say or the
    // manifest has none.
    function about(sdk, plugin, onError) {
        if (!plugin || !plugin.links || !plugin.links.length) return null;
        var row = el('div', 'about');
        row.appendChild(el('span', 'about-label', plugin.version ? 'Plugin ' + plugin.version + ' ·' : 'About'));
        plugin.links.forEach(function (l) {
            row.appendChild(
                link(
                    l.label,
                    function () {
                        sdk.openUrl(l.url).catch(onError || function () {});
                    },
                    l.url,
                ),
            );
        });
        return row;
    }

    function age(timestamp) {
        var t = A.time(timestamp);
        if (!t) return '';
        var seconds = Math.max(0, (Date.now() - t) / 1000);
        if (seconds < 10) return 'just now';
        if (seconds < 90) return Math.round(seconds) + 's ago';
        if (seconds < 5400) return Math.round(seconds / 60) + 'm ago';
        if (seconds < 172800) return Math.round(seconds / 3600) + 'h ago';
        return Math.round(seconds / 86400) + 'd ago';
    }

    function plural(n, one, many) {
        return n + ' ' + (n === 1 ? one : many || one + 's');
    }

    // A stacked bar of counts, worst first, each segment labelled underneath.
    // segments: [{ label, count, tone }]
    function stackBar(segments, onPick) {
        var total = segments.reduce(function (s, x) {
            return s + x.count;
        }, 0);
        var wrap = el('div', 'stack');
        var bar = el('div', 'stack-bar');
        var keys = el('div', 'stack-keys');
        segments.forEach(function (seg) {
            if (!seg.count) return;
            var part = el('i', 'seg ' + seg.tone);
            part.style.flexGrow = String(seg.count);
            part.title = seg.label + ': ' + seg.count;
            bar.appendChild(part);
            var key = onPick ? button('', 'stack-key', null, function () {
                onPick(seg);
            }) : el('span', 'stack-key');
            add(key, el('i', 'dot ' + seg.tone), el('span', '', seg.label), el('strong', '', String(seg.count)));
            keys.appendChild(key);
        });
        if (!total) bar.appendChild(el('i', 'seg muted empty'));
        add(wrap, bar, keys);
        return wrap;
    }

    // ----- the honeycomb -----------------------------------------------------

    // Every application as a hexagon, worst first, filled by its health and
    // ringed in amber when it has drifted from Git. Sized to fit: a cluster
    // with four hundred applications gets smaller cells, not a scrollbar.
    // opts: { width, onPick(app), onHover(app, event) }
    function honeycomb(apps, opts) {
        var width = Math.max(200, opts.width || 400);
        var n = Math.max(1, apps.length);
        var r = 17;
        var cols;
        for (;;) {
            var w0 = Math.sqrt(3) * r;
            cols = Math.max(1, Math.floor((width - w0 / 2) / (w0 + 3)));
            var rows = Math.ceil(n / cols);
            if (rows * (r * 1.5 + 3) < (opts.maxHeight || 230) || r <= 7) break;
            r -= 1;
        }
        var w = Math.sqrt(3) * r;
        var stepX = w + 3;
        var stepY = r * 1.5 + 3;
        var rowsUsed = Math.ceil(apps.length / cols);
        var H = rowsUsed * stepY + r * 0.5 + 6;
        var W = cols * stepX + stepX / 2 + 4;

        var node = svg('svg', { viewBox: '0 0 ' + W + ' ' + H, class: 'hive', role: 'img' });
        node.setAttribute('aria-label', apps.length + ' applications');
        node.style.maxWidth = W + 'px';

        function hexPath(cx, cy, rr) {
            var pts = [];
            for (var i = 0; i < 6; i++) {
                var a = (Math.PI / 180) * (60 * i - 90);
                pts.push((cx + rr * Math.cos(a)).toFixed(1) + ',' + (cy + rr * Math.sin(a)).toFixed(1));
            }
            return pts.join(' ');
        }

        apps.forEach(function (app, i) {
            var row = Math.floor(i / cols);
            var col = i % cols;
            var cx = col * stepX + w / 2 + 2 + (row % 2 ? stepX / 2 : 0);
            var cy = row * stepY + r + 3;
            var cell = svg('polygon', {
                points: hexPath(cx, cy, r),
                class: 'cell ' + (A.HEALTH_TONE[app.health] || 'muted') + (app.sync === 'OutOfSync' ? ' drift' : '') + (A.opRunning(app) ? ' busy' : ''),
                tabindex: '0',
                role: 'button',
            });
            cell.setAttribute('aria-label', app.name + ', ' + (app.health || 'Unknown') + ', ' + (app.sync || 'Unknown'));
            cell.dataset.key = app.key;
            node.appendChild(cell);
        });

        node.addEventListener('click', function (event) {
            var cell = event.target.closest && event.target.closest('.cell');
            if (cell && opts.onPick) opts.onPick(cell.dataset.key);
        });
        node.addEventListener('keydown', function (event) {
            if ((event.key === 'Enter' || event.key === ' ') && event.target.classList.contains('cell') && opts.onPick) {
                event.preventDefault();
                opts.onPick(event.target.dataset.key);
            }
        });
        return node;
    }

    // ----- the tooltip -------------------------------------------------------

    // One floating tip for a root, filled by `describe` for whatever element
    // matching `selector` is under the pointer.
    function tooltip(root, selector, describe) {
        var tip = el('div', 'tip');
        tip.hidden = true;
        tip.setAttribute('role', 'tooltip');
        document.body.appendChild(tip);

        function place(event) {
            var pad = 14;
            var x = event.clientX + pad;
            var y = event.clientY + pad;
            if (x + tip.offsetWidth > window.innerWidth - 8) x = event.clientX - tip.offsetWidth - pad;
            if (y + tip.offsetHeight > window.innerHeight - 8) y = event.clientY - tip.offsetHeight - pad;
            tip.style.left = Math.max(8, x) + 'px';
            tip.style.top = Math.max(8, y) + 'px';
        }

        root.addEventListener('pointerover', function (event) {
            var target = event.target.closest && event.target.closest(selector);
            if (!target) return;
            tip.textContent = '';
            if (!describe(target, tip)) {
                tip.hidden = true;
                return;
            }
            tip.hidden = false;
            place(event);
        });
        root.addEventListener('pointermove', function (event) {
            if (!tip.hidden) place(event);
        });
        root.addEventListener('pointerout', function (event) {
            var target = event.target.closest && event.target.closest(selector);
            if (target && !target.contains(event.relatedTarget)) tip.hidden = true;
        });
        return tip;
    }

    function describeApp(app, into) {
        add(into, el('strong', 'tip-title', app.name), el('div', 'tip-sub', app.project + ' · ' + A.destination(app)));
        var chips = el('div', 'tip-chips');
        add(chips, healthChip(app.health), syncChip(app.sync));
        if (app.auto) chips.appendChild(chip('auto', 'muted', 'auto'));
        into.appendChild(chips);
        if (app.revision) into.appendChild(el('div', 'tip-sub mono', A.short(app.revision) + (app.history[0] ? ' · deployed ' + age(app.history[0].deployedAt) : '')));
        if (app.healthMessage && app.health !== 'Healthy') into.appendChild(el('div', 'tip-note', app.healthMessage));
        return true;
    }

    // ----- the resource tree -------------------------------------------------

    // What an application deploys, as a tree: the application, then one node
    // per kind, then each resource -- coloured by health, dashed where it
    // differs from Git. Argo CD's own tree also follows owner references down
    // to pods; status.resources is what the Application object carries, which
    // is the top of that tree and the part Git describes.
    // opts: { width, readable, onOpen(ref) }
    function resourceTree(app, opts) {
        var MAX = 36;
        var byKind = {};
        app.resources.forEach(function (r) {
            (byKind[r.kind] = byKind[r.kind] || []).push(r);
        });
        var kinds = Object.keys(byKind).sort(function (a, b) {
            return worstKind(byKind[a]) - worstKind(byKind[b]) || a.localeCompare(b);
        });

        var rows = [];
        var shown = 0;
        kinds.forEach(function (kind) {
            var list = byKind[kind].slice().sort(function (a, b) {
                return healthRank(a) - healthRank(b) || a.name.localeCompare(b.name);
            });
            var room = Math.max(1, MAX - shown);
            var take = list.slice(0, Math.min(list.length, room));
            shown += take.length;
            rows.push({ kind: kind, items: take, more: list.length - take.length, total: list.length });
        });

        var width = Math.max(300, opts.width || 400);
        var rowH = 24;
        var groupGap = 8;
        var x0 = 4;
        var x1 = Math.round(width * 0.3);
        var x2 = Math.round(width * 0.56);
        var y = 4;
        rows.forEach(function (g) {
            g.top = y;
            g.lines = g.items.length + (g.more > 0 ? 1 : 0);
            y += g.lines * rowH + groupGap;
        });
        var H = Math.max(rowH * 2, y);

        var node = svg('svg', { viewBox: '0 0 ' + width + ' ' + H, class: 'tree', width: width, height: H });
        var rootY = H / 2;

        function text(x, yy, value, cls, max) {
            var t = svg('text', { x: x, y: yy, class: cls || '' });
            var s = String(value);
            if (max && s.length > max) s = s.slice(0, max - 1) + '…';
            t.textContent = s;
            if (s !== String(value)) {
                var title = svg('title', {});
                title.textContent = String(value);
                t.appendChild(title);
            }
            return t;
        }

        function curve(ax, ay, bx, by, cls) {
            var bend = (bx - ax) / 2;
            return svg('path', { d: 'M' + ax + ' ' + ay + ' C' + (ax + bend) + ' ' + ay + ' ' + (bx - bend) + ' ' + by + ' ' + bx + ' ' + by, class: 'edge ' + (cls || '') });
        }

        // The application itself.
        var root = svg('g', { class: 'node root ' + (A.HEALTH_TONE[app.health] || 'muted') });
        root.appendChild(svg('rect', { x: x0, y: rootY - 13, width: x1 - x0 - 18, height: 26, rx: 7 }));
        root.appendChild(text(x0 + 9, rootY + 4, app.name, 'label', Math.max(6, Math.floor((x1 - x0 - 30) / 6.6))));
        node.appendChild(root);

        rows.forEach(function (g) {
            var gy = g.top + (g.lines * rowH) / 2 - 2;
            node.insertBefore(curve(x1 - 14, rootY, x1, gy, ''), node.firstChild);
            var kindNode = svg('g', { class: 'node kind ' + toneOf(worstKind(g.items)) });
            kindNode.appendChild(svg('rect', { x: x1, y: gy - 11, width: x2 - x1 - 18, height: 22, rx: 11 }));
            kindNode.appendChild(text(x1 + 10, gy + 4, g.kind + ' ' + g.total, 'label', Math.max(6, Math.floor((x2 - x1 - 32) / 6.4))));
            node.appendChild(kindNode);

            g.items.forEach(function (r, i) {
                var ry = g.top + i * rowH + rowH / 2 - 2;
                var tone = A.HEALTH_TONE[(r.health && r.health.status) || ''] || (r.status === 'Synced' ? 'ok' : 'muted');
                node.insertBefore(curve(x2 - 18, gy, x2, ry, r.status === 'OutOfSync' ? 'drift' : ''), node.firstChild);
                var leaf = svg('g', { class: 'node leaf ' + tone + (r.status === 'OutOfSync' ? ' drift' : '') });
                leaf.appendChild(svg('circle', { cx: x2 + 7, cy: ry, r: 5 }));
                leaf.appendChild(text(x2 + 18, ry + 4, r.name + (r.namespace && r.namespace !== app.destNamespace ? '  · ' + r.namespace : ''), 'label', Math.max(8, Math.floor((width - x2 - 24) / 6.4))));
                var appKind = A.APP_KIND[r.kind];
                if (appKind && opts.readable && opts.readable.indexOf(appKind) >= 0) {
                    leaf.classList.add('openable');
                    leaf.setAttribute('tabindex', '0');
                    leaf.setAttribute('role', 'button');
                    var go = function () {
                        opts.onOpen({ kind: appKind, namespace: r.namespace || '', name: r.name });
                    };
                    leaf.addEventListener('click', go);
                    leaf.addEventListener('keydown', function (e) {
                        if (e.key === 'Enter') go();
                    });
                }
                var tip = svg('title', {});
                tip.textContent = r.kind + ' ' + (r.namespace ? r.namespace + '/' : '') + r.name + ' — ' + ((r.health && r.health.status) || 'no health') + ', ' + (r.status || 'unknown sync') + (r.health && r.health.message ? '\n' + r.health.message : '');
                leaf.appendChild(tip);
                node.appendChild(leaf);
            });
            if (g.more > 0) {
                node.appendChild(text(x2 + 18, g.top + g.items.length * rowH + rowH / 2 + 2, '+ ' + g.more + ' more', 'more'));
            }
        });
        return node;
    }

    function healthRank(r) {
        var h = (r.health && r.health.status) || '';
        var rank = A.HEALTH_RANK[h];
        return (rank === undefined ? 4 : rank) - (r.status === 'OutOfSync' ? 0.5 : 0);
    }

    function worstKind(list) {
        return list.reduce(function (m, r) {
            return Math.min(m, healthRank(r));
        }, 9);
    }

    function toneOf(rank) {
        if (rank < 0.6) return 'error';
        if (rank < 1.6) return 'warn';
        if (rank < 2.6) return 'info';
        if (rank < 4.6) return 'muted';
        return 'ok';
    }

    // Asks the app to apply a patch. The app shows it to the user first; a
    // "no" is not an error worth showing.
    function apply(sdk, ref, patch) {
        return sdk.patch({ kind: ref.kind, namespace: ref.namespace, name: ref.name, patch: patch }).then(
            function () {
                return true;
            },
            function (err) {
                if (/declined/.test(err.message)) return false;
                throw err;
            },
        );
    }

    // ----- the grip ----------------------------------------------------------

    // A grab strip on the inner edge of a panel on the right: drag it, or
    // focus it and use the arrow keys, to make the panel wider or narrower; a
    // double-click puts it back. The width is a custom property on the root,
    // so the panel and whatever makes room for it read the one value -- unset,
    // the stylesheet's own width stands. The page places the strip, and keeps
    // the width wherever it keeps the rest of its state.
    // opts: { panel, prop, min, room, initial, label, className, onResize(px, done) }
    //   room      what the panel always leaves of the window
    //   onResize  px is 0 once the panel is back to the stylesheet's width;
    //             done is false while a drag goes on, true when it ends
    function grip(opts) {
        var root = document.documentElement;
        var node = el('div', 'grip' + (opts.className ? ' ' + opts.className : ''));
        node.tabIndex = 0;
        node.title = 'Drag to resize · double-click to reset';
        node.setAttribute('role', 'separator');
        node.setAttribute('aria-orientation', 'vertical');
        node.setAttribute('aria-label', opts.label || 'Resize the panel');
        var wanted = opts.initial > 0 ? opts.initial : 0;
        var drag = null;
        var frame = 0;

        function fit(px) {
            return Math.round(Math.max(opts.min, Math.min(px, window.innerWidth - opts.room)));
        }

        // The width asked for is kept as asked: a smaller window takes what it
        // must from the panel and gives it back when it grows again.
        function apply() {
            if (wanted) root.style.setProperty(opts.prop, fit(wanted) + 'px');
            else root.style.removeProperty(opts.prop);
        }

        // While a drag goes on the page hears of it once a frame at most.
        function tell(done) {
            if (!opts.onResize) return;
            cancelAnimationFrame(frame);
            frame = 0;
            if (done) opts.onResize(wanted, true);
            else
                frame = requestAnimationFrame(function () {
                    frame = 0;
                    opts.onResize(wanted, false);
                });
        }

        function resize(px, done) {
            wanted = px ? fit(px) : 0;
            apply();
            tell(done);
        }

        function release(event) {
            if (!drag) return;
            drag = null;
            document.body.classList.remove('gripping');
            if (node.hasPointerCapture(event.pointerId)) node.releasePointerCapture(event.pointerId);
            tell(true);
        }

        node.addEventListener('pointerdown', function (event) {
            if (event.button !== 0) return;
            event.preventDefault();
            node.setPointerCapture(event.pointerId);
            drag = { x: event.clientX, from: opts.panel.getBoundingClientRect().width };
            document.body.classList.add('gripping');
        });
        node.addEventListener('pointermove', function (event) {
            // The panel is on the right: it grows as the pointer goes left.
            if (drag) resize(drag.from + drag.x - event.clientX, false);
        });
        node.addEventListener('pointerup', release);
        node.addEventListener('pointercancel', release);
        node.addEventListener('dblclick', function () {
            resize(0, true);
        });
        node.addEventListener('keydown', function (event) {
            var step = event.shiftKey ? 48 : 16;
            var now = wanted || opts.panel.getBoundingClientRect().width;
            if (event.key === 'ArrowLeft') resize(now + step, true);
            else if (event.key === 'ArrowRight') resize(now - step, true);
            else return;
            event.preventDefault();
        });
        window.addEventListener('resize', function () {
            if (!wanted) return;
            apply();
            tell(false);
        });
        apply();
        return node;
    }

    window.ArgoKit = {
        el: el,
        add: add,
        svg: svg,
        icon: icon,
        chip: chip,
        healthChip: healthChip,
        syncChip: syncChip,
        button: button,
        link: link,
        about: about,
        age: age,
        plural: plural,
        stackBar: stackBar,
        honeycomb: honeycomb,
        tooltip: tooltip,
        describeApp: describeApp,
        resourceTree: resourceTree,
        apply: apply,
        grip: grip,
    };
})();
