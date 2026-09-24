// Every alerting rule the cluster holds -- where it lives, whether a
// Prometheus loads it, whether it is firing -- and a builder for new ones that
// puts them somewhere a Prometheus will actually read them.
//
// The builder's one real job beyond a form is the ruleSelector: the operator
// only hands a Prometheus the PrometheusRules whose labels its ruleSelector
// matches, and a rule object without them is the usual reason a hand-written
// alert "does nothing". So a new object starts with the labels the
// Prometheus asks for, and every destination says who will load it.
(function () {
    'use strict';

    var sdk = window.k8sdockside;
    var P = window.Prom;
    var K = window.PromKit;
    var el = K.el;
    var add = K.add;

    var POLL = 15000;
    var CHARTS_EVERY = 30000;
    var DEFAULT_OBJECT = 'k8sdockside-alerts';
    var DEFAULT_GROUP = 'k8sdockside.rules';
    var SEVERITIES = ['critical', 'warning', 'info', 'none'];

    var ALERT_NAME = /^[A-Za-z_][A-Za-z0-9_:.-]*$/;
    var DURATION = /^([0-9]+(ms|s|m|h|d|w|y))+$/;
    var DNS = /^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$/;
    var PROM_LABEL = /^[a-zA-Z_][a-zA-Z0-9_]*$/;
    var KUBE_LABEL = /^([a-z0-9]([-a-z0-9.]*[a-z0-9])?\/)?[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$/;

    var state = { ctx: null, model: null, sig: '', act: null, q: '', sev: 'all', st: 'all', selected: '', mode: 'idle', form: null, notice: '', pick: null, busy: false };

    function $(id) {
        return document.getElementById(id);
    }

    function fail(err) {
        state.busy = false;
        $('error').textContent = (err && err.message) || String(err);
        $('error').hidden = false;
    }

    function ruleRef(ns, name) {
        return { kind: P.KINDS.rules, namespace: ns, name: name };
    }

    function findObj(ns, name) {
        return (
            state.model.ruleObjs.filter(function (o) {
                return o.metadata.namespace === ns && o.metadata.name === name;
            })[0] || null
        );
    }

    function currentRule() {
        if (!state.model || !state.selected) return null;
        return (
            state.model.alerts.filter(function (a) {
                return a.key === state.selected;
            })[0] || null
        );
    }

    function canWrite() {
        return !!(state.ctx && state.ctx.write && state.model && state.model.installed);
    }

    function notify(text) {
        state.notice = text;
        setTimeout(function () {
            if (state.notice !== text) return;
            state.notice = '';
            if (state.mode !== 'form') drawSide();
        }, 6000);
    }

    // "a=b\nc=d" <-> { a: 'b', c: 'd' }. Commas separate too; "a: b" is read
    // as well, since that is how labels are written in YAML.
    function parseKV(text) {
        var map = {};
        var bad = [];
        String(text || '')
            .split(/[\n,]/)
            .forEach(function (line) {
                line = line.trim();
                if (!line) return;
                var m = /^([^=:\s]+)\s*[=:]\s*(.*)$/.exec(line);
                if (!m) {
                    bad.push(line);
                    return;
                }
                map[m[1]] = m[2].trim().replace(/^"(.*)"$/, '$1');
            });
        return { map: map, bad: bad };
    }

    function kvText(map) {
        return Object.keys(map || {})
            .map(function (k) {
                return k + '=' + map[k];
            })
            .join('\n');
    }

    // ----- what each rule is doing ---------------------------------------------

    function stateOf(rule) {
        var out = { state: '', since: 0 };
        if (!state.act) return out;
        state.act.lanes.forEach(function (l) {
            if (l.alertname !== rule.alert || !l.state) return;
            if (l.state === 'firing' && out.state !== 'firing') {
                out.state = 'firing';
                out.since = l.since;
            } else if (l.state === 'pending' && !out.state) {
                out.state = 'pending';
                out.since = l.since;
            } else if (l.state === out.state && l.since < out.since) {
                out.since = l.since;
            }
        });
        return out;
    }

    function stateTone(rule, s) {
        return s.state === 'firing' ? P.severityTone(rule.severity) : s.state === 'pending' ? 'pending' : 'inactive';
    }

    // ----- the bar at the top -----------------------------------------------------

    function drawTop() {
        var m = state.model;
        if (!m) return;
        $('new').disabled = !canWrite();
        if (!m.installed) {
            $('sub').textContent = 'The Prometheus Operator is not installed here';
            return;
        }
        var firing = 0;
        var pending = 0;
        // Counted as the overview counts them: Watchdog and InfoInhibitor
        // fire on purpose and are not something to look at.
        m.alerts.forEach(function (a) {
            if (P.ALWAYS_ON[a.alert]) return;
            var s = stateOf(a).state;
            if (s === 'firing') firing++;
            if (s === 'pending') pending++;
        });
        var parts = [K.plural(m.alerts.length, 'alerting rule') + ' in ' + K.plural(m.ruleObjs.length, 'object')];
        if (state.act) parts.push(firing + ' firing', pending + ' pending');
        $('sub').textContent = parts.join(' · ');
    }

    // ----- the list ---------------------------------------------------------------

    function matches(rule) {
        var q = state.q.trim().toLowerCase();
        if (q) {
            var hay = [rule.alert, rule.expr, rule.objName, rule.namespace, rule.group, rule.annotations.summary || '', rule.annotations.description || ''].join('\n').toLowerCase();
            if (hay.indexOf(q) < 0) return false;
        }
        if (state.sev !== 'all') {
            var t = P.severityTone(rule.severity);
            if (state.sev === 'critical' && t !== 'error') return false;
            if (state.sev === 'warning' && t !== 'warn') return false;
            if (state.sev === 'info' && t !== 'info' && t !== 'muted') return false;
        }
        if (state.st !== 'all' && (stateOf(rule).state || 'inactive') !== state.st) return false;
        return true;
    }

    var STATE_RANK = { firing: 0, pending: 1, '': 2 };

    function drawList() {
        var box = $('list');
        var m = state.model;
        if (!m) return;
        var scroll = box.scrollTop;
        box.textContent = '';
        if (!m.installed) {
            box.appendChild(el('p', 'quiet pad', 'This cluster does not serve PrometheusRules, so there are no alerting rules to show. ' + (m.errors.rules || m.errors.servers || '')));
            return;
        }
        var rules = m.alerts.filter(matches);
        box.appendChild(el('div', 'list-count', rules.length === m.alerts.length ? K.plural(m.alerts.length, 'alerting rule') : rules.length + ' of ' + K.plural(m.alerts.length, 'alerting rule')));
        if (!rules.length) {
            box.appendChild(el('p', 'quiet pad', m.alerts.length ? 'No rule matches.' : 'No alerting rules yet. Press New alert to write the first.'));
            return;
        }

        var groups = [];
        var byKey = {};
        rules.forEach(function (r) {
            var key = r.namespace + '/' + r.objName;
            var g = byKey[key];
            if (!g) {
                g = byKey[key] = { key: key, obj: r.obj, rules: [], live: 0 };
                groups.push(g);
            }
            var s = stateOf(r);
            r._state = s;
            if (s.state) g.live++;
            g.rules.push(r);
        });
        groups.sort(function (a, b) {
            return b.live - a.live || a.key.localeCompare(b.key);
        });

        groups.forEach(function (g) {
            var section = el('section', 'obj');
            var head = el('div', 'obj-head');
            add(head, K.icon('rule'), el('span', 'obj-name', g.obj.metadata.name), el('span', 'faint', g.obj.metadata.namespace));
            if (P.helm(g.obj)) head.appendChild(K.chip('Helm', 'muted', null, 'Managed by Helm: an edit here is undone by the next helm upgrade.'));
            if (m.servers.length && !P.loadedBy(m, g.obj).length) {
                head.appendChild(K.chip('not loaded', 'warn', 'alert', P.whyNot(m.servers[0], P.labelsOf(g.obj), g.obj.metadata.namespace, m.nsLabels)));
            }
            head.appendChild(el('span', 'obj-count', String(g.rules.length)));
            section.appendChild(head);

            g.rules.sort(function (a, b) {
                return STATE_RANK[a._state.state] - STATE_RANK[b._state.state] || P.severityRank(a.severity) - P.severityRank(b.severity) || a.alert.localeCompare(b.alert);
            });
            g.rules.forEach(function (r) {
                var row = K.button('', 'rule-row' + (r.key === state.selected ? ' selected' : ''), null, function () {
                    selectRule(r.key);
                });
                var meta = r.group + (r.for ? ' · for ' + r.for : '');
                add(row, el('i', 'state-dot ' + stateTone(r, r._state)), el('span', 'rule-name', r.alert), K.chip(r.severity || 'none', P.severityTone(r.severity)), el('span', 'rule-meta', meta));
                if (r._state.state) row.title = r.alert + ' is ' + r._state.state;
                section.appendChild(row);
            });
            box.appendChild(section);
        });
        box.scrollTop = scroll;
    }

    function selectRule(key) {
        if (state.mode === 'form' && !confirmLeave()) return;
        state.selected = key;
        state.mode = 'detail';
        state.form = null;
        drawList();
        drawSide();
    }

    // Leaving a form someone has typed into is asked about; the bridge has no
    // dialog of its own for it, and window.confirm is refused in the sandbox,
    // so the first click only warns.
    var leaveArmed = false;
    function confirmLeave() {
        if (!state.form || !state.form.dirty || leaveArmed) {
            leaveArmed = false;
            return true;
        }
        leaveArmed = true;
        var d = $('derived');
        if (d) {
            var warn = el('div', 'banner warn', 'You have an unsaved alert. Click again to leave it.');
            d.insertBefore(warn, d.firstChild);
        }
        setTimeout(function () {
            leaveArmed = false;
        }, 4000);
        return false;
    }

    // ----- the side panel -----------------------------------------------------------

    function drawSide() {
        if (state.mode === 'form') return;
        var box = $('side');
        box.textContent = '';
        if (state.notice) {
            var note = el('div', 'notice');
            add(note, K.icon('check'), el('span', '', state.notice));
            box.appendChild(note);
        }
        var rule = currentRule();
        if (state.mode === 'detail' && rule) drawDetail(box, rule);
        else drawIdle(box);
    }

    function drawIdle(box) {
        var m = state.model;
        if (!m) return;
        var wrap = el('div', 'idle');
        var art = el('div', 'empty-art');
        art.appendChild(K.icon('bell'));
        wrap.appendChild(art);
        if (!m.installed) {
            add(wrap, el('h2', '', 'No PrometheusRules here'), el('p', 'quiet', 'This cluster does not serve the Prometheus Operator’s custom resources.'));
            box.appendChild(wrap);
            return;
        }
        add(
            wrap,
            el('h2', '', 'Pick a rule, or write a new one'),
            el('p', 'quiet', 'An alert is a PromQL expression that fires for every series it returns, once it has held for its “for”. It lives in a group inside a PrometheusRule, which the operator hands to every Prometheus whose ruleSelector matches the object’s labels.'),
        );
        if (canWrite()) {
            wrap.appendChild(
                K.button('New alert', 'primary', 'plus', function () {
                    openForm(newForm());
                }),
            );
        }
        box.appendChild(wrap);

        if (m.servers.length) {
            var where = el('div', 'section');
            where.appendChild(el('h3', 'section-title', 'Where rules are loaded from'));
            var ul = el('ul', 'loaders');
            m.servers.forEach(function (s) {
                var spec = s.spec || {};
                var nsSel = spec.ruleNamespaceSelector;
                var from = !nsSel ? ', from ' + s.metadata.namespace + ' only' : P.describeSelector(nsSel) === 'anything' ? ', from any namespace' : ', from namespaces labelled ' + P.describeSelector(nsSel);
                var li = el('li');
                add(
                    li,
                    K.icon('logo'),
                    el('strong', '', s.metadata.name),
                    el(
                        'span',
                        'faint',
                        spec.ruleSelector ? ' loads rules labelled ' + P.describeSelector(spec.ruleSelector) + from : ' has no ruleSelector and loads no rules',
                    ),
                );
                ul.appendChild(li);
            });
            where.appendChild(ul);
            box.appendChild(where);
        }
    }

    function section(title, body) {
        var s = el('div', 'section');
        add(s, el('h3', 'section-title', title), body);
        return s;
    }

    function drawDetail(box, rule) {
        var m = state.model;
        var s = stateOf(rule);
        var head = el('div', 'detail-head');
        add(head, el('h2', 'detail-title', rule.alert));
        box.appendChild(head);

        var chips = el('div', 'chips');
        add(
            chips,
            K.chip(rule.severity || 'no severity', P.severityTone(rule.severity)),
            s.state ? K.chip(s.state + (s.since && state.act ? ' ' + P.duration(state.act.now - s.since) : ''), s.state === 'firing' ? P.severityTone(rule.severity) : 'pending', s.state === 'firing' ? 'bell' : 'clock') : K.chip('inactive', 'muted', 'check'),
            rule.for ? K.chip('for ' + rule.for, 'muted', 'clock') : null,
        );
        box.appendChild(chips);

        var a = rule.annotations;
        if (a.summary || a.message) box.appendChild(el('p', 'detail-summary', a.summary || a.message));
        if (a.description) box.appendChild(el('p', 'detail-text', a.description));

        box.appendChild(section('Expression', el('pre', 'code', rule.expr)));

        var labels = Object.keys(rule.labels).filter(function (k) {
            return k !== 'severity';
        });
        if (labels.length) {
            var lc = el('div', 'chips');
            labels.forEach(function (k) {
                lc.appendChild(K.chip(k + '=' + rule.labels[k], 'muted'));
            });
            box.appendChild(section('Labels', lc));
        }
        var others = Object.keys(a).filter(function (k) {
            return ['summary', 'message', 'description'].indexOf(k) < 0;
        });
        if (others.length) {
            var dl = el('dl', 'kv');
            others.forEach(function (k) {
                var dd = el('dd');
                if (/^https?:\/\//.test(a[k])) {
                    dd.appendChild(
                        K.link(a[k], function () {
                            sdk.openUrl(a[k]).catch(fail);
                        }),
                    );
                } else {
                    dd.textContent = a[k];
                }
                add(dl, el('dt', '', k), dd);
            });
            box.appendChild(section('Annotations', dl));
        }

        var where = el('div', 'where');
        add(where, el('div', '', rule.namespace + ' / ' + rule.objName + ' › ' + rule.group));
        if (m.servers.length) {
            var by = P.loadedBy(m, rule.obj);
            var loaded = el('div', 'loaded ' + (by.length ? 'ok' : 'warn'));
            add(
                loaded,
                K.icon(by.length ? 'check' : 'alert'),
                el(
                    'span',
                    '',
                    by.length
                        ? 'Loaded by ' +
                              by
                                  .map(function (x) {
                                      return x.metadata.name;
                                  })
                                  .join(', ')
                        : 'Not loaded by any Prometheus, so it never fires: ' + P.whyNot(m.servers[0], P.labelsOf(rule.obj), rule.namespace, m.nsLabels) + '.',
                ),
            );
            where.appendChild(loaded);
        }
        if (P.helm(rule.obj)) {
            var helm = el('div', 'loaded muted');
            add(helm, K.icon('alert'), el('span', '', 'Managed by Helm: an edit here is undone by the next helm upgrade. Duplicate it into a rule object of your own to change it for good.'));
            where.appendChild(helm);
        }
        box.appendChild(section('Lives in', where));

        var tools = el('div', 'tools');
        if (canWrite()) {
            add(
                tools,
                K.button('Edit', 'primary', 'edit', function () {
                    openForm(editForm(rule));
                }),
                K.button('Duplicate', '', 'copy', function () {
                    openForm(newForm(rule, true));
                }),
                K.button('Delete', 'danger', 'trash', function () {
                    remove(rule);
                }),
            );
        }
        add(
            tools,
            K.button('Open object', 'ghost', 'open', function () {
                sdk.open(ruleRef(rule.namespace, rule.objName)).catch(fail);
            }),
            K.button('YAML', 'ghost', 'rule', function () {
                sdk.edit(ruleRef(rule.namespace, rule.objName)).catch(fail);
            }),
        );
        box.appendChild(tools);
    }

    // ----- the form -----------------------------------------------------------------

    function newForm(from, duplicate) {
        var m = state.model;
        var server = m.servers[0];
        var sel = server && server.spec && server.spec.ruleSelector;
        var ns = server ? server.metadata.namespace : m.namespaces.indexOf('monitoring') >= 0 ? 'monitoring' : m.namespaces[0] || 'default';
        var f = {
            mode: 'new',
            ref: null,
            base: null,
            template: 'blank',
            fromRule: !!from,
            alert: '',
            expr: '',
            for: '5m',
            severity: 'warning',
            summary: '',
            description: '',
            runbook: '',
            extra: '',
            dest: 'new',
            ns: ns,
            objName: DEFAULT_OBJECT,
            group: DEFAULT_GROUP,
            objLabels: sel && sel.matchLabels ? kvText(sel.matchLabels) : '',
            existing: '',
            existingGroup: '',
            dirty: false,
        };
        if (from) {
            fill(f, from);
            f.base = from.raw;
            if (duplicate) f.alert = from.alert + 'Copy';
        }
        return f;
    }

    function editForm(rule) {
        var f = newForm(rule);
        f.mode = 'edit';
        f.ref = { ns: rule.namespace, objName: rule.objName, group: rule.group, alert: rule.alert, ruleIndex: rule.ruleIndex };
        return f;
    }

    function fill(f, rule) {
        f.alert = rule.alert;
        f.expr = rule.expr;
        f['for'] = rule['for'] || '';
        f.severity = rule.severity || '';
        f.summary = rule.annotations.summary || '';
        f.description = rule.annotations.description || '';
        f.runbook = rule.annotations.runbook_url || '';
        var extra = {};
        Object.keys(rule.labels).forEach(function (k) {
            if (k !== 'severity') extra[k] = rule.labels[k];
        });
        f.extra = kvText(extra);
    }

    function openForm(f) {
        if (!state.model) return;
        state.form = f;
        state.mode = 'form';
        drawForm();
    }

    function cancel() {
        state.form = null;
        state.mode = currentRule() ? 'detail' : 'idle';
        drawSide();
    }

    // Where the rule will go, worked out from the form.
    function target(f) {
        if (f.mode === 'edit') {
            var o = findObj(f.ref.ns, f.ref.objName);
            return { edit: true, obj: o, ns: f.ref.ns, name: f.ref.objName, labels: P.labelsOf(o), group: f.ref.group };
        }
        if (f.dest === 'existing') {
            var e =
                state.model.ruleObjs.filter(function (x) {
                    return P.keyOf(x) === f.existing;
                })[0] || null;
            return { obj: e, ns: e ? e.metadata.namespace : '', name: e ? e.metadata.name : '', labels: P.labelsOf(e), group: f.existingGroup || f.group.trim() };
        }
        var name = f.objName.trim();
        var same = findObj(f.ns, name);
        return { obj: same, ns: f.ns, name: name, labels: same ? P.labelsOf(same) : parseKV(f.objLabels).map, group: f.group.trim() };
    }

    function buildRule(f) {
        var base = f.base || {};
        var labels = parseKV(f.extra).map;
        if (f.severity) labels.severity = f.severity;
        var annotations = {};
        Object.keys(base.annotations || {}).forEach(function (k) {
            if (['summary', 'description', 'runbook_url'].indexOf(k) < 0) annotations[k] = base.annotations[k];
        });
        if (f.summary.trim()) annotations.summary = f.summary.trim();
        if (f.description.trim()) annotations.description = f.description.trim();
        if (f.runbook.trim()) annotations.runbook_url = f.runbook.trim();
        var rule = { alert: f.alert.trim(), expr: f.expr.trim() };
        if (f['for'].trim()) rule['for'] = f['for'].trim();
        if (base.keep_firing_for) rule.keep_firing_for = base.keep_firing_for;
        if (Object.keys(labels).length) rule.labels = labels;
        if (Object.keys(annotations).length) rule.annotations = annotations;
        return rule;
    }

    function newObject(t, rule) {
        var meta = { name: t.name, namespace: t.ns };
        if (Object.keys(t.labels).length) meta.labels = t.labels;
        return { apiVersion: 'monitoring.coreos.com/v1', kind: 'PrometheusRule', metadata: meta, spec: { groups: [{ name: t.group, rules: [rule] }] } };
    }

    function check(f, t) {
        var errors = [];
        var warnings = [];
        var name = f.alert.trim();
        if (!name) errors.push('Give the alert a name.');
        else if (!ALERT_NAME.test(name)) errors.push('An alert name is letters, digits and _ : . -, starting with a letter.');
        if (!f.expr.trim()) errors.push('Write the expression the alert fires on.');
        if (f['for'].trim() && !DURATION.test(f['for'].trim())) errors.push('“For” is a duration such as 30s, 5m or 1h.');
        var extra = parseKV(f.extra);
        extra.bad.forEach(function (l) {
            errors.push('Label “' + l + '” is not key=value.');
        });
        Object.keys(extra.map).forEach(function (k) {
            if (!PROM_LABEL.test(k)) errors.push('“' + k + '” is not a Prometheus label name.');
        });
        if (f.runbook.trim() && !/^https?:\/\//.test(f.runbook.trim())) warnings.push('The runbook link is not an http(s) address.');

        if (!t.edit) {
            if (f.dest === 'existing' && !t.obj) errors.push('Choose the rule object to add it to.');
            if (f.dest === 'new') {
                if (!t.ns) errors.push('Choose a namespace.');
                if (!DNS.test(t.name)) errors.push('A rule object’s name is lowercase letters, digits, - and .');
                if (!t.obj) {
                    var ol = parseKV(f.objLabels);
                    ol.bad.forEach(function (l) {
                        errors.push('Object label “' + l + '” is not key=value.');
                    });
                    Object.keys(ol.map).forEach(function (k) {
                        if (!KUBE_LABEL.test(k)) errors.push('“' + k + '” is not a Kubernetes label key.');
                    });
                }
            }
            if (!t.group) errors.push('Name the group the rule goes in.');
            if (t.obj && name) {
                var g = (P.dig(t.obj, 'spec.groups') || []).filter(function (x) {
                    return x.name === t.group;
                })[0];
                if (
                    g &&
                    (g.rules || []).some(function (r) {
                        return r.alert === name;
                    })
                ) {
                    warnings.push(t.group + ' already has an alert called ' + name + '. Prometheus allows it, but both fire under one name.');
                }
            }
        }
        if (t.obj && P.helm(t.obj)) warnings.push(t.name + ' is managed by Helm: the next helm upgrade will undo this.');

        var m = state.model;
        var loaders = [];
        var reasons = [];
        if (t.ns) {
            m.servers.forEach(function (s) {
                var why = P.whyNot(s, t.labels, t.ns, m.nsLabels);
                if (why) reasons.push(why);
                else loaders.push(s.metadata.name);
            });
        }
        return { errors: errors, warnings: warnings, loaders: loaders, reasons: reasons };
    }

    function preview(f, t) {
        var rule = buildRule(f);
        if (!t.edit && !t.obj) return P.yaml(newObject(t, rule));
        var where = t.ns + '/' + t.name + ', group ' + t.group;
        return '# ' + (t.edit ? 'replaces ' + f.ref.alert + ' in ' : 'added to ') + where + '\n' + P.yaml([rule]);
    }

    // ----- drawing the form ----------------------------------------------------------

    function field(label, control, hint) {
        var f = el('label', 'field');
        add(f, el('span', 'field-label', label), control);
        if (hint) f.appendChild(el('span', 'field-hint', hint));
        return f;
    }

    function input(key, placeholder, cls) {
        var node = el('input', cls || '');
        node.type = 'text';
        node.value = state.form[key];
        node.spellcheck = false;
        if (placeholder) node.placeholder = placeholder;
        node.addEventListener('input', function () {
            state.form[key] = node.value;
            state.form.dirty = true;
            update();
        });
        return node;
    }

    function textarea(key, rows, cls, placeholder) {
        var node = el('textarea', cls || '');
        node.rows = rows;
        node.value = state.form[key];
        node.spellcheck = false;
        if (placeholder) node.placeholder = placeholder;
        node.addEventListener('input', function () {
            state.form[key] = node.value;
            state.form.dirty = true;
            update();
        });
        return node;
    }

    function select(options, value, onChange) {
        var node = el('select');
        options.forEach(function (o) {
            var opt = el('option', '', o[1]);
            opt.value = o[0];
            if (o[0] === value) opt.selected = true;
            node.appendChild(opt);
        });
        node.addEventListener('change', function () {
            onChange(node.value);
        });
        return node;
    }

    function segmented(options, value, onPick) {
        var node = el('div', 'segmented');
        options.forEach(function (o) {
            node.appendChild(
                K.button(o[1], o[0] === value ? 'on' : '', null, function () {
                    onPick(o[0]);
                }),
            );
        });
        return node;
    }

    function drawForm() {
        var f = state.form;
        var m = state.model;
        var box = $('side');
        box.textContent = '';

        var head = el('div', 'detail-head');
        add(
            head,
            el('h2', 'detail-title', f.mode === 'edit' ? 'Edit ' + f.ref.alert : f.fromRule ? 'Duplicate ' + f.alert.replace(/Copy$/, '') : 'New alert'),
            K.button('Cancel', 'ghost small', 'close', cancel),
        );
        box.appendChild(head);

        var form = el('div', 'form');
        if (f.mode === 'new' && !f.fromRule) {
            form.appendChild(
                field(
                    'Start from',
                    select(
                        P.TEMPLATES.map(function (t) {
                            return [t.id, t.label];
                        }),
                        f.template,
                        function (v) {
                            var t = P.TEMPLATES.filter(function (x) {
                                return x.id === v;
                            })[0];
                            f.template = v;
                            ['alert', 'expr', 'for', 'severity', 'summary', 'description'].forEach(function (k) {
                                f[k] = t[k];
                            });
                            drawForm();
                        },
                    ),
                    'A common alert, filled in with kube-prometheus-stack’s metric names. Everything below stays editable.',
                ),
            );
        }
        form.appendChild(field('Alert name', input('alert', 'HighErrorRate'), 'What Alertmanager and every notification will call it.'));
        form.appendChild(field('Expression', textarea('expr', 5, 'code', 'rate(http_requests_total{code=~"5.."}[5m]) > 1'), 'PromQL. It fires once for every series this returns, for as long as it keeps returning it.'));

        var row = el('div', 'field-row');
        var sevs = SEVERITIES.slice();
        if (f.severity && sevs.indexOf(f.severity) < 0) sevs.unshift(f.severity);
        add(
            row,
            field('For', input('for', '5m'), 'How long it must hold before it fires. Empty fires at once.'),
            field(
                'Severity',
                select(
                    [['', '(no severity)']].concat(
                        sevs.map(function (s) {
                            return [s, s];
                        }),
                    ),
                    f.severity,
                    function (v) {
                        f.severity = v;
                        f.dirty = true;
                        update();
                    },
                ),
                'A label; Alertmanager routes on it.',
            ),
        );
        form.appendChild(row);
        form.appendChild(field('Summary', input('summary', '{{ $labels.namespace }}/{{ $labels.pod }} is unhappy'), '{{ $labels.<name> }} and {{ $value }} are filled in when it fires.'));
        form.appendChild(field('Description', textarea('description', 3)));
        form.appendChild(field('Runbook link', input('runbook', 'https://…')));
        form.appendChild(field('More labels', textarea('extra', 2, 'code', 'team=payments'), 'key=value, one per line. Labels are what Alertmanager routes and groups on.'));

        if (f.mode === 'new') form.appendChild(destination(f, m));
        else form.appendChild(field('Lives in', el('div', 'static', f.ref.ns + ' / ' + f.ref.objName + ' › ' + f.ref.group)));

        var derived = el('div', 'derived');
        derived.id = 'derived';
        form.appendChild(derived);
        box.appendChild(form);
        update();
    }

    function destination(f, m) {
        var box = el('fieldset', 'dest');
        box.appendChild(el('legend', '', 'Where it goes'));
        box.appendChild(
            segmented(
                [
                    ['new', 'A rule object of its own'],
                    ['existing', 'Add to an existing one'],
                ],
                f.dest,
                function (v) {
                    f.dest = v;
                    drawForm();
                },
            ),
        );
        if (f.dest === 'new') {
            var nsList = m.namespaces.length ? m.namespaces : [f.ns];
            var row = el('div', 'field-row');
            add(
                row,
                field(
                    'Namespace',
                    select(
                        nsList.map(function (n) {
                            return [n, n];
                        }),
                        f.ns,
                        function (v) {
                            f.ns = v;
                            f.dirty = true;
                            update();
                        },
                    ),
                ),
                field('Rule object', input('objName', DEFAULT_OBJECT)),
            );
            box.appendChild(row);
            box.appendChild(field('Group', input('group', DEFAULT_GROUP), 'Rules in a group are evaluated together, in order.'));
            box.appendChild(field('Object labels', textarea('objLabels', 2, 'code'), 'A Prometheus loads only the rule objects its ruleSelector matches; these start as the labels yours asks for.'));
        } else {
            var own = m.ruleObjs.slice().sort(function (a, b) {
                return (P.helm(a) ? 1 : 0) - (P.helm(b) ? 1 : 0) || P.keyOf(a).localeCompare(P.keyOf(b));
            });
            box.appendChild(
                field(
                    'Rule object',
                    select(
                        [['', 'Choose…']].concat(
                            own.map(function (o) {
                                return [P.keyOf(o), P.keyOf(o) + (P.helm(o) ? '  (Helm)' : '')];
                            }),
                        ),
                        f.existing,
                        function (v) {
                            f.existing = v;
                            f.existingGroup = '';
                            drawForm();
                        },
                    ),
                ),
            );
            var chosen = own.filter(function (o) {
                return P.keyOf(o) === f.existing;
            })[0];
            if (chosen) {
                var groups = (P.dig(chosen, 'spec.groups') || []).map(function (g) {
                    return [g.name, g.name];
                });
                if (!f.existingGroup && groups.length && f.group === DEFAULT_GROUP) f.existingGroup = groups[0][0];
                box.appendChild(
                    field(
                        'Group',
                        select(groups.concat([['', 'A new group…']]), f.existingGroup, function (v) {
                            f.existingGroup = v;
                            drawForm();
                        }),
                    ),
                );
                if (!f.existingGroup) box.appendChild(field('New group name', input('group', DEFAULT_GROUP)));
            }
        }
        return box;
    }

    // Everything the form's values decide: who will load it, what is wrong,
    // the YAML, and whether it can be saved. Redrawn on every keystroke; the
    // inputs themselves are left alone so typing is never interrupted.
    function update() {
        var d = $('derived');
        if (!d || !state.form) return;
        var f = state.form;
        var t = target(f);
        var c = check(f, t);
        d.textContent = '';

        if (!t.edit && f.dest === 'new' && t.obj) {
            var exists = el('div', 'loaded muted');
            add(exists, K.icon('rule'), el('span', '', t.ns + '/' + t.name + ' already exists: the rule is added to it, in group ' + (t.group || '…') + '.'));
            d.appendChild(exists);
        }
        if (state.model.servers.length && t.ns) {
            var loaded = el('div', 'loaded ' + (c.loaders.length ? 'ok' : 'warn'));
            add(loaded, K.icon(c.loaders.length ? 'check' : 'alert'), el('span', '', c.loaders.length ? 'Will be loaded by ' + c.loaders.join(', ') + '.' : 'No Prometheus will load this, so it will never fire: ' + c.reasons[0] + '.'));
            d.appendChild(loaded);
        }
        c.warnings.forEach(function (w) {
            var node = el('div', 'loaded warn');
            add(node, K.icon('alert'), el('span', '', w));
            d.appendChild(node);
        });
        if (c.errors.length && f.dirty) {
            var ul = el('ul', 'problems');
            c.errors.forEach(function (e) {
                ul.appendChild(el('li', '', e));
            });
            d.appendChild(ul);
        }

        d.appendChild(section('What will be written', el('pre', 'code preview', preview(f, t))));

        var tools = el('div', 'tools');
        var label = t.edit ? 'Save change' : t.obj ? 'Add to ' + t.name : 'Create ' + (t.name || 'rule object');
        var save = K.button(label, 'primary', t.edit ? 'check' : 'plus', function () {
            submit();
        });
        save.disabled = c.errors.length > 0 || state.busy;
        if (c.errors.length) save.title = c.errors.join('\n');
        add(tools, save, K.button('Cancel', 'ghost', null, cancel));
        tools.appendChild(el('span', 'faint small', 'The app shows you the change before anything is written.'));
        d.appendChild(tools);
    }

    // ----- writing ------------------------------------------------------------------

    // Reads the object fresh, lets change() rework its groups, and asks to
    // patch them back. A merge patch replaces a list whole, so the groups are
    // always sent entire -- read a moment ago, not when the page last polled.
    function changeGroups(ns, name, change) {
        return sdk.get(ruleRef(ns, name)).then(function (obj) {
            var groups = JSON.parse(JSON.stringify(P.dig(obj, 'spec.groups') || []));
            change(groups);
            return K.apply(sdk, ruleRef(ns, name), { spec: { groups: groups } });
        });
    }

    function indexOfRule(g, ref) {
        var rules = g.rules || [];
        if (rules[ref.ruleIndex] && rules[ref.ruleIndex].alert === ref.alert) return ref.ruleIndex;
        for (var i = 0; i < rules.length; i++) if (rules[i].alert === ref.alert) return i;
        return -1;
    }

    function groupNamed(groups, name, objName) {
        for (var i = 0; i < groups.length; i++) if (groups[i].name === name) return i;
        throw new Error('group ' + name + ' is no longer in ' + objName + ' -- it changed since the page read it');
    }

    function submit() {
        var f = state.form;
        if (!f || state.busy) return;
        var t = target(f);
        if (check(f, t).errors.length) return;
        var rule = buildRule(f);
        var done;
        state.busy = true;
        update();

        if (t.edit) {
            done = changeGroups(t.ns, t.name, function (groups) {
                var g = groups[groupNamed(groups, t.group, t.name)];
                var i = indexOfRule(g, f.ref);
                if (i < 0) throw new Error(f.ref.alert + ' is no longer in ' + t.name + ' -- it changed since the page read it');
                g.rules[i] = rule;
            });
        } else if (t.obj) {
            done = changeGroups(t.ns, t.name, function (groups) {
                var g = groups.filter(function (x) {
                    return x.name === t.group;
                })[0];
                if (g) (g.rules = g.rules || []).push(rule);
                else groups.push({ name: t.group, rules: [rule] });
            });
        } else {
            done = sdk.create({ kind: P.KINDS.rules, namespace: t.ns, object: newObject(t, rule) }).then(
                function () {
                    return true;
                },
                function (err) {
                    if (/declined/.test(err.message)) return false;
                    throw err;
                },
            );
        }

        done.then(function (ok) {
            state.busy = false;
            if (!ok) {
                update();
                return;
            }
            notify(t.edit ? 'Saved ' + rule.alert + '.' : t.obj ? 'Added ' + rule.alert + ' to ' + t.name + '.' : 'Created ' + t.name + ' with ' + rule.alert + '.');
            state.pick = { ns: t.ns, name: t.name, alert: rule.alert };
            state.form = null;
            state.mode = 'detail';
            return refresh();
        }).catch(function (err) {
            fail(err);
            update();
        });
    }

    function remove(rule) {
        changeGroups(rule.namespace, rule.objName, function (groups) {
            var gi = groupNamed(groups, rule.group, rule.objName);
            var g = groups[gi];
            var i = indexOfRule(g, { ruleIndex: rule.ruleIndex, alert: rule.alert });
            if (i < 0) throw new Error(rule.alert + ' is no longer in ' + rule.objName);
            g.rules.splice(i, 1);
            if (!g.rules.length) groups.splice(gi, 1);
        })
            .then(function (ok) {
                if (!ok) return;
                notify('Deleted ' + rule.alert + ' from ' + rule.objName + '.');
                state.selected = '';
                state.mode = 'idle';
                return refresh();
            })
            .catch(fail);
    }

    // ----- reading ------------------------------------------------------------------

    function applyModel(model) {
        state.model = model;
        state.sig = model.sig;
        if (state.pick) {
            var pick = state.pick;
            var found = model.alerts.filter(function (a) {
                return a.namespace === pick.ns && a.objName === pick.name && a.alert === pick.alert;
            });
            if (found.length) {
                state.selected = found[found.length - 1].key;
                state.pick = null;
            }
        }
        drawTop();
        drawList();
        if (state.mode === 'form') update();
        else drawSide();
    }

    function refresh() {
        return P.load(sdk).then(applyModel);
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

    // ----- wiring --------------------------------------------------------------------

    $('logo').appendChild(K.icon('bell'));
    $('new').insertBefore(K.icon('plus'), $('new').firstChild);
    $('new').addEventListener('click', function () {
        if (state.mode === 'form' && !confirmLeave()) return;
        openForm(newForm());
    });
    $('q').addEventListener('input', function () {
        state.q = $('q').value;
        drawList();
    });
    $('st').addEventListener('change', function () {
        state.st = $('st').value;
        drawList();
    });
    Array.prototype.forEach.call(document.querySelectorAll('#sev button'), function (b) {
        b.addEventListener('click', function () {
            state.sev = b.dataset.sev;
            Array.prototype.forEach.call(document.querySelectorAll('#sev button'), function (x) {
                x.classList.toggle('on', x === b);
            });
            drawList();
        });
    });

    // The side panel is as wide as it was left: the width is kept in the
    // frame's hash, which outlives the page when its tab is switched away.
    var keptSide = /(?:^#|&)side=(\d+)/.exec(location.hash || '');
    $('side').parentNode.insertBefore(
        K.grip({
            panel: $('side'),
            prop: '--side-w',
            min: 320,
            room: 300,
            initial: keptSide ? Number(keptSide[1]) : 0,
            label: 'Resize the side panel',
            onResize: function (px, done) {
                if (!done) return;
                try {
                    history.replaceState(null, '', '#' + (px ? 'side=' + px : ''));
                } catch (e) {
                    // A sandboxed frame may refuse; the panel still resizes.
                }
            },
        }),
        $('side'),
    );

    sdk.ready()
        .then(function (context) {
            state.ctx = context;
            every(POLL, function () {
                return P.load(sdk).then(function (model) {
                    $('error').hidden = true;
                    if (model.sig === state.sig) return;
                    applyModel(model);
                });
            });
            every(CHARTS_EVERY, function () {
                return sdk.charts({ minutes: 60 }).then(function (panel) {
                    state.act = panel.source && panel.source.available ? P.activeAlerts(panel) : null;
                    if (!state.model) return;
                    drawTop();
                    drawList();
                    if (state.mode === 'detail') drawSide();
                });
            });
        })
        .catch(fail);
})();
