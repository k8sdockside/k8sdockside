// Package gateway is the web version's front door.
//
// In the web version the app is a Wails server listening on loopback, and this
// is what browsers talk to instead. It decides who gets in: it serves the
// sign-in and admin pages, keeps users, sessions and sign-in providers in the
// data directory, and passes every request it lets through to the app with the
// user attached -- which the app's services read back through package session.
//
// It also relays the app's event socket. Wails sends every event to every
// connected browser; the relay drops each one that belongs to a stream someone
// else opened, so one user never receives another's terminal output.
package gateway

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/session"
)

const (
	sessionCookie = "k8sdockside_session"
	oauthCookie   = "k8sdockside_oauth"

	// headerSecret carries a secret only this process knows, so the app can
	// tell a request the gateway let through from one that reached its
	// loopback port some other way. headerUser names who it is for.
	headerSecret = "X-Dockside-Gateway" // #nosec G101 -- a header name; the secret is made at startup
	headerUser   = "X-Dockside-User"

	// maxFormBytes bounds a form, which is at most a kubeconfig and a few
	// fields.
	maxFormBytes = 2 << 20
)

// Deps are the parts of the app the gateway works with.
type Deps struct {
	// Owners says who opened each stream; see session.Owners.
	Owners *session.Owners
	// OwnedEvents names, per event, the payload field holding the ID of the
	// stream it belongs to.
	OwnedEvents map[string]string
	// Resync makes the app rescan its kubeconfigs after a change here.
	Resync func()
	Logger *slog.Logger
}

// Gateway authenticates browsers and passes them through to the app.
type Gateway struct {
	cfg     Config
	deps    Deps
	log     *slog.Logger
	store   *store
	managed []Provider
	oauth   *oauthClient
	pending *pendingLogins
	// Failed sign-ins, per address and per username.
	ipLimit, userLimit *limiter
	secret             string
	upstream           atomic.Pointer[url.URL]
	proxy              *httputil.ReverseProxy
	probe              *http.Client
	presence           *presence
	pages              map[string]*template.Template
	mux                *http.ServeMux
}

// New opens the gateway's store in the data directory and builds the gateway.
// The address of the app behind it is set with SetUpstream.
func New(cfg Config, deps Deps) (*Gateway, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	st, err := openStore(cfg.authFile())
	if err != nil {
		return nil, err
	}
	managed, err := loadManagedProviders(cfg.ProvidersFile)
	if err != nil {
		return nil, err
	}
	pages, err := loadPages()
	if err != nil {
		return nil, err
	}

	g := &Gateway{
		cfg:       cfg,
		deps:      deps,
		log:       deps.Logger,
		store:     st,
		managed:   managed,
		oauth:     newOAuthClient(),
		pending:   &pendingLogins{m: map[string]pendingLogin{}},
		ipLimit:   newLimiter(30, 15*time.Minute),
		userLimit: newLimiter(10, 15*time.Minute),
		secret:    randomToken(32),
		probe:     &http.Client{Timeout: 2 * time.Second},
		presence:  newPresence(deps.Owners, clientGrace),
		pages:     pages,
	}
	g.proxy = &httputil.ReverseProxy{
		Rewrite: g.rewrite,
		// Streams -- the runtime's own, a plugin view's -- must arrive as they
		// are written rather than when a buffer fills.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			g.log.Warn("the app did not answer", "path", r.URL.Path, "err", err)
			http.Error(w, "K8s Dockside is still starting; try again in a moment", http.StatusBadGateway)
		},
	}
	if err := g.routes(); err != nil {
		return nil, err
	}
	if err := g.bootstrap(); err != nil {
		return nil, err
	}
	return g, nil
}

// SetUpstream says where the app behind the gateway listens.
func (g *Gateway) SetUpstream(hostport string) {
	g.upstream.Store(&url.URL{Scheme: "http", Host: hostport})
}

// bootstrap creates the first administrator from the environment, or says how
// the first one will be made.
func (g *Gateway) bootstrap() error {
	if g.store.userCount() > 0 {
		return nil
	}
	if g.cfg.AdminUsername != "" {
		hash, err := hashPassword(g.cfg.AdminPassword)
		if err != nil {
			return err
		}
		if _, err := g.store.createUser(User{Username: g.cfg.AdminUsername, Role: roleAdmin, PasswordHash: hash}, true); err != nil {
			return fmt.Errorf("creating the administrator %q: %w", g.cfg.AdminUsername, err)
		}
		g.log.Info("created the administrator from the environment", "username", g.cfg.AdminUsername)
		return nil
	}
	if g.cfg.SetupToken != "" {
		g.log.Info("no users yet: open K8s Dockside and create the administrator with the setup token")
	} else {
		g.log.Warn("no users yet: the first person to open K8s Dockside creates the administrator account")
	}
	return nil
}

// Serve listens on the configured address until ctx is done.
func (g *Gateway) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", g.cfg.ListenAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler: g.Handler(),
		// No read or write timeout: the event socket and the app's streams are
		// held open for as long as a tab is. Headers must still arrive
		// promptly.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(g.log.Handler(), slog.LevelWarn),
	}
	g.log.Info("K8s Dockside is listening", "address", ln.Addr().String())
	go g.housekeeping(ctx)

	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// housekeeping forgets what has expired, now and then.
func (g *Gateway) housekeeping(ctx context.Context) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := g.store.purge(); err != nil {
			g.log.Warn("could not forget expired sessions", "err", err)
		}
		g.pending.purge()
		g.ipLimit.prune()
		g.userLimit.prune()
	}
}

// Handler is everything the gateway serves.
func (g *Gateway) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/-/") {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Cache-Control", "no-store")
		}
		// Cookies are SameSite=Lax, which already keeps them off another
		// site's POST; this refuses the rest -- a sibling subdomain, a browser
		// that ignores SameSite -- for every request that changes something,
		// including the calls into the app.
		if !safeMethod(r.Method) && !g.sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		g.mux.ServeHTTP(w, r)
	})
}

func (g *Gateway) routes() error {
	static, err := fs.Sub(webFS, "web/static")
	if err != nil {
		return err
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /-/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	m.HandleFunc("GET /-/readyz", g.readyz)
	m.Handle("GET /-/static/", http.StripPrefix("/-/static/", http.FileServerFS(static)))

	m.HandleFunc("GET /-/setup", g.setupPage)
	m.HandleFunc("POST /-/setup", g.setupSubmit)
	m.HandleFunc("GET /-/login", g.loginPage)
	m.HandleFunc("POST /-/login", g.loginSubmit)
	m.HandleFunc("/-/logout", g.logout)
	m.HandleFunc("GET /-/oauth/{provider}/start", g.oauthStart)
	m.HandleFunc("GET /-/oauth/{provider}/callback", g.oauthCallback)

	m.HandleFunc("GET /-/account", g.signedIn(g.accountPage))
	m.HandleFunc("POST /-/account/password", g.signedIn(g.accountPassword))

	m.HandleFunc("GET /-/admin", g.adminOnly(func(w http.ResponseWriter, r *http.Request, _ visit) {
		http.Redirect(w, r, "/-/admin/users", http.StatusSeeOther)
	}))
	m.HandleFunc("GET /-/admin/users", g.adminOnly(g.usersPage))
	m.HandleFunc("POST /-/admin/users", g.adminOnly(g.usersCreate))
	m.HandleFunc("POST /-/admin/users/{id}/{action}", g.adminOnly(g.usersAction))
	m.HandleFunc("GET /-/admin/providers", g.adminOnly(g.providersPage))
	m.HandleFunc("POST /-/admin/providers", g.adminOnly(g.providersSave))
	m.HandleFunc("POST /-/admin/providers/{id}/{action}", g.adminOnly(g.providersAction))
	m.HandleFunc("GET /-/admin/clusters", g.adminOnly(g.clustersPage))
	m.HandleFunc("POST /-/admin/clusters", g.adminOnly(g.clustersUpload))
	m.HandleFunc("POST /-/admin/clusters/{name}/delete", g.adminOnly(g.clustersDelete))
	m.HandleFunc("/-/", http.NotFound)

	m.HandleFunc("GET /wails/events", g.events)
	m.HandleFunc("/", g.app)
	g.mux = m
	return nil
}

// ---- the app ---------------------------------------------------------------

type forwardedUser struct{}

// app passes a signed-in request through to the app, and sends anyone else to
// sign in -- except for a plugin view's own files, which are passed through for
// nobody in particular.
func (g *Gateway) app(w http.ResponseWriter, r *http.Request) {
	u, _, ok := g.current(r)
	if !ok {
		if pluginView(r) {
			g.proxy.ServeHTTP(w, r)
			return
		}
		g.unauthenticated(w, r)
		return
	}
	g.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), forwardedUser{}, u.ID)))
}

// pluginView reports whether a request reads a file of a plugin's own view.
//
// Those need no session. The views are drawn in frames sandboxed without
// allow-same-origin, and the browser treats what such a frame asks for as
// cross-site: the SameSite session cookie is left off the requests for its own
// stylesheets, scripts and images, and a view that cannot load them is a bare
// page that never gets past "Looking for…". What is served there is the
// plugins' static files and nothing else; whatever a view shows of a cluster it
// asks the frame around it for, and that goes through the app, signed in. See
// plugins.Middleware.
func pluginView(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		strings.HasPrefix(r.URL.Path, plugins.UIPath)
}

// rewrite points a request at the app and says who it is for. Whatever the
// browser sent in the gateway's own headers is thrown away first: only the
// gateway may say who someone is.
func (g *Gateway) rewrite(pr *httputil.ProxyRequest) {
	if up := g.upstream.Load(); up != nil {
		pr.SetURL(up)
	}
	// Wails checks a WebSocket's Origin against Host, and the Origin is the
	// address the browser used.
	pr.Out.Host = pr.In.Host
	pr.Out.Header.Del(headerSecret)
	pr.Out.Header.Del(headerUser)
	stripCookies(pr.Out, sessionCookie, oauthCookie)
	// Everything proxied came through the gateway; only what a user asked for
	// names one.
	pr.Out.Header.Set(headerSecret, g.secret)
	if id, ok := pr.In.Context().Value(forwardedUser{}).(string); ok {
		pr.Out.Header.Set(headerUser, id)
	}
}

// Identity is the app-side half of the gateway: middleware for the app's asset
// server that refuses any request the gateway did not send, and puts the user
// the gateway named into the request's context -- from where Wails hands it to
// every service call.
//
// The user is looked up afresh on every request, so an administrator who is
// demoted or switched off stops being one at once, not at their next sign-in.
func (g *Gateway) Identity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(headerSecret)), []byte(g.secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := r.Header.Get(headerUser)
		if id == "" && pluginView(r) {
			// Nobody in particular, and nothing but a plugin view's files:
			// the plugin middleware answers every such request itself.
			next.ServeHTTP(w, r)
			return
		}
		u, ok := g.store.user(id)
		if !ok || u.Disabled {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := session.WithUser(r.Context(), u.sessionUser())
		ctx = session.WithClient(ctx, r.Header.Get("x-wails-client-id"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (g *Gateway) readyz(w http.ResponseWriter, r *http.Request) {
	up := g.upstream.Load()
	if up == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, up.String()+"/health", nil)
	if err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	resp, err := g.probe.Do(req) // #nosec G704 -- the app's own loopback address
	if err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok\n"))
}

// ---- who is asking ---------------------------------------------------------

// visit is a signed-in request.
type visit struct {
	user User
	sess storedSession
}

// current resolves the session cookie.
func (g *Gateway) current(r *http.Request) (User, storedSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return User{}, storedSession{}, false
	}
	sess, u, ok := g.store.sessionFor(c.Value)
	return u, sess, ok
}

// unauthenticated sends a page load to sign in -- or, on a fresh deployment,
// to create the administrator -- and refuses anything else.
func (g *Gateway) unauthenticated(w http.ResponseWriter, r *http.Request) {
	if !navigation(r) {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	if g.store.userCount() == 0 {
		http.Redirect(w, r, "/-/setup", http.StatusSeeOther)
		return
	}
	target := "/-/login"
	if next := r.URL.RequestURI(); next != "/" {
		target += "?next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

type visitHandler func(http.ResponseWriter, *http.Request, visit)

// signedIn admits only a signed-in user, and a form only with the session's
// CSRF token.
func (g *Gateway) signedIn(h visitHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, sess, ok := g.current(r)
		if !ok {
			http.Redirect(w, r, "/-/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if !g.parseForm(w, r) {
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(sess.CSRF)) != 1 {
				http.Error(w, "this form has expired; go back, reload the page and try again", http.StatusForbidden)
				return
			}
		}
		h(w, r, visit{user: u, sess: sess})
	}
}

// adminOnly admits only an administrator.
func (g *Gateway) adminOnly(h visitHandler) http.HandlerFunc {
	return g.signedIn(func(w http.ResponseWriter, r *http.Request, v visit) {
		if !v.user.Admin() {
			g.render(w, http.StatusForbidden, "error", view{
				Title: "Administrators only",
				User:  &v.user,
				Error: "This page is for administrators.",
			})
			return
		}
		h(w, r, v)
	})
}

func (g *Gateway) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(maxFormBytes) // #nosec G120 -- the body is bounded by MaxBytesReader above
	} else {
		err = r.ParseForm() // #nosec G120 -- the body is bounded by MaxBytesReader above
	}
	if err != nil {
		http.Error(w, "that form could not be read", http.StatusBadRequest)
		return false
	}
	return true
}

// ---- where the request came from --------------------------------------------

// externalHost is the host the browser used.
func (g *Gateway) externalHost(r *http.Request) string {
	if g.cfg.TrustForwarded {
		if fwd := firstValue(r.Header.Get("X-Forwarded-Host")); fwd != "" {
			return fwd
		}
	}
	return r.Host
}

// secure reports whether the browser reached the gateway over https, which
// decides whether cookies are marked Secure.
func (g *Gateway) secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if g.cfg.TrustForwarded {
		if proto := firstValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			return strings.EqualFold(proto, "https")
		}
	}
	// Behind an ingress that says nothing, a request for the public host came
	// the public way.
	pub := g.cfg.PublicURL
	return pub != nil && pub.Scheme == "https" && strings.EqualFold(r.Host, pub.Host)
}

// baseURL is the address of the gateway as the browser sees it.
func (g *Gateway) baseURL(r *http.Request) string {
	if g.cfg.PublicURL != nil {
		return g.cfg.PublicURL.String()
	}
	scheme := "http"
	if g.secure(r) {
		scheme = "https"
	}
	return scheme + "://" + g.externalHost(r)
}

func (g *Gateway) callbackURL(r *http.Request, provider string) string {
	return g.baseURL(r) + "/-/oauth/" + provider + "/callback"
}

// sameOrigin reports whether a request came from one of the gateway's own
// pages. A request that says nothing about where it came from -- not a
// browser -- carries no cookie a browser could have been tricked into sending.
func (g *Gateway) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(g.ownHosts(r), func(h string) bool { return strings.EqualFold(h, u.Host) })
}

// ownHosts are the hosts a page of the gateway may be served from: the one
// asked for, the one an ingress forwarded, and the public one.
func (g *Gateway) ownHosts(r *http.Request) []string {
	hosts := []string{r.Host}
	if g.cfg.TrustForwarded {
		if fwd := firstValue(r.Header.Get("X-Forwarded-Host")); fwd != "" {
			hosts = append(hosts, fwd)
		}
	}
	if g.cfg.PublicURL != nil {
		hosts = append(hosts, g.cfg.PublicURL.Host)
	}
	return hosts
}

// clientIP is the address failed sign-ins are counted against.
func (g *Gateway) clientIP(r *http.Request) string {
	if g.cfg.TrustForwarded {
		if fwd := firstValue(r.Header.Get("X-Forwarded-For")); fwd != "" {
			return fwd
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- helpers ---------------------------------------------------------------

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// navigation reports whether a request is a page load, as opposed to a call a
// page makes -- the one gets a redirect to sign in, the other a 401.
func navigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// safeNext keeps a post-sign-in redirect on this site.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/"
	}
	return next
}

func firstValue(header string) string {
	first, _, _ := strings.Cut(header, ",")
	return strings.TrimSpace(first)
}

func stripCookies(r *http.Request, names ...string) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, c := range cookies {
		if !slices.Contains(names, c.Name) {
			r.AddCookie(c)
		}
	}
}

// sentence turns an error into something to show on a page.
func sentence(err error) string {
	msg := err.Error()
	if msg == "" {
		return ""
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "?") && !strings.HasSuffix(msg, "!") {
		msg += "."
	}
	return msg
}

// ---- OAuth sign-ins in flight -------------------------------------------------

type pendingLogin struct {
	provider string
	verifier string
	next     string
	expires  time.Time
}

// pendingLogins are sign-ins sent to a provider and not yet back, by state.
type pendingLogins struct {
	mu sync.Mutex
	m  map[string]pendingLogin
}

const (
	pendingTTL = 10 * time.Minute
	maxPending = 10_000
)

func (p *pendingLogins) put(state string, pl pendingLogin) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.m) >= maxPending {
		p.purgeLocked()
		if len(p.m) >= maxPending {
			return errors.New("too many sign-ins are in progress; try again in a few minutes")
		}
	}
	p.m[state] = pl
	return nil
}

func (p *pendingLogins) take(state string) (pendingLogin, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pl, ok := p.m[state]
	delete(p.m, state)
	if !ok || time.Now().After(pl.expires) {
		return pendingLogin{}, false
	}
	return pl, true
}

func (p *pendingLogins) purge() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.purgeLocked()
}

func (p *pendingLogins) purgeLocked() {
	now := time.Now()
	for state, pl := range p.m {
		if now.After(pl.expires) {
			delete(p.m, state)
		}
	}
}
