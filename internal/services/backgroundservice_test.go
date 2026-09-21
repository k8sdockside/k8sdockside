package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile puts a file in dir, failing the test when it cannot.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A folder of pictures is read for its pictures: not the folders inside it,
// not its hidden files, not whatever else happens to be kept beside them.
func TestABackgroundFolderListsItsImagesAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b-harbour.JPG", "jpg")
	writeFile(t, dir, "a cluster.png", "png")
	writeFile(t, dir, "notes.txt", "text")
	writeFile(t, dir, "drawing.svg", "<svg/>")
	writeFile(t, dir, ".hidden.png", "png")
	if err := os.Mkdir(filepath.Join(dir, "more.png"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := readBackgroundFolder(dir)

	if got.Problem != "" {
		t.Fatalf("problem reading a readable folder: %s", got.Problem)
	}
	var names []string
	for _, img := range got.Images {
		names = append(names, img.Name)
	}
	if strings.Join(names, ",") != "a cluster.png,b-harbour.JPG" {
		t.Fatalf("images = %v", names)
	}
	if !strings.HasPrefix(got.Images[0].URL, BackgroundPath+"a%20cluster.png?v=") {
		t.Errorf("url = %q, want the escaped name with a version", got.Images[0].URL)
	}
}

func TestAMissingBackgroundFolderSaysWhy(t *testing.T) {
	got := readBackgroundFolder(filepath.Join(t.TempDir(), "gone"))
	if got.Problem == "" {
		t.Error("a folder that is not there was read without a problem")
	}
	if got.Images == nil {
		t.Error("images should be an empty list, not null")
	}
}

func TestNoBackgroundFolderIsNotAProblem(t *testing.T) {
	got := readBackgroundFolder("")
	if got.Problem != "" || len(got.Images) != 0 {
		t.Errorf("got %+v, want an empty answer", got)
	}
}

// The handler serves one name inside one folder. Every way of reaching past
// it is refused before the disk is asked.
func TestTheBackgroundHandlerServesOnlyImagesInTheFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pictures")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "sky.png", "png-bytes")
	writeFile(t, dir, "notes.txt", "secret")
	writeFile(t, root, "outside.png", "outside")
	if err := os.Symlink(filepath.Join(root, "outside.png"), filepath.Join(dir, "link.png")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path string
		want int
	}{
		{BackgroundPath + "sky.png", http.StatusOK},
		{BackgroundPath + "notes.txt", http.StatusNotFound},
		{BackgroundPath + "..%2Foutside.png", http.StatusNotFound},
		{BackgroundPath + "../outside.png", http.StatusNotFound},
		{BackgroundPath + "link.png", http.StatusNotFound},
		{BackgroundPath + "missing.png", http.StatusNotFound},
		{BackgroundPath, http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+BackgroundPath+"x", nil)
		// Set the path as the router would hand it over, decoded.
		req.URL.Path = mustUnescape(t, tc.path)
		rec := httptest.NewRecorder()
		serveBackground(rec, req, dir)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.want)
		}
		if tc.want == http.StatusOK {
			if got := rec.Header().Get("Content-Type"); got != "image/png" {
				t.Errorf("%s: content type %q", tc.path, got)
			}
			if rec.Body.String() != "png-bytes" {
				t.Errorf("%s: body %q", tc.path, rec.Body.String())
			}
		}
	}

	// With no folder chosen there is nothing to serve at all.
	rec := httptest.NewRecorder()
	serveBackground(rec, httptest.NewRequest(http.MethodGet, BackgroundPath+"sky.png", nil), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("no folder: status %d", rec.Code)
	}
}

func TestTheBackgroundHandlerPassesOtherPathsOn(t *testing.T) {
	s := &BackgroundService{}
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	s.assetMiddleware()(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !reached {
		t.Error("a request for the app itself did not reach the app")
	}
}

// In the web version the folder would be one on the server, which is nobody's
// to choose from a browser.
func TestTheWebVersionServesNoBackgroundFolder(t *testing.T) {
	s := &BackgroundService{server: true}
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("passed on") })
	s.assetMiddleware()(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, BackgroundPath+"a.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
	if got := s.Folder(); got.Path != "" || len(got.Images) != 0 {
		t.Errorf("folder = %+v, want nothing", got)
	}
}

func mustUnescape(t *testing.T, p string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://x"+p, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req.URL.Path
}
