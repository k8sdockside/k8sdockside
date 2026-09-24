// The Overrides view, on an Application and on an ApplicationSet: drawn as
// a panel in the detail view (overrides-panel.js) and in the application
// board's drawer (board.js).
//
// On an Application it edits what `argocd app set` does -- the target
// revision, Helm parameters, values, value files and release name, Kustomize
// images, replicas, prefix, suffix and namespace -- and says first who owns
// the Application, because that decides whether an override made here lasts:
// an ApplicationSet rewrites the Applications it makes, and a parent in an app
// of apps puts its children back.
//
// On an ApplicationSet it edits the same things in the template every
// Application it makes is stamped from, and shows what its generators feed
// into that template.
//
// Every change goes through the app's own dialog, which shows the patch and
// the object before anything is written.
(function () {
    'use strict';

    var A = window.Argo;
    var K = window.ArgoKit;
    var O = window.ArgoOverrides;
    var Editor = window.ArgoOverrideEditor;
    var el = K.el;
    var add = K.add;

    var POLL = 5000;

    /**
     * One Overrides view, for one Application or ApplicationSet. opts:
     *   sdk      the bridge
     *   ref      { kind, namespace, name } of the object
     *   read     () => Promise of the object, read live
     *   write    whether the plugin may change anything
     *   onError  (err) => shown wherever the host shows errors
     *   onRecover () => called when a read works again, to clear it
     * Returns { node, start, stop }: node is the view's own element, which the
     * host places and may move -- its state, typing included, lives with it.
     */
    function create(opts) {
        var sdk = opts.sdk;
        var fail = opts.onError;
        var node = el('div', 'ov-root');
        var timer = 0;

        var state = {
            isSet: opts.ref.kind === A.KINDS.appsets,
            obj: null,
            apps: [],
            appsets: [],
            sources: null, // { multi, list } as read
            forms: [], // one per source, edited in place
            index: 0,
            dirty: false,
            version: '',
            stale: false,
            busy: false,
        };

        function open(ref) {
            sdk.open(ref).catch(fail);
        }

        function ref() {
            return { kind: opts.ref.kind, namespace: opts.ref.namespace, name: opts.ref.name };
        }

        function specOf(obj) {
            if (state.isSet) return (obj.spec && obj.spec.template && obj.spec.template.spec) || {};
            return obj.spec || {};
        }

        function at() {
            return state.isSet ? ['spec', 'template', 'spec'] : ['spec'];
        }

        /** What Argo CD reported renders each source, where it has. */
        function reportedTypes(obj) {
            var status = obj.status || {};
            if (Array.isArray(status.sourceTypes)) return status.sourceTypes;
            return status.sourceType ? [status.sourceType] : [];
        }

        /** Reads the object into fresh forms, dropping whatever was being edited. */
        function adopt(obj) {
            state.obj = obj;
            state.version = obj.metadata.resourceVersion || '';
            state.sources = O.sourcesOf(specOf(obj));
            var types = state.isSet ? [] : reportedTypes(obj);
            state.forms = state.sources.list.map(function (s, i) {
                return O.readSource(s, types[i]);
            });
            if (state.index >= state.forms.length) state.index = 0;
            state.dirty = false;
            state.stale = false;
        }

        function edited() {
            return state.sources.list.map(function (s, i) {
                return O.writeSource(s, state.forms[i]);
            });
        }

        function changes() {
            var types = state.isSet ? [] : reportedTypes(state.obj);
            var out = [];
            state.sources.list.forEach(function (s, i) {
                var label = state.sources.multi ? O.sourceLabel(s, i) : '';
                out = out.concat(O.describeChanges(O.readSource(s, types[i]), state.forms[i], label));
            });
            return out;
        }

        function changed() {
            var was = state.dirty;
            state.dirty = !!O.patchFor(specOf(state.obj), at(), edited());
            if (was !== state.dirty) drawFooter();
            else if (state.dirty) drawFooter();
        }

        // ----- drawing --------------------------------------------------------------

        function ownerBanner() {
            if (state.isSet) return null;
            var owner = O.ownerOf(state.obj, state.apps, state.appsets);
            if (!owner.kind) return null;
            var box = el('div', 'why ' + (owner.reverts ? 'warn' : 'info'));
            var body = el('div', 'ov-owner');
            body.appendChild(el('span', '', owner.text));
            if (!owner.missing) {
                var target =
                    owner.kind === 'appset'
                        ? { kind: A.KINDS.appsets, namespace: owner.namespace, name: owner.name }
                        : { kind: A.KINDS.apps, namespace: owner.namespace, name: owner.name };
                body.appendChild(
                    K.link('Open ' + (owner.kind === 'appset' ? 'the ApplicationSet' : owner.name), function () {
                        open(target);
                    }, 'Make the change there, where it lasts'),
                );
            }
            add(box, K.icon(owner.reverts ? 'alert' : 'app'), body);
            state.owner = owner;
            return box;
        }

        function setIntro() {
            var spec = state.obj.spec || {};
            var box = el('div', 'ov-set');
            var made = state.apps.filter(function (a) {
                return ((a.metadata && a.metadata.ownerReferences) || []).some(function (o) {
                    return o.kind === 'ApplicationSet' && o.name === state.obj.metadata.name;
                });
            });
            var line = el('p', 'small dim');
            line.appendChild(
                document.createTextNode(
                    'The template every Application this set makes is stamped from' +
                        (made.length ? ' — ' + K.plural(made.length, 'Application') + ' now. ' : '. ') +
                        'A change here reaches all of them on the set’s next pass.',
                ),
            );
            box.appendChild(line);
            if (made.length) {
                var list = el('div', 'ov-made');
                made.slice(0, 12).forEach(function (a) {
                    list.appendChild(
                        K.button(a.metadata.name, 'link-chip', 'app', function () {
                            open({ kind: A.KINDS.apps, namespace: a.metadata.namespace, name: a.metadata.name });
                        }),
                    );
                });
                if (made.length > 12) list.appendChild(el('span', 'faint small', '+' + (made.length - 12) + ' more'));
                box.appendChild(list);
            }
            if (spec.templatePatch) {
                var note = el('div', 'why info');
                add(
                    note,
                    K.icon('edit'),
                    el('span', '', 'This set also has a templatePatch, applied over the template per Application. It can change what is set here; it is edited in the YAML editor.'),
                );
                box.appendChild(note);
            }
            var generators = O.generatorsOf(state.obj);
            if (generators.length) box.appendChild(generatorView(generators));
            return box;
        }

        function generatorView(generators) {
            var details = el('details', 'ov-fold ov-generators');
            details.appendChild(el('summary', '', 'Generators · what fills the template’s parameters'));
            function draw(into, list, depth) {
                list.forEach(function (g) {
                    var block = el('div', 'ov-gen' + (depth ? ' nested' : ''));
                    var head = el('div', 'ov-gen-head');
                    add(head, el('code', '', g.kind), g.note ? el('span', 'faint small', g.note) : null);
                    block.appendChild(head);
                    if (g.elements.length) {
                        var keys = [];
                        g.elements.forEach(function (e) {
                            Object.keys(e).forEach(function (k) {
                                if (keys.indexOf(k) < 0) keys.push(k);
                            });
                        });
                        var table = el('table', 'ov-table');
                        var tr = el('tr', '');
                        keys.forEach(function (k) {
                            tr.appendChild(el('th', '', k));
                        });
                        table.appendChild(tr);
                        g.elements.slice(0, 30).forEach(function (e) {
                            var row = el('tr', '');
                            keys.forEach(function (k) {
                                var v = e[k];
                                row.appendChild(el('td', 'mono', v === undefined ? '' : typeof v === 'object' ? JSON.stringify(v) : String(v)));
                            });
                            table.appendChild(row);
                        });
                        block.appendChild(table);
                    }
                    if (g.parts.length) draw(block, g.parts, depth + 1);
                    into.appendChild(block);
                });
            }
            draw(details, generators, 0);
            details.appendChild(el('p', 'faint small', 'Generators are changed in the YAML editor.'));
            return details;
        }

        function tabs() {
            if (!state.sources.multi) return null;
            var bar = el('div', 'ov-tabs');
            bar.setAttribute('role', 'tablist');
            state.sources.list.forEach(function (s, i) {
                var t = el('button', 'ov-tab' + (i === state.index ? ' on' : ''), O.sourceLabel(s, i));
                t.type = 'button';
                t.setAttribute('role', 'tab');
                t.setAttribute('aria-selected', String(i === state.index));
                t.title = (s.repoURL || '') + (s.path ? ' · ' + s.path : '') + (s.chart ? ' · chart ' + s.chart : '');
                t.addEventListener('click', function () {
                    state.index = i;
                    draw();
                });
                bar.appendChild(t);
            });
            return bar;
        }

        function editYaml() {
            sdk.edit(ref()).catch(fail);
        }

        function drawFooter() {
            var foot = node.querySelector('.ov-foot');
            if (!foot) return;
            foot.textContent = '';
            if (state.stale) {
                var stale = el('div', 'why warn');
                add(
                    stale,
                    K.icon('alert'),
                    el('span', '', 'This changed in the cluster while you were editing. Applying writes your changes over the new version.'),
                    K.button('Discard mine and reload', 'small', 'refresh', function () {
                        state.stale = false;
                        reload(true);
                    }),
                );
                foot.appendChild(stale);
            }
            if (state.dirty) {
                var list = el('ul', 'ov-changes');
                changes().forEach(function (line) {
                    list.appendChild(el('li', '', line));
                });
                foot.appendChild(list);
            }
            var row = el('div', 'ov-actions');
            var reverts = state.owner && state.owner.reverts;
            var apply = K.button(state.busy ? 'Applying…' : reverts ? 'Apply anyway' : 'Apply', 'small primary', 'check', applyChanges);
            apply.disabled = !state.dirty || state.busy || !opts.write;
            if (!opts.write) apply.title = 'This plugin may not change anything here.';
            if (reverts) apply.title = 'It will be put back by ' + state.owner.name + '; see above.';
            var discard = K.button('Discard', 'small', 'close', function () {
                adopt(state.obj);
                draw();
            });
            discard.disabled = !state.dirty || state.busy;
            add(row, apply, discard, K.button('Edit YAML', 'small ghost', 'edit', editYaml));
            foot.appendChild(row);
        }

        function draw() {
            var root = node;
            root.textContent = '';
            state.owner = null;

            if (state.isSet) root.appendChild(setIntro());
            else {
                var banner = ownerBanner();
                if (banner) root.appendChild(banner);
            }

            if (!state.forms.length) {
                root.appendChild(el('p', 'faint', 'No source to override: this ' + (state.isSet ? 'template' : 'Application') + ' names none.'));
                return;
            }
            var t = tabs();
            if (t) root.appendChild(t);
            var box = el('div', 'ov-editor');
            Editor.render(box, state.forms[state.index], {
                onChange: changed,
                onEditYaml: editYaml,
                templated: state.isSet,
            });
            root.appendChild(box);
            var foot = el('div', 'ov-foot');
            root.appendChild(foot);
            drawFooter();
        }

        // ----- reading and writing ------------------------------------------------------

        function applyChanges() {
            var patch = O.patchFor(specOf(state.obj), at(), edited());
            if (!patch) return;
            state.busy = true;
            drawFooter();
            K.apply(sdk, ref(), patch)
                .then(function (done) {
                    state.busy = false;
                    if (done) return reload(true);
                    drawFooter();
                })
                .catch(function (err) {
                    state.busy = false;
                    fail(err);
                    drawFooter();
                });
        }

        /**
         * Every Application and ApplicationSet, for working out who owns this
         * one. Read on the first load and then once a minute, not on every
         * poll: in a cluster with hundreds of Applications that is the
         * heaviest read the view makes, and who owns an Application almost
         * never changes while somebody is looking at it.
         */
        var LISTS_EVERY = 60000;
        var listed = { at: 0, value: null };
        function lists(force) {
            if (!force && listed.value && Date.now() - listed.at < LISTS_EVERY) return Promise.resolve(listed.value);
            var apps = sdk.list({ kind: A.KINDS.apps }).catch(function () {
                return [];
            });
            var sets = sdk.list({ kind: A.KINDS.appsets }).catch(function () {
                return [];
            });
            return Promise.all([apps, sets]).then(function (value) {
                listed = { at: Date.now(), value: value };
                return value;
            });
        }

        /**
         * Reads the object again. Unedited, a new version simply replaces the
         * form; while somebody is editing, it is only noted -- their typing is
         * not thrown away under them.
         */
        function reload(force) {
            return Promise.all([opts.read(), lists(force)])
                .then(function (got) {
                    if (opts.onRecover) opts.onRecover();
                    var obj = got[0];
                    state.apps = got[1][0];
                    state.appsets = got[1][1];
                    var version = obj.metadata.resourceVersion || '';
                    if (force || !state.obj) {
                        adopt(obj);
                        draw();
                        return;
                    }
                    if (version === state.version) return;
                    if (state.dirty) {
                        // Argo CD rewrites an Application's status all the
                        // time; only somebody changing the sources being
                        // edited is worth a word. Anything else is taken in
                        // quietly -- the edits are diffed against the
                        // sources, which have not moved.
                        var before = JSON.stringify(O.sourcesOf(specOf(state.obj)));
                        var after = JSON.stringify(O.sourcesOf(specOf(obj)));
                        if (before === after) {
                            state.obj = obj;
                            state.version = version;
                            return;
                        }
                        if (!state.stale) {
                            state.stale = true;
                            drawFooter();
                        }
                        return;
                    }
                    adopt(obj);
                    draw();
                })
                .catch(fail);
        }

        function start() {
            if (timer) return;
            reload(true);
            timer = setInterval(function () {
                if (!state.busy && document.visibilityState !== 'hidden') reload(false);
            }, POLL);
        }

        function stop() {
            clearInterval(timer);
            timer = 0;
        }

        return { node: node, start: start, stop: stop };
    }

    window.ArgoOverridesView = { create: create };
})();
