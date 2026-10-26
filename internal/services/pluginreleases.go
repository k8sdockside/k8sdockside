package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/ghreleases"
	"github.com/k8sdockside/k8sdockside/internal/updates"
)

// What a page is told while asking GitHub for release lists is switched off.
// Pages are expected to fall back on what they ship with.
var (
	errReleasesOffDesktop = errors.New("release lists from GitHub are switched off in Settings → Notifications → Release lists for plugins")
	errReleasesOffServer  = errors.New("release lists from GitHub are switched off on this server (updateCheck: false)")
)

// newReleasesClient keeps the lists under the user's cache directory, or in
// memory only when there is none (a pod without a writable home, say).
func newReleasesClient() *ghreleases.Client {
	dir := ""
	if base, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(base, "k8sdockside", "releases")
	}
	return ghreleases.New("k8sdockside/"+DisplayVersion()+" (+https://github.com/"+updates.Repo+")", dir)
}

// releasesAllowed is whether GitHub may be asked for release lists right now:
// the user's choice on the desktop, the operator's in the web version, where
// the same switch that stops the update check and the plugin list stops this.
func (s *PluginService) releasesAllowed() error {
	if s.server {
		if s.releasesOff {
			return errReleasesOffServer
		}
		return nil
	}
	if s.store != nil && !s.store.PluginReleases() {
		return errReleasesOffDesktop
	}
	return nil
}

// Releases answers a plugin's own view with the published releases of a
// GitHub repository its manifest names, newest first and never a draft.
//
// The view names the repository, so it must be one the manifest declares:
// otherwise a plugin could have the app ask GitHub about anything. The app
// asks anonymously, keeps the answer for hours, and serves the last good one
// marked stale when GitHub cannot be reached or is rate-limiting.
func (s *PluginService) Releases(ctx context.Context, pluginID, repo string, refresh bool) (ghreleases.List, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return ghreleases.List{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return ghreleases.List{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	repo = strings.TrimSpace(repo)
	if !plugin.CanAskReleases(repo) {
		return ghreleases.List{}, fmt.Errorf("the %s plugin does not declare \"ui\": {\"releases\": [{\"github\": %q}]}, so its views cannot ask for that repository's releases", plugin.Name, repo)
	}
	if err := s.releasesAllowed(); err != nil {
		return ghreleases.List{}, err
	}
	if s.releases == nil {
		s.releases = newReleasesClient()
	}
	return s.releases.Releases(ctx, repo, refresh)
}
