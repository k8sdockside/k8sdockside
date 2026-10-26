// Package ghreleases asks GitHub which releases a public repository has, on
// behalf of a plugin whose pages cannot reach the network themselves.
//
// It is kept as plain as package knownlist: one anonymous GET of GitHub's
// public releases endpoint per repository, carrying a User-Agent naming the
// app and the ETag of the copy already held, so an unchanged list costs a 304.
// Answers are kept in memory and on disk for TTL, so a page opened twenty
// times a day asks GitHub at most a few times, and a launch without a network
// still has the last list. When GitHub cannot be reached, or rate-limits the
// address (sixty anonymous requests an hour), the last good list is served
// marked stale rather than nothing.
//
// Only repositories a plugin's manifest names are ever asked about; that check
// is the caller's -- see services.PluginService.Releases.
package ghreleases

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BaseURL is GitHub's REST API.
const BaseURL = "https://api.github.com"

const (
	// TTL is how long a list is used before GitHub is asked again.
	TTL = 6 * time.Hour
	// refreshFloor is how fresh a list must be for a refresh to be answered
	// from it anyway: a page that asks to refresh on every load must not turn
	// into a request on every load.
	refreshFloor = time.Minute
	// maxBody is the most read of an answer. A hundred releases with long
	// notes run to a few megabytes; anything past this is not a release list.
	maxBody = 16 << 20
	// timeout bounds one request.
	timeout = 20 * time.Second
	// MaxRepos is how many repositories one plugin may declare.
	MaxRepos = 10
)

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ValidRepo reports whether repo is a GitHub owner/repo: an owner of letters,
// digits and dashes (not starting with a dash, at most 39), and a repository
// name of letters, digits, dots, dashes and underscores.
func ValidRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !ownerPattern.MatchString(owner) || !repoPattern.MatchString(name) {
		return false
	}
	return name != "." && name != ".." && !strings.HasSuffix(name, ".git")
}

// Release is one published release, as a page is handed it.
type Release struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
}

// List is what a page is answered with.
type List struct {
	// Repo is the repository asked about, owner/repo.
	Repo string `json:"repo"`
	// Releases are newest first; never a draft.
	Releases []Release `json:"releases"`
	// FetchedAt is when GitHub last answered for this list, RFC 3339.
	FetchedAt string `json:"fetchedAt"`
	// Stale is set when GitHub could not be asked just now -- unreachable,
	// failing or rate-limiting -- and this is the last good list instead.
	Stale bool `json:"stale"`
	// Error says why the list is stale; empty otherwise.
	Error string `json:"error"`
}

// ErrRateLimited is GitHub refusing more anonymous requests from this address
// for now.
var ErrRateLimited = errors.New("GitHub's limit on anonymous requests from this address has been reached")

// Client asks GitHub and remembers the answers. Use New.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string
	// Dir is where lists are kept between runs; empty keeps them in memory
	// only.
	Dir string
	TTL time.Duration

	now func() time.Time

	mu      sync.Mutex
	lists   map[string]*entry
	flights map[string]*flight
	// limitedUntil is when GitHub said its rate limit resets; no request is
	// made before then.
	limitedUntil time.Time
}

// entry is a list held, as kept on disk too.
type entry struct {
	Repo      string    `json:"repo"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
	Releases  []Release `json:"releases"`
}

type flight struct {
	done chan struct{}
	err  error
}

// New returns a client naming itself userAgent and keeping lists in dir.
// It goes through the default transport, so HTTPS_PROXY and the rest apply.
func New(userAgent, dir string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: timeout},
		BaseURL:   BaseURL,
		UserAgent: userAgent,
		Dir:       dir,
		TTL:       TTL,
	}
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Releases answers with repo's releases: from what is held when it is fresh,
// else from GitHub. refresh asks GitHub even when the list held is fresh,
// unless it is under a minute old. Errors only when there is nothing to
// answer with at all.
func (c *Client) Releases(ctx context.Context, repo string, refresh bool) (List, error) {
	if !ValidRepo(repo) {
		return List{}, fmt.Errorf("%q is not a GitHub owner/repo", repo)
	}
	key := strings.ToLower(repo)

	for {
		c.mu.Lock()
		held := c.held(key)
		now := c.clock()
		if held != nil {
			age := now.Sub(held.FetchedAt)
			if age < refreshFloor || (!refresh && age < c.ttl()) {
				c.mu.Unlock()
				return answer(held, nil), nil
			}
		}
		if now.Before(c.limitedUntil) {
			c.mu.Unlock()
			return c.fallback(held, ErrRateLimited)
		}
		if f, ok := c.flights[key]; ok {
			c.mu.Unlock()
			select {
			case <-f.done:
			case <-ctx.Done():
				return List{}, ctx.Err()
			}
			c.mu.Lock()
			held = c.held(key)
			c.mu.Unlock()
			if f.err != nil {
				return c.fallback(held, f.err)
			}
			if held != nil {
				return answer(held, nil), nil
			}
			continue
		}
		f := &flight{done: make(chan struct{})}
		if c.flights == nil {
			c.flights = map[string]*flight{}
		}
		c.flights[key] = f
		etag := ""
		if held != nil {
			etag = held.ETag
		}
		c.mu.Unlock()

		// Not the caller's context: others may be waiting on this request, and
		// one page closing must not fail it for them. The client's timeout
		// bounds it.
		got, err := c.fetch(context.WithoutCancel(ctx), repo, etag)

		c.mu.Lock()
		switch {
		case err != nil:
		case got == nil: // 304: what is held is current
			// A copy: entries are never changed once handed out, so they
			// can be read without the lock.
			renewed := *held
			renewed.FetchedAt = c.clock()
			held = &renewed
		default:
			held = got
		}
		if err == nil {
			c.lists[key] = held
		}
		f.err = err
		delete(c.flights, key)
		close(f.done)
		saved := held
		c.mu.Unlock()

		if err != nil {
			return c.fallback(saved, err)
		}
		c.save(key, saved)
		return answer(saved, nil), nil
	}
}

func (c *Client) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return TTL
}

// held is the list kept for key, read from disk the first time. Call with mu
// held.
func (c *Client) held(key string) *entry {
	if c.lists == nil {
		c.lists = map[string]*entry{}
	}
	if e, ok := c.lists[key]; ok {
		return e
	}
	e := c.load(key)
	if e != nil {
		c.lists[key] = e
	}
	return e
}

// fallback answers with what is held, marked stale, or with err when nothing is.
func (c *Client) fallback(held *entry, err error) (List, error) {
	if held == nil {
		return List{}, err
	}
	return answer(held, err), nil
}

func answer(e *entry, err error) List {
	l := List{
		Repo:      e.Repo,
		Releases:  slices.Clone(e.Releases),
		FetchedAt: e.FetchedAt.UTC().Format(time.RFC3339),
	}
	if l.Releases == nil {
		l.Releases = []Release{}
	}
	if err != nil {
		l.Stale = true
		l.Error = err.Error()
	}
	return l
}

// wire is a release as GitHub writes it.
type wire struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	CreatedAt   string `json:"created_at"`
	HTMLURL     string `json:"html_url"`
}

// fetch asks GitHub. It answers nil, nil for a 304.
func (c *Client) fetch(ctx context.Context, repo, etag string) (*entry, error) {
	base := strings.TrimRight(cmp.Or(c.BaseURL, BaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repo+"/releases?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req) // #nosec G704 -- GitHub's API, for a repository the plugin's manifest names
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified && etag != "":
		return nil, nil
	case resp.StatusCode == http.StatusOK:
	case limited(resp):
		c.mu.Lock()
		c.limitedUntil = resetOf(resp, c.clock())
		c.mu.Unlock()
		return nil, ErrRateLimited
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("GitHub has no public repository %s", repo)
	default:
		return nil, fmt.Errorf("GitHub answered %s for %s's releases", resp.Status, repo)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s's releases: %w", repo, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("GitHub's answer for %s's releases is larger than any release list would be; ignored", repo)
	}
	releases, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("GitHub's answer for %s's releases: %w", repo, err)
	}
	return &entry{Repo: repo, ETag: resp.Header.Get("ETag"), FetchedAt: c.clock(), Releases: releases}, nil
}

// limited reports whether GitHub is rate-limiting: a 429, or a 403 that says
// no requests remain.
func limited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// resetOf is when GitHub said its limit resets, or a while from now when it
// did not say.
func resetOf(resp *http.Response, now time.Time) time.Time {
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
		return now.Add(time.Duration(secs) * time.Second)
	}
	if unix, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		at := time.Unix(unix, 0)
		if at.After(now) && at.Sub(now) <= 2*time.Hour {
			return at
		}
	}
	return now.Add(15 * time.Minute)
}

// Parse reads GitHub's list of releases: drafts dropped, newest first.
func Parse(body []byte) ([]Release, error) {
	var raw []wire
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("not a list of releases")
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		tag := strings.TrimSpace(r.TagName)
		if r.Draft || tag == "" {
			continue
		}
		published := cmp.Or(r.PublishedAt, r.CreatedAt)
		out = append(out, Release{
			Tag:         tag,
			Name:        cmp.Or(strings.TrimSpace(r.Name), tag),
			Prerelease:  r.Prerelease,
			PublishedAt: published,
			URL:         webURL(r.HTMLURL),
		})
	}
	// RFC 3339 in UTC, as GitHub writes it, sorts as text.
	slices.SortStableFunc(out, func(a, b Release) int { return strings.Compare(b.PublishedAt, a.PublishedAt) })
	return out, nil
}

// webURL keeps a link only when it points at github.com over https.
func webURL(u string) string {
	if strings.HasPrefix(u, "https://github.com/") {
		return u
	}
	return ""
}

// path is where key's list is kept on disk; owner names cannot hold an
// underscore, so the two halves cannot run together.
func (c *Client) path(key string) string {
	if c.Dir == "" {
		return ""
	}
	return filepath.Join(c.Dir, strings.Replace(key, "/", "__", 1)+".json")
}

func (c *Client) load(key string) *entry {
	p := c.path(key)
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p) // #nosec G304 -- the app's own cache file, named from a validated owner/repo
	if err != nil {
		return nil
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil || e.FetchedAt.IsZero() || !strings.EqualFold(e.Repo, key) {
		return nil
	}
	return &e
}

// save keeps e on disk, best effort: a cache that cannot be written only means
// the next launch asks again.
func (c *Client) save(key string, e *entry) {
	p := c.path(key)
	if p == "" || e == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}
