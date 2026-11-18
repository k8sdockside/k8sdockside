package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/k8sdockside/k8sdockside/internal/session"
)

func TestMain(m *testing.M) {
	// A real hash takes a good part of a second; the tests make dozens.
	passwordIterations = 1000
	os.Exit(m.Run())
}

const testPassword = "correct horse battery"

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: prod
  cluster:
    server: https://prod.example.com:6443
contexts:
- name: prod-admin
  context:
    cluster: prod
    user: admin
users:
- name: admin
  user:
    token: abc
current-context: prod-admin
`

// harness is a gateway in front of a stand-in for the app.
type harness struct {
	t       *testing.T
	gw      *Gateway
	srv     *httptest.Server
	app     *httptest.Server
	owners  *session.Owners
	resyncs atomic.Int32

	mu    sync.Mutex
	seen  []*http.Request
	appWS http.HandlerFunc
}

func newHarness(t *testing.T, tweak func(*Config)) *harness {
	t.Helper()
	cfg := Config{
		DataDir:       t.TempDir(),
		InCluster:     "false",
		InClusterName: "in-cluster",
		SessionTTL:    time.Hour,
		PasswordLogin: true,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	if err := os.MkdirAll(cfg.UploadDir(), 0o700); err != nil {
		t.Fatal(err)
	}

	h := &harness{t: t, owners: session.NewOwners()}
	h.app = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wails/events" && h.appWS != nil {
			h.appWS(w, r)
			return
		}
		h.mu.Lock()
		h.seen = append(h.seen, r.Clone(context.Background()))
		h.mu.Unlock()
		_, _ = io.WriteString(w, "the app")
	}))
	t.Cleanup(h.app.Close)

	gw, err := New(cfg, Deps{
		Owners:      h.owners,
		OwnedEvents: map[string]string{"terminal:data": "sessionId"},
		Resync:      func() { h.resyncs.Add(1) },
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	gw.SetUpstream(strings.TrimPrefix(h.app.URL, "http://"))
	h.gw = gw
	h.srv = httptest.NewServer(gw.Handler())
	t.Cleanup(h.srv.Close)
	return h
}

// browser keeps cookies and does not follow redirects, so a test sees where
// it is sent.
func (h *harness) browser() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *harness) do(c *http.Client, req *http.Request) *http.Response {
	h.t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// page loads a page the way a browser navigates to one.
func (h *harness) page(c *http.Client, path string) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.srv.URL+path, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Accept", "text/html")
	return h.do(c, req)
}

func (h *harness) post(c *http.Client, path string, form url.Values) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.do(c, req)
}

// setUp creates the first administrator through the setup page and returns a
// browser signed in as them.
func (h *harness) setUp(username string) *http.Client {
	h.t.Helper()
	c := h.browser()
	resp := h.post(c, "/-/setup", url.Values{"username": {username}, "password": {testPassword}, "confirm": {testPassword}, "token": {h.gw.cfg.SetupToken}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		h.t.Fatalf("setup answered %d to %q: %s", resp.StatusCode, resp.Header.Get("Location"), readBody(h.t, resp))
	}
	return c
}

// addUser creates a user directly in the store.
func (h *harness) addUser(username, role string) User {
	h.t.Helper()
	hash, err := hashPassword(testPassword)
	if err != nil {
		h.t.Fatal(err)
	}
	u, err := h.gw.store.createUser(User{Username: username, Role: role, PasswordHash: hash}, false)
	if err != nil {
		h.t.Fatal(err)
	}
	return u
}

func (h *harness) signIn(username string) *http.Client {
	h.t.Helper()
	c := h.browser()
	h.signInWith(c, username)
	return c
}

// signInWith signs username in, in a browser that may already have been
// signed in as someone else.
func (h *harness) signInWith(c *http.Client, username string) {
	h.t.Helper()
	resp := h.post(c, "/-/login", url.Values{"username": {username}, "password": {testPassword}})
	if resp.StatusCode != http.StatusSeeOther {
		h.t.Fatalf("signing in as %s answered %d: %s", username, resp.StatusCode, readBody(h.t, resp))
	}
}

func (h *harness) lastSeen() *http.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.seen) == 0 {
		h.t.Fatal("the app saw no request")
	}
	return h.seen[len(h.seen)-1]
}

func (h *harness) sessionToken(c *http.Client) string {
	u, _ := url.Parse(h.srv.URL)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == sessionCookie {
			return ck.Value
		}
	}
	h.t.Fatal("no session cookie")
	return ""
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (h *harness) csrf(c *http.Client, path string) string {
	h.t.Helper()
	m := csrfPattern.FindStringSubmatch(readBody(h.t, h.page(c, path)))
	if m == nil {
		h.t.Fatalf("no CSRF token on %s", path)
	}
	return m[1]
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- first run ---------------------------------------------------------------

func TestAFreshDeploymentSendsEveryoneToSetup(t *testing.T) {
	h := newHarness(t, nil)
	c := h.browser()

	if resp := h.page(c, "/"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/-/setup" {
		t.Fatalf("GET / = %d %q, want a redirect to setup", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := h.page(c, "/-/login"); resp.Header.Get("Location") != "/-/setup" {
		t.Fatalf("the login page must send to setup while nobody exists, got %q", resp.Header.Get("Location"))
	}
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/wails/runtime.js", nil)
	if resp := h.do(c, req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a call that is not a page load must get 401, got %d", resp.StatusCode)
	}
	if body := readBody(t, h.page(c, "/-/setup")); !strings.Contains(body, "Create the administrator") {
		t.Fatalf("setup page: %s", body)
	}
}

func TestSetupCreatesTheAdministratorOnce(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")

	u, ok := h.gw.store.userByUsername("alice")
	if !ok || !u.Admin() {
		t.Fatalf("alice = %+v, %v; want an administrator", u, ok)
	}
	if resp := h.page(c, "/"); resp.StatusCode != http.StatusOK || readBody(t, resp) != "the app" {
		t.Fatalf("a signed-in administrator must reach the app, got %d", resp.StatusCode)
	}

	resp := h.post(h.browser(), "/-/setup", url.Values{"username": {"mallory"}, "password": {testPassword}, "confirm": {testPassword}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/-/login" {
		t.Fatalf("a second setup must be sent to sign in, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h.gw.store.userCount() != 1 {
		t.Fatal("a second setup must not create anyone")
	}
}

func TestSetupTokenIsRequiredWhenConfigured(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SetupToken = "s3cret-token" })
	form := url.Values{"username": {"alice"}, "password": {testPassword}, "confirm": {testPassword}}

	if resp := h.post(h.browser(), "/-/setup", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup without the token = %d, want 403", resp.StatusCode)
	}
	form.Set("token", "s3cret-token")
	if resp := h.post(h.browser(), "/-/setup", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup with the token = %d, want 303", resp.StatusCode)
	}
}

func TestWithoutAConfiguredTokenSetupNeedsTheOneInTheLog(t *testing.T) {
	var logs bytes.Buffer
	cfg := Config{DataDir: t.TempDir(), InCluster: "false", SessionTTL: time.Hour, PasswordLogin: true}
	gw, err := New(cfg, Deps{Owners: session.NewOwners(), Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gw.Handler())
	t.Cleanup(srv.Close)
	h := &harness{t: t, gw: gw, srv: srv}

	m := regexp.MustCompile(`setupToken=(\S+)`).FindStringSubmatch(logs.String())
	if m == nil {
		t.Fatalf("the log must carry the setup token: %s", logs.String())
	}
	if body := readBody(t, h.page(h.browser(), "/-/setup")); !strings.Contains(body, "grep setupToken") {
		t.Fatalf("the setup page must say where the token is: %s", body)
	}

	form := url.Values{"username": {"mallory"}, "password": {testPassword}, "confirm": {testPassword}}
	if resp := h.post(h.browser(), "/-/setup", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup without the token = %d, want 403", resp.StatusCode)
	}
	form.Set("token", "a-guess")
	if resp := h.post(h.browser(), "/-/setup", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup with a wrong token = %d, want 403", resp.StatusCode)
	}
	if gw.store.userCount() != 0 {
		t.Fatal("nobody must have been created without the token")
	}
	form.Set("username", "alice")
	form.Set("token", m[1])
	if resp := h.post(h.browser(), "/-/setup", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup with the logged token = %d, want 303", resp.StatusCode)
	}
}

func TestEveryStartWithNobodyMakesANewSetupToken(t *testing.T) {
	dir := t.TempDir()
	token := func() string {
		gw, err := New(Config{DataDir: dir, InCluster: "false", SessionTTL: time.Hour}, Deps{Owners: session.NewOwners(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatal(err)
		}
		return gw.cfg.SetupToken
	}
	first, second := token(), token()
	if first == "" || first == second {
		t.Fatalf("tokens %q and %q: each start must make a new, non-empty one", first, second)
	}
}

func TestNoSetupTokenIsMadeWhenItIsNotNeeded(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.AdminUsername, c.AdminPassword = "root", testPassword })
	if h.gw.cfg.SetupToken != "" || h.gw.generatedToken {
		t.Fatal("a deployment that made its administrator at startup needs no setup token")
	}
	h = newHarness(t, func(c *Config) { c.SetupToken = "configured" })
	if h.gw.cfg.SetupToken != "configured" || h.gw.generatedToken {
		t.Fatal("a configured setup token must be the one used")
	}
}

func TestBootstrapAdministratorFromTheEnvironment(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.AdminUsername, c.AdminPassword = "root", testPassword
	})
	if u, ok := h.gw.store.userByUsername("root"); !ok || !u.Admin() {
		t.Fatal("the administrator from the environment must exist")
	}
	h.signIn("root")
}

// ---- passing requests through ----------------------------------------------------

func TestTheAppIsToldWhoIsAskingAndNothingElse(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	alice, _ := h.gw.store.userByUsername("alice")

	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/assets/app.js", nil)
	req.Header.Set(headerUser, "somebody-else")
	req.Header.Set(headerSecret, "a-guess")
	if resp := h.do(c, req); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d", resp.StatusCode)
	}
	seen := h.lastSeen()
	if got := seen.Header.Get(headerUser); got != alice.ID {
		t.Fatalf("the app was told the user is %q, want %q", got, alice.ID)
	}
	if got := seen.Header.Get(headerSecret); got != h.gw.secret {
		t.Fatal("the app must be sent the gateway's own secret, not the browser's")
	}
	if strings.Contains(seen.Header.Get("Cookie"), sessionCookie) {
		t.Fatal("the session cookie must not be passed on to the app")
	}
}

// Wails refuses a loopback listener any Host but its own address, and checks
// Origin against Host; the browser's host goes along for the plugin views.
func TestTheAppIsAskedForByItsLoopbackAddress(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	app := strings.TrimPrefix(h.app.URL, "http://")
	public := strings.TrimPrefix(h.srv.URL, "http://")

	send := func(origin string) *http.Request {
		req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/assets/app.js", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("X-Forwarded-Host", "evil.example")
		if resp := h.do(c, req); resp.StatusCode != http.StatusOK {
			t.Fatalf("GET = %d", resp.StatusCode)
		}
		return h.lastSeen()
	}

	seen := send(h.srv.URL)
	if seen.Host != app {
		t.Fatalf("the app was asked for as %q, want %q", seen.Host, app)
	}
	if got := seen.Header.Get("X-Forwarded-Host"); got != public {
		t.Fatalf("X-Forwarded-Host = %q, want %q", got, public)
	}
	if got := seen.Header.Get("Origin"); got != h.app.URL {
		t.Fatalf("the gateway's own Origin reached the app as %q, want %q", got, h.app.URL)
	}

	for _, origin := range []string{"https://evil.example", "null"} {
		if got := send(origin).Header.Get("Origin"); got != origin {
			t.Fatalf("Origin %q reached the app as %q; only the gateway's own is translated", origin, got)
		}
	}
	if got := send("").Header.Get("Origin"); got != "" {
		t.Fatalf("a request without an Origin reached the app with %q", got)
	}
}

func TestCrossSiteRequestsAreRefused(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")

	call := func(header, value string) int {
		req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/wails/runtime", strings.NewReader(`{}`))
		req.Header.Set(header, value)
		return h.do(c, req).StatusCode
	}
	if got := call("Origin", "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("a POST from another origin = %d, want 403", got)
	}
	if got := call("Sec-Fetch-Site", "cross-site"); got != http.StatusForbidden {
		t.Fatalf("a cross-site POST = %d, want 403", got)
	}
	if got := call("Sec-Fetch-Site", "same-site"); got != http.StatusForbidden {
		t.Fatalf("a POST from a sibling site = %d, want 403", got)
	}
	if got := call("Origin", h.srv.URL); got != http.StatusOK {
		t.Fatalf("a POST from the gateway's own origin = %d, want 200", got)
	}
}

func TestIdentityRefusesRequestsTheGatewayDidNotSend(t *testing.T) {
	h := newHarness(t, nil)
	alice := h.addUser("alice", roleAdmin)

	var got session.User
	var client string
	handler := h.gw.Identity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = session.FromContext(r.Context())
		client = session.Client(r.Context())
	}))
	serve := func(secret, user string) int {
		req := httptest.NewRequest(http.MethodPost, "/wails/runtime", nil)
		req.Header.Set(headerSecret, secret)
		req.Header.Set(headerUser, user)
		req.Header.Set("x-wails-client-id", "tab-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := serve("", alice.ID); code != http.StatusForbidden {
		t.Fatalf("no secret = %d, want 403", code)
	}
	if code := serve(h.gw.secret, "nobody"); code != http.StatusForbidden {
		t.Fatalf("an unknown user = %d, want 403", code)
	}
	if code := serve(h.gw.secret, alice.ID); code != http.StatusOK {
		t.Fatalf("a request from the gateway = %d, want 200", code)
	}
	if got.ID != alice.ID || !got.Admin || client != "tab-1" {
		t.Fatalf("the context carried %+v from %q", got, client)
	}
}

func TestPluginViewsNeedNoSignIn(t *testing.T) {
	h := newHarness(t, nil)
	h.setUp("alice")
	// A sandboxed view's requests for its own files carry no cookie.
	frame := h.browser()
	seen := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.seen)
	}
	get := func(method, path string) int {
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		req.Header.Set("Origin", "null")
		return h.do(frame, req).StatusCode
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if code := get(method, "/plugin-ui/argocd/argo.css"); code != http.StatusOK {
			t.Fatalf("%s a view's file = %d, want 200", method, code)
		}
		req := h.lastSeen()
		if req.Header.Get(headerSecret) != h.gw.secret {
			t.Fatal("the app must be told the request came through the gateway")
		}
		if id := req.Header.Get(headerUser); id != "" {
			t.Fatalf("a request without a session was sent to the app as %q", id)
		}
	}

	before := seen()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/plugin-ui/argocd/argo.css"},
		{http.MethodGet, "/wails/runtime.js"},
		{http.MethodGet, "/plugin-uix/argocd/argo.css"},
		// Cleaned, and so redirected, before it is matched.
		{http.MethodGet, "/plugin-ui/../wails/runtime.js"},
	} {
		if code := get(c.method, c.path); code == http.StatusOK {
			t.Errorf("%s %s without a session = 200", c.method, c.path)
		}
	}
	if n := seen() - before; n != 0 {
		t.Fatalf("the app saw %d requests without a session that were not for a view's files", n)
	}
}

func TestIdentityLetsNobodyReadOnlyPluginViews(t *testing.T) {
	h := newHarness(t, nil)

	var reached bool
	var named bool
	handler := h.gw.Identity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_, named = session.FromContext(r.Context())
	}))
	serve := func(secret, method, path string) int {
		reached = false
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set(headerSecret, secret)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := serve(h.gw.secret, http.MethodGet, "/plugin-ui/argocd/overview.html"); code != http.StatusOK || !reached {
		t.Fatalf("a view's file from the gateway = %d, want 200", code)
	}
	if named {
		t.Fatal("a request for nobody must not carry a user")
	}
	for _, c := range []struct{ secret, method, path string }{
		{"", http.MethodGet, "/plugin-ui/argocd/overview.html"},
		{h.gw.secret, http.MethodPost, "/plugin-ui/argocd/overview.html"},
		{h.gw.secret, http.MethodGet, "/wails/runtime.js"},
		{h.gw.secret, http.MethodPost, "/wails/runtime"},
	} {
		if code := serve(c.secret, c.method, c.path); code != http.StatusForbidden || reached {
			t.Errorf("%s %s for nobody (secret %t) = %d, want 403", c.method, c.path, c.secret != "", code)
		}
	}
}

// ---- signing in -------------------------------------------------------------------

func TestSignInAndOut(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("alice", roleAdmin)
	c := h.browser()

	if resp := h.post(c, "/-/login", url.Values{"username": {"alice"}, "password": {"not the password"}}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password = %d, want 401", resp.StatusCode)
	}
	resp := h.post(c, "/-/login", url.Values{"username": {"ALICE"}, "password": {testPassword}, "next": {"/somewhere"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/somewhere" {
		t.Fatalf("signing in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := h.page(c, "/"); resp.StatusCode != http.StatusOK {
		t.Fatalf("signed in, GET / = %d", resp.StatusCode)
	}

	if resp := h.page(c, "/-/logout"); resp.Header.Get("Location") != "/-/login?done=signed-out" {
		t.Fatalf("signing out sent to %q", resp.Header.Get("Location"))
	}
	if resp := h.page(c, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signed out, GET / = %d, want a redirect to sign in", resp.StatusCode)
	}
}

func TestSignInIsRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("alice", roleAdmin)
	c := h.browser()
	for range 10 {
		h.post(c, "/-/login", url.Values{"username": {"alice"}, "password": {"wrong password"}})
	}
	if resp := h.post(c, "/-/login", url.Values{"username": {"alice"}, "password": {testPassword}}); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after ten failures even the right password = %d, want 429", resp.StatusCode)
	}
}

// signInFrom tries a password sign-in that arrives through a proxy, which
// sends xff as X-Forwarded-For.
func (h *harness) signInFrom(xff, username, password string) *http.Response {
	h.t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/-/login", strings.NewReader(form.Encode()))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", xff)
	return h.do(h.browser(), req)
}

func TestForwardedFor(t *testing.T) {
	for _, tc := range []struct {
		lines []string
		hops  int
		want  string
	}{
		{[]string{"203.0.113.7"}, 1, "203.0.113.7"},
		// The client wrote the first entry; the ingress appended the second.
		{[]string{"198.51.100.1, 203.0.113.7"}, 1, "203.0.113.7"},
		{[]string{"198.51.100.1", "203.0.113.7"}, 1, "203.0.113.7"},
		// A load balancer, then the ingress.
		{[]string{"198.51.100.1, 203.0.113.7, 10.0.0.5"}, 2, "203.0.113.7"},
		{[]string{"203.0.113.7"}, 3, "203.0.113.7"},
		{[]string{"[2001:db8::1]:4711"}, 1, "2001:db8::1"},
		{[]string{"::ffff:203.0.113.7"}, 1, "203.0.113.7"},
		{[]string{"203.0.113.7, not-an-address"}, 1, ""},
		{[]string{" , "}, 1, ""},
		{nil, 1, ""},
	} {
		if got := forwardedFor(tc.lines, tc.hops); got != tc.want {
			t.Errorf("forwardedFor(%q, %d) = %q, want %q", tc.lines, tc.hops, got, tc.want)
		}
	}
}

func TestASpoofedForwardedForIsStillCountedAgainstTheRealAddress(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TrustForwarded, c.ProxyHops = true, 1 })
	h.addUser("alice", roleAdmin)
	// Thirty failures, each claiming a new address in front of the one the
	// ingress appended, and each for a different name so that only the
	// per-address limit is in play.
	for i := range 30 {
		h.signInFrom(fmt.Sprintf("198.51.100.%d, 203.0.113.7", i), fmt.Sprintf("guess%d", i), "wrong password")
	}
	if resp := h.signInFrom("198.51.100.200, 203.0.113.7", "alice", testPassword); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the address that failed thirty times = %d, want 429", resp.StatusCode)
	}
	if resp := h.signInFrom("203.0.113.8", "alice", testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("another address = %d, want 303", resp.StatusCode)
	}
}

func TestOneAddressCannotLockSomeoneElseOut(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TrustForwarded = true })
	h.addUser("alice", roleAdmin)
	for range 10 {
		h.signInFrom("203.0.113.66", "alice", "wrong password")
	}
	if resp := h.signInFrom("203.0.113.66", "alice", testPassword); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the failing address = %d, want 429", resp.StatusCode)
	}
	if resp := h.signInFrom("203.0.113.7", "alice", testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("alice from her own address = %d, want 303", resp.StatusCode)
	}
}

func TestGuessingOneNameFromManyAddressesIsLimited(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TrustForwarded = true })
	h.addUser("alice", roleAdmin)
	for i := range 100 {
		h.signInFrom(fmt.Sprintf("198.51.100.%d", i/9), "alice", "wrong password")
	}
	if resp := h.signInFrom("203.0.113.7", "alice", testPassword); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after a hundred failures from many addresses = %d, want 429", resp.StatusCode)
	}
}

func TestSwitchedOffUsersCannotSignIn(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("alice", roleAdmin)
	bob := h.addUser("bob", roleUser)
	c := h.signIn("bob")
	if _, err := h.gw.store.updateUser(bob.ID, func(u *User) error { u.Disabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if resp := h.page(c, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("switching bob off must end his session, GET / = %d", resp.StatusCode)
	}
	if resp := h.post(h.browser(), "/-/login", url.Values{"username": {"bob"}, "password": {testPassword}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a switched-off user signing in = %d, want 403", resp.StatusCode)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "/",
		"/":                    "/",
		"/-/admin/users":       "/-/admin/users",
		"//evil.example/":      "/",
		"https://evil.example": "/",
		"/\\evil.example":      "/",
		"/ok?x=1":              "/ok?x=1",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- administration ----------------------------------------------------------------

func TestAdminPagesAreForAdministrators(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.setUp("alice")
	h.addUser("bob", roleUser)
	bob := h.signIn("bob")

	for _, path := range []string{"/-/admin/users", "/-/admin/providers", "/-/admin/clusters"} {
		if resp := h.page(bob, path); resp.StatusCode != http.StatusForbidden {
			t.Errorf("bob GET %s = %d, want 403", path, resp.StatusCode)
		}
		if resp := h.page(admin, path); resp.StatusCode != http.StatusOK {
			t.Errorf("alice GET %s = %d, want 200: %s", path, resp.StatusCode, readBody(t, resp))
		}
	}
	if resp := h.page(bob, "/-/account"); resp.StatusCode != http.StatusOK {
		t.Fatalf("bob's own account page = %d", resp.StatusCode)
	}
}

func TestAdminFormsNeedTheSessionsCSRFToken(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	form := url.Values{"username": {"carol"}, "password": {testPassword}, "role": {"user"}}

	if resp := h.post(c, "/-/admin/users", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a form without the token = %d, want 403", resp.StatusCode)
	}
	form.Set("csrf", h.csrf(c, "/-/admin/users"))
	if resp := h.post(c, "/-/admin/users", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a form with the token = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if _, ok := h.gw.store.userByUsername("carol"); !ok {
		t.Fatal("carol must have been added")
	}
}

func TestTheLastAdministratorStays(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	alice, _ := h.gw.store.userByUsername("alice")
	token := h.csrf(c, "/-/admin/users")

	if resp := h.post(c, "/-/admin/users/"+alice.ID+"/role-user", url.Values{"csrf": {token}}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("demoting the last administrator = %d, want 400", resp.StatusCode)
	}
	if u, _ := h.gw.store.user(alice.ID); !u.Admin() {
		t.Fatal("alice must still be an administrator")
	}
	if resp := h.post(c, "/-/admin/users/"+alice.ID+"/delete", url.Values{"csrf": {token}}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("removing yourself = %d, want 400", resp.StatusCode)
	}
}

func TestRemovingAUserClosesTheirStreams(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	bob := h.addUser("bob", roleUser)
	var closed atomic.Bool
	ctx := session.WithUser(context.Background(), session.User{ID: bob.ID})
	if err := h.owners.Claim(ctx, "term-9", func() { closed.Store(true) }); err != nil {
		t.Fatal(err)
	}
	resp := h.post(c, "/-/admin/users/"+bob.ID+"/delete", url.Values{"csrf": {h.csrf(c, "/-/admin/users")}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("removing bob = %d", resp.StatusCode)
	}
	if !closed.Load() {
		t.Fatal("bob's terminal must be closed when he is removed")
	}
}

func TestUploadingAndRemovingAKubeconfig(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")

	upload := func(name, content string) *http.Response {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("csrf", h.csrf(c, "/-/admin/clusters"))
		_ = mw.WriteField("name", name)
		_ = mw.WriteField("content", content)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/-/admin/clusters", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return h.do(c, req)
	}

	if resp := upload("broken", "this: is: not a kubeconfig"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a file that is not a kubeconfig = %d, want 400", resp.StatusCode)
	}
	if resp := upload("../escape", testKubeconfig); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a name with a path in it = %d, want 400", resp.StatusCode)
	}
	if resp := upload("prod", testKubeconfig); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a good kubeconfig = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	for name, config := range map[string]string{
		"shell":     strings.Replace(testKubeconfig, "    token: abc", "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: sh\n      args: [\"-c\", \"id\"]", 1),
		"sa-token":  strings.Replace(testKubeconfig, "    token: abc", "    tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token", 1),
		"local-key": strings.Replace(testKubeconfig, "    token: abc", "    client-certificate: /data/a.crt\n    client-key: /data/a.key", 1),
	} {
		resp := upload(name, config)
		if body := readBody(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "only inline credentials") {
			t.Errorf("%s: an upload naming a program or a file = %d, want 400: %s", name, resp.StatusCode, body)
		}
		if _, err := os.Stat(filepath.Join(h.gw.cfg.UploadDir(), name+".yaml")); !os.IsNotExist(err) {
			t.Errorf("%s: a refused upload must not be kept", name)
		}
	}
	path := filepath.Join(h.gw.cfg.UploadDir(), "prod.yaml")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the kubeconfig must be kept, readable only by the app: %v %v", info, err)
	}
	if h.resyncs.Load() != 1 {
		t.Fatalf("the app must be told to rescan once, was told %d times", h.resyncs.Load())
	}
	if body := readBody(t, h.page(c, "/-/admin/clusters")); !strings.Contains(body, "prod-admin") {
		t.Fatal("the clusters page must list the new context")
	}
	if resp := upload("prod", testKubeconfig); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a second upload with the same name = %d, want 400", resp.StatusCode)
	}

	resp := h.post(c, "/-/admin/clusters/prod.yaml/delete", url.Values{"csrf": {h.csrf(c, "/-/admin/clusters")}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("removing = %d", resp.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the kubeconfig must be gone")
	}
}

// A kubeconfig kept before uploads were checked -- on a persistent volume,
// from an older version -- is refused by the app; the clusters page says why.
func TestTheClustersPageExplainsAnOldExecUpload(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	old := strings.Replace(testKubeconfig, "    token: abc", "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: sh", 1)
	if err := os.WriteFile(filepath.Join(h.gw.cfg.UploadDir(), "old.yaml"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, h.page(c, "/-/admin/clusters")); !strings.Contains(body, "The app does not use this file") {
		t.Fatalf("the clusters page must say the old upload is not used: %s", body)
	}
}

func TestProvidersCanBeAddedEditedAndRemoved(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	form := url.Values{
		"csrf": {h.csrf(c, "/-/admin/providers")}, "type": {"github"}, "id": {"github"},
		"clientId": {"abc"}, "clientSecret": {"shh"}, "enabled": {"on"},
	}
	if resp := h.post(c, "/-/admin/providers", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("adding = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if body := readBody(t, h.page(h.browser(), "/-/login")); !strings.Contains(body, "Continue with GitHub") {
		t.Fatal("the sign-in page must offer the new provider")
	}

	// Saving without a secret keeps the one there.
	form.Set("original", "github")
	form.Set("clientSecret", "")
	form.Set("name", "Company GitHub")
	if resp := h.post(c, "/-/admin/providers", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("editing = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if p, _ := h.gw.store.provider("github"); p.ClientSecret != "shh" || p.Name != "Company GitHub" {
		t.Fatalf("after editing: %+v", p)
	}

	resp := h.post(c, "/-/admin/providers/github/delete", url.Values{"csrf": {h.csrf(c, "/-/admin/providers")}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("removing = %d", resp.StatusCode)
	}
	if _, ok := h.gw.store.provider("github"); ok {
		t.Fatal("the provider must be gone")
	}
}

// ---- provider sign-in ------------------------------------------------------------

// fakeIssuer is an OpenID Connect provider that signs in whoever claims says.
func fakeIssuer(t *testing.T, claims map[string]any) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"userinfo_endpoint":      srv.URL + "/userinfo",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("code") != "the-code" || r.PostForm.Get("code_verifier") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"the-token","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer the-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(claims)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (h *harness) addProvider(p Provider) {
	h.t.Helper()
	if err := p.normalize(); err != nil {
		h.t.Fatal(err)
	}
	if err := h.gw.store.putProvider(p, ""); err != nil {
		h.t.Fatal(err)
	}
}

// providerSignIn goes through a provider sign-in in c and returns the
// callback's answer.
func (h *harness) providerSignIn(c *http.Client, provider string) *http.Response {
	h.t.Helper()
	resp := h.page(c, "/-/oauth/"+provider+"/start?next=/after")
	if resp.StatusCode != http.StatusFound {
		h.t.Fatalf("starting a sign-in = %d: %s", resp.StatusCode, readBody(h.t, resp))
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	if loc.Query().Get("code_challenge") == "" {
		h.t.Fatal("a sign-in must use PKCE")
	}
	return h.page(c, "/-/oauth/"+provider+"/callback?code=the-code&state="+url.QueryEscape(loc.Query().Get("state")))
}

func oidcProvider(issuer string, autoSignup bool) Provider {
	return Provider{ID: "sso", Type: "oidc", Name: "Company SSO", ClientID: "cid", ClientSecret: "sec", Issuer: issuer, AutoSignup: autoSignup, Enabled: true}
}

func TestProviderSignInCreatesAUserWhenAllowed(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-123", "email": "Dana@Example.com", "email_verified": true, "name": "Dana", "preferred_username": "dana"})
	h.addProvider(oidcProvider(issuer.URL, true))

	c := h.browser()
	resp := h.providerSignIn(c, "sso")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/after" {
		t.Fatalf("callback = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), readBody(t, resp))
	}
	dana, ok := h.gw.store.userByIdentity("sso", "u-123")
	if !ok || dana.Username != "dana" || dana.Email != "dana@example.com" || dana.Admin() {
		t.Fatalf("dana = %+v, %v", dana, ok)
	}
	if resp := h.page(c, "/"); resp.StatusCode != http.StatusOK {
		t.Fatalf("dana must be signed in, GET / = %d", resp.StatusCode)
	}

	// Signing in again is the same user, not a second one.
	h.providerSignIn(h.browser(), "sso")
	if n := h.gw.store.userCount(); n != 2 {
		t.Fatalf("there must be two users, there are %d", n)
	}
}

func TestProviderSignInWithoutAnAccountIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-1", "email": "eve@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, false))

	resp := h.providerSignIn(h.browser(), "sso")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "eve@example.com") {
		t.Fatalf("an unknown user = %d, want 403 naming the address", resp.StatusCode)
	}
	if h.gw.store.userCount() != 1 {
		t.Fatal("nobody must have been created")
	}
}

func TestProviderSignInLinksAnAddedUserByVerifiedEmail(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	frank, err := h.gw.store.createUser(User{Username: "frank", Email: "frank@example.com", Role: roleUser}, false)
	if err != nil {
		t.Fatal(err)
	}
	issuer := fakeIssuer(t, map[string]any{"sub": "u-77", "email": "frank@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, false))

	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	linked, ok := h.gw.store.userByIdentity("sso", "u-77")
	if !ok || linked.ID != frank.ID {
		t.Fatal("the sign-in must be linked to frank")
	}
}

func TestAnUnverifiedEmailMatchesNobody(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	if _, err := h.gw.store.createUser(User{Username: "grace", Email: "grace@example.com", Role: roleAdmin}, false); err != nil {
		t.Fatal(err)
	}
	issuer := fakeIssuer(t, map[string]any{"sub": "attacker", "email": "grace@example.com"})
	h.addProvider(oidcProvider(issuer.URL, false))

	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an address the provider does not vouch for = %d, want 403", resp.StatusCode)
	}
}

// With autoSignup on a public provider, the first to arrive could be anyone:
// on a deployment with nobody yet, a provider admits only a listed
// administrator.
func TestTheFirstProviderSignInIsNotTheAdministratorByBeingFirst(t *testing.T) {
	h := newHarness(t, nil)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-1", "email": "ops@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, true))

	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "setup page first") {
		t.Fatalf("callback = %d, want 403 sending to the setup page", resp.StatusCode)
	}
	if n := h.gw.store.userCount(); n != 0 {
		t.Fatalf("nobody must have been created, there are %d users", n)
	}
}

func TestAListedAdministratorCanBeTheFirstProviderSignIn(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.AdminEmails = []string{"ops@example.com"} })
	issuer := fakeIssuer(t, map[string]any{"sub": "u-1", "email": "ops@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, false))

	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if u, _ := h.gw.store.userByIdentity("sso", "u-1"); !u.Admin() {
		t.Fatal("a listed administrator must be the administrator")
	}
}

// A provider vouching for an address claims only an account nobody can sign
// in to yet; otherwise the least careful enabled provider would decide who
// may be the administrator.
func TestProviderSignInDoesNotTakeOverAnAccountWithAPassword(t *testing.T) {
	h := newHarness(t, nil)
	root := h.addUser("root", roleAdmin)
	if _, err := h.gw.store.updateUser(root.ID, func(u *User) error { u.Email = "root@example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	issuer := fakeIssuer(t, map[string]any{"sub": "attacker", "email": "root@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, true))

	resp := h.providerSignIn(h.browser(), "sso")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "account page") {
		t.Fatalf("callback = %d, want 403 pointing at the account page", resp.StatusCode)
	}
	if _, ok := h.gw.store.userByIdentity("sso", "attacker"); ok {
		t.Fatal("the provider account must not have been linked to anyone")
	}
	if n := h.gw.store.userCount(); n != 1 {
		t.Fatalf("no second account may be made for the address, there are %d users", n)
	}
}

func TestProviderSignInDoesNotTakeOverAnAccountAnotherProviderClaimed(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	if _, err := h.gw.store.createUser(User{Username: "frank", Email: "frank@example.com", Role: roleUser}, false); err != nil {
		t.Fatal(err)
	}
	first := fakeIssuer(t, map[string]any{"sub": "u-77", "email": "frank@example.com", "email_verified": true})
	h.addProvider(oidcProvider(first.URL, false))
	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("the first claim = %d: %s", resp.StatusCode, readBody(t, resp))
	}

	second := fakeIssuer(t, map[string]any{"sub": "other", "email": "frank@example.com", "email_verified": true})
	p := oidcProvider(second.URL, false)
	p.ID, p.Name = "other", "Other SSO"
	h.addProvider(p)
	if resp := h.providerSignIn(h.browser(), "other"); resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "already an account here") {
		t.Fatalf("a second provider = %d, want 403", resp.StatusCode)
	}
	if _, ok := h.gw.store.userByIdentity("other", "other"); ok {
		t.Fatal("the second provider account must not have been linked")
	}
}

// connect starts connecting a provider from c's account page and finishes
// it, returning the callback's answer.
func (h *harness) connect(c *http.Client, provider string) *http.Response {
	h.t.Helper()
	resp := h.post(c, "/-/account/connect/"+provider, url.Values{"csrf": {h.csrf(c, "/-/account")}})
	if resp.StatusCode != http.StatusFound {
		h.t.Fatalf("starting a connection = %d: %s", resp.StatusCode, readBody(h.t, resp))
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	return h.page(c, "/-/oauth/"+provider+"/callback?code=the-code&state="+url.QueryEscape(loc.Query().Get("state")))
}

func TestASignedInUserCanConnectAProvider(t *testing.T) {
	h := newHarness(t, nil)
	alice := h.addUser("alice", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-5", "email": "alice@example.com", "email_verified": true})
	h.addProvider(oidcProvider(issuer.URL, false))

	c := h.signIn("alice")
	if body := readBody(t, h.page(c, "/-/account")); !strings.Contains(body, "Connect Company SSO") {
		t.Fatalf("the account page must offer the provider: %s", body)
	}
	resp := h.connect(c, "sso")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/-/account?done=provider-connected" {
		t.Fatalf("callback = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), readBody(t, resp))
	}
	if u, ok := h.gw.store.userByIdentity("sso", "u-5"); !ok || u.ID != alice.ID {
		t.Fatal("the provider account must be connected to alice")
	}
	if body := readBody(t, h.page(c, "/-/account")); strings.Contains(body, "Connect Company SSO") {
		t.Fatal("a connected provider must not be offered again")
	}

	// And it signs her in from now on.
	if resp := h.providerSignIn(h.browser(), "sso"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signing in with the connected account = %d: %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestConnectingNeedsTheSessionsCSRFToken(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("alice", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-5"})
	h.addProvider(oidcProvider(issuer.URL, false))

	c := h.signIn("alice")
	if resp := h.post(c, "/-/account/connect/sso", url.Values{}); resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "form has expired") {
		t.Fatalf("a connection without the CSRF token = %d, want 403", resp.StatusCode)
	}
	if resp := h.post(h.browser(), "/-/account/connect/sso", url.Values{}); resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/-/login") {
		t.Fatalf("a connection without a sign-in = %d %q, want a redirect to sign in", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestConnectingAnotherUsersProviderAccountIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("alice", roleAdmin)
	bob := h.addUser("bob", roleUser)
	if _, err := h.gw.store.linkIdentity(bob.ID, Identity{Provider: "sso", Subject: "u-5"}); err != nil {
		t.Fatal(err)
	}
	issuer := fakeIssuer(t, map[string]any{"sub": "u-5"})
	h.addProvider(oidcProvider(issuer.URL, false))

	if resp := h.connect(h.signIn("alice"), "sso"); resp.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, resp), "already connected to another user") {
		t.Fatalf("connecting bob's account to alice = %d, want 409", resp.StatusCode)
	}
	if u, _ := h.gw.store.userByIdentity("sso", "u-5"); u.ID != bob.ID {
		t.Fatal("the provider account must still be bob's alone")
	}
}

func TestAConnectionFinishedAfterSigningOutIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	alice := h.addUser("alice", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-5"})
	h.addProvider(oidcProvider(issuer.URL, false))

	c := h.signIn("alice")
	resp := h.post(c, "/-/account/connect/sso", url.Values{"csrf": {h.csrf(c, "/-/account")}})
	loc, _ := url.Parse(resp.Header.Get("Location"))
	h.page(c, "/-/logout")
	h.addUser("mallory", roleUser)
	h.signInWith(c, "mallory")

	cb := h.page(c, "/-/oauth/sso/callback?code=the-code&state="+url.QueryEscape(loc.Query().Get("state")))
	if cb.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, cb), "no longer signed in as the account that asked") {
		t.Fatalf("finishing alice's connection as mallory = %d, want 403", cb.StatusCode)
	}
	if u, ok := h.gw.store.userByIdentity("sso", "u-5"); ok {
		t.Fatalf("the provider account must be connected to nobody, it is %s's (alice is %s)", u.Username, alice.ID)
	}
}

func TestACallbackFromAnotherBrowserIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.addUser("root", roleAdmin)
	issuer := fakeIssuer(t, map[string]any{"sub": "u-1", "email_verified": true, "email": "x@example.com"})
	h.addProvider(oidcProvider(issuer.URL, true))

	resp := h.page(h.browser(), "/-/oauth/sso/start")
	loc, _ := url.Parse(resp.Header.Get("Location"))
	// A different browser -- the victim's -- follows the attacker's link.
	victim := h.page(h.browser(), "/-/oauth/sso/callback?code=the-code&state="+url.QueryEscape(loc.Query().Get("state")))
	if victim.StatusCode != http.StatusBadRequest {
		t.Fatalf("a callback without the state cookie = %d, want 400", victim.StatusCode)
	}
}

// ---- events ------------------------------------------------------------------------

func TestVisible(t *testing.T) {
	h := newHarness(t, nil)
	ctx := session.WithUser(context.Background(), session.User{ID: "alice"})
	if err := h.owners.Claim(ctx, "term-1", nil); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		user, event string
		want        bool
	}{
		{"alice", `{"name":"terminal:data","data":{"sessionId":"term-1","data":"aGk="}}`, true},
		{"bob", `{"name":"terminal:data","data":{"sessionId":"term-1","data":"aGk="}}`, false},
		{"alice", `{"name":"terminal:data","data":{"sessionId":"term-2"}}`, false},
		{"bob", `{"name":"common:ready","data":null}`, true},
		{"alice", `not json`, false},
		{"alice", `{"name":"terminal:data","data":"not an object"}`, false},
	}
	for _, c := range cases {
		if got := h.gw.visible(c.user, []byte(c.event)); got != c.want {
			t.Errorf("visible(%s, %s) = %v, want %v", c.user, c.event, got, c.want)
		}
	}
}

func TestTheEventRelayDropsOtherUsersStreams(t *testing.T) {
	h := newHarness(t, nil)
	c := h.setUp("alice")
	alice, _ := h.gw.store.userByUsername("alice")
	for id, owner := range map[string]string{"term-1": alice.ID, "term-2": "bob"} {
		if err := h.owners.Claim(session.WithUser(context.Background(), session.User{ID: owner}), id, nil); err != nil {
			t.Fatal(err)
		}
	}

	gotClient := make(chan string, 1)
	h.appWS = func(w http.ResponseWriter, r *http.Request) {
		gotClient <- r.URL.Query().Get("clientId")
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for _, event := range []string{
			`{"name":"terminal:data","data":{"sessionId":"term-2","data":"Ym9i"}}`,
			`{"name":"terminal:data","data":{"sessionId":"term-1","data":"YWxpY2U="}}`,
			`{"name":"common:ready","data":null}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
				return
			}
		}
		<-conn.CloseRead(r.Context()).Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := "ws" + strings.TrimPrefix(h.srv.URL, "http") + "/wails/events?clientId=tab-a"
	conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {sessionCookie + "=" + h.sessionToken(c)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()

	if got := <-gotClient; got != "tab-a" {
		t.Fatalf("the app was told the tab is %q", got)
	}
	var got []string
	for range 2 {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(data))
	}
	if !strings.Contains(got[0], "term-1") || !strings.Contains(got[1], "common:ready") {
		t.Fatalf("alice received %v; want her own terminal and the common event, not bob's terminal", got)
	}
}

func TestTheEventRelayNeedsASignIn(t *testing.T) {
	h := newHarness(t, nil)
	h.setUp("alice")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.srv.URL, "http")+"/wails/events", nil)
	if err == nil {
		t.Fatal("an event socket without a session must be refused")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refused with %v", resp)
	}
}

func TestPresenceClosesWhatATabLeftBehind(t *testing.T) {
	owners := session.NewOwners()
	var closed atomic.Int32
	ctx := session.WithClient(session.WithUser(context.Background(), session.User{ID: "alice"}), "tab-a")
	if err := owners.Claim(ctx, "term-1", func() { closed.Add(1) }); err != nil {
		t.Fatal(err)
	}
	p := newPresence(owners, 30*time.Millisecond)

	// A reconnect inside the grace period keeps everything open.
	p.join("alice", "tab-a")
	p.leave("alice", "tab-a")
	p.join("alice", "tab-a")
	time.Sleep(80 * time.Millisecond)
	if closed.Load() != 0 {
		t.Fatal("a tab that came back must keep its streams")
	}

	p.leave("alice", "tab-a")
	deadline := time.Now().Add(2 * time.Second)
	for closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if closed.Load() != 1 {
		t.Fatalf("a tab gone for good must have its streams closed once, closed %d times", closed.Load())
	}
}
