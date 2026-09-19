package plugins

import (
	_ "embed"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// A plugin's own views are HTML and script from a folder beside its file. They
// are served by the app's asset server under UIPath and drawn in an iframe
// sandboxed without allow-same-origin, which gives them an opaque origin: they
// cannot reach the app's DOM, its storage, or -- through the guard below -- its
// services. Everything they learn about the cluster they ask for over
// postMessage, and the frame that hosts them (PluginFrame.svelte) answers only
// for the kinds the plugin declares.
//
// The protections are layered because none of them is enough alone:
//
//   - the iframe's sandbox attribute gives the frame an opaque origin;
//   - the Content-Security-Policy on every file served here repeats the sandbox
//     (so the file is sandboxed even if something opens it outside the frame),
//     forbids fetch, XHR and websockets outright, and limits scripts, styles
//     and images to the plugin's own folder and the SDK;
//   - Guard refuses any request to the Wails runtime that comes from an opaque
//     origin, which is what a sandboxed frame's requests carry.
//
// A custom view can still read every kind it declares and, if it navigates
// itself somewhere else, send what it read there. That is the honest limit of
// running someone's code, and why the kinds are declared up front and shown in
// Settings.

// UIPath is where the asset server serves plugin views: UIPath + <pluginID> +
// "/" + <file>.
const UIPath = "/plugin-ui/"

// sdkDir is where the bridge's client script is served. The leading underscore
// keeps it out of the space of plugin ids, which may not contain one.
const sdkDir = "_sdk"

// SDKFile is the client script a custom view includes to talk to the app.
const SDKFile = "k8sdockside.js"

//go:embed sdk/k8sdockside.js
var sdkScript []byte

// runtimePath is the prefix of every request the Wails runtime makes: service
// calls, events, streams.
const runtimePath = "/wails/"

// Middleware serves plugin views and guards the runtime from them. It wraps the
// app's own asset handler: anything that is neither passes straight through.
//
// catalogue is asked for the plugins on every request rather than captured, so
// a Reload in Settings is picked up without restarting anything.
func Middleware(catalogue func() Catalogue) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, UIPath):
				serveUI(w, r, catalogue)
			case strings.HasPrefix(r.URL.Path, runtimePath) && fromOpaqueOrigin(r):
				http.Error(w, "a plugin view cannot call the app directly", http.StatusForbidden)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// fromOpaqueOrigin reports whether a request came from a document with no
// origin of its own -- a sandboxed frame. Browsers send the literal "null".
func fromOpaqueOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == "null"
}

func serveUI(w http.ResponseWriter, r *http.Request, catalogue func() Catalogue) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, UIPath)
	pluginID, file, _ := strings.Cut(rest, "/")
	host := webviewHost(r)

	if pluginID == sdkDir {
		if file != SDKFile {
			http.NotFound(w, r)
			return
		}
		setUIHeaders(w, host, sdkDir)
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(sdkScript)
		return
	}

	plugin, ok := catalogue().Find(pluginID)
	if !ok || plugin.Disabled {
		http.NotFound(w, r)
		return
	}
	if file == "" {
		file = DefaultEntry
	}
	// fs.ValidPath refuses "..", absolute paths and empty elements; the
	// os.Root behind a folder on disk then refuses anything that escapes it
	// through a symlink.
	if !fs.ValidPath(file) {
		http.NotFound(w, r)
		return
	}

	files, done, ok := plugin.UIFiles()
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer done()

	f, err := files.Open(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "cannot read that file", http.StatusForbidden)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Both kinds of folder hand back seekable files -- *os.File from disk, an
	// embedded file from the binary -- which is what ranges and HEAD need.
	content, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "cannot read that file", http.StatusForbidden)
		return
	}

	// A logo is an image the *app* draws in its own sidebar, not a page drawn
	// in a frame. Sending it the sandbox policy the views get would be asking
	// the browser to treat a picture as an untrusted document, so it gets a
	// policy that fits what it is.
	if plugin.Logo != "" && file == plugin.Logo {
		setLogoHeaders(w)
	} else {
		setUIHeaders(w, host, plugin.ID)
	}
	http.ServeContent(w, r, path.Base(file), info.ModTime(), content)
}

// webviewHost is the host the page was loaded from -- "localhost" under the
// wails:// scheme on macOS and Linux, "wails.localhost" on Windows -- checked
// to be something that can go into a header without quoting.
func webviewHost(r *http.Request) string {
	host := r.Host
	if host == "" {
		return "localhost"
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == ':':
		default:
			return "localhost"
		}
	}
	return host
}

// setLogoHeaders writes the headers a plugin's logo carries. An SVG is markup,
// and markup from a plugin is not trusted any further here than anywhere else:
// the policy leaves it nothing it could reach even if something did run it,
// and nosniff keeps it being drawn as the image type it was served as.
func setLogoHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
}

// setUIHeaders writes the headers every file served to a plugin view carries.
func setUIHeaders(w http.ResponseWriter, host, folder string) {
	h := w.Header()
	h.Set("Content-Security-Policy", contentPolicy(host, folder))
	// A sandboxed frame's origin is opaque, so even its own module scripts and
	// fonts are cross-origin requests. These files are the plugin's own and
	// nothing secret, so they are open to any origin.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("X-Content-Type-Options", "nosniff")
	// An edited view should be what the next open shows, without a restart.
	h.Set("Cache-Control", "no-store")
}

// contentPolicy is the Content-Security-Policy for one plugin's files.
//
// Sources are path-restricted to the plugin's own folder and the SDK, which is
// what keeps an <img> or <script> from being pointed at the runtime or at
// another plugin. They are spelled out per scheme rather than as 'self',
// because what 'self' means inside a sandboxed document is not something every
// webview agrees on.
func contentPolicy(host, folder string) string {
	var sources []string
	for _, scheme := range []string{"wails", "http", "https"} {
		base := scheme + "://" + host + UIPath
		sources = append(sources, base+folder+"/", base+sdkDir+"/")
	}
	own := strings.Join(sources, " ")

	return strings.Join([]string{
		"sandbox allow-scripts",
		"default-src " + own,
		"script-src " + own + " 'unsafe-inline' 'unsafe-eval'",
		"style-src " + own + " 'unsafe-inline'",
		"img-src " + own + " data: blob:",
		"font-src " + own + " data:",
		"connect-src 'none'",
		"frame-src 'none'",
		"object-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
	}, "; ")
}
