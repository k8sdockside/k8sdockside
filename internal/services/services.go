// Package services holds the Wails services the frontend calls: one struct per
// area of the app, whose exported methods the binding generator turns into the
// TypeScript under frontend/bindings. They are built together by New, because
// most of them borrow from one another -- see the comments there -- and
// main.go registers what New returns and nothing else.
package services

import (
	"github.com/rogerwesterbo/k8sdockside/internal/appconfig"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// New wires the thirteen services the frontend calls and returns them ready to
// register with the application, along with the asset middleware that serves
// plugins' own views -- which needs the plugin catalogue, and so comes from
// here rather than from main.go.
func New(settings *appconfig.Store) ([]application.Service, application.Middleware) {
	configs := NewKubeconfigService(settings)
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

	return []application.Service{
		application.NewService(configs),
		application.NewService(NewSettingsService(settings)),
		application.NewService(resources),
		application.NewService(NewActionService(configs, resources.watcher)),
		application.NewService(NewLogService(configs, resources.watcher)),
		application.NewService(NewThemeService(settings)),
		application.NewService(solutions),
		application.NewService(graphs),
		application.NewService(charts),
		application.NewService(shells),
		application.NewService(tunnels),
		application.NewService(finder),
		application.NewService(news),
	}, solutions.assetMiddleware()
}
