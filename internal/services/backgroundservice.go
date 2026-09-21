package services

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// BackgroundPath is where the images in the user's background folder are
// served from, by name. Nothing else is: the handler answers for the files
// directly inside that one folder whose names end in an image extension, and
// for nothing else on the disk.
const BackgroundPath = "/user-backgrounds/"

// backgroundTypes are the files a background folder is read for, with the
// content type each is served as. SVG is left out on purpose: it is a document
// that can carry script, and this handler serves whatever the folder holds.
var backgroundTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".avif": "image/avif",
}

// BackgroundImage is one picture in the user's background folder.
type BackgroundImage struct {
	// Name is the file's name, which is also its id in the pinned setting.
	Name string `json:"name"`
	// URL is where the window loads it from. It carries the file's
	// modification time, so an image replaced under the same name is fetched
	// again rather than served from the webview's cache.
	URL string `json:"url"`
}

// BackgroundFolder is what the settings view shows about the folder: where it
// is, what is in it, and why nothing is, when that is the case.
type BackgroundFolder struct {
	Path   string            `json:"path"`
	Images []BackgroundImage `json:"images"`
	// Problem is why the folder could not be read. Empty when it could, even
	// when it holds no images -- that is said by Images being empty.
	Problem string `json:"problem"`
}

// BackgroundService is the start page's own pictures: a folder of the user's
// images, read here because the webview has no business being handed a path
// on disk, and served by the middleware below for the same reason.
//
// The built-in pictures are not here. They are drawn by the frontend in the
// colours of the theme in use, which only the frontend knows.
type BackgroundService struct {
	store *appconfig.Store
	// server is set in the web version, where "a folder on this machine" is a
	// folder in a pod nobody is sitting at.
	server bool
}

// NewBackgroundService wires the service to the settings store, which is where
// the folder is recorded.
func NewBackgroundService(store *appconfig.Store) *BackgroundService {
	return &BackgroundService{store: store}
}

// Folder lists the images in the background folder. An unset folder is not a
// problem, only an empty answer.
func (s *BackgroundService) Folder() BackgroundFolder {
	if s.server {
		return BackgroundFolder{Images: []BackgroundImage{}}
	}
	return readBackgroundFolder(s.store.BackgroundFolder())
}

// BrowseForFolder opens the native picker in directory mode and makes the
// folder chosen the one the start page reads. Cancelling leaves everything as
// it was and is not an error.
func (s *BackgroundService) BrowseForFolder(ctx context.Context) (BackgroundFolder, error) {
	if s.server {
		return s.Folder(), errDesktopOnly
	}
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Folder(), err
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Choose a folder of background images").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		ShowHiddenFiles(true)

	start := s.store.BackgroundFolder()
	if start == "" {
		if home, err := os.UserHomeDir(); err == nil {
			start = home
		}
	}
	if start != "" {
		dialog.SetDirectory(start)
	}

	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return s.Folder(), err
	}
	if path == "" {
		return s.Folder(), nil // cancelled
	}
	return s.SetFolder(ctx, path)
}

// SetFolder makes a directory the one the start page reads images from.
func (s *BackgroundService) SetFolder(ctx context.Context, path string) (BackgroundFolder, error) {
	if s.server {
		return s.Folder(), errDesktopOnly
	}
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Folder(), err
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return s.Folder(), err
	}
	if !info.IsDir() {
		return s.Folder(), errors.New(path + " is a file, not a folder")
	}
	if _, err := s.store.SetBackgroundFolder(path); err != nil {
		return s.Folder(), err
	}
	return s.Folder(), nil
}

// ClearFolder forgets the background folder. Nothing on disk is touched.
func (s *BackgroundService) ClearFolder(ctx context.Context) (BackgroundFolder, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Folder(), err
	}
	if _, err := s.store.SetBackgroundFolder(""); err != nil {
		return s.Folder(), err
	}
	return s.Folder(), nil
}

// RevealFolder opens the background folder in the platform's file manager.
// The path comes from the store rather than the frontend, so that nothing the
// webview says can decide what gets opened.
func (s *BackgroundService) RevealFolder() error {
	if s.server {
		return errDesktopOnly
	}
	dir := s.store.BackgroundFolder()
	if dir == "" {
		return errors.New("no background folder has been chosen")
	}
	return application.Get().Env.OpenFileManager(dir, false)
}

// assetMiddleware serves the images in the background folder at
// BackgroundPath, and passes everything else on.
func (s *BackgroundService) assetMiddleware() application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, BackgroundPath) {
				next.ServeHTTP(w, r)
				return
			}
			if s.server {
				http.NotFound(w, r)
				return
			}
			serveBackground(w, r, s.store.BackgroundFolder())
		})
	}
}

// serveBackground answers for one file directly inside dir. The name is taken
// as a name and nothing more: a path separator, a dot-dot or a type that is
// not an image is refused before the disk is looked at.
func serveBackground(w http.ResponseWriter, r *http.Request, dir string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, BackgroundPath)
	kind, ok := backgroundType(name)
	if dir == "" || !ok || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}

	// Through an os.Root on the folder, as the plugin views are served: it
	// refuses whatever would resolve outside it, links included, below any
	// check made here on the name.
	root, err := os.OpenRoot(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = root.Close() }()

	info, err := root.Lstat(name)
	// A link is not served even when it points inside the folder: what the
	// folder holds is what is in it.
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// backgroundType is the content type a file is served as, and whether it is
// an image at all.
func backgroundType(name string) (string, bool) {
	kind, ok := backgroundTypes[strings.ToLower(filepath.Ext(name))]
	return kind, ok
}

// readBackgroundFolder lists the images directly inside dir, by name.
// Folders inside it are not read: a folder of pictures is a folder, and
// walking into whatever else is below it is how a photo library ends up on
// the start page.
func readBackgroundFolder(dir string) BackgroundFolder {
	out := BackgroundFolder{Path: dir, Images: []BackgroundImage{}}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		out.Problem = err.Error()
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.Type().IsRegular() {
			continue
		}
		if _, ok := backgroundType(name); !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out.Images = append(out.Images, BackgroundImage{
			Name: name,
			URL:  BackgroundPath + url.PathEscape(name) + "?v=" + strconv.FormatInt(info.ModTime().Unix(), 10),
		})
	}
	slices.SortFunc(out.Images, func(a, b BackgroundImage) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}
