package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// providerType is a kind of sign-in provider the app knows how to talk to.
type providerType struct {
	ID    string
	Label string
}

var providerTypes = []providerType{
	{"github", "GitHub"},
	{"google", "Google"},
	{"facebook", "Facebook"},
	{"gitlab", "GitLab"},
	{"microsoft", "Microsoft"},
	{"oidc", "OpenID Connect"},
}

func typeLabel(id string) string {
	for _, t := range providerTypes {
		if t.ID == id {
			return t.Label
		}
	}
	return id
}

// Provider is one way of signing in with an account someone already has.
type Provider struct {
	// ID names the provider in its callback address, /-/oauth/<id>/callback.
	ID   string `json:"id"`
	Type string `json:"type"`
	// Name is the label on the sign-in button.
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
	// ClientSecret is the secret itself; ClientSecretEnv names an environment
	// variable holding it instead, so a deployment can keep it in a Secret.
	ClientSecret    string `json:"clientSecret,omitempty"`
	ClientSecretEnv string `json:"clientSecretEnv,omitempty"`
	// Issuer is an OpenID Connect provider's issuer, or a GitLab instance.
	Issuer string `json:"issuer,omitempty"`
	// Tenant is a Microsoft directory; "common" admits any Microsoft account.
	Tenant string   `json:"tenant,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
	// AllowedDomains restricts who AutoSignup admits, by verified email.
	AllowedDomains []string `json:"allowedDomains,omitempty"`
	// AutoSignup creates an account for anyone the provider signs in, rather
	// than only admitting people an administrator has added.
	AutoSignup  bool   `json:"autoSignup,omitempty"`
	DefaultRole string `json:"defaultRole,omitempty"`
	Enabled     bool   `json:"enabled"`
	// Managed marks a provider from the deployment's providers file, which
	// the admin page shows but cannot change.
	Managed bool `json:"-"`
}

var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// normalize tidies what was typed or read and says what is wrong with it.
func (p *Provider) normalize() error {
	p.ID = strings.ToLower(strings.TrimSpace(p.ID))
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.Name = strings.TrimSpace(p.Name)
	p.ClientID = strings.TrimSpace(p.ClientID)
	p.ClientSecretEnv = strings.TrimSpace(p.ClientSecretEnv)
	p.Issuer = strings.TrimRight(strings.TrimSpace(p.Issuer), "/")
	p.Tenant = strings.TrimSpace(p.Tenant)

	if !providerIDPattern.MatchString(p.ID) {
		return errors.New("the id is lowercase letters, digits and dashes, at most 32 characters")
	}
	if !slices.ContainsFunc(providerTypes, func(t providerType) bool { return t.ID == p.Type }) {
		return fmt.Errorf("%q is not a kind of provider this app knows", p.Type)
	}
	if p.Name == "" {
		p.Name = typeLabel(p.Type)
	}
	if p.ClientID == "" {
		return errors.New("a client ID is required")
	}
	switch p.DefaultRole {
	case "":
		p.DefaultRole = roleUser
	case roleUser, roleAdmin:
	default:
		return fmt.Errorf("%q is not a role; it is user or admin", p.DefaultRole)
	}
	if p.Type == "oidc" && p.Issuer == "" {
		return errors.New("an OpenID Connect provider needs its issuer URL")
	}
	if p.Issuer != "" {
		if err := checkIssuer(p.Issuer); err != nil {
			return err
		}
	}

	var domains []string
	for _, d := range p.AllowedDomains {
		if d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@")); d != "" {
			domains = append(domains, d)
		}
	}
	p.AllowedDomains = domains
	var scopes []string
	for _, s := range p.Scopes {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	p.Scopes = scopes
	return nil
}

// checkIssuer insists on https: the issuer hands out the addresses that
// tokens and credentials are sent to. Plain http is let through for a
// provider on this machine, which is what a test or a local Dex is.
func checkIssuer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an address", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		if ip := net.ParseIP(u.Hostname()); (ip != nil && ip.IsLoopback()) || u.Hostname() == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("the issuer %q must be an https address", raw)
}

// secret is the client secret, wherever it is kept.
func (p Provider) secret() string {
	if p.ClientSecret != "" {
		return p.ClientSecret
	}
	if p.ClientSecretEnv != "" {
		return os.Getenv(p.ClientSecretEnv)
	}
	return ""
}

// issuer is where an OpenID Connect provider publishes its endpoints.
func (p Provider) issuer() string {
	switch p.Type {
	case "google":
		return "https://accounts.google.com"
	case "gitlab":
		if p.Issuer != "" {
			return p.Issuer
		}
		return "https://gitlab.com"
	case "microsoft":
		tenant := p.Tenant
		if tenant == "" {
			tenant = "common"
		}
		return "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/v2.0"
	default:
		return p.Issuer
	}
}

func (p Provider) scopes() []string {
	if len(p.Scopes) > 0 {
		return p.Scopes
	}
	switch p.Type {
	case "github":
		return []string{"read:user", "user:email"}
	case "facebook":
		return []string{"email", "public_profile"}
	default:
		return []string{"openid", "email", "profile"}
	}
}

// allowsDomain reports whether an email address is in a domain the provider
// admits.
func (p Provider) allowsDomain(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	return slices.Contains(p.AllowedDomains, strings.ToLower(email[at+1:]))
}

// The fixed endpoints of the providers that are not OpenID Connect. Variables
// so a test can stand in for them.
var (
	githubAuthURL    = "https://github.com/login/oauth/authorize"
	githubTokenURL   = "https://github.com/login/oauth/access_token" // #nosec G101 -- an endpoint, not a credential
	githubAPI        = "https://api.github.com"
	facebookAuthURL  = "https://www.facebook.com/dialog/oauth"
	facebookTokenURL = "https://graph.facebook.com/oauth/access_token" // #nosec G101 -- an endpoint, not a credential
	facebookGraph    = "https://graph.facebook.com"
)

// externalIdentity is who a provider says someone is.
type externalIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Username      string
}

// oauthClient talks to sign-in providers.
type oauthClient struct {
	http *http.Client

	mu         sync.Mutex
	discovered map[string]discovery
}

type discovery struct {
	AuthURL     string `json:"authorization_endpoint"`
	TokenURL    string `json:"token_endpoint"`
	UserinfoURL string `json:"userinfo_endpoint"`
	fetched     time.Time
}

// discoveryTTL is how long a provider's published endpoints are trusted
// before they are fetched again.
const discoveryTTL = time.Hour

func newOAuthClient() *oauthClient {
	return &oauthClient{
		http:       &http.Client{Timeout: 15 * time.Second},
		discovered: map[string]discovery{},
	}
}

// ctx carries the client's HTTP client to x/oauth2, which reads it from there.
func (c *oauthClient) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.http)
}

// config builds the OAuth2 configuration for a provider, and returns the
// address its user information is read from, if it has one.
func (c *oauthClient) config(ctx context.Context, p Provider, redirect string) (*oauth2.Config, string, error) {
	cfg := &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.secret(),
		RedirectURL:  redirect,
		Scopes:       p.scopes(),
	}
	switch p.Type {
	case "github":
		cfg.Endpoint = oauth2.Endpoint{AuthURL: githubAuthURL, TokenURL: githubTokenURL}
		return cfg, "", nil
	case "facebook":
		cfg.Endpoint = oauth2.Endpoint{AuthURL: facebookAuthURL, TokenURL: facebookTokenURL}
		return cfg, "", nil
	}
	d, err := c.discover(ctx, p.issuer())
	if err != nil {
		return nil, "", err
	}
	cfg.Endpoint = oauth2.Endpoint{AuthURL: d.AuthURL, TokenURL: d.TokenURL}
	return cfg, d.UserinfoURL, nil
}

// discover reads an OpenID Connect provider's published endpoints.
func (c *oauthClient) discover(ctx context.Context, issuer string) (discovery, error) {
	c.mu.Lock()
	cached, ok := c.discovered[issuer]
	c.mu.Unlock()
	if ok && time.Since(cached.fetched) < discoveryTTL {
		return cached, nil
	}

	var d discovery
	if err := c.getJSON(ctx, c.http, issuer+"/.well-known/openid-configuration", &d); err != nil {
		return discovery{}, fmt.Errorf("reading %s's configuration: %w", issuer, err)
	}
	if d.AuthURL == "" || d.TokenURL == "" || d.UserinfoURL == "" {
		return discovery{}, fmt.Errorf("%s does not publish the endpoints signing in needs", issuer)
	}
	d.fetched = time.Now()
	c.mu.Lock()
	c.discovered[issuer] = d
	c.mu.Unlock()
	return d, nil
}

// identity asks the provider who a token belongs to.
func (c *oauthClient) identity(ctx context.Context, p Provider, cfg *oauth2.Config, userinfo string, tok *oauth2.Token) (externalIdentity, error) {
	client := cfg.Client(c.ctx(ctx), tok)
	switch p.Type {
	case "github":
		return c.github(ctx, client)
	case "facebook":
		return c.facebook(ctx, client)
	default:
		return c.oidc(ctx, client, userinfo)
	}
}

func (c *oauthClient) github(ctx context.Context, client *http.Client) (externalIdentity, error) {
	var user struct {
		ID    json.Number `json:"id"`
		Login string      `json:"login"`
		Name  string      `json:"name"`
	}
	if err := c.getJSON(ctx, client, githubAPI+"/user", &user); err != nil {
		return externalIdentity{}, err
	}
	id := externalIdentity{Subject: user.ID.String(), Username: user.Login, Name: user.Name}

	// The address on the profile is whatever the user chose to show, and may
	// be unverified; the list of addresses says which one is primary and
	// verified. Without the user:email scope the list is refused, and the
	// sign-in carries no address the app can trust.
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := c.getJSON(ctx, client, githubAPI+"/user/emails", &emails); err == nil {
		for _, e := range emails {
			if e.Primary && e.Verified {
				id.Email, id.EmailVerified = e.Email, true
			}
		}
	}
	return id, nil
}

func (c *oauthClient) facebook(ctx context.Context, client *http.Client) (externalIdentity, error) {
	var me struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := c.getJSON(ctx, client, facebookGraph+"/me?fields=id,name,email", &me); err != nil {
		return externalIdentity{}, err
	}
	// Facebook gives out only an address its user has confirmed, and none at
	// all otherwise.
	return externalIdentity{Subject: me.ID, Name: me.Name, Email: me.Email, EmailVerified: me.Email != ""}, nil
}

func (c *oauthClient) oidc(ctx context.Context, client *http.Client, userinfo string) (externalIdentity, error) {
	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     any    `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Nickname          string `json:"nickname"`
	}
	if err := c.getJSON(ctx, client, userinfo, &claims); err != nil {
		return externalIdentity{}, err
	}
	// Some providers send the flag as a string. A provider that does not send
	// it at all -- Microsoft -- does not vouch for the address, and the
	// address is then not trusted to match anyone.
	verified := false
	switch v := claims.EmailVerified.(type) {
	case bool:
		verified = v
	case string:
		verified = strings.EqualFold(v, "true")
	}
	username := claims.PreferredUsername
	if username == "" {
		username = claims.Nickname
	}
	return externalIdentity{
		Subject:       claims.Sub,
		Email:         claims.Email,
		EmailVerified: verified,
		Name:          claims.Name,
		Username:      username,
	}, nil
}

// getJSON fetches a JSON document, reading at most a megabyte of it.
func (c *oauthClient) getJSON(ctx context.Context, client *http.Client, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil) // #nosec G704 -- addresses come from provider configuration set by an administrator
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "k8sdockside")
	resp, err := client.Do(req) // #nosec G107 G704 -- addresses come from provider configuration set by an administrator
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return json.Unmarshal(body, out)
}

// fileProvider is a provider as the deployment's providers file writes it, in
// which enabled defaults to true.
type fileProvider struct {
	Provider
	Enabled *bool `json:"enabled"`
}

// loadManagedProviders reads the deployment's providers file.
func loadManagedProviders(path string) ([]Provider, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is the deployment's own configuration
	if err != nil {
		return nil, fmt.Errorf("reading the providers file: %w", err)
	}
	var listed []fileProvider
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, fmt.Errorf("reading the providers file %s: %w", path, err)
	}
	var out []Provider
	seen := map[string]bool{}
	for i, fp := range listed {
		p := fp.Provider
		p.Enabled = fp.Enabled == nil || *fp.Enabled
		p.Managed = true
		if err := p.normalize(); err != nil {
			return nil, fmt.Errorf("provider %d in %s: %w", i+1, path, err)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("the providers file %s lists %q twice", path, p.ID)
		}
		seen[p.ID] = true
		if p.secret() == "" {
			return nil, fmt.Errorf("provider %q in %s has no client secret: set clientSecret, or clientSecretEnv to a variable that holds it", p.ID, path)
		}
		out = append(out, p)
	}
	return out, nil
}
