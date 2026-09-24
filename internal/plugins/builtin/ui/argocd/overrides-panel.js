// The Overrides panel in an Application's or an ApplicationSet's detail
// view: the Overrides view (overrides-view.js), drawn for the panel's object.
(function () {
    'use strict';

    var sdk = window.k8sdockside;

    function fail(err) {
        var box = document.getElementById('error');
        box.textContent = (err && err.message) || String(err);
        box.hidden = false;
    }

    sdk.ready()
        .then(function (ctx) {
            if (!ctx.object) {
                fail(new Error('This page is a panel, drawn for one Application or ApplicationSet.'));
                return;
            }
            if (!ctx.write) fail(new Error('This plugin may not change anything, so overrides can only be read here.'));
            var view = window.ArgoOverridesView.create({
                sdk: sdk,
                ref: ctx.object,
                read: function () {
                    return sdk.object();
                },
                write: ctx.write,
                onError: fail,
                onRecover: function () {
                    if (ctx.write) document.getElementById('error').hidden = true;
                },
            });
            var root = document.getElementById('root');
            root.textContent = '';
            root.appendChild(view.node);
            view.start();
        })
        .catch(fail);
})();
