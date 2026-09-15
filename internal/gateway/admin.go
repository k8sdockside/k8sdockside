package gateway

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// ---- your account -------------------------------------------------------------

func (g *Gateway) accountPage(w http.ResponseWriter, r *http.Request, v visit) {
	g.renderAccount(w, r, v, http.StatusOK, "")
}

func (g *Gateway) renderAccount(w http.ResponseWriter, r *http.Request, v visit, status int, errMsg string) {
	g.render(w, status, "account", view{
		Title:         "Your account",
		Page:          "account",
		User:          &v.user,
		CSRF:          v.sess.CSRF,
		Notice:        noticeFor(r),
		Error:         errMsg,
		PasswordLogin: g.cfg.PasswordLogin,
		SignIn:        g.signInMethods(v.user),
	})
}

func (g *Gateway) accountPassword(w http.ResponseWriter, r *http.Request, v visit) {
	if !g.cfg.PasswordLogin {
		g.renderAccount(w, r, v, http.StatusForbidden, "Signing in with a password is switched off here.")
		return
	}
	if v.user.HasPassword() && !checkPassword(v.user.PasswordHash, r.PostFormValue("current")) {
		g.renderAccount(w, r, v, http.StatusBadRequest, "Your current password is not right.")
		return
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("confirm") {
		g.renderAccount(w, r, v, http.StatusBadRequest, "The two passwords are not the same.")
		return
	}
	if err := setPassword(g.store, v.user.ID, password); err != nil {
		g.renderAccount(w, r, v, http.StatusBadRequest, sentence(err))
		return
	}
	http.Redirect(w, r, "/-/account?done=password-changed", http.StatusSeeOther)
}

func setPassword(st *store, id, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = st.updateUser(id, func(u *User) error {
		u.PasswordHash = hash
		return nil
	})
	return err
}

// signInMethods names the ways a user can sign in.
func (g *Gateway) signInMethods(u User) []string {
	var out []string
	if u.HasPassword() {
		out = append(out, "Password")
	}
	names := map[string]string{}
	for _, p := range g.allProviders() {
		names[p.ID] = p.Name
	}
	for _, id := range u.Identities {
		name := names[id.Provider]
		if name == "" {
			name = id.Provider
		}
		out = append(out, name)
	}
	return out
}

// ---- users -------------------------------------------------------------------

// userRow is one line of the users table.
type userRow struct {
	User
	SignIn    []string
	LastLogin string
	Self      bool
}

func (g *Gateway) usersPage(w http.ResponseWriter, r *http.Request, v visit) {
	g.renderUsers(w, r, v, http.StatusOK, nil, "")
}

func (g *Gateway) renderUsers(w http.ResponseWriter, r *http.Request, v visit, status int, form map[string]string, errMsg string) {
	var rows []userRow
	for _, u := range g.store.users() {
		last := "Never"
		if !u.LastLoginAt.IsZero() {
			last = u.LastLoginAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		rows = append(rows, userRow{User: u, SignIn: g.signInMethods(u), LastLogin: last, Self: u.ID == v.user.ID})
	}
	g.render(w, status, "users", view{
		Title:  "Users",
		Page:   "users",
		User:   &v.user,
		CSRF:   v.sess.CSRF,
		Notice: noticeFor(r),
		Error:  errMsg,
		Form:   form,
		Users:  rows,
	})
}

func (g *Gateway) usersCreate(w http.ResponseWriter, r *http.Request, v visit) {
	form := formValues(r, "username", "name", "email", "role")
	fail := func(msg string) { g.renderUsers(w, r, v, http.StatusBadRequest, form, msg) }

	email, err := normalizeEmail(form["email"])
	if err != nil {
		fail(sentence(err))
		return
	}
	u := User{Username: form["username"], Name: form["name"], Email: email, Role: form["role"]}
	if password := r.PostFormValue("password"); password != "" {
		if err := validatePassword(password); err != nil {
			fail(sentence(err))
			return
		}
		if u.PasswordHash, err = hashPassword(password); err != nil {
			g.serverError(w, err)
			return
		}
	} else if email == "" {
		fail("Give the user a password, or an email address a sign-in provider can recognise them by.")
		return
	}
	if _, err := g.store.createUser(u, false); err != nil {
		fail(sentence(err))
		return
	}
	http.Redirect(w, r, "/-/admin/users?done=user-created", http.StatusSeeOther)
}

func (g *Gateway) usersAction(w http.ResponseWriter, r *http.Request, v visit) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if _, ok := g.store.user(id); !ok {
		http.NotFound(w, r)
		return
	}
	self := id == v.user.ID
	done := "user-updated"
	var err error
	switch action {
	case "role-admin", "role-user":
		_, err = g.store.updateUser(id, func(u *User) error {
			u.Role = strings.TrimPrefix(action, "role-")
			return nil
		})
	case "disable":
		if self {
			err = errors.New("you cannot switch off your own account")
			break
		}
		if _, err = g.store.updateUser(id, func(u *User) error { u.Disabled = true; return nil }); err == nil {
			g.deps.Owners.AbandonUser(id)
		}
	case "enable":
		_, err = g.store.updateUser(id, func(u *User) error { u.Disabled = false; return nil })
	case "password":
		err = setPassword(g.store, id, r.PostFormValue("password"))
	case "delete":
		if self {
			err = errors.New("you cannot remove your own account")
			break
		}
		if err = g.store.deleteUser(id); err == nil {
			g.deps.Owners.AbandonUser(id)
			done = "user-deleted"
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		g.renderUsers(w, r, v, http.StatusBadRequest, nil, sentence(err))
		return
	}
	// Someone who has just stopped being an administrator has no business on
	// the admin pages, including the one they were on.
	if self && action == "role-user" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/-/admin/users?done="+done, http.StatusSeeOther)
}

// ---- providers -----------------------------------------------------------------

func (g *Gateway) providersPage(w http.ResponseWriter, r *http.Request, v visit) {
	var form map[string]string
	editing := false
	if id := r.URL.Query().Get("edit"); id != "" {
		if p, ok := g.store.provider(id); ok && !g.isManaged(id) {
			form = providerToForm(p)
			editing = true
		}
	}
	g.renderProviders(w, r, v, http.StatusOK, form, editing, "")
}

func (g *Gateway) renderProviders(w http.ResponseWriter, r *http.Request, v visit, status int, form map[string]string, editing bool, errMsg string) {
	g.render(w, status, "providers", view{
		Title:         "Sign-in providers",
		Page:          "providers",
		User:          &v.user,
		CSRF:          v.sess.CSRF,
		Notice:        noticeFor(r),
		Error:         errMsg,
		Form:          form,
		Editing:       editing,
		Providers:     g.allProviders(),
		ProviderTypes: providerTypes,
		CallbackBase:  g.baseURL(r) + "/-/oauth/",
	})
}

func (g *Gateway) providersSave(w http.ResponseWriter, r *http.Request, v visit) {
	form := formValues(r, "original", "id", "type", "name", "clientId", "issuer", "tenant", "scopes", "allowedDomains", "defaultRole")
	form["autoSignup"] = checkbox(r, "autoSignup")
	form["enabled"] = checkbox(r, "enabled")
	original := form["original"]
	fail := func(msg string) { g.renderProviders(w, r, v, http.StatusBadRequest, form, original != "", msg) }

	p := Provider{
		ID:             form["id"],
		Type:           form["type"],
		Name:           form["name"],
		ClientID:       form["clientId"],
		ClientSecret:   strings.TrimSpace(r.PostFormValue("clientSecret")),
		Issuer:         form["issuer"],
		Tenant:         form["tenant"],
		Scopes:         splitList(form["scopes"]),
		AllowedDomains: splitList(form["allowedDomains"]),
		AutoSignup:     form["autoSignup"] == "on",
		DefaultRole:    form["defaultRole"],
		Enabled:        form["enabled"] == "on",
	}
	if err := p.normalize(); err != nil {
		fail(sentence(err))
		return
	}
	if g.isManaged(p.ID) || g.isManaged(original) {
		fail("That id belongs to a provider the deployment manages; change it there.")
		return
	}
	if original != "" && p.ClientSecret == "" {
		if old, ok := g.store.provider(original); ok {
			p.ClientSecret = old.ClientSecret
		}
	}
	if p.secret() == "" {
		fail("A client secret is required.")
		return
	}
	if err := g.store.putProvider(p, original); err != nil {
		fail(sentence(err))
		return
	}
	http.Redirect(w, r, "/-/admin/providers?done=provider-saved", http.StatusSeeOther)
}

func (g *Gateway) providersAction(w http.ResponseWriter, r *http.Request, v visit) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if g.isManaged(id) {
		g.renderProviders(w, r, v, http.StatusBadRequest, nil, false, "That provider is managed by the deployment; change it there.")
		return
	}
	p, ok := g.store.provider(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var err error
	done := "provider-saved"
	switch action {
	case "enable", "disable":
		p.Enabled = action == "enable"
		err = g.store.putProvider(p, id)
	case "delete":
		err = g.store.deleteProvider(id)
		done = "provider-deleted"
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		g.renderProviders(w, r, v, http.StatusBadRequest, nil, false, sentence(err))
		return
	}
	http.Redirect(w, r, "/-/admin/providers?done="+done, http.StatusSeeOther)
}

func providerToForm(p Provider) map[string]string {
	form := map[string]string{
		"original":       p.ID,
		"id":             p.ID,
		"type":           p.Type,
		"name":           p.Name,
		"clientId":       p.ClientID,
		"issuer":         p.Issuer,
		"tenant":         p.Tenant,
		"scopes":         strings.Join(p.Scopes, " "),
		"allowedDomains": strings.Join(p.AllowedDomains, ", "),
		"defaultRole":    p.DefaultRole,
		"autoSignup":     "off",
		"enabled":        "off",
	}
	if p.AutoSignup {
		form["autoSignup"] = "on"
	}
	if p.Enabled {
		form["enabled"] = "on"
	}
	return form
}

// ---- clusters ------------------------------------------------------------------

func (g *Gateway) clustersPage(w http.ResponseWriter, r *http.Request, v visit) {
	g.renderClusters(w, r, v, http.StatusOK, nil, "")
}

func (g *Gateway) renderClusters(w http.ResponseWriter, r *http.Request, v visit, status int, form map[string]string, errMsg string) {
	g.render(w, status, "clusters", view{
		Title:    "Clusters",
		Page:     "clusters",
		User:     &v.user,
		CSRF:     v.sess.CSRF,
		Notice:   noticeFor(r),
		Error:    errMsg,
		Form:     form,
		Clusters: g.clusterGroups(),
	})
}

func (g *Gateway) clustersUpload(w http.ResponseWriter, r *http.Request, v visit) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	var content []byte
	if f, header, err := r.FormFile("file"); err == nil {
		content, err = io.ReadAll(io.LimitReader(f, maxKubeconfigBytes+1))
		_ = f.Close()
		if err != nil {
			g.renderClusters(w, r, v, http.StatusBadRequest, map[string]string{"name": name}, "That file could not be read.")
			return
		}
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
		}
	}
	if len(content) == 0 {
		content = []byte(r.PostFormValue("content"))
	}
	if _, err := g.addKubeconfig(name, content, checkbox(r, "replace") == "on"); err != nil {
		g.renderClusters(w, r, v, http.StatusBadRequest, map[string]string{"name": name}, sentence(err))
		return
	}
	g.log.Info("a kubeconfig was added", "name", name, "by", v.user.Username)
	http.Redirect(w, r, "/-/admin/clusters?done=cluster-added", http.StatusSeeOther)
}

func (g *Gateway) clustersDelete(w http.ResponseWriter, r *http.Request, v visit) {
	name := r.PathValue("name")
	if err := g.removeKubeconfig(name); err != nil {
		g.renderClusters(w, r, v, http.StatusBadRequest, nil, sentence(err))
		return
	}
	g.log.Info("a kubeconfig was removed", "name", name, "by", v.user.Username)
	http.Redirect(w, r, "/-/admin/clusters?done=cluster-deleted", http.StatusSeeOther)
}

// ---- forms ---------------------------------------------------------------------

func formValues(r *http.Request, names ...string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = strings.TrimSpace(r.PostFormValue(name))
	}
	return out
}

// checkbox reads a checkbox as "on" or "off", since an unticked one is not
// sent at all.
func checkbox(r *http.Request, name string) string {
	if r.PostFormValue(name) != "" {
		return "on"
	}
	return "off"
}

// splitList splits on commas and whitespace.
func splitList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r' })
}

// notices are the messages a redirect after a change can ask for, by name --
// a name rather than the text, so a link cannot put words on the page.
var notices = map[string]string{
	"user-created":     "The user was added.",
	"user-updated":     "The user was updated.",
	"user-deleted":     "The user was removed.",
	"password-changed": "Your password was changed.",
	"provider-saved":   "The sign-in provider was saved.",
	"provider-deleted": "The sign-in provider was removed.",
	"cluster-added":    "The kubeconfig was added; its contexts are in the app's sidebar now.",
	"cluster-deleted":  "The kubeconfig was removed.",
	"signed-out":       "You are signed out.",
}

func noticeFor(r *http.Request) string {
	return notices[r.URL.Query().Get("done")]
}
