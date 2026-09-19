//go:build server

package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/gateway"
	"github.com/k8sdockside/k8sdockside/internal/services"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// main starts the web version: the same services and the same frontend as the
// desktop app, served by Wails as an HTTP server on loopback, behind the
// gateway that signs people in. Everything is configured from the environment
// -- see gateway.ConfigFromEnv.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := gateway.ConfigFromEnv()
	if err != nil {
		log.Fatalf("k8sdockside: %v", err)
	}
	// Before anything reads the environment Prepare adjusts: the settings
	// store below, and Wails.
	folders, err := cfg.Prepare()
	if err != nil {
		log.Fatalf("k8sdockside: preparing %s: %v", cfg.DataDir, err)
	}
	settings, err := appconfig.Open()
	if err != nil {
		log.Fatalf("k8sdockside: %v", err)
	}

	owners := session.NewOwners()
	built := services.New(settings, services.Options{
		Server:            true,
		Owners:            owners,
		KubeconfigFolders: folders,
	})

	gw, err := gateway.New(cfg, gateway.Deps{
		Owners:      owners,
		OwnedEvents: services.OwnedEvents,
		Resync:      built.Resync,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("k8sdockside: %v", err)
	}
	port, err := gateway.LoopbackPort()
	if err != nil {
		log.Fatalf("k8sdockside: %v", err)
	}
	gw.SetUpstream(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))

	app := application.New(application.Options{
		Name:        "K8s Dockside",
		Description: "A Kubernetes workspace\n\nVersion " + services.DisplayVersion(),
		Services:    built.Services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// The gateway's half comes first: nothing reaches the app, plugin
			// views included, without having been let through and named.
			Middleware: func(next http.Handler) http.Handler {
				return gw.Identity(built.PluginViews(next))
			},
		},
		Server: application.ServerOptions{
			// Loopback only: the gateway is the way in.
			Host: "127.0.0.1",
			Port: port,
			// Negative switches the timeouts off. The event socket, log
			// streams and terminals are held open for as long as a tab is,
			// and Wails' thirty-second defaults would cut them; only the
			// gateway can reach this listener, and it has its own.
			ReadTimeout:  -1,
			WriteTimeout: -1,
		},
	})

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		if err := gw.Serve(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("k8sdockside: %v", err)
		}
	}()

	// Run returns on SIGTERM or SIGINT, after the services have shut down --
	// which closes every terminal, and deletes every node shell's pod.
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
