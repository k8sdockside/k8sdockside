package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/k8sdockside/k8sdockside/internal/themes"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ThemeService is how the frontend gets its colours: the themes that ship with
// the app, the ones the user has installed, and the folders they are read from.
//
// The reading happens here rather than in the webview because a theme is a file
// on disk, and the frontend has no business knowing where a user's config lives
// or being handed a path to open. It also means the built-in themes and a
// stranger's downloaded one go through exactly one parser and one validator --
// see internal/themes.
//
// Like SettingsService, every mutator answers with the whole catalogue, so the
// frontend replaces its state with what actually happened rather than assuming
// its optimistic update stuck. In the web version the theme folders are
// everyone's, so only an administrator may change them.
type ThemeService struct {
	store *appconfig.Store
	// server is set in the web version, which has no file manager to open.
	server bool
}

// NewThemeService wires the service to the settings store, which is where the
// user's extra theme folders are recorded.
func NewThemeService(store *appconfig.Store) *ThemeService {
	return &ThemeService{store: store}
}

// Tokens is the list of colours a theme may set, with what each one is for. The
// settings view shows it so that writing a theme does not mean reading the
// source, and it comes from the same list the loader validates against so the
// documentation cannot drift from the rules.
func (s *ThemeService) Tokens() []themes.Token {
	return themes.Tokens
}

// List returns every theme available right now, each with its tokens already
// resolved against its base, along with anything that failed to load and why.
//
// It is one call for the whole gallery rather than a call per theme, because
// the settings view draws a real preview of every theme at once and thirteen
// round trips to paint one screen would be thirteen too many.
func (s *ThemeService) List() themes.Catalogue {
	return themes.Load(s.store.ThemesDir(), s.store.ThemeFolders())
}

// Dir is the folder user themes are read from by default, shown in the settings
// view so the user knows where to put a file they have downloaded.
func (s *ThemeService) Dir() string {
	return s.store.ThemesDir()
}

// RevealDir opens the themes folder in the platform's file manager, creating it
// first if it has never been used. Creating it is the point: "here is where
// your themes go" is not much help if opening it fails because nobody has put
// one there yet.
//
// The path comes from the store rather than the frontend, so that nothing the
// webview says can decide what gets opened.
func (s *ThemeService) RevealDir() error {
	if s.server {
		return errDesktopOnly
	}
	dir := s.store.ThemesDir()
	if err := themes.EnsureDir(dir); err != nil {
		return err
	}
	return application.Get().Env.OpenFileManager(dir, false)
}

// CreateExample writes a starter theme into the themes folder and returns the
// path it wrote, so the settings view can say where it went.
//
// It exists because the alternative first step -- read the token documentation,
// open an editor, get the JSON right -- is a much worse place to start than a
// file that already loads and can be edited a colour at a time.
func (s *ThemeService) CreateExample(ctx context.Context) (string, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return "", err
	}
	return themes.WriteExample(s.store.ThemesDir())
}

// AddFolder starts reading themes from another directory, for themes kept
// somewhere the user already syncs -- a dotfiles repo, a shared drive.
func (s *ThemeService) AddFolder(ctx context.Context, path string) (themes.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.List(), err
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return s.List(), err
	}
	if !info.IsDir() {
		return s.List(), errors.New(path + " is a file, not a folder")
	}
	if _, err := s.store.AddThemeFolder(path); err != nil {
		return s.List(), err
	}
	return s.List(), nil
}

// RemoveFolder stops reading themes from a directory. Nothing is deleted: the
// themes in it simply stop being offered, and a theme from it that was in use
// falls back to the default until the folder is added again.
func (s *ThemeService) RemoveFolder(ctx context.Context, path string) (themes.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.List(), err
	}
	if _, err := s.store.RemoveThemeFolder(path); err != nil {
		return s.List(), err
	}
	return s.List(), nil
}

// BrowseForFolder opens the native picker in directory mode and adds the folder
// the user chose. Cancelling leaves everything as it was and is not an error.
func (s *ThemeService) BrowseForFolder(ctx context.Context) (themes.Catalogue, error) {
	if s.server {
		return s.List(), errDesktopOnly
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Add a folder of themes").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		ShowHiddenFiles(true)

	if home, err := os.UserHomeDir(); err == nil {
		dialog.SetDirectory(home)
	}

	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return s.List(), err
	}
	if path == "" {
		return s.List(), nil // cancelled
	}
	return s.AddFolder(ctx, path)
}
