// Package services holds the Wails services the frontend calls: one struct per
// area of the app, whose exported methods the binding generator turns into the
// TypeScript under frontend/bindings. They are built together by New, because
// most of them borrow from one another -- see the comments there -- and
// main.go registers what New returns and nothing else.
package services

import (
	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Options says how the services are being run. The zero value is the desktop
// app.
type Options struct {
	// Server is set in the web version, built with -tags server, where the app
	// runs in a pod behind the gateway rather than in a window on the user's
	// own machine.
	Server bool
	// Owners records who opened each stream, so the gateway can deliver its
	// events to that user alone. Nil in the desktop app, which has one user.
	Owners *session.Owners
	// KubeconfigFolders are scanned on every sync on top of the user's own
	// sources, and are not the user's to stop watching: in the web version, the
	// in-cluster context, the Secrets the Helm chart mounts and the files
	// uploaded through the admin page.
	KubeconfigFolders []string
	// NoUpdateChecks forbids the web version from asking GitHub whether a
	// newer release exists even when somebody asks it to: for a server that
	// must not reach the internet. Ignored by the desktop app, whose own
	// setting decides.
	NoUpdateChecks bool
}

// Built is what New hands back for main to register.
type Built struct {
	Services []application.Service
	// PluginViews is the asset middleware that serves plugins' own views --
	// which needs the plugin catalogue, and so comes from here rather than from
	// main.go.
	PluginViews application.Middleware
	// Backgrounds is the asset middleware that serves the images in the
	// user's background folder, which the settings store knows the place of.
	Backgrounds application.Middleware
	// Resync rescans the kubeconfig sources, for the web version's admin page
	// to call once it has added or removed a cluster.
	Resync func()
}

// New wires the sixteen services the frontend calls and returns them ready to
// register with the application.
func New(settings *appconfig.Store, opts Options) Built {
	configs := NewKubeconfigService(settings)
	configs.extra = opts.KubeconfigFolders
	// The action service borrows the resource service's watcher rather than
	// opening its own: acting on an object in a context already showing in a
	// tab should cost no second connection and no second credential exec.
	resources := NewResourceService(configs)
	// The plugin service borrows the same watcher, so a plugin's overview
	// counts through the connection its tabs are already using rather than
	// opening a second one. The two know about each other because a tab opened
	// on a plugin's view has to be resolved back to a real kind -- see
	// ResourceService.view.
	solutions := NewPluginService(settings, configs, resources.watcher)
	resources.usePlugins(solutions)
	// Charts go through the same watcher again: a Prometheus query reaches the
	// cluster through the API server, so it rides the connection a tab already
	// has rather than opening its own.
	graphs := NewMetricsService(settings, configs, resources.watcher, solutions)
	resources.useMetrics(graphs)
	// Terminals and port forwards borrow the same watcher again. Both are
	// long-lived streams rather than requests -- an exec and a forward each
	// hold one connection open for as long as the window shows them -- so both
	// keep their own registry of what is open, the way the log service does.
	// Helm rides the same watcher for the same reason: reading a release is
	// reading Secrets, through the connection the cluster's tabs already have.
	charts := NewHelmService(configs, resources.watcher, settings)
	shells := NewTerminalService(configs, resources.watcher, settings)
	tunnels := NewPortForwardService(configs, resources.watcher, settings)
	// Search borrows the same watcher again: a search of a context already
	// open in a tab goes through that tab's connection, and one of a context
	// that was not leaves a warm client behind for the tab the reader is about
	// to open on what they found.
	finder := NewSearchService(configs, resources.watcher)
	// The one service that reaches beyond this machine and its clusters: it
	// asks GitHub whether a newer release exists. It reads the settings for
	// whether it may, and writes them for what the user has already seen.
	news := NewUpdateService(settings)
	actions := NewActionService(configs, resources.watcher)
	logs := NewLogService(configs, resources.watcher)
	prefs := NewSettingsService(settings)
	looks := NewThemeService(settings)
	backdrops := NewBackgroundService(settings)
	// The cluster alerts, posted as the system's own notifications. It reads
	// nothing and decides nothing -- the window compares one reading of a
	// cluster with the last and says what to post -- so it borrows nothing.
	alerts := &NotifyService{disabled: opts.Server}

	// Every service that opens a stream files it under whoever opened it, so
	// the web version can deliver the stream's events to that user alone.
	resources.owners = opts.Owners
	charts.owners = opts.Owners
	shells.owners = opts.Owners
	finder.owners = opts.Owners
	actions.owners = opts.Owners
	logs.owners = opts.Owners
	// And every feature that needs the user's own machine stands down in the
	// web version, where "this machine" is a pod nobody is sitting at.
	shells.server = opts.Server
	tunnels.server = opts.Server
	solutions.server = opts.Server
	prefs.server = opts.Server
	looks.server = opts.Server
	backdrops.server = opts.Server
	// The web version is updated by whoever deploys it, not by the person
	// using it: it never asks GitHub on its own there, only when somebody
	// presses Check now -- and not even then when the operator said so.
	news.server = opts.Server
	news.noChecks = opts.Server && opts.NoUpdateChecks

	return Built{
		Services: []application.Service{
			application.NewService(configs),
			application.NewService(prefs),
			application.NewService(resources),
			application.NewService(actions),
			application.NewService(logs),
			application.NewService(looks),
			application.NewService(backdrops),
			application.NewService(solutions),
			application.NewService(graphs),
			application.NewService(charts),
			application.NewService(shells),
			application.NewService(tunnels),
			application.NewService(finder),
			application.NewService(news),
			application.NewService(alerts),
			application.NewService(&SessionService{server: opts.Server}),
		},
		PluginViews: solutions.assetMiddleware(),
		Backgrounds: backdrops.assetMiddleware(),
		Resync:      func() { configs.Sync() },
	}
}
