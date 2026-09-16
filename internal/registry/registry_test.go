package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		image string
		want  Ref
	}{
		{"nginx", Ref{Registry: "docker.io", Repository: "library/nginx", Tag: "latest"}},
		{"nginx:1.27", Ref{Registry: "docker.io", Repository: "library/nginx", Tag: "1.27"}},
		{"grafana/grafana:10.4.2", Ref{Registry: "docker.io", Repository: "grafana/grafana", Tag: "10.4.2"}},
		{"index.docker.io/library/redis:7", Ref{Registry: "docker.io", Repository: "library/redis", Tag: "7"}},
		{"ghcr.io/org/app:v1@sha256:" + strings.Repeat("a", 64), Ref{Registry: "ghcr.io", Repository: "org/app", Tag: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}},
		{"quay.io/jetstack/cert-manager-cainjector@sha256:" + strings.Repeat("b", 64), Ref{Registry: "quay.io", Repository: "jetstack/cert-manager-cainjector", Digest: "sha256:" + strings.Repeat("b", 64)}},
		{"localhost:5000/tools/cli", Ref{Registry: "localhost:5000", Repository: "tools/cli", Tag: "latest"}},
		{"Registry.Example.com/team/app:2", Ref{Registry: "registry.example.com", Repository: "team/app", Tag: "2"}},
		{"  registry.k8s.io/coredns/coredns:v1.11.1  ", Ref{Registry: "registry.k8s.io", Repository: "coredns/coredns", Tag: "v1.11.1"}},
	} {
		got, err := Parse(c.image)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.image, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.image, got, c.want)
		}
	}

	for _, image := range []string{
		"",
		"nginx latest",
		"nginx:",
		"Nginx",
		"ghcr.io/",
		"ghcr.io/org/app@sha256:short",
		"evil.example/a/../b",
		"user@evil.example/app",
		"ho$t.example/app",
		"nginx:" + strings.Repeat("x", 129),
	} {
		if ref, err := Parse(image); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", image, ref)
		}
	}
}

func TestParseChallenge(t *testing.T) {
	ch, ok := parseChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull,push"`)
	if !ok {
		t.Fatal("a Bearer challenge was not read")
	}
	want := map[string]string{"realm": "https://auth.docker.io/token", "service": "registry.docker.io", "scope": "repository:library/nginx:pull,push"}
	for k, v := range want {
		if ch.params[k] != v {
			t.Errorf("%s = %q, want %q", k, ch.params[k], v)
		}
	}
	for _, header := range []string{`Basic realm="Registry"`, "", `Bearer service="x"`} {
		if _, ok := parseChallenge(header); ok {
			t.Errorf("parseChallenge(%q) was accepted", header)
		}
	}
}

func TestDockerHubIsAskedAtItsAPIHost(t *testing.T) {
	if got := defaultBase("docker.io"); got != "https://registry-1.docker.io" {
		t.Errorf("docker.io is asked at %s", got)
	}
	if got := defaultBase("ghcr.io"); got != "https://ghcr.io" {
		t.Errorf("ghcr.io is asked at %s", got)
	}
}

// fakeRegistry is a registry that wants an anonymous token, pages its tags two
// at a time, and knows one repository.
type fakeRegistry struct {
	srv    *httptest.Server
	client *Client
	clock  time.Time

	mu       sync.Mutex
	requests []string
	tokens   atomic.Int32
	// status, when set, is what every registry request answers.
	status int
	// basic makes the registry ask for a password instead of a token.
	basic bool
}

const fakeDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{clock: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	tags := []string{"1.25.0", "1.26.0", "1.27.0", "1.27.1", "latest"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		f.tokens.Add(1)
		if r.URL.Query().Get("scope") != "repository:team/app:pull" || r.URL.Query().Get("service") != "fake" {
			http.Error(w, "wrong scope", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, `{"token":"t0k3n","expires_in":300}`)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		status, basic := f.status, f.basic
		f.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if r.Header.Get("Authorization") != "Bearer t0k3n" {
			if basic {
				w.Header().Set("WWW-Authenticate", `Basic realm="fake"`)
			} else {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fake"`, f.srv.URL))
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v2/team/app/tags/list":
			start := 0
			if last := r.URL.Query().Get("last"); last != "" {
				start = slices.Index(tags, last) + 1
			}
			end := min(start+2, len(tags))
			if end < len(tags) {
				w.Header().Set("Link", fmt.Sprintf(`</v2/team/app/tags/list?n=2&last=%s>; rel="next"`, tags[end-1]))
			}
			quoted := make([]string, 0, end-start)
			for _, tag := range tags[start:end] {
				quoted = append(quoted, `"`+tag+`"`)
			}
			_, _ = fmt.Fprintf(w, `{"name":"team/app","tags":[%s]}`, strings.Join(quoted, ","))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/team/app/manifests/1.27.0":
			if !strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
				http.Error(w, "an index must be accepted first", http.StatusBadRequest)
				return
			}
			w.Header().Set("Docker-Content-Digest", fakeDigest)
		default:
			http.NotFound(w, r)
		}
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)

	f.client = New("k8sdockside/test")
	f.client.http.Transport = f.srv.Client().Transport
	f.client.base = func(h string) string { return "https://" + h }
	f.client.now = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.clock
	}
	return f
}

func (f *fakeRegistry) image(rest string) string {
	return strings.TrimPrefix(f.srv.URL, "https://") + "/team/app" + rest
}

func (f *fakeRegistry) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeRegistry) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
}

func (f *fakeRegistry) set(status int, basic bool) {
	f.mu.Lock()
	f.status, f.basic = status, basic
	f.mu.Unlock()
}

func TestLookupSignsInPagesAndReadsTheDigest(t *testing.T) {
	f := newFakeRegistry(t)
	report, err := f.client.Lookup(context.Background(), f.image(":1.27.0"), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusOK || report.Error != "" {
		t.Fatalf("status %q: %s", report.Status, report.Error)
	}
	if want := []string{"1.25.0", "1.26.0", "1.27.0", "1.27.1", "latest"}; !slices.Equal(report.Tags, want) {
		t.Errorf("tags = %v, want %v", report.Tags, want)
	}
	if report.Truncated {
		t.Error("a complete list was reported as cut short")
	}
	if report.Digest != fakeDigest {
		t.Errorf("digest = %q", report.Digest)
	}
	if report.Tag != "1.27.0" || report.Repository != "team/app" || report.CheckedAt != "2026-09-16T12:00:00Z" {
		t.Errorf("report = %+v", report)
	}
	if n := f.tokens.Load(); n != 1 {
		t.Errorf("signed in %d times, want once: the token is kept for the scope", n)
	}
}

func TestLookupKeepsItsAnswers(t *testing.T) {
	f := newFakeRegistry(t)
	ctx := context.Background()
	if _, err := f.client.Lookup(ctx, f.image(":1.27.0"), false); err != nil {
		t.Fatal(err)
	}
	before := len(f.asked())

	// Another tag of the same repository needs only its digest.
	if _, err := f.client.Lookup(ctx, f.image(":1.26.0"), false); err != nil {
		t.Fatal(err)
	}
	if got := f.asked()[before:]; len(got) != 1 || !strings.HasPrefix(got[0], "HEAD ") {
		t.Errorf("a second tag asked %v, want one HEAD", got)
	}
	before = len(f.asked())

	if _, err := f.client.Lookup(ctx, f.image(":1.27.0"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Lookup(ctx, f.image(":1.27.0"), true); err != nil {
		t.Fatal(err)
	}
	if got := f.asked()[before:]; len(got) != 0 {
		t.Errorf("a kept answer, or a refresh within a minute, asked %v", got)
	}

	f.advance(2 * time.Minute)
	if _, err := f.client.Lookup(ctx, f.image(":1.27.0"), true); err != nil {
		t.Fatal(err)
	}
	if len(f.asked()) == before {
		t.Error("a refresh of a two-minute-old answer asked nothing")
	}
	before = len(f.asked())

	f.advance(tagsTTL)
	if _, err := f.client.Lookup(ctx, f.image(":1.27.0"), false); err != nil {
		t.Fatal(err)
	}
	if len(f.asked()) == before {
		t.Error("an expired answer was used")
	}
}

func TestConcurrentLookupsShareOneRequest(t *testing.T) {
	f := newFakeRegistry(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if r, err := f.client.Lookup(context.Background(), f.image(":1.27.0"), false); err != nil || r.Status != StatusOK {
				t.Errorf("lookup: %v %+v", err, r)
			}
		})
	}
	wg.Wait()
	lists := 0
	for _, req := range f.asked() {
		if strings.HasPrefix(req, "GET /v2/team/app/tags/list?n=1000") {
			lists++
		}
	}
	// Once without a token and once with it.
	if lists != 2 {
		t.Errorf("the first page was asked for %d times, want 2", lists)
	}
}

func TestLookupReportsWhatARegistryWouldNotSay(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		basic  bool
		want   string
	}{
		{"password", 0, true, StatusAuth},
		{"forbidden", http.StatusForbidden, false, StatusAuth},
		{"missing", http.StatusNotFound, false, StatusMissing},
		{"limited", http.StatusTooManyRequests, false, StatusLimited},
		{"broken", http.StatusBadGateway, false, StatusError},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeRegistry(t)
			f.set(c.status, c.basic)
			report, err := f.client.Lookup(context.Background(), f.image(":1.27.0"), false)
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != c.want || report.Error == "" || len(report.Tags) != 0 || report.Digest != "" {
				t.Fatalf("report = %+v, want status %s with words", report, c.want)
			}
			if report.Tags == nil {
				t.Error("tags must be an empty list, not null")
			}

			// A failure is kept, but for less time than an answer.
			f.set(0, false)
			before := len(f.asked())
			if again, _ := f.client.Lookup(context.Background(), f.image(":1.27.0"), false); again.Status != c.want || len(f.asked()) != before {
				t.Errorf("a fresh failure was not kept: %+v", again)
			}
			f.advance(failureTTL)
			if again, _ := f.client.Lookup(context.Background(), f.image(":1.27.0"), false); again.Status != StatusOK {
				t.Errorf("an old failure was kept: %+v", again)
			}
		})
	}
}

func TestLookupOfAnUnreachableRegistry(t *testing.T) {
	f := newFakeRegistry(t)
	image := f.image(":1.27.0")
	f.srv.Close()
	report, err := f.client.Lookup(context.Background(), image, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUnreachable || !strings.Contains(report.Error, "127.0.0.1") {
		t.Errorf("report = %+v", report)
	}
}

func TestLookupRefusesWhatIsNotAReference(t *testing.T) {
	if _, err := New("x").Lookup(context.Background(), "not an image", false); err == nil {
		t.Error("a reference with a space was looked up")
	}
}

func TestPagesStayOnTheHostThatSentThem(t *testing.T) {
	for _, c := range []struct {
		link string
		want string
	}{
		{`</v2/a/tags/list?last=x>; rel="next"`, "https://reg.example/v2/a/tags/list?last=x"},
		{`<https://reg.example/v2/a/tags/list?last=x>; rel="next"`, "https://reg.example/v2/a/tags/list?last=x"},
		{`<https://evil.example/v2/a/tags/list>; rel="next"`, ""},
		{`<http://reg.example/v2/a/tags/list>; rel="next"`, ""},
		{`</v2/a/tags/list?last=x>; rel="prev"`, ""},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://reg.example/v2/a/tags/list", nil)
		resp := &http.Response{Header: http.Header{"Link": {c.link}}, Request: req}
		if got := nextPage(resp); got != c.want {
			t.Errorf("nextPage(%s) = %q, want %q", c.link, got, c.want)
		}
	}
}
