package gateway

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

var errSwitchedOff = errors.New("this account has been switched off; ask an administrator")

// ---- first run -------------------------------------------------------------

func (g *Gateway) setupPage(w http.ResponseWriter, r *http.Request) {
	if g.store.userCount() > 0 {
		http.Redirect(w, r, "/-/login", http.StatusSeeOther)
		return
	}
	g.render(w, http.StatusOK, "setup", g.setupView())
}

func (g *Gateway) setupView() view {
	return view{
		Title:         "Set up K8s Dockside",
		SetupToken:    g.cfg.SetupToken != "",
		TokenInLog:    g.generatedToken,
		PasswordLogin: g.cfg.PasswordLogin,
	}
}

// setupSubmit creates the first administrator, which is only possible while
// there is nobody at all.
func (g *Gateway) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if !g.parseForm(w, r) {
		return
	}
	if g.store.userCount() > 0 {
		http.Redirect(w, r, "/-/login", http.StatusSeeOther)
		return
	}
	v := g.setupView()
	v.Form = formValues(r, "username", "name", "email")
	fail := func(status int, msg string) {
		v.Error = msg
		g.render(w, status, "setup", v)
	}

	ip := g.clientIP(r)
	if g.cfg.SetupToken != "" {
		if g.ipLimit.blocked(ip) {
			fail(http.StatusTooManyRequests, "Too many wrong tokens. Wait a few minutes and try again.")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue("token")), []byte(g.cfg.SetupToken)) != 1 {
			g.ipLimit.fail(ip)
			fail(http.StatusForbidden, "That is not the setup token this deployment was given.")
			return
		}
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("confirm") {
		fail(http.StatusBadRequest, "The two passwords are not the same.")
		return
	}
	if err := validatePassword(password); err != nil {
		fail(http.StatusBadRequest, sentence(err))
		return
	}
	email, err := normalizeEmail(v.Form["email"])
	if err != nil {
		fail(http.StatusBadRequest, sentence(err))
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		g.serverError(w, err)
		return
	}
	u, err := g.store.createUser(User{
		Username:     v.Form["username"],
		Name:         v.Form["name"],
		Email:        email,
		Role:         roleAdmin,
		PasswordHash: hash,
	}, true)
	if errors.Is(err, errAlreadySetUp) {
		http.Redirect(w, r, "/-/login", http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(http.StatusBadRequest, sentence(err))
		return
	}
	g.log.Info("created the first administrator", "username", u.Username)
	g.startSession(w, r, u, "/")
}

// ---- password sign-in --------------------------------------------------------

func (g *Gateway) loginPage(w http.ResponseWriter, r *http.Request) {
	if g.store.userCount() == 0 {
		http.Redirect(w, r, "/-/setup", http.StatusSeeOther)
		return
	}
	next := safeNext(r.URL.Query().Get("next"))
	if _, _, ok := g.current(r); ok {
		http.Redirect(w, r, next, http.StatusSeeOther) // #nosec G710 -- next has been through safeNext, which keeps it on this site
		return
	}
	v := g.loginView(next)
	v.Notice = noticeFor(r)
	g.render(w, http.StatusOK, "login", v)
}

func (g *Gateway) loginView(next string) view {
	return view{
		Title:         "Sign in",
		Next:          next,
		PasswordLogin: g.cfg.PasswordLogin,
		Providers:     g.enabledProviders(),
	}
}

func (g *Gateway) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !g.parseForm(w, r) {
		return
	}
	next := safeNext(r.PostFormValue("next"))
	v := g.loginView(next)
	v.Form = formValues(r, "username")
	fail := func(status int, msg string) {
		v.Error = msg
		g.render(w, status, "login", v)
	}
	if !g.cfg.PasswordLogin {
		fail(http.StatusForbidden, "Signing in with a password is switched off here.")
		return
	}

	username := v.Form["username"]
	password := r.PostFormValue("password")
	// Failures count against the address, against the username from that
	// address, and -- far more generously -- against the username from
	// anywhere. A username is no secret, so a tight limit on the name alone
	// would let anyone lock its owner out by failing on purpose; the wide one
	// still stops guessing spread over many addresses.
	ip, name := g.clientIP(r), strings.ToLower(username)
	key := name + "\x00" + ip
	if g.ipLimit.blocked(ip) || g.userLimit.blocked(key) || g.nameLimit.blocked(name) {
		fail(http.StatusTooManyRequests, "Too many failed attempts. Wait a few minutes and try again.")
		return
	}

	u, found := g.store.userByUsername(username)
	matched := false
	if found && u.HasPassword() {
		matched = checkPassword(u.PasswordHash, password)
	} else {
		wasteTime(password)
	}
	if !matched {
		g.ipLimit.fail(ip)
		g.userLimit.fail(key)
		g.nameLimit.fail(name)
		fail(http.StatusUnauthorized, "That username and password do not match.")
		return
	}
	if u.Disabled {
		fail(http.StatusForbidden, sentence(errSwitchedOff))
		return
	}
	g.userLimit.forget(key)
	g.startSession(w, r, u, next)
}

// startSession signs a user in and sends them on.
func (g *Gateway) startSession(w http.ResponseWriter, r *http.Request, u User, next string) {
	token, sess, err := g.store.newSession(u.ID, g.cfg.SessionTTL)
	if err != nil {
		g.serverError(w, err)
		return
	}
	// Secure follows how the browser reached the gateway -- see Gateway.secure.
	// Forcing it would make a plain-http port-forward impossible to sign in on.
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is set whenever the request came over https
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   g.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther) // #nosec G710 -- every caller passes next through safeNext
}

// logout ends the session. It answers GET as well as POST so the app can link
// to it; the worst a forged sign-out does is sign someone out.
func (g *Gateway) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := g.store.endSession(c.Value); err != nil {
			g.log.Warn("could not end a session", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- clears the cookie; Secure as in startSession
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   g.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/-/login?done=signed-out", http.StatusSeeOther)
}

// ---- provider sign-in --------------------------------------------------------

// allProviders is every provider: the deployment's, then the admin page's. An
// admin-page provider with the id of one of the deployment's is left out.
func (g *Gateway) allProviders() []Provider {
	out := slices.Clone(g.managed)
	for _, p := range g.store.providers() {
		if !g.isManaged(p.ID) {
			out = append(out, p)
		}
	}
	return out
}

func (g *Gateway) enabledProviders() []Provider {
	return slices.DeleteFunc(g.allProviders(), func(p Provider) bool { return !p.Enabled })
}

func (g *Gateway) enabledProvider(id string) (Provider, bool) {
	for _, p := range g.enabledProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

func (g *Gateway) isManaged(id string) bool {
	return slices.ContainsFunc(g.managed, func(p Provider) bool { return p.ID == id })
}

// oauthStart sends the browser to the provider.
func (g *Gateway) oauthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := g.enabledProvider(r.PathValue("provider"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	next := safeNext(r.URL.Query().Get("next"))
	g.sendToProvider(w, r, p, pendingLogin{provider: p.ID, next: next}, func(status int, msg string) {
		g.loginError(w, next, status, msg)
	})
}

// accountConnect sends a signed-in user to a provider to connect an account
// there to their own. It is a form post, behind the session's CSRF token, so
// that another site cannot start it on a user's behalf.
func (g *Gateway) accountConnect(w http.ResponseWriter, r *http.Request, v visit) {
	p, ok := g.enabledProvider(r.PathValue("provider"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	g.sendToProvider(w, r, p, pendingLogin{provider: p.ID, next: "/-/account", linkUser: v.user.ID}, func(status int, msg string) {
		g.renderAccount(w, r, v, status, msg)
	})
}

// sendToProvider starts an authorisation with p for the sign-in or connection
// pl describes, and redirects the browser there.
func (g *Gateway) sendToProvider(w http.ResponseWriter, r *http.Request, p Provider, pl pendingLogin, fail func(status int, msg string)) {
	cfg, _, err := g.oauth.config(r.Context(), p, g.callbackURL(r, p.ID))
	if err != nil {
		g.log.Warn("could not reach a sign-in provider", "provider", p.ID, "err", err)
		fail(http.StatusBadGateway, fmt.Sprintf("%s cannot be reached right now. Try again, or sign in another way.", p.Name))
		return
	}

	state := randomToken(32)
	pl.expires = time.Now().Add(pendingTTL)
	var opts []oauth2.AuthCodeOption
	// PKCE ties the code to this sign-in, on top of the state. Facebook is
	// left out: it does not take part, and a verifier it does not expect is
	// not something to bet a sign-in on.
	if p.Type != "facebook" {
		pl.verifier = oauth2.GenerateVerifier()
		opts = append(opts, oauth2.S256ChallengeOption(pl.verifier))
	}
	if err := g.pending.put(state, pl); err != nil {
		fail(http.StatusServiceUnavailable, sentence(err))
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure as in startSession
		Name:     oauthCookie,
		Value:    state,
		Path:     "/-/oauth/",
		MaxAge:   int(pendingTTL.Seconds()),
		HttpOnly: true,
		Secure:   g.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, cfg.AuthCodeURL(state, opts...), http.StatusFound)
}

// oauthCallback finishes a provider sign-in.
func (g *Gateway) oauthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/-/oauth/", MaxAge: -1, HttpOnly: true, Secure: g.secure(r), SameSite: http.SameSiteLaxMode}) // #nosec G124 -- clears the cookie; Secure as in startSession

	// The state must be the one this browser was given: otherwise someone
	// could sign a victim in as themselves with a link.
	c, err := r.Cookie(oauthCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		g.loginError(w, "/", http.StatusBadRequest, "That sign-in did not start in this browser, or took too long. Start again.")
		return
	}
	pl, ok := g.pending.take(state)
	if !ok || pl.provider != r.PathValue("provider") {
		g.loginError(w, "/", http.StatusBadRequest, "That sign-in has expired. Start again.")
		return
	}
	p, ok := g.enabledProvider(pl.provider)
	if !ok {
		g.loginError(w, pl.next, http.StatusBadRequest, "That way of signing in is no longer available.")
		return
	}
	if reason := q.Get("error"); reason != "" {
		if desc := q.Get("error_description"); desc != "" {
			reason = desc
		}
		g.loginError(w, pl.next, http.StatusForbidden, fmt.Sprintf("%s did not sign you in: %s", p.Name, reason))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cfg, userinfo, err := g.oauth.config(ctx, p, g.callbackURL(r, p.ID))
	if err != nil {
		g.log.Warn("could not reach a sign-in provider", "provider", p.ID, "err", err)
		g.loginError(w, pl.next, http.StatusBadGateway, fmt.Sprintf("%s cannot be reached right now.", p.Name))
		return
	}
	var opts []oauth2.AuthCodeOption
	if pl.verifier != "" {
		opts = append(opts, oauth2.VerifierOption(pl.verifier))
	}
	tok, err := cfg.Exchange(g.oauth.ctx(ctx), q.Get("code"), opts...)
	if err != nil {
		g.log.Warn("a provider refused the sign-in code", "provider", p.ID, "err", err)
		g.loginError(w, pl.next, http.StatusBadGateway, fmt.Sprintf("%s did not accept the sign-in. Check the client ID and secret, and that the callback address is registered.", p.Name))
		return
	}
	ext, err := g.oauth.identity(ctx, p, cfg, userinfo, tok)
	if err != nil {
		g.log.Warn("could not read who signed in", "provider", p.ID, "err", err)
		g.loginError(w, pl.next, http.StatusBadGateway, fmt.Sprintf("%s did not say who you are.", p.Name))
		return
	}
	if pl.linkUser != "" {
		g.finishConnect(w, r, p, pl, ext)
		return
	}
	u, err := g.signInExternal(p, ext)
	if err != nil {
		g.loginError(w, pl.next, http.StatusForbidden, sentence(err))
		return
	}
	g.log.Info("signed in through a provider", "provider", p.ID, "username", u.Username)
	g.startSession(w, r, u, pl.next)
}

// finishConnect adds the provider account that came back to the user who
// asked for it -- who must still be the one signed in, in this browser, since
// the connection was started from their session and nowhere else.
func (g *Gateway) finishConnect(w http.ResponseWriter, r *http.Request, p Provider, pl pendingLogin, ext externalIdentity) {
	u, sess, ok := g.current(r)
	if !ok || u.ID != pl.linkUser {
		g.loginError(w, "/-/account", http.StatusForbidden, "You are no longer signed in as the account that asked to connect. Sign in, then connect from your account page.")
		return
	}
	if ext.Subject == "" {
		g.renderAccount(w, r, visit{user: u, sess: sess}, http.StatusBadGateway, fmt.Sprintf("%s did not say who you are.", p.Name))
		return
	}
	ident := Identity{Provider: p.ID, Subject: ext.Subject}
	if ext.EmailVerified {
		ident.Email = strings.ToLower(strings.TrimSpace(ext.Email))
	}
	if _, err := g.store.linkIdentity(u.ID, ident); err != nil {
		g.renderAccount(w, r, visit{user: u, sess: sess}, http.StatusConflict, sentence(err))
		return
	}
	g.log.Info("connected a provider account", "provider", p.ID, "username", u.Username)
	http.Redirect(w, r, "/-/account?done=provider-connected", http.StatusSeeOther)
}

func (g *Gateway) loginError(w http.ResponseWriter, next string, status int, msg string) {
	v := g.loginView(next)
	v.Error = msg
	g.render(w, status, "login", v)
}

// signInExternal finds, links or creates the user a provider signed in.
//
// In order: someone who has signed in with this account before; someone an
// administrator added with the address the provider vouches for; one of the
// deployment's administrator addresses; and, if the provider lets anyone join,
// a new user. Anyone else is turned away.
func (g *Gateway) signInExternal(p Provider, ext externalIdentity) (User, error) {
	if ext.Subject == "" {
		return User{}, fmt.Errorf("%s did not say who you are", p.Name)
	}
	email := strings.ToLower(strings.TrimSpace(ext.Email))
	verified := ext.EmailVerified && email != ""

	if u, ok := g.store.userByIdentity(p.ID, ext.Subject); ok {
		if u.Disabled {
			return User{}, errSwitchedOff
		}
		return u, nil
	}

	if verified {
		if u, ok := g.store.userByEmail(email); ok {
			if u.Disabled {
				return User{}, errSwitchedOff
			}
			// An address is enough to claim only an account nobody can sign
			// in to yet. One that already has a password or a provider
			// account belongs to someone, and handing it to whoever a
			// provider vouches for would make the least careful enabled
			// provider the way into every account -- the administrator's
			// included. Its owner connects more providers while signed in.
			return g.store.updateUser(u.ID, func(u *User) error {
				if !u.AwaitingFirstSignIn() {
					return fmt.Errorf("there is already an account here for %s. Sign in to it the way you usually do, then connect %s on your account page", email, p.Name)
				}
				u.Identities = append(u.Identities, Identity{Provider: p.ID, Subject: ext.Subject, Email: email})
				if u.Name == "" {
					u.Name = ext.Name
				}
				return nil
			})
		}
	}

	listedAdmin := verified && slices.Contains(g.cfg.AdminEmails, email)
	admitted := p.AutoSignup && (len(p.AllowedDomains) == 0 || (verified && p.allowsDomain(email)))
	if !listedAdmin && !admitted {
		if email != "" {
			return User{}, fmt.Errorf("no account here matches %s; ask an administrator to add you", email)
		}
		return User{}, errors.New("there is no account here for you; ask an administrator to add you")
	}

	role := p.DefaultRole
	// While nobody exists, the administrator is made on the setup page, with
	// a token -- see bootstrap -- or is one of the deployment's listed
	// administrator addresses. Whoever a provider happens to admit first is
	// neither: with autoSignup on a public provider, that is anyone at all.
	if g.store.userCount() == 0 && !listedAdmin {
		return User{}, errors.New("the administrator is created on the setup page first")
	}
	if listedAdmin {
		role = roleAdmin
	}
	local, _, _ := strings.Cut(email, "@")
	u := User{
		Username:   g.store.freeUsername(ext.Username, local, p.ID+"-user"),
		Name:       ext.Name,
		Role:       role,
		Identities: []Identity{{Provider: p.ID, Subject: ext.Subject, Email: email}},
	}
	// An address the provider did not vouch for is not kept: it is what later
	// sign-ins are matched against.
	if verified {
		u.Email = email
	}
	return g.store.createUser(u, false)
}

// normalizeEmail checks an address someone typed, allowing none.
func normalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return "", fmt.Errorf("%q is not an email address", raw)
	}
	return strings.ToLower(addr.Address), nil
}
