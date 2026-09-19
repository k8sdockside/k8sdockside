package plugins

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const customPlugin = `{
    "id": "acme",
    "name": "Acme",
    "requires": [{ "kind": "crd:meshes.acme.io" }],
    "logo": "logo.svg",
    "ui": { "kinds": ["pods"], "write": true },
    "views": [
        { "id": "map", "label": "Map", "type": "custom" },
        { "id": "second", "label": "Second", "type": "custom", "entry": "pages/second.html" },
        { "id": "meshes", "label": "Meshes", "kind": "crd:meshes.acme.io" }
    ]
}`

// installCustom writes a plugin with its own views into a fresh plugins folder,
// with a ui folder beside it, and loads it.
func installCustom(t *testing.T) (Catalogue, string) {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "acme")
	write(t, pluginDir, "plugin.json", customPlugin)
	write(t, filepath.Join(pluginDir, "ui"), "index.html", "<p>map</p>")
	write(t, filepath.Join(pluginDir, "ui"), "logo.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/></svg>`)
	write(t, filepath.Join(pluginDir, "ui", "pages"), "second.html", "<p>second</p>")
	write(t, pluginDir, "secret.txt", "not for the frame")

	cat := Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems loading: %v", cat.Problems)
	}
	return cat, dir
}

func TestACustomViewLoadsWithItsDefaults(t *testing.T) {
	cat, _ := installCustom(t)
	p, ok := cat.Find("acme")
	if !ok {
		t.Fatal("acme did not load")
	}
	v, _ := p.View("map")
	if v.Type != ViewCustom || v.Entry != DefaultEntry {
		t.Errorf("map view = %+v, want a custom view opening %s", v, DefaultEntry)
	}
	if p.UI == nil || p.UI.Dir != DefaultUIDir {
		t.Fatalf("ui = %+v, want the default folder", p.UI)
	}
	// Everything the plugin names, plus what its UI asks for -- once each.
	want := []string{"crd:meshes.acme.io", "pods"}
	if !slices.Equal(p.UI.Readable, want) {
		t.Errorf("readable = %v, want %v", p.UI.Readable, want)
	}
	if !p.CanWrite("pods") || p.CanRead("secrets") || p.CanRead("deployments") {
		t.Errorf("permissions wrong: write pods %v, read secrets %v, read deployments %v",
			p.CanWrite("pods"), p.CanRead("secrets"), p.CanRead("deployments"))
	}

	resolved, err := cat.ResolveKind(ViewKind("acme", "second"))
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Custom || resolved.Entry != "pages/second.html" || resolved.Kind != "" {
		t.Errorf("resolved = %+v", resolved)
	}
}

func TestOnlyADeclaredViewMayAskRegistries(t *testing.T) {
	cat, _ := installCustom(t)
	if p, _ := cat.Find("acme"); p.CanAskRegistries() {
		t.Error("a plugin that does not declare registries may ask them")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "images"), "plugin.json", `{
    "id": "images",
    "name": "Images",
    "requires": [{ "kind": "pods" }],
    "ui": { "registries": true },
    "views": [{ "id": "updates", "label": "Updates", "type": "custom" }]
}`)
	write(t, filepath.Join(dir, "images", "ui"), "index.html", "<p>updates</p>")
	cat = Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems loading: %v", cat.Problems)
	}
	if p, _ := cat.Find("images"); !p.CanAskRegistries() || p.CanWrite("pods") {
		t.Errorf("registries %v, write %v; want registries and no write", p.CanAskRegistries(), p.CanWrite("pods"))
	}
}

func TestACustomViewWithoutAUIBlockGetsOne(t *testing.T) {
	p, err := validate(Plugin{ID: "x", Views: []View{{ID: "v", Type: ViewCustom}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.UI == nil || p.UI.Dir != DefaultUIDir || p.UI.Write {
		t.Errorf("ui = %+v, want the read-only default", p.UI)
	}
}

func TestValidateRefusesBadCustomViews(t *testing.T) {
	cases := map[string]Plugin{
		"entry escapes":       {ID: "x", Views: []View{{ID: "v", Type: ViewCustom, Entry: "../other/index.html"}}},
		"absolute entry":      {ID: "x", Views: []View{{ID: "v", Type: ViewCustom, Entry: "/etc/passwd"}}},
		"custom with a kind":  {ID: "x", Views: []View{{ID: "v", Type: ViewCustom, Kind: "pods"}}},
		"table with an entry": {ID: "x", Views: []View{{ID: "v", Kind: "pods", Entry: "index.html"}}},
		"ui dir escapes":      {ID: "x", UI: &UI{Dir: "../elsewhere"}, Views: []View{{ID: "v", Type: ViewCustom}}},
		"ui reads secrets":    {ID: "x", UI: &UI{Kinds: []string{"secrets"}}, Views: []View{{ID: "v", Type: ViewCustom}}},
		"ui reads nonsense":   {ID: "x", UI: &UI{Kinds: []string{"widgets"}}, Views: []View{{ID: "v", Type: ViewCustom}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := validate(p); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestAFocusedViewGetsItsDefaultsAndMayReadItsKind(t *testing.T) {
	p, err := validate(Plugin{
		ID:    "x",
		Views: []View{{ID: "board", Type: ViewCustom, Focus: &Focus{Kind: " crd:meshes.acme.io "}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := p.View("board")
	if v.Focus == nil || v.Focus.Kind != "crd:meshes.acme.io" || v.Focus.Hash != DefaultFocusHash {
		t.Errorf("focus = %+v, want the kind trimmed and the default hash", v.Focus)
	}
	if !p.CanRead("crd:meshes.acme.io") {
		t.Error("a view that can be opened on a kind should be able to read it")
	}
}

func TestAFocusHashLosesItsHashMark(t *testing.T) {
	p, err := validate(Plugin{
		ID:    "x",
		Views: []View{{ID: "board", Type: ViewCustom, Focus: &Focus{Kind: "pods", Hash: "#selected={namespace}/{name}"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.View("board"); v.Focus.Hash != "selected={namespace}/{name}" {
		t.Errorf("hash = %q", v.Focus.Hash)
	}
}

func TestValidateRefusesBadFocuses(t *testing.T) {
	cases := map[string]View{
		"on a table view":    {ID: "v", Kind: "pods", Focus: &Focus{Kind: "pods"}},
		"with no kind":       {ID: "v", Type: ViewCustom, Focus: &Focus{Hash: "name={name}"}},
		"on nonsense":        {ID: "v", Type: ViewCustom, Focus: &Focus{Kind: "widgets"}},
		"on a plugin view":   {ID: "v", Type: ViewCustom, Focus: &Focus{Kind: "plugin:x/y"}},
		"on secrets":         {ID: "v", Type: ViewCustom, Focus: &Focus{Kind: "secrets"}},
		"naming the unknown": {ID: "v", Type: ViewCustom, Focus: &Focus{Kind: "pods", Hash: "uid={uid}"}},
		"a second hash mark": {ID: "v", Type: ViewCustom, Focus: &Focus{Kind: "pods", Hash: "a={name}#b"}},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := validate(Plugin{ID: "x", Views: []View{v}}); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// A secret named anywhere else in the plugin still does not become readable.
func TestSecretsAreNeverReadableByAView(t *testing.T) {
	p, err := validate(Plugin{
		ID:    "x",
		Views: []View{{ID: "s", Kind: "secrets"}, {ID: "v", Type: ViewCustom}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.CanRead("secrets") {
		t.Error("a custom view may read secrets")
	}
}

func TestAPluginWhoseUIFolderIsMissingIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "plugin.json", customPlugin)
	cat := Load(dir, nil, nil)
	if _, ok := cat.Find("acme"); ok {
		t.Error("loaded without its ui folder")
	}
	if len(cat.Problems) != 1 || !strings.Contains(cat.Problems[0].Message, "ui folder") {
		t.Errorf("problems = %v", cat.Problems)
	}
}

func serve(t *testing.T, cat Catalogue, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	Middleware(func() Catalogue { return cat })(next).ServeHTTP(rec, req)
	return rec
}

func TestMiddlewareServesAViewWithItsSandbox(t *testing.T) {
	cat, _ := installCustom(t)

	req := httptest.NewRequest(http.MethodGet, "/plugin-ui/acme/pages/second.html", nil)
	req.Host = "localhost"
	rec := serve(t, cat, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "second") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"sandbox allow-scripts",
		"connect-src 'none'",
		"wails://localhost/plugin-ui/acme/",
		"wails://localhost/plugin-ui/_sdk/",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("policy %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("policy %q gives the frame the app's origin", csp)
	}
}

func TestMiddlewareServesALogoAsAnImageNotAPage(t *testing.T) {
	cat, _ := installCustom(t)

	req := httptest.NewRequest(http.MethodGet, "/plugin-ui/acme/logo.svg", nil)
	req.Host = "localhost"
	rec := serve(t, cat, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("logo served as %q, which the app will not draw as an image", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("logo served without nosniff")
	}
	// The views' policy sandboxes the response as a document and names the
	// plugin's folder as a source. Neither belongs on a picture, and the
	// sandbox in particular is what a browser may refuse to draw.
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "allow-scripts") {
		t.Errorf("logo carries the page sandbox: %q", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("logo policy %q does not shut everything out", csp)
	}
}

// A file that is not the declared logo keeps the page policy, whatever it is
// named: the exemption is for the one file the manifest points at, not for
// every image in the folder.
func TestOnlyTheDeclaredLogoEscapesThePageSandbox(t *testing.T) {
	cat, dir := installCustom(t)
	write(t, filepath.Join(dir, "acme", "ui"), "other.svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`)

	req := httptest.NewRequest(http.MethodGet, "/plugin-ui/acme/other.svg", nil)
	req.Host = "localhost"
	rec := serve(t, cat, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox allow-scripts") {
		t.Errorf("policy %q is not the page sandbox", csp)
	}
}

func TestMiddlewareServesTheEntryForTheBareFolder(t *testing.T) {
	cat, _ := installCustom(t)
	rec := serve(t, cat, httptest.NewRequest(http.MethodGet, "/plugin-ui/acme/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "map") {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareServesNothingOutsideTheUIFolder(t *testing.T) {
	cat, _ := installCustom(t)
	for _, path := range []string{
		"/plugin-ui/acme/../secret.txt",
		"/plugin-ui/acme/%2e%2e/secret.txt",
		"/plugin-ui/acme/../plugin.json",
		"/plugin-ui/acme/missing.html",
		"/plugin-ui/argocd/index.html", // a built-in has no folder
		"/plugin-ui/nobody/index.html",
		"/plugin-ui/_sdk/other.js",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = strings.ReplaceAll(path, "%2e", ".")
		rec := serve(t, cat, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s was served: %q", path, rec.Body.String())
		}
	}
}

func TestMiddlewareServesNothingForADisabledPlugin(t *testing.T) {
	_, dir := installCustom(t)
	cat := Load(dir, nil, []string{"acme"})
	rec := serve(t, cat, httptest.NewRequest(http.MethodGet, "/plugin-ui/acme/index.html", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d", rec.Code)
	}
}

func TestMiddlewareServesTheSDK(t *testing.T) {
	rec := serve(t, Catalogue{}, httptest.NewRequest(http.MethodGet, UIPath+sdkDir+"/"+SDKFile, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "window.k8sdockside") {
		t.Errorf("got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("content type %q", rec.Header().Get("Content-Type"))
	}
}

func TestMiddlewareRefusesTheRuntimeToASandboxedFrame(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(`{"object":0,"method":0}`))
	req.Header.Set("Origin", "null")
	if rec := serve(t, Catalogue{}, req); rec.Code != http.StatusForbidden {
		t.Errorf("sandboxed call got %d", rec.Code)
	}

	// The app's own calls pass through untouched.
	req = httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(`{}`))
	req.Header.Set("Origin", "wails://localhost")
	if rec := serve(t, Catalogue{}, req); rec.Code != http.StatusTeapot {
		t.Errorf("the app's own call got %d", rec.Code)
	}
	if rec := serve(t, Catalogue{}, httptest.NewRequest(http.MethodGet, "/index.html", nil)); rec.Code != http.StatusTeapot {
		t.Errorf("an ordinary asset got %d", rec.Code)
	}
}

// A built-in's pages are embedded in the app and served like any plugin's,
// with the same headers -- the sandbox travels with the file, not the folder.
func TestABuiltinServesItsEmbeddedPages(t *testing.T) {
	cat := Load(t.TempDir(), nil, nil)
	argo, ok := cat.Find("argocd")
	if !ok {
		t.Fatal("no argocd built-in")
	}
	if argo.UI == nil || argo.Overview == nil {
		t.Fatalf("argocd built-in has ui %+v and overview %+v, want both", argo.UI, argo.Overview)
	}

	for _, file := range []string{argo.Overview.Entry, "apps.html", "app.html", "model.js", "kit.js", "argo.css"} {
		rec := serve(t, cat, httptest.NewRequest(http.MethodGet, "/plugin-ui/argocd/"+file, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", file, rec.Code)
			continue
		}
		if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") {
			t.Errorf("%s is served without its sandbox", file)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s is empty", file)
		}
	}

	// Nothing outside its own folder, embedded or not.
	for _, file := range []string{"../argocd.json", "../flux.json", "missing.html"} {
		rec := serve(t, cat, httptest.NewRequest(http.MethodGet, "/plugin-ui/argocd/"+file, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want it refused", file)
		}
	}
}

// Every page a built-in opens is in its embedded folder.
func TestEveryBuiltinPageIsEmbedded(t *testing.T) {
	for _, p := range Builtin() {
		files, done, ok := p.UIFiles()
		if !ok {
			continue
		}
		var entries []string
		for _, v := range p.Views {
			if v.Type == ViewCustom {
				entries = append(entries, v.Entry)
			}
		}
		for _, s := range p.Sections {
			entries = append(entries, s.Entry)
		}
		if p.Overview != nil {
			entries = append(entries, p.Overview.Entry)
		}
		for _, entry := range entries {
			if _, err := fs.Stat(files, entry); err != nil {
				t.Errorf("built-in %q opens %s, which is not embedded: %v", p.ID, entry, err)
			}
		}
		done()
	}
}

// sdk/k8sdockside.d.ts is what a plugin written in TypeScript copies to type
// the bridge, so every call the bridge has must be declared there too.
func TestTheBridgeTypesDeclareEveryCall(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("sdk", "k8sdockside.js"))
	if err != nil {
		t.Fatal(err)
	}
	dts, err := os.ReadFile(filepath.Join("sdk", "k8sdockside.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(js)
	start := strings.Index(body, "window.k8sdockside = {")
	if start < 0 {
		t.Fatal("cannot find window.k8sdockside in the bridge")
	}
	calls := regexp.MustCompile(`(?m)^ {8}([a-zA-Z]+): function`).FindAllStringSubmatch(body[start:], -1)
	if len(calls) < 10 {
		t.Fatalf("found only %d calls in the bridge", len(calls))
	}
	for _, call := range calls {
		if !regexp.MustCompile(`(?m)^\s+` + call[1] + `[<(]`).Match(dts) {
			t.Errorf("k8sdockside.d.ts does not declare %s()", call[1])
		}
	}
}
