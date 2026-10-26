package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/ghreleases"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
)

func releasesServiceFor(t *testing.T) (*PluginService, *appconfig.Store, *atomic.Int32) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := appconfig.Open()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/repos/siderolabs/talos/releases" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"v1.14.5","name":"v1.14.5","published_at":"2026-10-15T10:00:00Z","html_url":"https://github.com/siderolabs/talos/releases/tag/v1.14.5"},
			{"tag_name":"v1.15.0","draft":true}]`))
	}))
	t.Cleanup(srv.Close)

	talos := plugins.Plugin{ID: "talos", Name: "Talos", UI: &plugins.UI{Releases: []plugins.UIRelease{{GitHub: "siderolabs/talos"}}}}
	off := plugins.Plugin{ID: "off", Name: "Off", Disabled: true, UI: talos.UI}
	client := ghreleases.New("k8sdockside/test", "")
	client.BaseURL = srv.URL
	s := &PluginService{store: store, releases: client, cached: &plugins.Catalogue{Plugins: []plugins.Plugin{talos, off}}}
	return s, store, &hits
}

func TestReleasesAnswersOnlyForADeclaredRepository(t *testing.T) {
	s, _, hits := releasesServiceFor(t)
	ctx := context.Background()

	for name, tc := range map[string]struct{ plugin, repo, want string }{
		"another plugin": {"acme", "siderolabs/talos", `no plugin called "acme"`},
		"switched off":   {"off", "siderolabs/talos", "switched off"},
		"undeclared":     {"talos", "siderolabs/omni", "does not declare"},
		"not a repo":     {"talos", "../../x", "does not declare"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Releases(ctx, tc.plugin, tc.repo, false); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want one saying %q", err, tc.want)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("GitHub was asked %d times for refused requests", hits.Load())
	}

	got, err := s.Releases(ctx, "talos", "siderolabs/talos", false)
	if err != nil || len(got.Releases) != 1 || got.Releases[0].Tag != "v1.14.5" || got.Stale {
		t.Fatalf("declared: %+v, %v", got, err)
	}
}

func TestReleasesFollowsThePreference(t *testing.T) {
	s, store, hits := releasesServiceFor(t)
	off := false
	prefs := store.Get().Preferences
	prefs.PluginReleases = &off
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Releases(context.Background(), "talos", "siderolabs/talos", false); err == nil || !strings.Contains(err.Error(), "switched off in Settings") {
		t.Errorf("with the preference off: %v", err)
	}
	if hits.Load() != 0 {
		t.Error("GitHub was asked with the preference off")
	}
}

func TestReleasesInTheWebVersionFollowsTheOperator(t *testing.T) {
	s, store, hits := releasesServiceFor(t)
	s.server = true
	// The desktop preference means nothing on a server.
	off := false
	prefs := store.Get().Preferences
	prefs.PluginReleases = &off
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Releases(context.Background(), "talos", "siderolabs/talos", false); err != nil {
		t.Errorf("on a server with requests to GitHub allowed: %v", err)
	}
	s.releasesOff = true
	if _, err := s.Releases(context.Background(), "talos", "siderolabs/talos", true); err == nil || !strings.Contains(err.Error(), "updateCheck: false") {
		t.Errorf("with updateCheck: false: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests, want the one allowed", hits.Load())
	}
}
