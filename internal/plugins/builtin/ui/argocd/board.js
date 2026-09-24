// The application board: every Application as a tile, grouped by project,
// health or repository, narrowed from a rail of counts -- and one of them in
// full in a drawer: what it deploys as a tree, where from and to, what its
// last sync did, what has been deployed, with Sync and Refresh one click away.
(function () {
    'use strict';

    var sdk = window.k8sdockside;
    var A = window.Argo;
    var K = window.ArgoKit;
    var el = K.el;
    var add = K.add;
    var POLL = 5000;

    var GROUPS = [
        { id: 'project', label: 'Project' },
        { id: 'health', label: 'Health' },
        { id: 'repo', label: 'Repository' },
    ];
    var HEALTHS = ['Degraded', 'Missing', 'Progressing', 'Suspended', 'Unknown', 'Healthy'];
    var SYNCS = ['OutOfSync', 'Synced', 'Unknown'];

    var state = {
        ctx: null,
        model: null,
        sig: '',
        query: '',
        group: 'project',
        health: '',
        sync: '',
        project: '',
        selected: '',
        notice: '',
        // How wide the drawer was dragged; empty is the stylesheet's width.
        width: '',
        // Whether the drawer's Overrides section is open: '1', or empty.
        overrides: '',
    };

    // The drawer's Overrides section: the same view the detail panel has
    // (overrides-view.js), for the application in the drawer. Kept across the
    // drawer's redraws -- it holds whatever is being typed into it -- and
    // made afresh only when another application is chosen.
    var ov = { key: '', view: null, fold: null };

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

    // The selection and the filters live in the frame's own hash, so switching
    // tabs away and back -- which unloads the page -- comes back to them.
    var REMEMBERED = ['group', 'health', 'sync', 'project', 'selected', 'width', 'overrides'];

    function saveHash() {
        var parts = [];
        REMEMBERED.forEach(function (k) {
            if (state[k]) parts.push(k + '=' + encodeURIComponent(state[k]));
        });
        try {
            history.replaceState(null, '', '#' + parts.join('&'));
        } catch (e) {
            // A sandboxed frame may refuse; the board still works.
        }
    }

    function loadHash() {
        (location.hash || '').replace(/^#/, '').split('&').forEach(function (pair) {
            var cut = pair.split('=');
            if (cut.length === 2 && REMEMBERED.indexOf(cut[0]) >= 0) state[cut[0]] = decodeURIComponent(cut[1]);
        });
        if (!GROUPS.some(function (g) { return g.id === state.group; })) state.group = 'project';
    }

    function healthOf(app) {
        return app.health || 'Unknown';
    }

    function syncOf(app) {
        return app.sync || 'Unknown';
    }

    function matches(app) {
        if (state.health && healthOf(app) !== state.health) return false;
        if (state.sync && syncOf(app) !== state.sync) return false;
        if (state.project && app.project !== state.project) return false;
        var q = state.query.trim().toLowerCase();
        if (!q) return true;
        return [app.name, app.namespace, app.project, app.repo, app.path, app.chart, app.destNamespace, app.destName].some(function (v) {
            return String(v || '').toLowerCase().indexOf(q) >= 0;
        });
    }

    // ----- the rail ----------------------------------------------------------

    function drawRail(model) {
        var rail = $('rail');
        rail.textContent = '';

        function section(title, items, current, key) {
            var box = el('div', 'rail-section');
            box.appendChild(el('div', 'rail-title', title));
            items.forEach(function (it) {
                var b = K.button('', 'rail-item' + (current === it.value ? ' on' : '') + (it.count ? '' : ' zero'), null, function () {
                    state[key] = state[key] === it.value ? '' : it.value;
                    saveHash();
                    render();
                });
                b.setAttribute('aria-pressed', current === it.value ? 'true' : 'false');
                if (it.tone) b.appendChild(el('i', 'dot ' + it.tone));
                add(b, el('span', 'rail-label', it.label), el('span', 'rail-count', String(it.count)));
                box.appendChild(b);
            });
            rail.appendChild(box);
        }

        var total = model.apps.length;
        var all = K.button('', 'rail-item all' + (!state.health && !state.sync && !state.project ? ' on' : ''), null, function () {
            state.health = state.sync = state.project = '';
            saveHash();
            render();
        });
        add(all, el('span', 'rail-label', 'All applications'), el('span', 'rail-count', String(total)));
        rail.appendChild(all);

        section(
            'Health',
            HEALTHS.map(function (h) {
                return { value: h, label: h, tone: A.HEALTH_TONE[h === 'Unknown' ? '' : h], count: model.apps.filter(function (a) { return healthOf(a) === h; }).length };
            }).filter(function (it) {
                return it.count || it.value === 'Degraded' || it.value === 'Healthy';
            }),
            state.health,
            'health',
        );
        section(
            'Sync',
            SYNCS.map(function (s) {
                return { value: s, label: s === 'OutOfSync' ? 'Out of sync' : s, tone: A.SYNC_TONE[s], count: model.apps.filter(function (a) { return syncOf(a) === s; }).length };
            }).filter(function (it) {
                return it.count || it.value !== 'Unknown';
            }),
            state.sync,
            'sync',
        );
        section(
            'Projects',
            model.projects.map(function (p) {
                return { value: p.name, label: p.name, count: p.apps.length };
            }),
            state.project,
            'project',
        );
    }

    // ----- the tiles ---------------------------------------------------------

    function tile(app) {
        var tone = A.HEALTH_TONE[app.health] || 'muted';
        var t = K.button('', 'tile ' + tone + (app.sync === 'OutOfSync' ? ' drift' : '') + (state.selected === app.key ? ' sel' : ''), null, function () {
            state.selected = state.selected === app.key ? '' : app.key;
            saveHash();
            drawGroups(state.model);
            drawDrawer(state.model);
        });
        t.dataset.key = app.key;
        var head = el('span', 'tile-head');
        add(head, el('span', 'tile-name', app.name));
        if (app.auto) head.appendChild(K.icon('auto', 'tile-auto'));
        if (A.opRunning(app)) head.appendChild(K.icon('sync', 'tile-spin'));
        t.appendChild(head);
        t.appendChild(el('span', 'tile-sub', A.destination(app)));
        var chips = el('span', 'tile-chips');
        add(chips, K.healthChip(app.health), K.syncChip(app.sync));
        t.appendChild(chips);
        var foot = el('span', 'tile-foot');
        if (app.revision) foot.appendChild(el('code', 'rev', A.short(app.revision)));
        var last = app.history[0];
        foot.appendChild(el('span', '', last ? 'deployed ' + K.age(last.deployedAt) : 'never deployed'));
        if (A.opFailed(app)) foot.appendChild(K.chip('sync failed', 'error', 'alert'));
        t.appendChild(foot);
        return t;
    }

    function groupKey(app) {
        if (state.group === 'health') return healthOf(app);
        if (state.group === 'repo') return A.repoLabel(app.repo) || '(no repository)';
        return app.project;
    }

    function drawGroups(model) {
        var root = $('groups');
        root.textContent = '';
        var shown = model.apps.filter(matches);
        if (!shown.length) {
            root.appendChild(el('p', 'quiet nothing', model.apps.length ? 'No application matches.' : 'No applications in this cluster yet.'));
            return;
        }
        var groups = {};
        var order = [];
        shown.forEach(function (app) {
            var k = groupKey(app);
            if (!groups[k]) {
                groups[k] = [];
                order.push(k);
            }
            groups[k].push(app);
        });
        if (state.group === 'health') {
            order.sort(function (a, b) {
                return HEALTHS.indexOf(a) - HEALTHS.indexOf(b);
            });
        } else {
            order.sort(function (a, b) {
                return A.worstFirst(groups[a][0], groups[b][0]) || a.localeCompare(b);
            });
        }

        order.forEach(function (k) {
            var list = groups[k];
            var sec = el('section', 'group');
            var head = el('div', 'group-head');
            var tally = {};
            list.forEach(function (a) {
                var t = A.HEALTH_TONE[a.health] || 'muted';
                tally[t] = (tally[t] || 0) + 1;
            });
            var bar = el('span', 'mini-bar');
            ['error', 'warn', 'info', 'paused', 'muted', 'ok'].forEach(function (t) {
                if (!tally[t]) return;
                var seg = el('i', 'seg ' + t);
                seg.style.flexGrow = String(tally[t]);
                bar.appendChild(seg);
            });
            add(head, K.icon(state.group === 'repo' ? 'git' : state.group === 'health' ? 'heart' : 'project'), el('h2', '', k), el('span', 'faint', K.plural(list.length, 'app')), bar);
            sec.appendChild(head);
            var grid = el('div', 'tiles');
            list.forEach(function (app) {
                grid.appendChild(tile(app));
            });
            sec.appendChild(grid);
            root.appendChild(sec);
        });
    }

    // ----- the drawer --------------------------------------------------------

    function act(app, what) {
        var patch = what === 'sync' ? A.patches.sync() : A.patches.refresh(what === 'hard');
        K.apply(sdk, A.appRef(app), patch)
            .then(function (done) {
                if (!done) return;
                state.notice = { sync: 'Sync requested for ', refresh: 'Refresh requested for ', hard: 'Hard refresh requested for ' }[what] + app.name + '.';
                drawDrawer(state.model);
                setTimeout(function () {
                    state.notice = '';
                    drawDrawer(state.model);
                }, 5000);
            })
            .catch(fail);
    }

    function fact(label, value) {
        var row = el('div', 'fact-row');
        add(row, el('span', 'fact-label', label), typeof value === 'string' ? el('span', 'fact-value', value) : value);
        return row;
    }

    // The tree is drawn to the drawer's width, so it is drawn again whenever
    // the drawer is made wider or narrower.
    function drawTree() {
        var box = $('tree-box');
        var app = state.model && state.selected && state.model.apps.find(function (a) {
            return a.key === state.selected;
        });
        if (!box || !app || $('drawer').hidden) return;
        box.textContent = '';
        box.appendChild(
            K.resourceTree(app, {
                width: Math.max(320, ($('drawer').clientWidth || 460) - 36),
                readable: state.ctx.readable,
                onOpen: open,
            }),
        );
    }

    /**
     * The Overrides section for an application: a fold holding its Overrides
     * view, read only while the fold is open.
     */
    function overridesFold(app) {
        if (ov.key === app.key && ov.fold) return ov.fold;
        dropOverrides();
        var ref = A.appRef(app);
        var view = window.ArgoOverridesView.create({
            sdk: sdk,
            ref: ref,
            read: function () {
                return sdk.get(ref);
            },
            write: !!state.ctx.write,
            onError: fail,
        });
        var fold = el('details', 'drawer-fold');
        var summary = el('summary', 'drawer-section', 'Overrides');
        summary.appendChild(el('span', 'faint', ' target revision, Helm and Kustomize'));
        fold.appendChild(summary);
        fold.appendChild(view.node);
        fold.open = state.overrides === '1';
        if (fold.open) view.start();
        fold.addEventListener('toggle', function () {
            state.overrides = fold.open ? '1' : '';
            saveHash();
            if (fold.open) view.start();
            else view.stop();
        });
        ov = { key: app.key, view: view, fold: fold };
        return fold;
    }

    function dropOverrides() {
        if (ov.view) ov.view.stop();
        if (ov.fold && ov.fold.parentNode) ov.fold.parentNode.removeChild(ov.fold);
        ov = { key: '', view: null, fold: null };
    }

    function drawDrawer(model) {
        var drawer = $('drawer');
        var app = state.selected && model.apps.find(function (a) {
            return a.key === state.selected;
        });
        drawer.hidden = !app;
        document.body.classList.toggle('drawer-open', !!app);
        if (!app) {
            dropOverrides();
            return;
        }

        // Everything is drawn afresh except the Overrides section, which is
        // left where it is: taking it out and putting it back would lose the
        // caret, and a redraw can come at any moment the cluster changes.
        var fold = overridesFold(app);
        var kept = fold.parentNode === drawer ? fold : null;
        Array.prototype.slice.call(drawer.childNodes).forEach(function (n) {
            if (n !== kept) drawer.removeChild(n);
        });
        var placeholder = el('div', '');
        var out = document.createDocumentFragment();
        var target = drawer;
        drawer = out;

        var head = el('header', 'drawer-head ' + (A.HEALTH_TONE[app.health] || 'muted'));
        var title = el('div', 'drawer-title');
        add(title, el('h2', '', app.name), el('div', 'faint small', app.project + ' · ' + app.namespace));
        var close = K.button('', 'icon-button', 'close', function () {
            state.selected = '';
            saveHash();
            drawGroups(model);
            drawDrawer(model);
        });
        close.setAttribute('aria-label', 'Close');
        add(head, title, close);
        drawer.appendChild(head);

        var chips = el('div', 'drawer-chips');
        add(chips, K.healthChip(app.health), K.syncChip(app.sync));
        if (app.auto) chips.appendChild(K.chip('auto-sync' + (app.prune ? ' · prune' : '') + (app.selfHeal ? ' · self-heal' : ''), 'muted', 'auto'));
        if (A.opRunning(app)) chips.appendChild(K.chip('syncing', 'info', 'sync'));
        drawer.appendChild(chips);

        if (state.notice) {
            var note = el('div', 'notice');
            add(note, K.icon('check'), el('span', '', state.notice));
            drawer.appendChild(note);
        }

        var tools = el('div', 'drawer-tools');
        if (state.ctx.write) {
            var sync = K.button('Sync', 'primary', 'sync', function () {
                act(app, 'sync');
            });
            sync.disabled = A.opRunning(app);
            add(
                tools,
                sync,
                K.button('Refresh', '', 'refresh', function () {
                    act(app, 'refresh');
                }),
                K.button('Hard refresh', 'ghost', 'refresh', function () {
                    act(app, 'hard');
                }),
            );
        }
        add(
            tools,
            K.button('YAML', 'ghost', 'edit', function () {
                sdk.edit(A.appRef(app)).catch(fail);
            }),
            K.button('Details', 'ghost', 'open', function () {
                open(A.appRef(app));
            }),
        );
        drawer.appendChild(tools);

        if (app.healthMessage && app.health !== 'Healthy') {
            var why = el('div', 'why ' + (A.HEALTH_TONE[app.health] || 'muted'));
            add(why, K.icon('alert'), el('span', '', app.healthMessage));
            drawer.appendChild(why);
        }

        // Where from, where to.
        var route = el('div', 'route');
        var from = el('div', 'route-end');
        add(from, K.icon('git'), el('div', '', ''));
        from.lastChild.appendChild(el('div', 'route-main', A.repoLabel(app.repo) || '—'));
        from.lastChild.appendChild(el('div', 'route-sub', app.chart ? 'chart ' + app.chart + ' ' + app.target : (app.path || '/') + ' @ ' + app.target));
        var to = el('div', 'route-end');
        add(to, K.icon('cluster'), el('div', '', ''));
        to.lastChild.appendChild(el('div', 'route-main', A.destination(app)));
        to.lastChild.appendChild(el('div', 'route-sub', app.revision ? 'at ' + A.short(app.revision) : 'nothing deployed yet'));
        add(route, from, K.icon('arrow', 'route-arrow'), to);
        if (app.sources > 1) route.appendChild(el('div', 'route-note', 'and ' + (app.sources - 1) + ' more source' + (app.sources > 2 ? 's' : '')));
        drawer.appendChild(route);

        // What `argocd app set` would change, right under where it comes from.
        drawer.appendChild(placeholder);

        // What it deploys.
        var resHead = el('h3', 'drawer-section', 'What it deploys');
        resHead.appendChild(el('span', 'faint', ' ' + app.resources.length + (app.outOfSync ? ' · ' + app.outOfSync + ' out of sync' : '')));
        drawer.appendChild(resHead);
        if (app.resources.length) {
            var treeBox = el('div', 'tree-box');
            treeBox.id = 'tree-box';
            drawer.appendChild(treeBox);
        } else {
            drawer.appendChild(el('p', 'quiet', 'Argo CD has not reported any resources for it yet.'));
        }

        // The last operation.
        if (app.op) {
            drawer.appendChild(el('h3', 'drawer-section', 'Last sync'));
            var op = el('div', 'op ' + (A.opFailed(app) ? 'error' : A.opRunning(app) ? 'info' : 'ok'));
            var line = el('div', 'op-line');
            add(line, K.chip(app.op.phase || 'Unknown', A.opFailed(app) ? 'error' : A.opRunning(app) ? 'info' : 'ok'), el('span', 'faint', [app.op.initiatedBy === 'automated' ? 'automatic' : app.op.initiatedBy ? 'by ' + app.op.initiatedBy : '', K.age(app.op.finishedAt || app.op.startedAt)].filter(Boolean).join(' · ')));
            op.appendChild(line);
            if (app.op.message) op.appendChild(el('div', 'op-message', app.op.message));
            var failed = app.op.results.filter(function (r) {
                return r.status && r.status !== 'Synced' && r.status !== 'Pruned';
            });
            failed.slice(0, 4).forEach(function (r) {
                op.appendChild(el('div', 'op-result', r.kind + ' ' + r.name + ': ' + (r.message || r.status)));
            });
            drawer.appendChild(op);
        }

        // Conditions.
        if (app.conditions.length) {
            drawer.appendChild(el('h3', 'drawer-section', 'Conditions'));
            app.conditions.forEach(function (c) {
                var row = el('div', 'why ' + (/Error$/.test(c.type) ? 'error' : 'warn'));
                add(row, K.icon('alert'), el('span', '', c.type + (c.message ? ': ' + c.message : '')));
                drawer.appendChild(row);
            });
        }

        // History.
        if (app.history.length) {
            drawer.appendChild(el('h3', 'drawer-section', 'Deployed'));
            var ol = el('ol', 'timeline compact');
            app.history.slice(0, 6).forEach(function (h, i) {
                var li = el('li', 'tl' + (i === 0 ? ' current' : ''));
                var mark = el('span', 'tl-mark');
                mark.appendChild(K.icon('git'));
                li.appendChild(mark);
                var body = el('div', 'tl-body');
                var l = el('div', 'tl-line');
                add(l, el('code', 'rev', A.short(h.revision) || '—'), el('span', '', i === 0 ? ' live now' : ''));
                body.appendChild(l);
                body.appendChild(el('div', 'tl-meta', [K.age(h.deployedAt), h.initiatedBy ? (h.initiatedBy === 'automated' ? 'automatically' : 'by ' + h.initiatedBy) : '', h.target && h.target !== app.target ? 'from ' + h.target : ''].filter(Boolean).join(' · ')));
                li.appendChild(body);
                ol.appendChild(li);
            });
            drawer.appendChild(ol);
        }

        // Links and images.
        if (app.urls.length || app.images.length) {
            drawer.appendChild(el('h3', 'drawer-section', 'Where it lives'));
            var links = el('div', 'links');
            app.urls.forEach(function (u) {
                links.appendChild(
                    K.button(u.replace(/^https?:\/\//, ''), 'link-chip', 'link', function () {
                        sdk.openUrl(u).catch(fail);
                    }),
                );
            });
            drawer.appendChild(links);
            if (app.images.length) {
                var imgs = el('ul', 'images');
                app.images.slice(0, 8).forEach(function (img) {
                    var li = el('li');
                    add(li, K.icon('image'), el('code', '', img));
                    imgs.appendChild(li);
                });
                drawer.appendChild(imgs);
            }
        }

        // Into the page, around the Overrides section.
        var kids = Array.prototype.slice.call(out.childNodes);
        var at = kids.indexOf(placeholder);
        if (kept) {
            kids.slice(0, at).forEach(function (n) {
                target.insertBefore(n, kept);
            });
            kids.slice(at + 1).forEach(function (n) {
                target.appendChild(n);
            });
        } else {
            out.replaceChild(fold, placeholder);
            target.appendChild(out);
        }
        drawTree();
    }

    // ----- putting it together -----------------------------------------------

    function drawGroupControl() {
        var box = $('group');
        box.textContent = '';
        box.appendChild(el('span', 'seg-label', 'Group by'));
        GROUPS.forEach(function (g) {
            var b = K.button(g.label, state.group === g.id ? 'on' : '', null, function () {
                state.group = g.id;
                saveHash();
                drawGroupControl();
                drawGroups(state.model);
            });
            b.setAttribute('aria-pressed', state.group === g.id ? 'true' : 'false');
            box.appendChild(b);
        });
    }

    function drawEmpty(model) {
        var box = $('empty');
        box.textContent = '';
        box.hidden = false;
        var art = el('div', 'empty-art');
        art.appendChild(K.icon('logo'));
        add(box, art, el('h2', '', 'Argo CD is not installed in ' + state.ctx.contextName), el('p', 'faint', 'This cluster does not serve Applications, so there is nothing to put on the board.'));
        if (model.missing) box.appendChild(el('p', 'faint small', model.missing));
    }

    function render() {
        var model = state.model;
        if (!model) return;
        var where = state.ctx.contextName;
        if (model.version) where += ' · Argo CD ' + model.version;
        where += ' · ' + K.plural(model.apps.length, 'application');
        $('where').textContent = where;
        if (!model.installed) {
            $('board').hidden = true;
            drawEmpty(model);
            return;
        }
        $('empty').hidden = true;
        $('board').hidden = false;
        drawRail(model);
        drawGroups(model);
        drawDrawer(model);
    }

    function tick() {
        A.load(sdk)
            .then(function (model) {
                $('error').hidden = true;
                if (model.sig !== state.sig) {
                    state.model = model;
                    state.sig = model.sig;
                    render();
                }
            })
            .catch(fail)
            .then(function () {
                setTimeout(tick, POLL);
            });
    }

    $('logo').appendChild(K.icon('logo'));
    $('search-icon').appendChild(K.icon('search'));
    $('query').addEventListener('input', function (event) {
        state.query = event.target.value;
        if (state.model) drawGroups(state.model);
    });
    document.addEventListener('keydown', function (event) {
        var typing = /^(INPUT|TEXTAREA|SELECT)$/.test((document.activeElement && document.activeElement.tagName) || '');
        if (event.key === 'Escape' && state.selected && !typing) {
            state.selected = '';
            saveHash();
            if (state.model) {
                drawGroups(state.model);
                drawDrawer(state.model);
            }
        } else if (event.key === '/' && document.activeElement === document.body) {
            event.preventDefault();
            $('query').focus();
        }
    });

    loadHash();
    drawGroupControl();
    // The drawer is as wide as it was left: the width is kept in the hash
    // with the rest of the board's state.
    document.body.appendChild(
        K.grip({
            panel: $('drawer'),
            prop: '--drawer-w',
            className: 'drawer-grip',
            min: 380,
            room: 320,
            initial: Number(state.width) || 0,
            label: 'Resize the application drawer',
            onResize: function (px, done) {
                if (done) {
                    state.width = px ? String(px) : '';
                    saveHash();
                }
                drawTree();
            },
        }),
    );

    sdk.ready()
        .then(function (context) {
            state.ctx = context;
            $('where').textContent = context.contextName;
            tick();
        })
        .catch(fail);
})();
