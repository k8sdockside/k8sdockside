// The form an Application's -- or an ApplicationSet template's -- overrides
// are edited in: one source at a time, with the fields its tool takes.
//
// It edits the form object it is given, in place, and says so through
// onChange; it redraws itself only when a row is added or removed, so typing
// never loses the caret to a redraw.
(function () {
    'use strict';

    var K = window.ArgoKit;
    var el = K.el;
    var add = K.add;

    function input(value, placeholder, onInput, className) {
        var node = el('input', 'ov-input' + (className ? ' ' + className : ''));
        node.type = 'text';
        node.value = value;
        node.placeholder = placeholder || '';
        node.spellcheck = false;
        node.addEventListener('input', function () {
            onInput(node.value);
        });
        return node;
    }

    function field(label, hint, control) {
        var row = el('label', 'ov-field');
        var text = el('span', 'ov-label', label);
        if (hint) text.title = hint;
        add(row, text, control);
        return row;
    }

    function removeButton(title, onClick) {
        var b = K.button('', 'icon-button ov-remove', 'close', onClick);
        b.title = title;
        b.setAttribute('aria-label', title);
        return b;
    }

    function addButton(text, onClick) {
        return K.button(text, 'small ghost ov-add', 'plus', onClick);
    }

    /**
     * A list of single strings -- value files, images -- with a row each and
     * a way to add one.
     */
    function stringList(title, items, placeholder, changed, hint) {
        var box = el('div', 'ov-group');
        var head = el('div', 'ov-group-head');
        var h = el('span', 'ov-group-title', title);
        if (hint) h.title = hint;
        head.appendChild(h);
        box.appendChild(head);
        var rows = el('div', 'ov-rows');
        function draw() {
            rows.textContent = '';
            items.forEach(function (value, i) {
                var row = el('div', 'ov-row single');
                add(
                    row,
                    input(value, placeholder, function (v) {
                        items[i] = v;
                        changed();
                    }, 'mono'),
                    removeButton('Remove', function () {
                        items.splice(i, 1);
                        changed();
                        draw();
                    }),
                );
                rows.appendChild(row);
            });
            if (!items.length) rows.appendChild(el('p', 'faint small ov-none', 'None.'));
        }
        draw();
        box.appendChild(rows);
        box.appendChild(
            addButton('Add', function () {
                items.push('');
                changed();
                draw();
                var inputs = rows.querySelectorAll('input');
                if (inputs.length) inputs[inputs.length - 1].focus();
            }),
        );
        return box;
    }

    /** Helm's parameters: name, value and whether to keep the value a string. */
    function parameters(params, changed) {
        var box = el('div', 'ov-group');
        var head = el('div', 'ov-group-head');
        add(head, el('span', 'ov-group-title', 'Parameters'), el('span', 'faint small', 'as --set name=value'));
        box.appendChild(head);
        var rows = el('div', 'ov-rows');
        function draw() {
            rows.textContent = '';
            params.forEach(function (p, i) {
                var row = el('div', 'ov-row param');
                var force = el('label', 'ov-force');
                var box2 = el('input', '');
                box2.type = 'checkbox';
                box2.checked = p.forceString;
                box2.addEventListener('change', function () {
                    p.forceString = box2.checked;
                    changed();
                });
                add(force, box2, el('span', '', 'string'));
                force.title = 'Keep the value a string, as --set-string does: "true" or "8080" would otherwise become a boolean or a number.';
                add(
                    row,
                    input(p.name, 'name, e.g. image.tag', function (v) {
                        p.name = v;
                        changed();
                    }, 'mono'),
                    input(p.value, 'value', function (v) {
                        p.value = v;
                        changed();
                    }, 'mono'),
                    force,
                    removeButton('Remove this parameter', function () {
                        params.splice(i, 1);
                        changed();
                        draw();
                    }),
                );
                rows.appendChild(row);
            });
            if (!params.length) rows.appendChild(el('p', 'faint small ov-none', 'None: the chart’s own values apply.'));
        }
        draw();
        box.appendChild(rows);
        box.appendChild(
            addButton('Add parameter', function () {
                params.push({ name: '', value: '', forceString: false });
                changed();
                draw();
                var inputs = rows.querySelectorAll('input[type="text"]');
                if (inputs.length > 1) inputs[inputs.length - 2].focus();
            }),
        );
        return box;
    }

    /** Kustomize's replicas: a resource name and a count. */
    function replicas(list, changed) {
        var box = el('div', 'ov-group');
        var head = el('div', 'ov-group-head');
        add(head, el('span', 'ov-group-title', 'Replicas'), el('span', 'faint small', 'per Deployment or StatefulSet'));
        box.appendChild(head);
        var rows = el('div', 'ov-rows');
        function draw() {
            rows.textContent = '';
            list.forEach(function (r, i) {
                var row = el('div', 'ov-row pair');
                add(
                    row,
                    input(r.name, 'resource name', function (v) {
                        r.name = v;
                        changed();
                    }, 'mono'),
                    input(r.count, 'count', function (v) {
                        r.count = v;
                        changed();
                    }, 'mono narrow'),
                    removeButton('Remove', function () {
                        list.splice(i, 1);
                        changed();
                        draw();
                    }),
                );
                rows.appendChild(row);
            });
            if (!list.length) rows.appendChild(el('p', 'faint small ov-none', 'None.'));
        }
        draw();
        box.appendChild(rows);
        box.appendChild(
            addButton('Add', function () {
                list.push({ name: '', count: '1' });
                changed();
                draw();
            }),
        );
        return box;
    }

    function helmFields(form, changed, opts) {
        var helm = form.helm;
        var section = el('section', 'ov-section');
        section.appendChild(el('h3', 'ov-section-title', 'Helm'));
        var grid = el('div', 'ov-grid');
        grid.appendChild(
            field('Release name', 'Leave empty for the Application’s name.', input(helm.releaseName, 'the Application’s name', function (v) {
                helm.releaseName = v;
                changed();
            }, 'mono')),
        );
        section.appendChild(grid);
        section.appendChild(parameters(helm.parameters, changed));
        section.appendChild(
            stringList('Value files', helm.valueFiles, 'values-prod.yaml, or $values/path/values.yaml', changed, 'Files of values in the chart, applied in order. A multi-source app can take them from another source by its ref: $values/…'),
        );

        var values = el('div', 'ov-group');
        var head = el('div', 'ov-group-head');
        add(head, el('span', 'ov-group-title', 'Values'), el('span', 'faint small', 'YAML, applied after the value files'));
        values.appendChild(head);
        var area = el('textarea', 'ov-values');
        area.value = helm.values;
        area.rows = Math.min(14, Math.max(4, helm.values.split('\n').length + 1));
        area.spellcheck = false;
        area.placeholder = 'replicaCount: 3\nimage:\n  tag: v2';
        area.addEventListener('input', function () {
            helm.values = area.value;
            changed();
        });
        values.appendChild(area);
        if (helm.valuesObject !== null) {
            var note = el('div', 'ov-note');
            add(
                note,
                el('span', '', 'This source also has valuesObject, which is merged over these. It is shown as it is; change it in the YAML editor.'),
                opts.onEditYaml ? K.button('Edit YAML', 'small', 'edit', opts.onEditYaml) : null,
            );
            values.appendChild(note);
            values.appendChild(el('pre', 'ov-pre', JSON.stringify(helm.valuesObject, null, 2)));
        }
        section.appendChild(values);
        return section;
    }

    function kustomizeFields(form, changed) {
        var k = form.kustomize;
        var section = el('section', 'ov-section');
        section.appendChild(el('h3', 'ov-section-title', 'Kustomize'));
        section.appendChild(
            stringList('Images', k.images, 'nginx=nginx:1.27, or ghcr.io/org/app:v2', changed, 'As kustomize edit set image takes them: name=newName:tag, or name:tag.'),
        );
        section.appendChild(replicas(k.replicas, changed));
        var grid = el('div', 'ov-grid three');
        add(
            grid,
            field('Name prefix', '', input(k.namePrefix, 'none', function (v) {
                k.namePrefix = v;
                changed();
            }, 'mono')),
            field('Name suffix', '', input(k.nameSuffix, 'none', function (v) {
                k.nameSuffix = v;
                changed();
            }, 'mono')),
            field('Namespace', 'Overrides the namespace of every resource Kustomize renders.', input(k.namespace, 'none', function (v) {
                k.namespace = v;
                changed();
            }, 'mono')),
        );
        section.appendChild(grid);
        return section;
    }

    /**
     * Draws the form for one source into box. opts: { onChange, onEditYaml,
     * templated } -- templated says the values may hold {{ }} template
     * expressions, as an ApplicationSet's do.
     */
    function render(box, form, opts) {
        box.textContent = '';
        var changed = opts.onChange;

        var grid = el('div', 'ov-grid');
        grid.appendChild(
            field('Target revision', 'A branch, a tag, a commit -- or for a Helm chart, its version.', input(form.targetRevision, form.type === 'Helm' ? 'chart version, e.g. 1.4.2' : 'HEAD, a branch, a tag or a commit', function (v) {
                form.targetRevision = v;
                changed();
            }, 'mono')),
        );
        box.appendChild(grid);

        if (opts.templated) {
            box.appendChild(el('p', 'faint small ov-hint', 'Values here can use the generators’ parameters, such as {{.name}} or {{name}}.'));
        }

        if (form.type === 'Helm') {
            box.appendChild(helmFields(form, changed, opts));
        } else if (form.type === 'Kustomize') {
            box.appendChild(kustomizeFields(form, changed));
        } else if (form.type === 'Directory' || form.type === 'Plugin') {
            box.appendChild(el('p', 'faint small ov-hint', 'A ' + form.type.toLowerCase() + ' source has no Helm or Kustomize settings to override; its target revision is the one to change.'));
        } else {
            // Argo CD has not said yet which tool renders this path: both are
            // offered, folded.
            var helm = el('details', 'ov-fold');
            helm.appendChild(el('summary', '', 'Helm settings'));
            helm.appendChild(helmFields(form, changed, opts));
            var kust = el('details', 'ov-fold');
            kust.appendChild(el('summary', '', 'Kustomize settings'));
            kust.appendChild(kustomizeFields(form, changed));
            add(box, el('p', 'faint small ov-hint', 'Argo CD has not reported which tool renders this source yet.'), helm, kust);
        }
    }

    window.ArgoOverrideEditor = { render: render };
})();
