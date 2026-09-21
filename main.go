//go:build !server

package main

import (
	"log"
	"net/http"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/services"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// main starts the app: it opens the settings file, registers the services the
// frontend calls -- built and wired in internal/services -- and shows the
// main window.
//
// This is the desktop app. The web version, built with -tags server, starts
// from main_server.go instead.
func main() {
	// The settings store holds the user's kubeconfig paths, context aliases and
	// colours. A failure here means we could not read an existing settings file,
	// and carrying on would silently discard the user's customisation.
	settings, err := appconfig.Open()
	if err != nil {
		log.Fatalf("k8sdockside: %v", err)
	}

	built := services.New(settings, services.Options{})

	app := application.New(application.Options{
		Name: "K8s Dockside",
		// Wails renders Name as the title and Description as the body of the
		// About dialog under the app menu, and uses Description nowhere else,
		// so the version goes here to be seen there.
		Description: "A Kubernetes workspace for your local kubeconfig contexts\n\nVersion " + services.DisplayVersion(),
		Services:    built.Services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// Serves plugins' own views from their folders, and refuses those
			// views' sandboxed frames any direct call into the services above.
			// The start page's own images are served ahead of them.
			Middleware: func(next http.Handler) http.Handler {
				return built.Backgrounds(built.PluginViews(next))
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	windowOptions := application.WebviewWindowOptions{
		Title:     "K8s Dockside",
		Width:     defaultWidth,
		Height:    defaultHeight,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		Mac: application.MacWindow{
			// Matches the height of the frontend's own top bar, so the traffic
			// lights sit centred in it rather than over the content below.
			InvisibleTitleBarHeight: titleBarHeight,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(0x0f, 0x13, 0x1a),
		URL:              "/",
	}
	// The window opens where it was last left, and the app keeps note of
	// where that is as it moves: see window.go.
	saved := settings.Get().Window
	placeWindow(&windowOptions, saved)
	window := app.Window.NewWithOptions(windowOptions)
	app.OnShutdown(keepWindow(app, window, saved, settings).save)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
