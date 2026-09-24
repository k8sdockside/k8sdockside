// Argo CD's own overview, in place of the page the app generates for every
// plugin. It says first whether Argo CD is in this cluster at all, then what
// the generated page could not: which applications want a person and why,
// with the button that fixes it; what has been deployed lately; and every
// application at once, so the one red cell among forty green ones is the
// first thing seen.
(function () {
    'use strict';

    var sdk = window.k8sdockside;
    var A = window.Argo;
    var K = window.ArgoKit;
    var el = K.el;
    var add = K.add;

    var POLL = 5000;
    var SUMMARY_EVERY = 30000;
    var CHARTS_EVERY = 60000;
    var HISTORY_MINUTES = 360;
    var FALLBACK = ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9', '#e66767'];

    var state = { ctx: null, model: null, sig: '', summary: null, panel: null, notice: '' };

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

    function openView(id) {
        sdk.openView(id).catch(fail);
    }

    // A series named after a health or sync state wears that state's colour,
    // as it does everywhere else on the page; anything else takes the chart
    // colours in order.
    var STATE_COLOUR = {
        Healthy: 'var(--a-ok)',
        Synced: 'var(--a-ok)',
        Degraded: 'var(--a-error)',
        Missing: 'var(--a-warn)',
        OutOfSync: 'var(--a-warn)',
        Progressing: 'var(--a-info)',
        Suspended: 'var(--a-paused)',
        Unknown: 'var(--a-faint)',
    };

    function colour(i, name) {
        if (name && STATE_COLOUR[name]) return STATE_COLOUR[name];
        return 'var(--chart-' + Math.min(i + 1, 8) + ', ' + FALLBACK[Math.min(i, 7)] + ')';
    }

    function act(app, what) {
        var patch = what === 'sync' ? A.patches.sync() : A.patches.refresh(what === 'hard');
        K.apply(sdk, A.appRef(app), patch)
            .then(function (done) {
                if (!done) return;
                state.notice = (what === 'sync' ? 'Asked Argo CD to sync ' : 'Asked Argo CD to refresh ') + app.name + '.';
                drawNeeds(state.model, true);
                setTimeout(function () {
                    state.notice = '';
                    if (state.model) drawNeeds(state.model);
                }, 6000);
            })
            .catch(fail);
    }

    // ----- the hero ----------------------------------------------------------

    function verdict(model) {
        var apps = model.apps.length;
        if (apps === 0) return { tone: 'ok', icon: 'check', text: 'Argo CD is ready — no applications yet' };
        var wanting = {};
        var errors = 0;
        model.attention.forEach(function (a) {
            wanting[a.app.key] = true;
            if (a.tone === 'error') errors++;
        });
        var n = Object.keys(wanting).length;
        if (n === 0) return { tone: 'ok', icon: 'check', text: apps === 1 ? 'The application is healthy and in sync' : 'All ' + apps + ' applications are healthy and in sync' };
        if (!errors) {
            var drifting = model.attention.every(function (a) {
                return a.what === 'Out of sync';
            });
            return { tone: 'warn', icon: 'sync', text: drifting ? K.plural(n, 'application has', 'applications have') + ' drifted from Git' : K.plural(n, 'application needs', 'applications need') + ' a look' };
        }
        return { tone: 'error', icon: 'alert', text: K.plural(n, 'application needs', 'applications need') + ' you' };
    }

    function story(model) {
        var p = el('p', 'story');
        function b(text, cls) {
            return el('strong', cls || '', text);
        }
        if (model.apps.length === 0) {
            add(p, 'Nothing is being delivered from Git yet. Create an Application and it will appear here, with its health and whether the cluster matches Git.');
            return p;
        }
        var repos = Object.keys(model.repos).length;
        add(p, 'Delivering ', b(K.plural(model.apps.length, 'application')), ' in ', b(K.plural(model.projects.length, 'project')), ' from ', b(K.plural(repos, 'repository', 'repositories')));
        add(p, model.auto ? '; ' : '. ', model.auto ? b(model.auto === model.apps.length ? 'all of them' : String(model.auto)) : '', model.auto ? ' sync themselves.' : '');
        var last = A.stream(model.apps, 1).filter(function (d) {
            return !d.running;
        })[0];
        var running = model.apps.filter(A.opRunning).length;
        if (running) {
            add(p, ' ', b(running + ' syncing', 'busy'), ' right now.');
        } else if (last && last.when) {
            add(p, ' The last deploy was ', b(last.app.name), last.revision ? el('code', 'rev', A.short(last.revision)) : '', ', ' + K.age(last.when) + '.');
        }
        return p;
    }

    function drawHero(model) {
        var hero = $('hero');
        hero.textContent = '';
        var v = verdict(model);
        hero.className = 'hero ' + v.tone;

        var main = el('div', 'hero-main');
        var eyebrow = el('div', 'eyebrow');
        var logo = el('span', 'logo');
        logo.appendChild(K.icon('logo'));
        add(eyebrow, logo, el('span', '', 'Argo CD' + (model.version ? ' ' + model.version : '')), el('span', 'faint', '· ' + state.ctx.contextName + (model.namespace ? ' · ' + model.namespace : '')));
        main.appendChild(eyebrow);

        var head = el('h1', 'verdict ' + v.tone);
        add(head, K.icon(v.icon), el('span', '', v.text));
        main.appendChild(head);
        main.appendChild(story(model));

        var cta = el('div', 'cta');
        add(
            cta,
            K.button('Open the application board', 'primary', 'grid', function () {
                openView('board');
            }),
        );
        var outOfSync = model.apps.filter(function (a) {
            return a.sync === 'OutOfSync' && !A.opRunning(a);
        });
        if (state.ctx.write && outOfSync.length > 1) {
            cta.appendChild(
                K.button('Sync the ' + outOfSync.length + ' out of sync', 'ghost', 'sync', function () {
                    // One at a time: the app asks about each, so nothing is
                    // synced that was not looked at.
                    var queue = outOfSync.slice();
                    (function next() {
                        var app = queue.shift();
                        if (!app) return;
                        K.apply(sdk, A.appRef(app), A.patches.sync()).then(function (done) {
                            if (done) next();
                        }, fail);
                    })();
                }),
            );
        }
        main.appendChild(cta);
        hero.appendChild(main);

        if (model.apps.length) {
            var side = el('div', 'hero-side');
            var hiveBox = el('div', 'hive-box');
            // Beside the text on a wide pane; under it, and as wide as it,
            // once the hero has folded to one column.
            var heroWidth = hero.clientWidth || 900;
            var width = heroWidth < 920 ? Math.max(240, heroWidth - 64) : Math.min(560, Math.max(240, heroWidth * 0.42));
            hiveBox.appendChild(
                K.honeycomb(model.apps, {
                    width: width,
                    maxHeight: 230,
                    onPick: function (key) {
                        var app = model.apps.find(function (a) {
                            return a.key === key;
                        });
                        if (app) open(A.appRef(app));
                    },
                }),
            );
            side.appendChild(hiveBox);
            var legend = el('div', 'hive-legend');
            [
                ['ok', 'Healthy'],
                ['info', 'Progressing'],
                ['error', 'Degraded'],
                ['warn', 'Missing'],
                ['paused', 'Suspended'],
            ].forEach(function (pair) {
                if (!model.health[pair[1]]) return;
                var key = el('span', 'key');
                add(key, el('i', 'hex ' + pair[0]), el('span', '', pair[1]));
                legend.appendChild(key);
            });
            if (model.sync.OutOfSync) {
                var drift = el('span', 'key');
                add(drift, el('i', 'hex drift'), el('span', '', 'out of sync'));
                legend.appendChild(drift);
            }
            side.appendChild(legend);
            hero.appendChild(side);
        }
    }

    // ----- the bars ----------------------------------------------------------

    function drawBars(model) {
        var box = $('bars');
        box.textContent = '';
        box.hidden = model.apps.length === 0;
        if (box.hidden) return;

        function block(title, segments) {
            var b = el('div', 'bar-block');
            add(b, el('div', 'bar-title', title), K.stackBar(segments));
            return b;
        }
        var h = model.health;
        var s = model.sync;
        add(
            box,
            block('Health', [
                { label: 'Degraded', count: h.Degraded || 0, tone: 'error' },
                { label: 'Missing', count: h.Missing || 0, tone: 'warn' },
                { label: 'Progressing', count: h.Progressing || 0, tone: 'info' },
                { label: 'Suspended', count: h.Suspended || 0, tone: 'paused' },
                { label: 'Unknown', count: (h.Unknown || 0) + (h[''] || 0), tone: 'muted' },
                { label: 'Healthy', count: h.Healthy || 0, tone: 'ok' },
            ]),
            block('Sync', [
                { label: 'Out of sync', count: s.OutOfSync || 0, tone: 'warn' },
                { label: 'Unknown', count: (s.Unknown || 0) + (s[''] || 0), tone: 'muted' },
                { label: 'Synced', count: s.Synced || 0, tone: 'ok' },
            ]),
        );

        var facts = el('div', 'facts');
        function fact(iconName, big, small) {
            var f = el('div', 'fact');
            add(f, K.icon(iconName), el('strong', '', big), el('span', '', small));
            return f;
        }
        add(
            facts,
            fact('auto', String(model.auto), 'sync automatically'),
            fact('git', String(Object.keys(model.repos).length), Object.keys(model.repos).length === 1 ? 'repository' : 'repositories'),
            fact('stack', String(model.appsets.length), model.appsets.length === 1 ? 'application set' : 'application sets'),
        );
        box.appendChild(facts);
    }

    // ----- needs you ---------------------------------------------------------

    function drawNeeds(model, force) {
        var box = $('needs');
        if (!force && box.contains(document.activeElement) && document.activeElement !== document.body) return;
        box.textContent = '';
        var list = model.attention;
        var head = el('div', 'card-head');
        add(head, K.icon(list.length ? 'alert' : 'check'), el('h2', '', list.length ? 'Needs you' : 'Nothing needs you'));
        if (list.length) head.appendChild(el('span', 'count', String(list.length)));
        box.appendChild(head);
        box.className = 'card needs' + (list.length ? '' : ' clear');

        if (state.notice) {
            var note = el('div', 'notice');
            add(note, K.icon('check'), el('span', '', state.notice));
            box.appendChild(note);
        }
        if (!list.length) {
            box.appendChild(el('p', 'quiet', model.apps.length ? 'Every application is healthy, matches Git, and synced cleanly the last time it tried.' : 'No applications yet.'));
            return;
        }

        var ul = el('ul', 'needs-list');
        list.slice(0, 7).forEach(function (item) {
            var li = el('li', 'need ' + item.tone);
            var body = el('div', 'need-body');
            var title = el('div', 'need-title');
            add(
                title,
                K.link(item.app.name, function () {
                    open(A.appRef(item.app));
                }),
                el('span', 'need-what ' + item.tone, item.what),
            );
            add(body, title, el('div', 'need-text', item.text));
            li.appendChild(body);
            if (state.ctx.write) {
                var tools = el('div', 'need-tools');
                if (item.fix === 'sync') {
                    tools.appendChild(
                        K.button('Sync', 'small primary', 'sync', function () {
                            act(item.app, 'sync');
                        }),
                    );
                }
                tools.appendChild(
                    K.button('Refresh', 'small', 'refresh', function () {
                        act(item.app, 'refresh');
                    }),
                );
                li.appendChild(tools);
            }
            ul.appendChild(li);
        });
        box.appendChild(ul);
        if (list.length > 7) {
            box.appendChild(
                K.button('All ' + list.length + ' on the board', 'ghost small', 'arrow', function () {
                    openView('board');
                }),
            );
        }
    }

    // ----- what was deployed -------------------------------------------------

    function drawStream(model) {
        var box = $('stream');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('activity'), el('h2', '', 'Deploy stream'));
        box.appendChild(head);

        var items = A.stream(model.apps, 12);
        if (!items.length) {
            box.appendChild(el('p', 'quiet', 'Nothing has been deployed yet. Each sync lands here with the revision it deployed.'));
            return;
        }
        var ol = el('ol', 'timeline');
        items.forEach(function (d) {
            var li = el('li', 'tl' + (d.running ? ' running' : ''));
            var mark = el('span', 'tl-mark');
            mark.appendChild(K.icon(d.running ? 'sync' : 'git'));
            li.appendChild(mark);
            var body = el('div', 'tl-body');
            var line = el('div', 'tl-line');
            add(
                line,
                K.link(d.app.name, function () {
                    open(A.appRef(d.app));
                }),
                el('span', '', d.running ? ' is syncing' : ' deployed'),
                d.revision ? el('code', 'rev', A.short(d.revision)) : '',
            );
            body.appendChild(line);
            var meta = [];
            if (d.when) meta.push(K.age(d.when));
            if (d.by) meta.push(d.by === 'automated' ? 'automatically' : 'by ' + d.by);
            if (d.repo) meta.push(A.repoLabel(d.repo) + (d.path ? ' / ' + d.path : ''));
            body.appendChild(el('div', 'tl-meta', meta.join(' · ')));
            li.appendChild(body);
            ol.appendChild(li);
        });
        box.appendChild(ol);
    }

    // ----- Argo CD itself, and the projects ----------------------------------

    function drawSystem(model) {
        var box = $('system');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('cluster'), el('h2', '', 'Argo CD itself'));
        box.appendChild(head);
        if (!model.componentsKnown) {
            box.appendChild(el('p', 'quiet', 'No pods labelled app.kubernetes.io/part-of=argocd were found, so Argo CD’s own components cannot be shown.'));
            return;
        }
        var ul = el('ul', 'components');
        model.components.forEach(function (c) {
            var tone = c.ready === c.total ? 'ok' : c.ready === 0 ? 'error' : 'warn';
            var li = el('li', 'component ' + tone);
            var dots = el('span', 'pod-dots');
            for (var i = 0; i < c.total; i++) dots.appendChild(el('i', i < c.ready ? 'ok' : 'error'));
            add(li, el('span', 'component-name', c.name), dots, el('span', 'component-count', c.ready + ' / ' + c.total));
            if (c.restarts > 0) li.appendChild(K.chip(c.restarts + ' restart' + (c.restarts === 1 ? '' : 's'), c.restarts > 5 ? 'warn' : 'muted', 'refresh'));
            li.title = c.pods
                .map(function (p) {
                    return p.metadata.name;
                })
                .join('\n');
            ul.appendChild(li);
        });
        box.appendChild(ul);
        box.appendChild(
            K.button("Argo CD's own workloads", 'ghost small', 'arrow', function () {
                openView('components');
            }),
        );
    }

    function drawProjects(model) {
        var box = $('projects');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('project'), el('h2', '', 'Projects'));
        box.appendChild(head);
        if (!model.projects.length) {
            box.appendChild(el('p', 'quiet', 'No projects.'));
            return;
        }
        var ul = el('ul', 'projects');
        model.projects.slice(0, 8).forEach(function (p) {
            var li = el('li');
            var row = K.button('', 'project-row', null, function () {
                if (p.obj) open({ kind: A.KINDS.projects, namespace: p.obj.metadata.namespace, name: p.name });
                else openView('projects');
            });
            var bar = el('span', 'mini-bar');
            var tally = {};
            p.apps.forEach(function (a) {
                var t = A.HEALTH_TONE[a.health] || 'muted';
                tally[t] = (tally[t] || 0) + 1;
            });
            ['error', 'warn', 'info', 'paused', 'muted', 'ok'].forEach(function (t) {
                if (!tally[t]) return;
                var seg = el('i', 'seg ' + t);
                seg.style.flexGrow = String(tally[t]);
                bar.appendChild(seg);
            });
            if (!p.apps.length) bar.appendChild(el('i', 'seg muted empty'));
            add(row, el('span', 'project-name', p.name), el('span', 'project-count', K.plural(p.apps.length, 'app')), bar);
            if (p.description) row.title = p.description;
            li.appendChild(row);
            ul.appendChild(li);
        });
        box.appendChild(ul);
    }

    // ----- history -----------------------------------------------------------

    function spark(chart) {
        var card = el('article', 'chart');
        card.appendChild(el('h3', '', chart.label));
        if (chart.description) card.title = chart.description;
        var series = chart.series.filter(function (s) {
            return s.points.length > 0;
        });
        if (chart.error || !series.length) {
            card.appendChild(el('p', 'quiet', chart.error || 'No data for this window. ' + (chart.description || '')));
            return card;
        }
        var minT = Infinity;
        var maxT = -Infinity;
        var maxV = 0;
        series.forEach(function (s) {
            s.points.forEach(function (p) {
                minT = Math.min(minT, p.t);
                maxT = Math.max(maxT, p.t);
                if (isFinite(p.v)) maxV = Math.max(maxV, p.v);
            });
        });
        if (maxT === minT) maxT = minT + 1;
        var W = 600;
        var H = 120;
        var top = maxV > 0 ? maxV * 1.15 : 1;
        var node = K.svg('svg', { viewBox: '0 0 ' + W + ' ' + H, preserveAspectRatio: 'none', class: 'spark' });
        [0.33, 0.66].forEach(function (f) {
            node.appendChild(K.svg('line', { x1: 0, x2: W, y1: H * f, y2: H * f, class: 'gridline' }));
        });
        var step = (maxT - minT) / 60;
        series.forEach(function (s, i) {
            var d = '';
            var prev = null;
            s.points.forEach(function (p) {
                if (!isFinite(p.v)) {
                    prev = null;
                    return;
                }
                var x = ((p.t - minT) / (maxT - minT)) * W;
                var y = H - (p.v / top) * H;
                d += (prev === null || p.t - prev > step * 3 ? 'M' : 'L') + x.toFixed(1) + ' ' + y.toFixed(1) + ' ';
                prev = p.t;
            });
            var path = K.svg('path', { d: d, class: 'line' });
            path.style.stroke = colour(i, s.name);
            node.appendChild(path);
        });
        card.appendChild(node);
        var legend = el('div', 'chart-legend');
        series.forEach(function (s, i) {
            var last = s.points[s.points.length - 1];
            var key = el('span', 'key');
            var dot = el('i', 'dot');
            dot.style.background = colour(i, s.name);
            add(key, dot, el('span', '', s.name || chart.label), el('strong', '', String(Math.round(last.v * 100) / 100)));
            legend.appendChild(key);
        });
        card.appendChild(legend);
        return card;
    }

    function drawHistory() {
        var box = $('history');
        box.textContent = '';
        var panel = state.panel;
        box.hidden = !panel || !panel.attached;
        if (box.hidden) return;
        var head = el('div', 'section-head');
        add(head, K.icon('chart'), el('h2', '', 'Over the last ' + Math.round(HISTORY_MINUTES / 60) + ' hours'));
        box.appendChild(head);
        if (!panel.source.available) {
            box.appendChild(el('p', 'quiet', 'No Prometheus was found in this cluster, so there is no history to draw. ' + (panel.source.error || '')));
            return;
        }
        var row = el('div', 'chart-row');
        panel.charts.forEach(function (c) {
            row.appendChild(spark(c));
        });
        box.appendChild(row);
    }

    // ----- the foot ----------------------------------------------------------

    var DESTINATIONS = [
        { id: 'board', label: 'Application board', icon: 'grid' },
        { id: 'applications', label: 'Applications', icon: 'app' },
        { id: 'applicationsets', label: 'Application sets', icon: 'stack' },
        { id: 'projects', label: 'Projects', icon: 'project' },
        { id: 'components', label: "Argo CD's own workloads", icon: 'cluster' },
    ];

    function drawFoot() {
        var box = $('foot');
        box.textContent = '';
        box.hidden = false;
        var go = el('div', 'go');
        DESTINATIONS.forEach(function (d) {
            go.appendChild(
                K.button(d.label, 'go-tile', d.icon, function () {
                    openView(d.id);
                }),
            );
        });
        box.appendChild(go);
        if (state.summary && state.summary.requirements.length) {
            var reqs = el('div', 'reqs');
            reqs.appendChild(el('span', 'reqs-label', 'This cluster serves'));
            state.summary.requirements.forEach(function (r) {
                reqs.appendChild(K.chip(r.label, r.error ? 'warn' : r.served ? 'ok' : r.optional ? 'muted' : 'error', r.error ? 'alert' : r.served ? 'check' : 'close', r.error || r.kind));
            });
            box.appendChild(reqs);
        }
        add(box, K.about(sdk, state.ctx && state.ctx.plugin, fail));
    }

    // ----- not here, or not reachable ----------------------------------------

    function drawAbsent(summary, model) {
        var hero = $('hero');
        hero.textContent = '';
        hero.className = 'hero absent';
        ['bars', 'columns', 'lower', 'history', 'foot'].forEach(function (id) {
            $(id).hidden = true;
        });
        var main = el('div', 'hero-main');
        var art = el('div', 'empty-art');
        art.appendChild(K.icon('logo'));
        main.appendChild(art);
        var unreachable = summary && !summary.checked;
        main.appendChild(el('h1', 'verdict', unreachable ? 'This cluster did not answer' : 'Argo CD is not installed in ' + state.ctx.contextName));
        main.appendChild(
            el(
                'p',
                'story',
                unreachable
                    ? 'Whether Argo CD is here could not be checked, which is not the same as it being absent. ' + (summary.error || '')
                    : 'This cluster does not serve Argo CD’s Applications, so nothing here is being kept in step with Git by it. The plugin stays in the sidebar for the clusters that do have it.',
            ),
        );
        if (summary && summary.requirements.length) {
            var ul = el('ul', 'req-list');
            summary.requirements.forEach(function (r) {
                var li = el('li', r.served ? 'ok' : r.optional ? 'muted' : 'error');
                add(li, K.icon(r.served ? 'check' : 'close'), el('span', '', r.label), el('code', 'faint', r.kind.replace(/^crd:/, '')));
                if (r.optional) li.appendChild(el('span', 'faint small', 'optional'));
                ul.appendChild(li);
            });
            main.appendChild(ul);
        } else if (model && model.missing) {
            main.appendChild(el('p', 'faint small', model.missing));
        }
        if (!unreachable) {
            var cta = el('div', 'cta');
            cta.appendChild(
                K.button('Getting started with Argo CD', 'primary', 'open', function () {
                    sdk.openUrl('https://argo-cd.readthedocs.io/en/stable/getting_started/').catch(fail);
                }),
            );
            main.appendChild(cta);
        }
        add(main, K.about(sdk, state.ctx && state.ctx.plugin, fail));
        hero.appendChild(main);
    }

    // ----- putting it together -----------------------------------------------

    function render() {
        var model = state.model;
        var summary = state.summary;
        if (summary && (!summary.checked || !summary.installed)) {
            drawAbsent(summary, model);
            return;
        }
        if (!model) return;
        if (!model.installed) {
            drawAbsent(summary, model);
            return;
        }
        $('columns').hidden = false;
        $('lower').hidden = false;
        drawHero(model);
        drawBars(model);
        drawNeeds(model);
        drawStream(model);
        drawSystem(model);
        drawProjects(model);
        drawHistory();
        drawFoot();
    }

    function every(ms, fn) {
        function run() {
            // Nothing is read while the window is hidden: the next turn waits
            // for it to be shown, and runs the moment it is.
            if (document.visibilityState === 'hidden') {
                document.addEventListener('visibilitychange', function shown() {
                    if (document.visibilityState === 'hidden') return;
                    document.removeEventListener('visibilitychange', shown);
                    run();
                });
                return;
            }
            Promise.resolve()
                .then(fn)
                .catch(fail)
                .then(function () {
                    setTimeout(run, ms);
                });
        }
        run();
    }

    var tipRoot = $('hero');
    K.tooltip(tipRoot, '.cell', function (cell, into) {
        var app = state.model && state.model.apps.find(function (a) {
            return a.key === cell.dataset.key;
        });
        return app ? K.describeApp(app, into) : false;
    });

    var lastWidth = 0;
    if (typeof ResizeObserver === 'function') {
        new ResizeObserver(function () {
            // The honeycomb is laid out for a width; redraw it when that moves.
            var w = document.body.clientWidth;
            if (Math.abs(w - lastWidth) > 40 && state.model && state.model.installed) {
                lastWidth = w;
                drawHero(state.model);
            }
        }).observe(document.body);
    }

    sdk.ready()
        .then(function (context) {
            state.ctx = context;
            every(POLL, function () {
                return A.load(sdk).then(function (model) {
                    $('error').hidden = true;
                    if (model.sig === state.sig) return;
                    state.model = model;
                    state.sig = model.sig;
                    render();
                });
            });
            every(SUMMARY_EVERY, function () {
                return sdk.summary().then(function (summary) {
                    var changed = JSON.stringify(summary) !== JSON.stringify(state.summary);
                    state.summary = summary;
                    if (changed) render();
                });
            });
            every(CHARTS_EVERY, function () {
                if (!sdk.charts) return null;
                return sdk.charts({ minutes: HISTORY_MINUTES }).then(function (panel) {
                    state.panel = panel;
                    if (state.model && state.model.installed) drawHistory();
                });
            });
        })
        .catch(fail);
})();
