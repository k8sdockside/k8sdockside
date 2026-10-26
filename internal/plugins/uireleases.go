package plugins

import (
	"errors"
	"fmt"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/ghreleases"
)

// Release lists a plugin's views may ask for.
//
// A plugin about a product often wants to say how far the cluster is from
// that product's latest release, and a page has no network to find out. So a
// manifest may name the public GitHub repositories whose releases it follows,
// and its views ask the app for their release lists. The page only picks from
// what is declared; the app asks GitHub anonymously, keeps the answer for
// hours, and the user can switch it off. See ghreleases.

// UIRelease is one repository a plugin's views may ask about.
type UIRelease struct {
	// GitHub is the repository, owner/repo: "siderolabs/talos".
	GitHub string `json:"github"`
}

// validateReleases checks the repositories a plugin's views may ask about.
func validateReleases(pluginID string, list []UIRelease) ([]UIRelease, error) {
	if len(list) > ghreleases.MaxRepos {
		return list, fmt.Errorf("plugin %q asks for the releases of %d repositories; at most %d may be declared", pluginID, len(list), ghreleases.MaxRepos)
	}
	var errs []error
	seen := map[string]bool{}
	for i, r := range list {
		repo := strings.TrimSpace(r.GitHub)
		if !ghreleases.ValidRepo(repo) {
			errs = append(errs, fmt.Errorf("plugin %q asks for the releases of %q, which is not a GitHub owner/repo", pluginID, r.GitHub))
			continue
		}
		key := strings.ToLower(repo)
		if seen[key] {
			errs = append(errs, fmt.Errorf("plugin %q names the repository %q twice in \"releases\"", pluginID, repo))
			continue
		}
		seen[key] = true
		list[i] = UIRelease{GitHub: repo}
	}
	return list, errors.Join(errs...)
}
