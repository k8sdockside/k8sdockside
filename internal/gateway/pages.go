package gateway

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"strings"
)

//go:embed web
var webFS embed.FS

// view is everything a page of the gateway may show. One struct for every page
// rather than one per page: most fields are shared, and the templates are
// easier to follow when they all read from the same shape.
type view struct {
	Title string
	// Page names the admin tab to highlight.
	Page   string
	User   *User
	CSRF   string
	Notice string
	Error  string
	// Form refills a form that came back with an error.
	Form map[string]string

	Next          string
	PasswordLogin bool
	SetupToken    bool
	Providers     []Provider
	ProviderTypes []providerType
	CallbackBase  string
	Editing       bool
	SignIn        []string
	Users         []userRow
	Clusters      []clusterGroup
}

var pageNames = []string{"login", "setup", "account", "users", "providers", "clusters", "error"}

func loadPages() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"typeLabel": typeLabel,
		"join":      strings.Join,
	}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New(name).Funcs(funcs).ParseFS(webFS, "web/layout.html", "web/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return pages, nil
}

// render writes a page. It is rendered into a buffer first, so a template
// that fails halfway sends an error rather than half a page.
func (g *Gateway) render(w http.ResponseWriter, status int, page string, v view) {
	var buf bytes.Buffer
	if err := g.pages[page].ExecuteTemplate(&buf, "layout", v); err != nil {
		g.log.Error("could not render a page", "page", page, "err", err)
		http.Error(w, "this page could not be shown", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (g *Gateway) serverError(w http.ResponseWriter, err error) {
	g.log.Error("request failed", "err", err)
	http.Error(w, "something went wrong; the details are in the server's log", http.StatusInternalServerError)
}
