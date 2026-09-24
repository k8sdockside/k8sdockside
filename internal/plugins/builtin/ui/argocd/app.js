// An Application's panel in its detail view: health and sync at a glance, what
// it deploys, what its last sync did and what has been deployed, and the
// plugin's own buttons for it -- the same Sync and Refresh as on the action
// bar, which the app asks about before running.
(function () {
    'use strict';

    var sdk = window.k8sdockside;
    var A = window.Argo;
    var K = window.ArgoKit;
    var el = K.el;
    var add = K.add;
    var POLL = 4000;

    var state = { ctx: null, sig: '', busy: false, last: null, treeOpen: null };

    var $ = function (id) {
        return document.getElementById(id);
    };

    function fail(err) {
        $('error').textContent = (err && err.message) || String(err);
        $('error').hidden = false;
    }

    function open(ref) {
        sdk.open(ref).catch(fail);
    }

    var ACTION_ICON = { sync: 'sync', refresh: 'refresh', 'hard-refresh': 'refresh' };

    function run(action) {
        state.busy = true;
        if (state.last) draw(state.last.app, state.last.offered);
        sdk.run(action.id)
            .catch(function (err) {
                if (!/declined/.test(err.message)) fail(err);
            })
            .then(function () {
                state.busy = false;
                state.sig = '';
                tick();
            });
    }

    function draw(app, offered) {
        state.last = { app: app, offered: offered };
        var root = $('root');
        root.textContent = '';

        var head = el('div', 'panel-head');
        add(head, K.healthChip(app.health), K.syncChip(app.sync));
        if (app.auto) head.appendChild(K.chip('auto-sync', 'muted', 'auto'));
        if (A.opRunning(app)) head.appendChild(K.chip('syncing', 'info', 'sync'));
        if (app.revision) head.appendChild(el('code', 'rev', A.short(app.revision)));
        var buttons = el('div', 'panel-buttons');
        (offered || []).forEach(function (action) {
            var b = K.button(action.label, action.id === 'sync' ? 'small primary' : 'small', ACTION_ICON[action.id] || 'sync', function () {
                run(action);
            });
            b.disabled = state.busy;
            buttons.appendChild(b);
        });
        head.appendChild(buttons);
        root.appendChild(head);

        if (app.healthMessage && app.health !== 'Healthy') {
            var why = el('div', 'why ' + (A.HEALTH_TONE[app.health] || 'muted'));
            add(why, K.icon('alert'), el('span', '', app.healthMessage));
            root.appendChild(why);
        }

        var route = el('div', 'panel-route');
        add(route, K.icon('git'), el('span', '', A.source(app)), K.icon('arrow', 'route-arrow'), K.icon('cluster'), el('span', '', A.destination(app)));
        root.appendChild(route);

        // What it deploys: health across its resources, then the tree.
        var rh = app.resourceHealth;
        var total = app.resources.length;
        if (total) {
            var bars = el('div', 'panel-bars');
            add(
                bars,
                K.stackBar([
                    { label: 'Degraded', count: rh.Degraded || 0, tone: 'error' },
                    { label: 'Missing', count: rh.Missing || 0, tone: 'warn' },
                    { label: 'Progressing', count: rh.Progressing || 0, tone: 'info' },
                    { label: 'Suspended', count: rh.Suspended || 0, tone: 'paused' },
                    { label: 'Healthy', count: rh.Healthy || 0, tone: 'ok' },
                    { label: 'no health', count: rh[''] || 0, tone: 'muted' },
                ]),
            );
            root.appendChild(bars);

            var details = el('details', 'panel-tree');
            // Open for a small application, folded for a big one, and then
            // however the reader last left it.
            details.open = state.treeOpen === null ? total <= 16 : state.treeOpen;
            details.addEventListener('toggle', function () {
                state.treeOpen = details.open;
            });
            var summary = el('summary', '', K.plural(total, 'resource') + (app.outOfSync ? ' · ' + app.outOfSync + ' out of sync' : ''));
            details.appendChild(summary);
            var box = el('div', 'tree-box');
            box.appendChild(K.resourceTree(app, { width: Math.max(320, document.body.clientWidth - 30), readable: state.ctx.readable, onOpen: open }));
            details.appendChild(box);
            root.appendChild(details);
        }

        if (app.op) {
            var op = el('div', 'panel-op');
            add(op, el('span', 'panel-label', 'Last sync'), K.chip(app.op.phase || 'Unknown', A.opFailed(app) ? 'error' : A.opRunning(app) ? 'info' : 'ok'), el('span', 'faint', K.age(app.op.finishedAt || app.op.startedAt)));
            root.appendChild(op);
            if (app.op.message && (A.opFailed(app) || A.opRunning(app))) root.appendChild(el('div', 'op-message', app.op.message));
        }

        if (app.history.length) {
            var hist = el('div', 'panel-history');
            hist.appendChild(el('span', 'panel-label', 'Deployed'));
            app.history.slice(0, 4).forEach(function (h, i) {
                var item = el('span', 'hist' + (i === 0 ? ' current' : ''));
                add(item, el('code', 'rev', A.short(h.revision) || '—'), el('span', 'faint', K.age(h.deployedAt)));
                hist.appendChild(item);
            });
            root.appendChild(hist);
        }

        if (app.urls.length) {
            var links = el('div', 'links');
            app.urls.forEach(function (u) {
                links.appendChild(
                    K.button(u.replace(/^https?:\/\//, ''), 'link-chip', 'link', function () {
                        sdk.openUrl(u).catch(fail);
                    }),
                );
            });
            root.appendChild(links);
        }
    }

    function tick() {
        Promise.all([sdk.object(), sdk.actions().catch(function () {
            return [];
        })])
            .then(function (got) {
                $('error').hidden = true;
                var obj = got[0];
                var sig = obj.metadata.resourceVersion + '|' + JSON.stringify(got[1]) + '|' + state.busy;
                if (sig === state.sig) return;
                state.sig = sig;
                draw(A.buildApp(obj), got[1]);
            })
            .catch(fail);
    }

    sdk.ready()
        .then(function (context) {
            state.ctx = context;
            if (!context.object) {
                fail(new Error('This page is a panel, drawn for one Application.'));
                return;
            }
            tick();
            setInterval(function () {
                if (!state.busy && document.visibilityState !== 'hidden') tick();
            }, POLL);
            // Shown again after being hidden: read now rather than at the next turn.
            document.addEventListener('visibilitychange', function () {
                if (document.visibilityState !== 'hidden' && !state.busy) tick();
            });
        })
        .catch(fail);
})();
