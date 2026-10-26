package ghreleases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const talosReleases = `[
  {"tag_name":"v1.15.0-alpha.1","name":"v1.15.0-alpha.1","draft":false,"prerelease":true,"published_at":"2026-10-20T10:00:00Z","html_url":"https://github.com/siderolabs/talos/releases/tag/v1.15.0-alpha.1","body":"notes"},
  {"tag_name":"v1.15.0-alpha.2","name":"","draft":true,"prerelease":true,"published_at":null,"html_url":"https://github.com/siderolabs/talos/releases/tag/untagged-1"},
  {"tag_name":"v1.13.12","name":"v1.13.12","draft":false,"prerelease":false,"published_at":"2026-10-01T10:00:00Z","html_url":"https://github.com/siderolabs/talos/releases/tag/v1.13.12"},
  {"tag_name":"v1.14.5","name":"v1.14.5","draft":false,"prerelease":false,"published_at":"2026-10-15T10:00:00Z","html_url":"javascript:alert(1)"}
]`

// server answers like GitHub: the list with an ETag, a 304 for that ETag.
type server struct {
	hits    atomic.Int32
	status  atomic.Int32 // non-zero: answer with this instead
	headers http.Header
	gate    chan struct{} // when set, every request waits for it
	agent   atomic.Value
	accept  atomic.Value
	path    atomic.Value
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	if s.gate != nil {
		<-s.gate
	}
	s.agent.Store(r.UserAgent())
	s.accept.Store(r.Header.Get("Accept"))
	s.path.Store(r.URL.RequestURI())
	if code := s.status.Load(); code != 0 {
		for k, v := range s.headers {
			w.Header()[k] = v
		}
		w.WriteHeader(int(code))
		return
	}
	if r.Header.Get("If-None-Match") == `"v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", `"v1"`)
	_, _ = w.Write([]byte(talosReleases))
}

func setup(t *testing.T, dir string) (*server, *Client, *time.Time) {
	t.Helper()
	s := &server{}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	c := New("k8sdockside/test", dir)
	c.BaseURL = srv.URL
	c.now = func() time.Time { return now }
	return s, c, &now
}

func TestParseDropsDraftsAndSortsNewestFirst(t *testing.T) {
	got, err := Parse([]byte(talosReleases))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, r := range got {
		tags = append(tags, r.Tag)
		if r.Draft {
			t.Errorf("draft %s returned", r.Tag)
		}
	}
	if strings.Join(tags, " ") != "v1.15.0-alpha.1 v1.14.5 v1.13.12" {
		t.Errorf("tags %v", tags)
	}
	if !got[0].Prerelease || got[1].Prerelease {
		t.Errorf("prerelease flags wrong: %+v", got)
	}
	if got[1].URL != "" {
		t.Errorf("a link that is not github.com was kept: %q", got[1].URL)
	}
	if got[2].URL != "https://github.com/siderolabs/talos/releases/tag/v1.13.12" || got[2].PublishedAt != "2026-10-01T10:00:00Z" {
		t.Errorf("release %+v", got[2])
	}
	if _, err := Parse([]byte(`{"message":"Not Found"}`)); err == nil {
		t.Error("an object was read as a list")
	}
}

func TestValidRepo(t *testing.T) {
	for _, ok := range []string{"siderolabs/talos", "k8sdockside/k8sdockside", "a/b", "Owner-1/repo.name_x"} {
		if !ValidRepo(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "talos", "/talos", "siderolabs/", "-x/y", "a/b/c", "a_b/c", "a/..", "a/.", "a/b c", "a/b?x=1", "a/b.git", strings.Repeat("o", 40) + "/r", "a/" + strings.Repeat("r", 101)} {
		if ValidRepo(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFetchesOnceThenServesFromCacheUntilTheTTL(t *testing.T) {
	s, c, now := setup(t, t.TempDir())
	ctx := context.Background()

	first, err := c.Releases(ctx, "siderolabs/talos", false)
	if err != nil || len(first.Releases) != 3 || first.Stale || first.FetchedAt != "2026-10-26T12:00:00Z" {
		t.Fatalf("first: %+v, %v", first, err)
	}
	if got := s.path.Load(); got != "/repos/siderolabs/talos/releases?per_page=100" {
		t.Errorf("asked for %v", got)
	}
	if s.agent.Load() != "k8sdockside/test" || s.accept.Load() != "application/vnd.github+json" {
		t.Errorf("sent agent %v, accept %v", s.agent.Load(), s.accept.Load())
	}

	*now = now.Add(5 * time.Hour)
	if _, err := c.Releases(ctx, "siderolabs/talos", false); err != nil || s.hits.Load() != 1 {
		t.Fatalf("within the TTL GitHub was asked again (%d hits), %v", s.hits.Load(), err)
	}

	*now = now.Add(2 * time.Hour)
	again, err := c.Releases(ctx, "siderolabs/talos", false)
	if err != nil || s.hits.Load() != 2 {
		t.Fatalf("after the TTL: %d hits, %v", s.hits.Load(), err)
	}
	// The second answer was a 304: the list is the one held, freshly dated.
	if len(again.Releases) != 3 || again.FetchedAt != "2026-10-26T19:00:00Z" || again.Stale {
		t.Errorf("after a 304: %+v", again)
	}
}

func TestRefreshAsksAgainButNotWithinAMinute(t *testing.T) {
	s, c, now := setup(t, "")
	ctx := context.Background()
	_, _ = c.Releases(ctx, "siderolabs/talos", false)
	_, _ = c.Releases(ctx, "siderolabs/talos", true)
	if s.hits.Load() != 1 {
		t.Errorf("a refresh seconds later asked GitHub (%d hits)", s.hits.Load())
	}
	*now = now.Add(2 * time.Minute)
	_, _ = c.Releases(ctx, "siderolabs/talos", true)
	if s.hits.Load() != 2 {
		t.Errorf("a refresh two minutes later did not ask (%d hits)", s.hits.Load())
	}
}

func TestTheDiskCacheOutlastsTheClient(t *testing.T) {
	dir := t.TempDir()
	s, c, _ := setup(t, dir)
	if _, err := c.Releases(context.Background(), "siderolabs/talos", false); err != nil {
		t.Fatal(err)
	}
	// A new client, as after a restart, with GitHub gone.
	s.status.Store(http.StatusInternalServerError)
	_, c2, _ := setup(t, dir)
	c2.BaseURL = c.BaseURL
	got, err := c2.Releases(context.Background(), "SideroLabs/Talos", false)
	if err != nil || len(got.Releases) != 3 || got.Stale {
		t.Fatalf("from disk: %+v, %v", got, err)
	}
	if s.hits.Load() != 1 {
		t.Errorf("a fresh list on disk still asked GitHub (%d hits)", s.hits.Load())
	}
}

func TestServesTheLastListMarkedStaleWhenGitHubFails(t *testing.T) {
	s, c, now := setup(t, "")
	ctx := context.Background()
	if _, err := c.Releases(ctx, "siderolabs/talos", false); err != nil {
		t.Fatal(err)
	}
	s.status.Store(http.StatusBadGateway)
	*now = now.Add(7 * time.Hour)
	got, err := c.Releases(ctx, "siderolabs/talos", false)
	if err != nil || !got.Stale || len(got.Releases) != 3 || got.FetchedAt != "2026-10-26T12:00:00Z" || !strings.Contains(got.Error, "502") {
		t.Fatalf("stale: %+v, %v", got, err)
	}
	// With nothing held there is nothing to serve.
	if _, err := c.Releases(ctx, "siderolabs/other", false); err == nil {
		t.Error("a failure with nothing held was not an error")
	}
}

func TestUnreachableWithNothingHeldIsAnError(t *testing.T) {
	c := New("t", "")
	c.BaseURL = "http://127.0.0.1:1"
	if _, err := c.Releases(context.Background(), "siderolabs/talos", false); err == nil || !strings.Contains(err.Error(), "could not reach GitHub") {
		t.Errorf("err %v", err)
	}
}

func TestRateLimitServesStaleAndStopsAsking(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		headers http.Header
	}{
		"403 with nothing remaining": {http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}}},
		"429":                        {http.StatusTooManyRequests, http.Header{"Retry-After": {"600"}}},
	} {
		t.Run(name, func(t *testing.T) {
			s, c, now := setup(t, "")
			ctx := context.Background()
			if _, err := c.Releases(ctx, "siderolabs/talos", false); err != nil {
				t.Fatal(err)
			}
			s.status.Store(int32(tc.status))
			s.headers = tc.headers
			*now = now.Add(7 * time.Hour)
			got, err := c.Releases(ctx, "siderolabs/talos", false)
			if err != nil || !got.Stale || got.Error != ErrRateLimited.Error() || len(got.Releases) != 3 {
				t.Fatalf("limited: %+v, %v", got, err)
			}
			hits := s.hits.Load()
			// Another repository is not asked about either until the limit resets.
			if _, err := c.Releases(ctx, "siderolabs/omni", false); err != ErrRateLimited || s.hits.Load() != hits {
				t.Errorf("while limited: %v, %d hits (was %d)", err, s.hits.Load(), hits)
			}
			*now = now.Add(time.Hour)
			s.status.Store(0)
			if got, err := c.Releases(ctx, "siderolabs/talos", false); err != nil || got.Stale {
				t.Errorf("after the reset: %+v, %v", got, err)
			}
		})
	}
}

func TestA403WithRequestsLeftIsNotARateLimit(t *testing.T) {
	s, c, _ := setup(t, "")
	s.status.Store(http.StatusForbidden)
	s.headers = http.Header{"X-Ratelimit-Remaining": {"12"}}
	_, err := c.Releases(context.Background(), "siderolabs/talos", false)
	if err == nil || err == ErrRateLimited {
		t.Errorf("err %v", err)
	}
}

func TestConcurrentAsksShareOneRequest(t *testing.T) {
	s, c, _ := setup(t, "")
	s.gate = make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			got, err := c.Releases(context.Background(), "siderolabs/talos", false)
			if err == nil && len(got.Releases) != 3 {
				err = context.Canceled
			}
			errs <- err
		})
	}
	// Let every caller arrive before GitHub answers.
	time.Sleep(50 * time.Millisecond)
	close(s.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if s.hits.Load() != 1 {
		t.Errorf("%d requests for one repository", s.hits.Load())
	}
}

func TestRefusesWhatIsNotARepository(t *testing.T) {
	s, c, _ := setup(t, "")
	if _, err := c.Releases(context.Background(), "../../etc/passwd", false); err == nil {
		t.Error("a path was asked about")
	}
	if s.hits.Load() != 0 {
		t.Error("GitHub was asked")
	}
}

func TestRefusesAHugeAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[" + strings.Repeat(" ", maxBody) + "]"))
	}))
	defer srv.Close()
	c := New("t", "")
	c.BaseURL = srv.URL
	if _, err := c.Releases(context.Background(), "siderolabs/talos", false); err == nil {
		t.Error("an answer over the limit was read")
	}
}
