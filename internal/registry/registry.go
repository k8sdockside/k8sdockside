// Package registry asks container registries which tags an image has, on
// behalf of a plugin's own views -- which have no network access of their own.
//
// It speaks the read-only half of the OCI distribution API and nothing else:
// the tag list of a repository, and the digest a tag points at. It asks
// anonymously, over https only, so what it can see is what anyone on the
// internet can see; a private image answers "auth", which is reported rather
// than worked around. Answers are kept for a while, because a page asks about
// every image in a cluster at once and registries -- Docker Hub above all --
// ration anonymous callers.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How a registry answered. Everything but StatusOK is a registry that could
// not tell, which a page shows next to the image rather than as an error.
const (
	StatusOK = "ok"
	// StatusAuth is a registry that wants credentials: a private image.
	StatusAuth = "auth"
	// StatusMissing is a repository the registry does not know.
	StatusMissing = "missing"
	// StatusLimited is a registry rationing requests from this address.
	StatusLimited = "limited"
	// StatusUnreachable is a registry that could not be reached at all:
	// DNS, TLS, a timeout, or one that only speaks plain http.
	StatusUnreachable = "unreachable"
	StatusError       = "error"
)

// MaxTags is how many tags are read from one repository. Past it the list is
// cut short and says so; the busiest public repositories have a few thousand.
const MaxTags = 10000

const (
	// pageSize is how many tags are asked for at once. Registries may send
	// fewer, and some ignore it.
	pageSize = 1000
	// maxPages stops a registry that pages ten at a time from taking forever.
	maxPages = 100
	// maxBody bounds one answer. A page of a thousand tags is tens of
	// kilobytes.
	maxBody = 8 << 20

	// How long an answer is kept. A failure is kept for less, so a registry
	// that was down is asked again soon -- but not on every page load, which
	// is what gets an address rate-limited.
	tagsTTL    = 30 * time.Minute
	failureTTL = 5 * time.Minute
	// minRefresh is how old an answer must be before "ask again" asks again.
	minRefresh = time.Minute

	// lookupTimeout bounds everything one lookup does, all pages included.
	lookupTimeout = 45 * time.Second
	// inFlight is how many requests go out at once, to all registries
	// together.
	inFlight = 6
)

// Report is what a lookup found out about one image.
type Report struct {
	// Image is the reference as asked.
	Image string `json:"image"`
	// Registry is the host, with Docker Hub's aliases folded into docker.io.
	Registry string `json:"registry"`
	// Repository is the path in the registry, library/ included for Docker
	// Hub's official images.
	Repository string `json:"repository"`
	// Tag is the tag the reference names: latest when none is written, empty
	// for a reference with only a digest.
	Tag string `json:"tag"`
	// Tags are every tag the registry lists, in the order it lists them.
	Tags []string `json:"tags"`
	// Truncated says the registry had more than MaxTags.
	Truncated bool `json:"truncated"`
	// Digest is what Tag points at in the registry now -- for a multi-platform
	// image the index, which is also what a pod's imageID records when it was
	// pulled by tag. Empty when unknown.
	Digest string `json:"digest"`
	// CheckedAt is when the registry was asked, RFC 3339.
	CheckedAt string `json:"checkedAt"`
	// Status is one of the Status constants.
	Status string `json:"status"`
	// Error says what went wrong, in words; empty when Status is StatusOK.
	Error string `json:"error"`
}

// Client asks registries and remembers their answers. Use New.
type Client struct {
	http      *http.Client
	userAgent string
	// base is the address a registry host is asked at: https://<host>,
	// except for Docker Hub. Tests point it at a test server.
	base func(host string) string
	now  func() time.Time
	sem  chan struct{}

	mu      sync.Mutex
	tags    map[string]*tagEntry
	digests map[string]*digestEntry
	tokens  map[string]token
	flights map[string]*flight
}

type tagEntry struct {
	tags      []string
	truncated bool
	status    string
	err       string
	at        time.Time
}

type digestEntry struct {
	digest string
	ok     bool
	at     time.Time
}

type token struct {
	value   string
	expires time.Time
}

// flight is a request someone is already making, which a second caller waits
// for rather than repeats: a page asks about nginx:1.27 and nginx:1.28 at
// once, and the tag list is the same.
type flight struct {
	done chan struct{}
}

// New returns a client that names itself as userAgent.
func New(userAgent string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{
		http: &http.Client{
			Transport: transport,
			// registry.k8s.io, for one, answers every request with a redirect
			// to a mirror. Go drops the Authorization header when a redirect
			// changes host; a mirror that wants its own token says so, and
			// get asks for one.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "https" {
					return fmt.Errorf("redirected to %s, which is not https", req.URL.Redacted())
				}
				return nil
			},
		},
		userAgent: userAgent,
		base:      defaultBase,
		now:       time.Now,
		sem:       make(chan struct{}, inFlight),
		tags:      map[string]*tagEntry{},
		digests:   map[string]*digestEntry{},
		tokens:    map[string]token{},
		flights:   map[string]*flight{},
	}
}

// defaultBase is where a registry host is asked. Docker Hub's API lives on a
// host of its own.
func defaultBase(host string) string {
	if host == DockerHub {
		return "https://registry-1.docker.io"
	}
	return "https://" + host
}

// Lookup asks the image's registry for its tags, and for what its tag points
// at. It returns an error only for a reference that is not one; a registry
// that fails is reported in the Report.
//
// refresh asks again instead of using a kept answer, unless that answer is
// under a minute old.
func (c *Client) Lookup(ctx context.Context, image string, refresh bool) (Report, error) {
	ref, err := Parse(image)
	if err != nil {
		return Report{}, err
	}
	// The work is shared with whoever else asks meanwhile, so it must not end
	// because this one caller went away.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lookupTimeout)
	defer cancel()

	tags := c.tagList(ctx, ref, refresh)
	report := Report{
		Image:      image,
		Registry:   ref.Registry,
		Repository: ref.Repository,
		Tag:        ref.Tag,
		Tags:       tags.tags,
		Truncated:  tags.truncated,
		CheckedAt:  tags.at.UTC().Format(time.RFC3339),
		Status:     tags.status,
		Error:      tags.err,
	}
	if report.Tags == nil {
		report.Tags = []string{}
	}
	if tags.status == StatusOK && ref.Tag != "" {
		report.Digest = c.tagDigest(ctx, ref, refresh)
	}
	return report, nil
}

// fresh reports whether an answer from at can still be used.
func (c *Client) fresh(at time.Time, ttl time.Duration, refresh bool) bool {
	age := c.now().Sub(at)
	if refresh {
		return age < minRefresh
	}
	return age < ttl
}

// share runs fn once for everyone asking under key at the same time. It
// reports whether this caller should do the work; one that should not has
// waited for the one that did.
func (c *Client) share(key string) (lead bool, finish func()) {
	c.mu.Lock()
	if f, ok := c.flights[key]; ok {
		c.mu.Unlock()
		<-f.done
		return false, func() {}
	}
	f := &flight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	return true, func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(f.done)
	}
}

func (c *Client) tagList(ctx context.Context, ref Ref, refresh bool) tagEntry {
	key := "tags " + ref.Key()
	for {
		c.mu.Lock()
		kept, ok := c.tags[ref.Key()]
		c.mu.Unlock()
		if ok {
			ttl := tagsTTL
			if kept.status != StatusOK {
				ttl = failureTTL
			}
			if c.fresh(kept.at, ttl, refresh) {
				return *kept
			}
		}
		lead, finish := c.share(key)
		if !lead {
			// Whoever led has kept an answer by now; read it, and refresh no
			// further than they did.
			refresh = false
			continue
		}
		entry := c.fetchTags(ctx, ref)
		c.mu.Lock()
		c.tags[ref.Key()] = &entry
		c.mu.Unlock()
		finish()
		return entry
	}
}

func (c *Client) fetchTags(ctx context.Context, ref Ref) tagEntry {
	entry := tagEntry{at: c.now(), status: StatusOK, tags: []string{}}
	scope := "repository:" + ref.Repository + ":pull"
	next := c.base(ref.Registry) + "/v2/" + ref.Repository + "/tags/list?n=" + strconv.Itoa(pageSize)
	seen := map[string]bool{}

	for page := 0; next != ""; page++ {
		if page >= maxPages || seen[next] {
			entry.truncated = true
			break
		}
		seen[next] = true

		resp, body, err := c.get(ctx, http.MethodGet, next, scope, "application/json")
		if err != nil {
			entry.status, entry.err = unreachable(ref.Registry, err)
			return entry
		}
		if status, words := judge(ref, resp); status != StatusOK {
			entry.status, entry.err = status, words
			return entry
		}
		var payload struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			entry.status, entry.err = StatusError, fmt.Sprintf("%s sent a tag list that could not be read: %v", ref.Registry, err)
			return entry
		}
		for _, tag := range payload.Tags {
			if len(entry.tags) >= MaxTags {
				entry.truncated = true
				return entry
			}
			entry.tags = append(entry.tags, tag)
		}
		next = nextPage(resp)
	}
	return entry
}

// nextPage is the address of the next page of tags, from the Link header, or
// empty. It must be on the host that sent this page, over https: a registry
// does not get to send the client anywhere else.
func nextPage(resp *http.Response) string {
	for _, link := range resp.Header.Values("Link") {
		for part := range strings.SplitSeq(link, ",") {
			target, params, ok := strings.Cut(part, ";")
			if !ok || !strings.Contains(strings.ReplaceAll(params, " ", ""), `rel="next"`) {
				continue
			}
			target = strings.Trim(strings.TrimSpace(target), "<>")
			u, err := resp.Request.URL.Parse(target)
			if err != nil || u.Scheme != "https" || u.Host != resp.Request.URL.Host {
				return ""
			}
			return u.String()
		}
	}
	return ""
}

var digestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]{32,}$`)

// manifestTypes are what a HEAD of a tag accepts. The index types come first:
// a multi-platform image is pulled by its index, and the index's digest is the
// one a pod's imageID records.
var manifestTypes = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// tagDigest is what a tag points at now, or empty. A HEAD: Docker Hub does not
// count those against its pull limit.
func (c *Client) tagDigest(ctx context.Context, ref Ref, refresh bool) string {
	key := ref.Key() + ":" + ref.Tag
	for {
		c.mu.Lock()
		kept, ok := c.digests[key]
		c.mu.Unlock()
		if ok {
			ttl := tagsTTL
			if !kept.ok {
				ttl = failureTTL
			}
			if c.fresh(kept.at, ttl, refresh) {
				return kept.digest
			}
		}
		lead, finish := c.share("digest " + key)
		if !lead {
			refresh = false
			continue
		}
		entry := digestEntry{at: c.now()}
		target := c.base(ref.Registry) + "/v2/" + ref.Repository + "/manifests/" + ref.Tag
		resp, _, err := c.get(ctx, http.MethodHead, target, "repository:"+ref.Repository+":pull", manifestTypes)
		if err == nil && resp.StatusCode == http.StatusOK {
			if d := resp.Header.Get("Docker-Content-Digest"); digestPattern.MatchString(d) {
				entry.digest, entry.ok = d, true
			}
		}
		c.mu.Lock()
		c.digests[key] = &entry
		c.mu.Unlock()
		finish()
		return entry.digest
	}
}

// judge turns a registry's answer into a status, with the words for it.
func judge(ref Ref, resp *http.Response) (string, string) {
	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return StatusAuth, ref.Registry + " wants credentials for " + ref.Repository + "; only public images are checked"
	case http.StatusNotFound:
		return StatusMissing, ref.Registry + " does not know " + ref.Repository
	case http.StatusTooManyRequests:
		return StatusLimited, ref.Registry + " is limiting requests from this address; try again later"
	default:
		return StatusError, ref.Registry + " answered " + resp.Status
	}
}

func unreachable(host string, err error) (string, string) {
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return StatusUnreachable, host + " did not answer in time"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return StatusUnreachable, host + " could not be found (" + dns.Err + ")"
	}
	return StatusUnreachable, "could not reach " + host + ": " + err.Error()
}

// get makes one request, signing in anonymously when the registry asks for it,
// and hands back the response with its body read. A status other than 200 is
// not an error here; judge says what it means.
func (c *Client) get(ctx context.Context, method, target, scope, accept string) (*http.Response, []byte, error) {
	for attempt := 0; ; attempt++ {
		u, err := url.Parse(target)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, target, nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", c.userAgent)
		if t, ok := c.token(u.Host, scope); ok {
			req.Header.Set("Authorization", "Bearer "+t)
		}

		resp, body, err := c.do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return resp, body, nil
		}
		// The host that answered may not be the one asked, after a redirect;
		// the token is for it, and so is the retry.
		challenge, ok := parseChallenge(resp.Header.Get("WWW-Authenticate"))
		if !ok {
			return resp, body, nil
		}
		// A registry that will not sign an anonymous caller in is saying no,
		// which is what the 401 already says.
		answered := resp.Request.URL
		if c.signIn(ctx, answered.Host, scope, challenge) != nil {
			return resp, body, nil
		}
		target = answered.String()
	}
}

// do sends a request, no more than inFlight at once.
func (c *Client) do(req *http.Request) (*http.Response, []byte, error) {
	select {
	case c.sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, nil, req.Context().Err()
	}
	defer func() { <-c.sem }()

	resp, err := c.http.Do(req) // #nosec G704 -- only hosts a pod in the cluster pulls from; see services.PluginService.RegistryLookup
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, err
	}
	return resp, body, nil
}

func (c *Client) token(host, scope string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[host+" "+scope]
	if !ok || !c.now().Before(t.expires) {
		return "", false
	}
	return t.value, true
}

// signIn fetches an anonymous token the way a registry's Bearer challenge
// says to, and keeps it for host and scope.
func (c *Client) signIn(ctx context.Context, host, scope string, ch challenge) error {
	realm, err := url.Parse(ch.params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return errors.New("the registry named no https address to sign in at")
	}
	// The challenge's own scope, when it gives one, is what the registry will
	// check the token against.
	if s := ch.params["scope"]; s != "" {
		scope = s
	}
	q := realm.Query()
	if service := ch.params["service"]; service != "" {
		q.Set("service", service)
	}
	q.Set("scope", scope)
	realm.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, body, err := c.do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signing in answered %s", resp.Status)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	value := payload.Token
	if value == "" {
		value = payload.AccessToken
	}
	if value == "" {
		return errors.New("signing in gave no token")
	}
	// The spec's default is a minute; a little is taken off so a token is
	// not used in its last seconds.
	life := time.Duration(payload.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Minute
	}
	life -= min(life/10, 30*time.Second)

	c.mu.Lock()
	c.tokens[host+" "+scope] = token{value: value, expires: c.now().Add(life)}
	c.mu.Unlock()
	return nil
}

type challenge struct {
	params map[string]string
}

// parseChallenge reads a Bearer WWW-Authenticate header:
//
//	Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"
//
// Values are quoted and may hold commas, so it is read a character at a time.
// Any other scheme -- Basic, above all -- is not one to answer anonymously.
func parseChallenge(header string) (challenge, bool) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return challenge{}, false
	}
	params := map[string]string{}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ,")
		name, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		name = strings.ToLower(strings.TrimSpace(name))
		var value string
		if strings.HasPrefix(after, `"`) {
			var b strings.Builder
			i := 1
			for ; i < len(after) && after[i] != '"'; i++ {
				if after[i] == '\\' && i+1 < len(after) {
					i++
				}
				b.WriteByte(after[i])
			}
			value = b.String()
			rest = after[min(i+1, len(after)):]
		} else {
			value, rest, _ = strings.Cut(after, ",")
			value = strings.TrimSpace(value)
		}
		params[name] = value
	}
	if params["realm"] == "" {
		return challenge{}, false
	}
	return challenge{params: params}, true
}
