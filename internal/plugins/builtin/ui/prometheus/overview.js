// Prometheus's own overview, in place of the page the app generates for every
// plugin. It says first whether monitoring is working at all -- is anything
// firing, is anything not being scraped -- and then what the generated page
// could not: what fired over the last hours, each job's scrape health, the
// servers themselves, and whether the rules the cluster holds are rules a
// Prometheus actually loaded.
(function () {
    'use strict';

    var sdk = window.k8sdockside;
    var P = window.Prom;
    var K = window.PromKit;
    var el = K.el;
    var add = K.add;

    var POLL = 15000;
    var SUMMARY_EVERY = 30000;
    var CHARTS_EVERY = 30000;
    var RANGES = [
        { minutes: 60, label: '1h' },
        { minutes: 360, label: '6h' },
        { minutes: 1440, label: '24h' },
        { minutes: 10080, label: '7d' },
    ];
    var MAX_LANES = 16;

    var state = { ctx: null, model: null, sig: '', summary: null, panel: null, minutes: 360 };

    function $(id) {
        return document.getElementById(id);
    }

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

    function ruleNamed(m, alertname) {
        for (var i = 0; i < m.alerts.length; i++) if (m.alerts[i].alert === alertname) return m.alerts[i];
        return null;
    }

    function openRule(rule) {
        open({ kind: P.KINDS.rules, namespace: rule.namespace, name: rule.objName });
    }

    function charted() {
        return !!(state.panel && state.panel.source && state.panel.source.available);
    }

    function rangeLabel() {
        for (var i = 0; i < RANGES.length; i++) if (RANGES[i].minutes === state.minutes) return RANGES[i].label;
        return Math.round(state.minutes / 60) + 'h';
    }

    // ----- the numbers ---------------------------------------------------------

    function numbers() {
        var p = state.panel;
        var n = {
            total: P.latestSum(p, 'targets-by-job'),
            up: P.latestSum(p, 'targets-up-by-job'),
            down: P.latestSum(p, 'targets-down'),
            samples: P.latestSum(p, 'samples-ingested'),
            series: P.latestSum(p, 'head-series'),
            disk: P.latestSum(p, 'tsdb-disk'),
            volume: P.latestMax(p, 'tsdb-volume-full'),
        };
        var totals = P.latestBy(p, 'targets-by-job');
        var ups = P.latestBy(p, 'targets-up-by-job');
        n.jobs = Object.keys(totals)
            .map(function (job) {
                var total = Math.round(totals[job]);
                var up = ups[job] === undefined ? total : Math.min(total, Math.round(ups[job]));
                return { job: job, total: total, up: up, down: total - up };
            })
            .sort(function (a, b) {
                return b.down - a.down || b.total - a.total || a.job.localeCompare(b.job);
            });
        if (n.up === null && n.total !== null) n.up = n.total - (n.down || 0);
        if (n.down === null && n.total !== null) n.down = n.total - n.up;
        return n;
    }

    function firingNow(act) {
        return act.lanes.filter(function (l) {
            return l.state === 'firing' && !P.ALWAYS_ON[l.alertname];
        });
    }

    // ----- the hero ------------------------------------------------------------

    function verdict(m, n, act) {
        var firing = firingNow(act);
        var critical = firing.filter(function (l) {
            return P.severityTone(l.severity) === 'error';
        });
        var down = m.servers.filter(function (s) {
            return P.available(s) === false;
        });
        if (!m.servers.length) return { tone: 'muted', icon: 'logo', text: 'No Prometheus server yet' };
        if (down.length === m.servers.length) return { tone: 'error', icon: 'alert', text: m.servers.length === 1 ? 'Prometheus is not running' : 'No Prometheus server is running' };
        if (critical.length) return { tone: 'error', icon: 'alert', text: K.plural(firing.length, 'alert') + ' firing, ' + critical.length + ' critical' };
        if (firing.length) return { tone: 'warn', icon: 'bell', text: K.plural(firing.length, 'alert') + ' firing' };
        if (n.down > 0) return { tone: 'warn', icon: 'target', text: K.plural(Math.round(n.down), 'scrape target') + ' down' };
        if (down.length) return { tone: 'warn', icon: 'server', text: K.plural(down.length, 'Prometheus server is', 'Prometheus servers are') + ' not available' };
        if (state.panel && !charted()) return { tone: 'warn', icon: 'logo', text: 'Prometheus is installed, but did not answer' };
        if (n.total) return { tone: 'ok', icon: 'check', text: 'All ' + Math.round(n.total) + ' targets up, nothing firing' };
        return { tone: 'ok', icon: 'check', text: 'Nothing firing' };
    }

    function story(m, n) {
        var p = el('p', 'story');
        function b(text) {
            return el('strong', '', text);
        }
        if (!m.servers.length) {
            add(p, 'The Prometheus Operator is installed, but nothing has asked it for a Prometheus yet. Create a Prometheus object and it will appear here with what it scrapes and what fires.');
            return p;
        }
        var spec = m.servers[0].spec || {};
        add(p, b(K.plural(m.servers.length, 'Prometheus server')));
        if (spec.retention) add(p, ' keeping ', b(spec.retention));
        if (n.total !== null) add(p, ', scraping ', b(K.plural(Math.round(n.total), 'target')), ' in ', b(K.plural(n.jobs.length, 'job')));
        add(p, '.');
        if (n.series !== null) add(p, ' ', b(P.fmt(n.series, 'count')), ' series in memory');
        if (n.samples !== null) add(p, n.series !== null ? ', ' : ' ', b(P.fmt(n.samples, 'ops/s')), ' samples ingested');
        if (n.series !== null || n.samples !== null) add(p, '.');
        add(p, ' ', b(K.plural(m.alerts.length, 'alerting rule')), ' in ', b(K.plural(m.ruleObjs.length, 'rule object')));
        if (m.alertmanagers.length) add(p, ', delivered through ', b(K.plural(m.alertmanagers.length, 'Alertmanager')));
        add(p, '.');
        if (state.panel && !charted()) {
            add(p, ' ', el('span', 'faint', 'No Prometheus answered the app’s queries, so there are no numbers or history here. ' + (state.panel.source.error || '')));
        }
        return p;
    }

    function drawHero(m, n, act) {
        var hero = $('hero');
        hero.textContent = '';
        var v = verdict(m, n, act);
        hero.className = 'hero ' + v.tone;

        var main = el('div', 'hero-main');
        var eyebrow = el('div', 'eyebrow');
        var logo = el('span', 'logo');
        logo.appendChild(K.icon('logo'));
        var version = m.servers[0] && m.servers[0].spec && m.servers[0].spec.version;
        add(eyebrow, logo, el('span', '', 'Prometheus' + (version ? ' ' + version : '')), el('span', 'faint', '· ' + state.ctx.contextName + (charted() ? ' · ' + state.panel.source.describe : '')));
        main.appendChild(eyebrow);

        var head = el('h1', 'verdict ' + v.tone);
        add(head, K.icon(v.icon), el('span', '', v.text));
        main.appendChild(head);
        main.appendChild(story(m, n));

        var cta = el('div', 'cta');
        add(
            cta,
            K.button('Alerts & rules', 'primary', 'bell', function () {
                openView('alerts');
            }),
            K.button('Service monitors', 'ghost', 'share', function () {
                openView('servicemonitors');
            }),
        );
        main.appendChild(cta);
        hero.appendChild(main);

        if (n.total) {
            var side = el('div', 'hero-side');
            side.appendChild(
                K.ring(
                    [
                        { count: n.up, tone: 'ok' },
                        { count: Math.max(0, n.total - n.up), tone: 'error' },
                    ],
                    String(Math.round(n.up)),
                    'of ' + Math.round(n.total) + ' targets up',
                ),
            );
            hero.appendChild(side);
        }
    }

    // ----- the tiles -----------------------------------------------------------

    function tile(iconName, label, value, sub, tone, points, chartId) {
        var t = el('article', 'tile' + (tone ? ' ' + tone : ''));
        var head = el('div', 'tile-head');
        add(head, K.icon(iconName), el('span', '', label));
        var c = P.chart(state.panel, chartId);
        if (c && c.description) t.title = c.description;
        add(t, head, el('strong', 'tile-value', value), el('span', 'tile-sub', sub), K.spark(points, tone));
        return t;
    }

    function drawTiles(n, act) {
        var box = $('tiles');
        box.textContent = '';
        box.hidden = !charted();
        if (box.hidden) return;
        var p = state.panel;
        var firing = firingNow(act);
        var critical = firing.filter(function (l) {
            return P.severityTone(l.severity) === 'error';
        }).length;
        var volumeTone = n.volume === null ? '' : n.volume > 0.85 ? 'error' : n.volume > 0.7 ? 'warn' : '';
        var spec = (state.model.servers[0] && state.model.servers[0].spec) || {};
        add(
            box,
            tile('target', 'Targets up', n.total === null ? '—' : Math.round(n.up) + ' / ' + Math.round(n.total), n.down ? K.plural(Math.round(n.down), 'target') + ' down' : 'every target answering', n.down ? 'error' : 'ok', P.sumOverTime(p, 'targets-up-by-job'), 'targets-up-by-job'),
            tile(
                'bell',
                'Firing alerts',
                String(firing.length),
                critical ? critical + ' critical' : firing.length ? 'none critical' : 'quiet',
                critical ? 'error' : firing.length ? 'warn' : 'ok',
                P.sumOverTime(p, 'alerts-active', function (name) {
                    var l = P.parseLabels(name);
                    return l.alertstate === 'firing' && !P.ALWAYS_ON[l.alertname];
                }),
                'alerts-active',
            ),
            tile('activity', 'Samples ingested', P.fmt(n.samples, 'ops/s'), 'appended per second', '', P.sumOverTime(p, 'samples-ingested'), 'samples-ingested'),
            tile('layers', 'Series in memory', P.fmt(n.series, 'count'), 'in the head block', '', P.sumOverTime(p, 'head-series'), 'head-series'),
            tile('disk', 'TSDB on disk', P.fmt(n.disk, 'bytes'), spec.retention ? 'kept for ' + spec.retention : 'blocks on disk', '', P.sumOverTime(p, 'tsdb-disk'), 'tsdb-disk'),
            tile('disk', 'Fullest volume', P.fmt(n.volume, 'percent'), n.volume === null ? 'no Prometheus volume found' : 'of a Prometheus volume', volumeTone, P.sumOverTime(p, 'tsdb-volume-full', null, true), 'tsdb-volume-full'),
        );
    }

    // ----- firing now ------------------------------------------------------------

    function drawFiring(m, act) {
        var box = $('firing');
        box.textContent = '';
        var live = act.lanes.filter(function (l) {
            return l.state && !P.ALWAYS_ON[l.alertname];
        });
        var firing = live.filter(function (l) {
            return l.state === 'firing';
        });
        var head = el('div', 'card-head');
        add(head, K.icon(firing.length ? 'bell' : 'check'), el('h2', '', firing.length ? 'Firing now' : live.length ? 'Pending' : 'Nothing firing'));
        if (live.length) head.appendChild(el('span', 'count', String(live.length)));
        box.appendChild(head);
        box.className = 'card firing' + (live.length ? '' : ' clear');

        if (!state.panel) {
            box.appendChild(el('p', 'quiet', 'Asking Prometheus…'));
            return;
        }
        if (!charted() || !act.known) {
            box.appendChild(el('p', 'quiet', 'What is firing is read from Prometheus, which did not answer.'));
            return;
        }
        if (!live.length) {
            box.appendChild(el('p', 'quiet', 'No alert is pending or firing. Anything that starts to will be listed here, worst first, with how long it has been going.'));
        } else {
            var t0 = act.now - (state.panel.range || state.minutes) * 60;
            var ul = el('ul', 'alert-list');
            live.slice(0, 8).forEach(function (l) {
                var tone = l.state === 'firing' ? P.severityTone(l.severity) : 'pending';
                var li = el('li', 'alert-row ' + tone);
                var rule = ruleNamed(m, l.alertname);
                var title = el('div', 'alert-title');
                add(
                    title,
                    K.chip(l.severity || 'no severity', P.severityTone(l.severity)),
                    rule
                        ? K.link(l.alertname, function () {
                              openRule(rule);
                          }, 'Open ' + rule.namespace + '/' + rule.objName)
                        : el('strong', '', l.alertname),
                    l.namespace ? el('span', 'faint', l.namespace) : null,
                );
                var since = l.since <= t0 + act.step ? 'over ' + rangeLabel() : P.duration(act.now - l.since);
                add(li, title, el('span', 'alert-when', (l.state === 'firing' ? 'firing ' : 'pending ') + since));
                var summary = rule && P.untemplate(rule.annotations.summary || rule.annotations.message || rule.annotations.description);
                if (summary) li.appendChild(el('div', 'alert-text', summary));
                ul.appendChild(li);
            });
            box.appendChild(ul);
            if (live.length > 8) {
                box.appendChild(
                    K.button('All ' + live.length + ' in Alerts & rules', 'ghost small', 'arrow', function () {
                        openView('alerts');
                    }),
                );
            }
        }

        // Watchdog fires all the time on purpose; it not firing is the
        // one alert that says the alerting itself is broken.
        var watchdog = act.lanes.filter(function (l) {
            return l.alertname === 'Watchdog';
        })[0];
        if (watchdog && watchdog.state === 'firing') {
            var ok = el('div', 'heartbeat ok');
            add(ok, K.icon('activity'), el('span', '', 'Watchdog is firing, as it always should: rules are being evaluated.'));
            box.appendChild(ok);
        } else if (ruleNamed(m, 'Watchdog')) {
            var bad = el('div', 'heartbeat warn');
            add(bad, K.icon('alert'), el('span', '', 'Watchdog is not firing. It is meant to fire all the time, so rule evaluation may not be working.'));
            box.appendChild(bad);
        }
    }

    // ----- scrape health ---------------------------------------------------------

    function drawJobs(n) {
        var box = $('jobs');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('target'), el('h2', '', 'Scrape health'));
        if (n.jobs.length) head.appendChild(el('span', 'count', K.plural(n.jobs.length, 'job')));
        box.appendChild(head);
        if (!state.panel) {
            box.appendChild(el('p', 'quiet', 'Asking Prometheus…'));
            return;
        }
        if (!charted() || !n.jobs.length) {
            box.appendChild(el('p', 'quiet', charted() ? 'Prometheus reports no scrape targets.' : 'Scrape health is read from Prometheus, which did not answer.'));
            return;
        }
        var ul = el('ul', 'jobs');
        n.jobs.slice(0, 14).forEach(function (j) {
            var tone = j.down === 0 ? 'ok' : j.up === 0 ? 'error' : 'warn';
            var li = el('li', 'job ' + tone);
            var name = el('span', 'job-name', j.job || '(no job label)');
            name.title = j.job;
            var bar = el('span', 'mini-bar');
            if (j.up) {
                var up = el('i', 'seg ok');
                up.style.flexGrow = String(j.up);
                bar.appendChild(up);
            }
            if (j.down) {
                var down = el('i', 'seg error');
                down.style.flexGrow = String(j.down);
                bar.appendChild(down);
            }
            add(li, name, bar, el('span', 'job-count', j.up + ' / ' + j.total));
            ul.appendChild(li);
        });
        box.appendChild(ul);
        // Worst first, so the ones left over are all up when the first of them is.
        if (n.jobs.length > 14) box.appendChild(el('p', 'faint small', '+ ' + K.plural(n.jobs.length - 14, 'more job') + (n.jobs[14].down ? '' : ', all up')));
    }

    // ----- the alert timeline ----------------------------------------------------

    function tickEvery(minutes) {
        if (minutes <= 60) return 600;
        if (minutes <= 360) return 3600;
        if (minutes <= 1440) return 3 * 3600;
        return 86400;
    }

    function clock(t, minutes) {
        var d = new Date(t * 1000);
        if (minutes > 1440) return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' });
        return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    }

    function drawTimeline(m, act) {
        var box = $('timeline');
        box.textContent = '';
        box.hidden = !charted();
        if (box.hidden) return;

        var head = el('div', 'card-head');
        add(head, K.icon('clock'), el('h2', '', 'Alert timeline'));
        var seg = el('div', 'segmented');
        RANGES.forEach(function (r) {
            seg.appendChild(
                K.button(r.label, r.minutes === state.minutes ? 'on' : '', null, function () {
                    if (state.minutes === r.minutes) return;
                    state.minutes = r.minutes;
                    loadCharts().catch(fail);
                }),
            );
        });
        head.appendChild(seg);
        box.appendChild(head);

        if (!act.known) {
            box.appendChild(el('p', 'quiet', 'This plugin has no alert history chart.'));
            return;
        }
        if (act.error) box.appendChild(el('p', 'quiet', act.error));
        var lanes = act.lanes.filter(function (l) {
            return !P.ALWAYS_ON[l.alertname] && (l.firing.length || l.pending.length);
        });
        if (!lanes.length) {
            box.appendChild(el('p', 'quiet', 'Nothing has been pending or firing in the last ' + rangeLabel() + '. Each alert that does gets a lane here, shaded while it was pending and filled while it fired.'));
            return;
        }

        var shown = lanes.slice(0, MAX_LANES);
        var width = Math.max(480, box.clientWidth - 34);
        var labelW = Math.min(250, Math.round(width * 0.28));
        var rowH = 24;
        var top = 22;
        var H = top + shown.length * rowH + 4;
        var t1 = act.now;
        var t0 = t1 - (state.panel.range || state.minutes) * 60;
        var plotW = width - labelW - 10;
        function X(t) {
            return labelW + Math.max(0, Math.min(1, (t - t0) / (t1 - t0))) * plotW;
        }

        var node = K.svg('svg', { viewBox: '0 0 ' + width + ' ' + H, width: width, height: H, class: 'lanes', role: 'img' });
        node.setAttribute('aria-label', 'Alerts pending and firing over the last ' + rangeLabel());

        var every = tickEvery(state.panel.range || state.minutes);
        for (var t = Math.ceil(t0 / every) * every; t <= t1; t += every) {
            var x = X(t);
            node.appendChild(K.svg('line', { x1: x, x2: x, y1: top - 4, y2: H, class: 'gridline' }));
            var label = K.svg('text', { x: x, y: 12, class: 'tick', 'text-anchor': 'middle' });
            label.textContent = clock(t, state.panel.range || state.minutes);
            node.appendChild(label);
        }

        shown.forEach(function (lane, i) {
            var y = top + i * rowH;
            if (i % 2) node.appendChild(K.svg('rect', { x: 0, y: y, width: width, height: rowH, class: 'stripe' }));
            var rule = ruleNamed(m, lane.alertname);
            var name = lane.alertname + (lane.namespace ? '  ' + lane.namespace : '');
            var max = Math.floor((labelW - 26) / 6.4);
            var text = K.svg('text', { x: 18, y: y + rowH / 2 + 4, class: 'lane-label' + (rule ? ' openable' : '') });
            text.textContent = name.length > max ? name.slice(0, max - 1) + '…' : name;
            var tip = K.svg('title', {});
            tip.textContent = lane.alertname + (lane.namespace ? ' in ' + lane.namespace : '') + (lane.severity ? ' · ' + lane.severity : '');
            text.appendChild(tip);
            if (rule) {
                text.addEventListener('click', function () {
                    openRule(rule);
                });
            }
            node.appendChild(text);
            node.appendChild(K.svg('circle', { cx: 8, cy: y + rowH / 2, r: 3.5, class: 'lane-dot ' + (lane.state === 'firing' ? P.severityTone(lane.severity) : lane.state === 'pending' ? 'pending' : 'muted') }));

            function draw(run, cls, what) {
                var x0 = X(run.start);
                var w = Math.max(3, X(run.end + act.step) - x0);
                var r = K.svg('rect', { x: x0, y: y + 5, width: w, height: rowH - 10, rx: 3, class: 'run ' + cls });
                var t = K.svg('title', {});
                t.textContent = lane.alertname + ' ' + what + ' ' + clock(run.start, 0) + '–' + clock(run.end, 0) + ' (' + P.duration(run.end - run.start + act.step) + ')';
                r.appendChild(t);
                node.appendChild(r);
            }
            lane.pending.forEach(function (run) {
                draw(run, 'pending', 'pending');
            });
            lane.firing.forEach(function (run) {
                draw(run, 'firing ' + P.severityTone(lane.severity), 'firing');
            });
        });
        node.appendChild(K.svg('line', { x1: X(t1), x2: X(t1), y1: top - 4, y2: H, class: 'now' }));

        var scroll = el('div', 'lanes-box');
        scroll.appendChild(node);
        box.appendChild(scroll);

        var legend = el('div', 'lane-legend');
        [
            ['error', 'critical'],
            ['warn', 'warning'],
            ['info', 'info'],
            ['pending', 'pending'],
        ].forEach(function (pair) {
            var key = el('span', 'key');
            add(key, el('i', 'swatch ' + pair[0]), el('span', '', pair[1]));
            legend.appendChild(key);
        });
        if (lanes.length > MAX_LANES) legend.appendChild(el('span', 'faint', '+ ' + (lanes.length - MAX_LANES) + ' more alerts'));
        box.appendChild(legend);
    }

    // ----- the servers -----------------------------------------------------------

    function serverRow(obj, prometheus, m) {
        var spec = obj.spec || {};
        var st = obj.status || {};
        var want = (spec.replicas === undefined || spec.replicas === null ? 1 : spec.replicas) * (prometheus && spec.shards ? spec.shards : 1);
        var ready = st.availableReplicas || 0;
        var avail = P.available(obj);
        var tone = avail === false || (want > 0 && ready === 0) ? 'error' : ready < want || avail === 'degraded' ? 'warn' : 'ok';
        var li = el('li', 'server ' + tone);
        var top = el('div', 'server-top');
        var kind = prometheus ? P.KINDS.servers : P.KINDS.alertmanagers;
        var dots = el('span', 'pod-dots');
        for (var i = 0; i < Math.min(want, 12); i++) dots.appendChild(el('i', i < ready ? 'ok' : 'error'));
        add(
            top,
            K.icon(prometheus ? 'logo' : 'bell'),
            K.link(obj.metadata.name, function () {
                open({ kind: kind, namespace: obj.metadata.namespace, name: obj.metadata.name });
            }),
            el('span', 'faint', obj.metadata.namespace),
            spec.version ? K.chip(spec.version, 'muted') : null,
            el('span', 'spacer'),
            dots,
            el('span', 'server-count', ready + ' / ' + want),
        );
        li.appendChild(top);

        var facts = [];
        if (spec.retention) facts.push(spec.retention + ' retention');
        if (spec.retentionSize) facts.push('up to ' + spec.retentionSize);
        if (prometheus) {
            var size = P.dig(spec, 'storage.volumeClaimTemplate.spec.resources.requests.storage');
            facts.push(size ? size + ' volume' : 'no persistent volume');
            var loaded = m.ruleObjs.filter(function (o) {
                return P.loads(obj, P.labelsOf(o), o.metadata.namespace, m.nsLabels);
            }).length;
            facts.push('loads ' + loaded + ' of ' + K.plural(m.ruleObjs.length, 'rule object'));
        } else {
            facts.push('Alertmanager');
        }
        li.appendChild(el('div', 'server-facts', facts.join(' · ')));

        var bad = (st.conditions || []).filter(function (c) {
            return c.status !== 'True';
        });
        if (bad.length) {
            var chips = el('div', 'server-chips');
            bad.forEach(function (c) {
                chips.appendChild(K.chip(c.type + ': ' + c.status, c.status === 'False' ? 'error' : 'warn', 'alert', c.message || c.reason || ''));
            });
            li.appendChild(chips);
        }
        return li;
    }

    function drawServers(m) {
        var box = $('servers');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('server'), el('h2', '', 'Servers'));
        box.appendChild(head);
        if (!m.servers.length && !m.alertmanagers.length) {
            box.appendChild(el('p', 'quiet', 'The operator runs no Prometheus or Alertmanager here yet.'));
            return;
        }
        var ul = el('ul', 'servers');
        m.servers.forEach(function (s) {
            ul.appendChild(serverRow(s, true, m));
        });
        m.alertmanagers.forEach(function (a) {
            ul.appendChild(serverRow(a, false, m));
        });
        box.appendChild(ul);
        if (!m.alertmanagers.length && !m.errors.alertmanagers) {
            box.appendChild(el('p', 'faint small', 'No Alertmanager: alerts fire in Prometheus, but nothing sends them anywhere.'));
        }
    }

    // ----- the rules -------------------------------------------------------------

    function drawRules(m) {
        var box = $('rules');
        box.textContent = '';
        var head = el('div', 'card-head');
        add(head, K.icon('rule'), el('h2', '', 'Rules'));
        box.appendChild(head);

        var sev = { error: 0, warn: 0, info: 0, muted: 0 };
        m.alerts.forEach(function (a) {
            sev[P.severityTone(a.severity)]++;
        });
        box.appendChild(
            K.stackBar([
                { label: 'critical', count: sev.error, tone: 'error' },
                { label: 'warning', count: sev.warn, tone: 'warn' },
                { label: 'info', count: sev.info, tone: 'info' },
                { label: 'no severity', count: sev.muted, tone: 'muted' },
            ]),
        );

        var facts = el('div', 'facts');
        function fact(iconName, big, small) {
            var f = el('div', 'fact');
            add(f, K.icon(iconName), el('strong', '', big), el('span', '', small));
            return f;
        }
        add(facts, fact('bell', String(m.alerts.length), 'alerting'), fact('layers', String(m.recording), 'recording'), fact('rule', String(m.ruleObjs.length), 'rule objects'));
        box.appendChild(facts);

        if (m.servers.length && m.ruleObjs.length) {
            var unloaded = m.ruleObjs.filter(function (o) {
                return !P.loadedBy(m, o).length;
            });
            if (unloaded.length) {
                var warn = el('div', 'unloaded');
                var line = el('div', 'unloaded-head');
                add(line, K.icon('alert'), el('strong', '', K.plural(unloaded.length, 'rule object is', 'rule objects are') + ' not loaded by any Prometheus'));
                warn.appendChild(line);
                warn.appendChild(el('p', 'unloaded-why', 'Their rules never fire. ' + P.whyNot(m.servers[0], P.labelsOf(unloaded[0]), unloaded[0].metadata.namespace, m.nsLabels) + '.'));
                var ul = el('ul', 'unloaded-list');
                unloaded.slice(0, 5).forEach(function (o) {
                    var li = el('li');
                    add(
                        li,
                        K.link(o.metadata.name, function () {
                            open({ kind: P.KINDS.rules, namespace: o.metadata.namespace, name: o.metadata.name });
                        }),
                        el('span', 'faint', ' ' + o.metadata.namespace),
                    );
                    ul.appendChild(li);
                });
                if (unloaded.length > 5) ul.appendChild(el('li', 'faint', '+ ' + (unloaded.length - 5) + ' more'));
                warn.appendChild(ul);
                box.appendChild(warn);
            } else {
                var ok = el('div', 'heartbeat ok');
                add(ok, K.icon('check'), el('span', '', 'Every rule object is loaded by a Prometheus.'));
                box.appendChild(ok);
            }
        }
        box.appendChild(
            K.button(state.ctx.write ? 'Browse rules and add an alert' : 'Browse rules', 'ghost small', 'arrow', function () {
                openView('alerts');
            }),
        );
    }

    // ----- the foot ----------------------------------------------------------------

    var DESTINATIONS = [
        { id: 'alerts', label: 'Alerts & rules', icon: 'bell' },
        { id: 'servers', label: 'Prometheus servers', icon: 'server' },
        { id: 'alertmanagers', label: 'Alertmanagers', icon: 'bell' },
        { id: 'servicemonitors', label: 'Service monitors', icon: 'share' },
        { id: 'podmonitors', label: 'Pod monitors', icon: 'box' },
        { id: 'scrapeconfigs', label: 'Scrape configs', icon: 'sliders' },
        { id: 'rules', label: 'Prometheus rules', icon: 'rule' },
        { id: 'probes', label: 'Probes', icon: 'probe' },
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

    // ----- not here, or not reachable ------------------------------------------------

    function drawAbsent(summary, model) {
        var hero = $('hero');
        hero.textContent = '';
        hero.className = 'hero absent';
        ['tiles', 'columns', 'timeline', 'lower', 'foot'].forEach(function (id) {
            $(id).hidden = true;
        });
        var main = el('div', 'hero-main');
        var art = el('div', 'empty-art');
        art.appendChild(K.icon('logo'));
        main.appendChild(art);
        var unreachable = summary && !summary.checked;
        main.appendChild(el('h1', 'verdict', unreachable ? 'This cluster did not answer' : 'The Prometheus Operator is not installed in ' + state.ctx.contextName));
        main.appendChild(
            el(
                'p',
                'story',
                unreachable
                    ? 'Whether Prometheus is here could not be checked, which is not the same as it being absent. ' + (summary.error || '')
                    : 'This cluster does not serve the Prometheus Operator’s custom resources. The graphs elsewhere in the app still work if some other Prometheus is reachable; this page is about the operator’s objects.',
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
        } else if (model && model.errors.servers) {
            main.appendChild(el('p', 'faint small', model.errors.servers));
        }
        if (!unreachable) {
            var cta = el('div', 'cta');
            cta.appendChild(
                K.button('Getting started with the Prometheus Operator', 'primary', 'open', function () {
                    sdk.openUrl('https://prometheus-operator.dev/docs/getting-started/introduction/').catch(fail);
                }),
            );
            main.appendChild(cta);
        }
        add(main, K.about(sdk, state.ctx && state.ctx.plugin, fail));
        hero.appendChild(main);
    }

    // ----- putting it together -----------------------------------------------------

    function render() {
        var m = state.model;
        var summary = state.summary;
        if (summary && (!summary.checked || !summary.installed)) {
            drawAbsent(summary, m);
            return;
        }
        if (!m) return;
        if (!m.installed) {
            drawAbsent(summary, m);
            return;
        }
        var n = numbers();
        var act = P.activeAlerts(state.panel);
        $('columns').hidden = false;
        $('lower').hidden = false;
        drawHero(m, n, act);
        drawTiles(n, act);
        drawFiring(m, act);
        drawJobs(n);
        drawTimeline(m, act);
        drawServers(m);
        drawRules(m);
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

    function loadCharts() {
        var minutes = state.minutes;
        return sdk.charts({ minutes: minutes }).then(function (panel) {
            if (minutes !== state.minutes) return;
            state.panel = panel;
            render();
        });
    }

    var lastWidth = 0;
    if (typeof ResizeObserver === 'function') {
        new ResizeObserver(function () {
            // The timeline is laid out for a width; redraw it when that moves.
            var w = document.body.clientWidth;
            if (Math.abs(w - lastWidth) > 40 && state.model && state.model.installed) {
                lastWidth = w;
                drawTimeline(state.model, P.activeAlerts(state.panel));
            }
        }).observe(document.body);
    }

    sdk.ready()
        .then(function (context) {
            state.ctx = context;
            every(POLL, function () {
                return P.load(sdk).then(function (model) {
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
            every(CHARTS_EVERY, loadCharts);
        })
        .catch(fail);
})();
