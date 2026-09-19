// Package updates asks GitHub whether a newer release of this app has been
// published.
//
// Beside package registry, which asks image registries for plugins that
// declare it, it is the one place the app reaches out to anything but the
// user's own clusters, so it is kept small and plain: a single GET of the public releases
// endpoint, unauthenticated, carrying nothing but the request itself and a
// User-Agent naming the app and its version. The answer is which release is
// newest; deciding what to do about that is the service's job.
package updates

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository whose releases are looked at.
const Repo = "k8sdockside/k8sdockside"

// LatestURL is GitHub's "latest release" endpoint for Repo. It answers with
// the newest published release that is neither a draft nor a pre-release --
// exactly the one somebody on a stable build should be told about, and the
// reason the whole release list is not fetched and sorted here instead.
const LatestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// ReleasesPage lists every release, for when there is no particular one to
// point at.
const ReleasesPage = "https://github.com/" + Repo + "/releases"

// Release is one published release, as much of it as the app shows.
type Release struct {
	// Version is the tag as published, e.g. v1.2.0.
	Version string `json:"version"`
	// Name is the release's title. Usually the tag again, sometimes a sentence.
	Name string `json:"name"`
	// URL is the release's page on GitHub: the notes and the downloads.
	URL string `json:"url"`
	// PublishedAt is when it went out, RFC 3339 as GitHub reports it.
	PublishedAt string `json:"publishedAt"`
	// Assets are the names of the app's own files on the release -- see
	// Install.AssetName for how they are named -- so a download can be offered
	// for the platform this build is on. Never nil once a check has succeeded.
	Assets []string `json:"assets"`
}

// Checker fetches the latest release. Use New rather than the zero value: a
// nil Client would panic and an empty URL asks nothing.
type Checker struct {
	// Client makes the request. New gives it a timeout, because a check that
	// hangs must never become a check that blocks.
	Client *http.Client
	// URL is the endpoint asked -- LatestURL, unless a test says otherwise.
	URL string
	// UserAgent is what the request identifies itself as. GitHub asks that API
	// clients name themselves, and turns away those that do not.
	UserAgent string
}

// New returns a checker for LatestURL that names the app and its version.
func New(version string) *Checker {
	return &Checker{
		Client:    &http.Client{Timeout: 15 * time.Second},
		URL:       LatestURL,
		UserAgent: "k8sdockside/" + version + " (+https://github.com/" + Repo + ")",
	}
}

// Latest asks GitHub for the newest published release.
//
// Every failure comes back worded for the settings page rather than for a
// log: the user pressed a button and is owed a sentence, not a status code.
func (c *Checker) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("reaching GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A megabyte is a hundred times what a release record comes to; anything
	// past it is not the answer that was asked for.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Release{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, errors.New("GitHub has no published release to compare against")
	case rateLimited(resp):
		return Release{}, errors.New("GitHub's API rate limit was reached from this address; it resets within the hour")
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}

	var payload struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	if !IsVersion(payload.TagName) {
		return Release{}, fmt.Errorf("the latest release is tagged %q, which is not a version", payload.TagName)
	}

	// The page is built from the tag rather than taken from the response's
	// html_url. The one thing the app does with this address is hand it to the
	// browser, and the tag has just been checked to be a version, so nothing
	// the network says can send the browser anywhere but this repository's
	// own release pages.
	// Only the app's own files are kept, by name: the download address is
	// built from the tag and the name the same way, so nothing else the
	// response says about them is needed.
	assets := []string{}
	for _, a := range payload.Assets {
		if ownAsset(a.Name) {
			assets = append(assets, a.Name)
		}
	}

	return Release{
		Version:     payload.TagName,
		Name:        payload.Name,
		URL:         ReleasesPage + "/tag/" + url.PathEscape(payload.TagName),
		PublishedAt: payload.PublishedAt,
		Assets:      assets,
	}, nil
}

// rateLimited recognises GitHub's two ways of saying "too many requests".
// Unauthenticated callers get sixty an hour per address, which a machine
// running several API clients can use up, and the classic answer is a 403
// with the remaining count at zero rather than a 429 -- though the newer
// limits do use that.
func rateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// ---- versions --------------------------------------------------------------

// version is a tag taken apart: the three numbers, and whatever pre-release
// identifiers followed them.
type version struct {
	nums [3]int
	pre  []string
}

// parse reads vMAJOR.MINOR.PATCH, with or without the v, with an optional
// -pre-release and +build suffix. Build metadata is dropped, as semver says it
// takes no part in ordering. The shape is exactly what the release workflow
// accepts for a tag, so anything else is not a release of this app.
func parse(tag string) (version, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if at := strings.IndexByte(s, '+'); at >= 0 {
		s = s[:at]
	}

	var v version
	core := s
	if at := strings.IndexByte(s, '-'); at >= 0 {
		core = s[:at]
		v.pre = strings.Split(s[at+1:], ".")
		for _, id := range v.pre {
			if id == "" {
				return version{}, false
			}
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return version{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return version{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// IsVersion reports whether a tag names a release: vMAJOR.MINOR.PATCH with an
// optional pre-release suffix. "development build" is not one, and neither is
// a tag somebody pushed by hand that the release workflow would have refused.
func IsVersion(tag string) bool {
	_, ok := parse(tag)
	return ok
}

// Compare orders two tags: negative when a is older than b, zero when they name
// the same version, positive when a is newer.
//
// A pre-release is older than the release it precedes -- v1.2.0-rc.1 comes
// before v1.2.0 -- and two pre-releases of the same number order by their
// identifiers as semver specifies: numerically where both are numbers, as text
// otherwise, with a number before a word. A tag that is not a version is older
// than any that is, and equal to any other that is not.
func Compare(a, b string) int {
	va, oka := parse(a)
	vb, okb := parse(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range va.nums {
		if c := cmp.Compare(va.nums[i], vb.nums[i]); c != 0 {
			return c
		}
	}
	return comparePre(va.pre, vb.pre)
}

// comparePre orders two pre-release suffixes, where none at all is the release
// itself and outranks every one of them.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		na, errA := strconv.Atoi(a[i])
		nb, errB := strconv.Atoi(b[i])
		var c int
		switch {
		case errA == nil && errB == nil:
			c = cmp.Compare(na, nb)
		case errA == nil:
			c = -1
		case errB == nil:
			c = 1
		default:
			c = strings.Compare(a[i], b[i])
		}
		if c != 0 {
			return c
		}
	}
	// rc.1.1 is a step past rc.1: the longer list of identifiers is the newer.
	return cmp.Compare(len(a), len(b))
}

// Newer reports whether latest is a later version than current. A current that
// is not a version -- a development build -- is never told it is behind, since
// it has nothing to be behind.
func Newer(current, latest string) bool {
	if !IsVersion(current) || !IsVersion(latest) {
		return false
	}
	return Compare(latest, current) > 0
}
