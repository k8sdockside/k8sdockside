package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// KubeconfigService is the frontend's view of the kubeconfig files on disk. It
// caches the last scan so that opening tabs and rendering the sidebar does not
// re-read every file, and rescans only when the user asks (Sync) or changes the
// set of files.
//
// In the web version the clusters are everyone's, so every change to the
// sources is an administrator's to make.
type KubeconfigService struct {
	store *appconfig.Store
	// extra are folders scanned on every sync beside the user's own sources,
	// which the user cannot stop watching -- see Options.KubeconfigFolders.
	extra []string
	// server is set in the web version, which has no file manager to open.
	server bool

	mu    sync.RWMutex
	files []kube.File
	index map[string]kube.Context
}

// NewKubeconfigService wires the service to the settings store, which owns the
// list of kubeconfig paths the user added by hand.
func NewKubeconfigService(store *appconfig.Store) *KubeconfigService {
	return &KubeconfigService{store: store, files: []kube.File{}, index: map[string]kube.Context{}}
}

// Sync rescans every source -- ~/.kube/config, $KUBECONFIG, the user's own
// paths, and the rest of ~/.kube -- takes out the contexts the user has
// removed, and returns what is left.
func (s *KubeconfigService) Sync() []kube.File {
	files := s.withoutRemoved(kube.Discover(kube.Sources{
		Files:    s.store.ManualFiles(),
		Folders:  slices.Concat(s.store.ManualFolders(), s.extra),
		Excluded: s.store.ExcludedFiles(),
	}))

	index := make(map[string]kube.Context)
	for _, f := range files {
		for _, c := range f.Contexts {
			index[c.ID] = c
		}
	}

	s.mu.Lock()
	s.files, s.index = files, index
	s.mu.Unlock()
	return files
}

// withoutRemoved takes the removed contexts out of the files that still hold
// them, and forgets every removal that no file matched.
//
// A removal lives in the app's own settings, since a kubeconfig is never
// written. It is honoured while the context is in its file, and forgotten as
// soon as a scan no longer finds it, so that a context added again -- by the
// tool that wrote it the first time, or by re-adding the file it lives in --
// shows up rather than vanishing into an old removal nobody remembers making.
// While it is honoured it is listed, under Hidden beside the excluded files,
// which is where RestoreContext undoes it.
//
// A file that could not be read says nothing about what it holds, so its
// removals are kept until it can be read again.
func (s *KubeconfigService) withoutRemoved(files []kube.File) []kube.File {
	removed := s.store.ExcludedContexts()
	if len(removed) == 0 {
		return files
	}

	drop := make(map[string]bool, len(removed))
	for _, id := range removed {
		drop[id] = true
	}

	found := make(map[string]bool, len(removed))
	unreadable := map[string]bool{}
	for i := range files {
		if files[i].Error != "" {
			unreadable[files[i].Path] = true
		}
		files[i].Contexts = slices.DeleteFunc(files[i].Contexts, func(c kube.Context) bool {
			if !drop[c.ID] {
				return false
			}
			found[c.ID] = true
			return true
		})
	}

	var stale []string
	for _, id := range removed {
		if found[id] || unreadable[contextFile(id)] {
			continue
		}
		stale = append(stale, id)
	}
	// A removal that could not be forgotten now is tried again on the next
	// scan; the sidebar is right either way, since the context is not there.
	_, _ = s.store.ForgetContexts(stale)
	return files
}

// contextFile is the path half of a context id, as kube.ContextID builds it.
func contextFile(id string) string {
	if at := strings.LastIndex(id, "::"); at != -1 {
		return id[:at]
	}
	return ""
}

// Files returns the last scan, running one first if the app has just started.
func (s *KubeconfigService) Files() []kube.File {
	s.mu.RLock()
	cached := s.files
	s.mu.RUnlock()

	if cached == nil {
		return s.Sync()
	}
	return cached
}

// Contexts flattens the last scan into a single list, which is what the sidebar
// actually renders.
func (s *KubeconfigService) Contexts() []kube.Context {
	out := []kube.Context{}
	for _, f := range s.Files() {
		out = append(out, f.Contexts...)
	}
	return out
}

// AddFile remembers a kubeconfig path chosen by the user and rescans. The file
// is parsed before being stored so that picking something that is not a
// kubeconfig fails immediately, with a message, instead of quietly adding a
// broken entry to the sidebar.
func (s *KubeconfigService) AddFile(ctx context.Context, path string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if path == "" {
		return s.Files(), errors.New("no file selected")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return s.Files(), err
	}
	if parsed := kube.ParseFile(abs, kube.SourceManual); parsed.Error != "" {
		return s.Files(), errors.New(parsed.Error)
	}
	if _, err := s.store.AddManualFile(abs); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// RemoveFile takes a kubeconfig out of the sidebar, whatever put it there.
//
// From the user's side this is one action -- "I do not want this file" -- but
// it has two implementations. A file they added by hand is simply forgotten. A
// file discovery found cannot be forgotten, because the next sync would find it
// again, so refusing it is recorded as an exclusion instead.
func (s *KubeconfigService) RemoveFile(ctx context.Context, path string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if path == "" {
		return s.Files(), errors.New("no file given")
	}

	manual := false
	for _, p := range s.store.ManualFiles() {
		if p == path {
			manual = true
			break
		}
	}

	var err error
	if manual {
		_, err = s.store.RemoveManualFile(path)
	} else {
		_, err = s.store.ExcludeFile(path)
	}
	if err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// RestoreFile un-hides a file that was excluded, letting discovery find it
// again on this sync.
func (s *KubeconfigService) RestoreFile(ctx context.Context, path string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if _, err := s.store.UnexcludeFile(path); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// RemoveContext takes one context out of the sidebar and leaves the rest of
// its file there. The kubeconfig itself is not touched -- this app never
// writes one -- so the removal is remembered in the app's own settings, and a
// rescan leaves the context out again for as long as the file holds it.
//
// Which is why RestoreContext exists beside it. A rescan finds the context and
// hides it again, so without somewhere to undo the removal the only way back
// was to take the context out of the kubeconfig, sync, and put it back --
// something nobody would guess and the confirmation did not say.
func (s *KubeconfigService) RemoveContext(ctx context.Context, id string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if id == "" {
		return s.Files(), errors.New("no context given")
	}
	if _, err := s.store.ExcludeContext(id); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// RestoreContext brings back a context the user removed, so the next scan lists
// it again.
//
// Nothing checks that the id still names anything: a removal whose context has
// since left its file is forgotten by withoutRemoved on the next scan anyway,
// and refusing to undo a removal because the thing behind it is missing would
// leave the entry on screen with a button that does nothing.
func (s *KubeconfigService) RestoreContext(ctx context.Context, id string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if id == "" {
		return s.Files(), errors.New("no context given")
	}
	if _, err := s.store.UnexcludeContext(id); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// Excluded returns the files the user has hidden, so the sidebar can say how
// many a folder is holding back and offer to show them again. Hidden state that
// cannot be seen anywhere is hidden state nobody can undo.
func (s *KubeconfigService) Excluded() []string {
	return s.store.ExcludedFiles()
}

// RemovedContexts returns the contexts the user has removed one by one, for the
// same reason Excluded lists the hidden files: it is the only place the removal
// can be seen, and the only place it can be undone.
func (s *KubeconfigService) RemovedContexts() []string {
	return s.store.ExcludedContexts()
}

// Folders returns the directories being watched, so the sidebar can list them
// and offer to stop watching one.
func (s *KubeconfigService) Folders() []string {
	return s.store.ManualFolders()
}

// AddFolder starts watching a directory for kubeconfigs and rescans.
//
// The folder is scanned before being stored so that choosing one with nothing
// in it fails with a message, rather than being accepted and then appearing to
// have done nothing.
func (s *KubeconfigService) AddFolder(ctx context.Context, path string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if path == "" {
		return s.Files(), errors.New("no folder selected")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return s.Files(), err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return s.Files(), err
	}
	if !info.IsDir() {
		return s.Files(), errors.New("not a folder")
	}
	if found := kube.ScanFolder(abs); len(found) == 0 {
		return s.Files(), fmt.Errorf("no kubeconfig files in %s", filepath.Base(abs))
	}
	if _, err := s.store.AddManualFolder(abs); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// RemoveFolder stops watching a directory. The configs found through it leave
// the sidebar with the rescan this triggers.
//
// Anything hidden inside that folder is forgotten with it: keeping those
// exclusions would mean re-adding the folder silently produced fewer files than
// it contains, with nothing on screen explaining why.
func (s *KubeconfigService) RemoveFolder(ctx context.Context, path string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	if _, err := s.store.RemoveManualFolder(path); err != nil {
		return s.Files(), err
	}
	if _, err := s.store.ClearExclusionsIn(path); err != nil {
		return s.Files(), err
	}
	return s.Sync(), nil
}

// BrowseForFile opens the native file picker and adds everything the user
// chose. It returns the new file list; cancelling the dialog leaves everything
// as it was and is not an error.
//
// Several files can be picked at once, and one bad choice does not discard the
// good ones: every file is tried, and the failures are reported together.
func (s *KubeconfigService) BrowseForFile(ctx context.Context) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Add kubeconfigs").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		ShowHiddenFiles(true)

	if home, err := os.UserHomeDir(); err == nil {
		dialog.SetDirectory(filepath.Join(home, ".kube"))
	}

	paths, err := dialog.PromptForMultipleSelection()
	if err != nil {
		return s.Files(), err
	}
	if len(paths) == 0 {
		return s.Files(), nil // cancelled
	}
	return s.AddFiles(ctx, paths)
}

// AddFiles remembers several kubeconfig paths at once. Files that parse are
// kept even when others alongside them do not, because discarding a good
// selection over one bad file would mean picking them all again.
func (s *KubeconfigService) AddFiles(ctx context.Context, paths []string) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	var failures []string
	added := 0

	for _, path := range paths {
		if _, err := s.AddFile(ctx, path); err != nil {
			failures = append(failures, filepath.Base(path)+": "+err.Error())
			continue
		}
		added++
	}

	files := s.Files()
	if len(failures) == 0 {
		return files, nil
	}
	if added == 0 {
		return files, errors.New(strings.Join(failures, "; "))
	}
	return files, fmt.Errorf("added %d of %d -- %s", added, len(paths), strings.Join(failures, "; "))
}

// BrowseForFolder opens the native picker in directory mode and watches the
// folder the user chose.
func (s *KubeconfigService) BrowseForFolder(ctx context.Context) ([]kube.File, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.Files(), err
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Add a folder of kubeconfigs").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		ShowHiddenFiles(true)

	if home, err := os.UserHomeDir(); err == nil {
		dialog.SetDirectory(home)
	}

	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return s.Files(), err
	}
	if path == "" {
		return s.Files(), nil // cancelled
	}
	return s.AddFolder(ctx, path)
}

// RevealFile shows the kubeconfig file a context was read from in the file
// manager, selected. It is where a context's credentials live, so it is where
// a user goes once the dashboard says a token or certificate is running out.
func (s *KubeconfigService) RevealFile(id string) error {
	if s.server {
		return errDesktopOnly
	}
	kc, ok := s.lookup(id)
	if !ok {
		return fmt.Errorf("no context %q in the kubeconfig files", id)
	}
	if kc.File == "" {
		return fmt.Errorf("context %q was not read from a file", kc.Name)
	}
	if _, err := os.Stat(kc.File); err != nil {
		return fmt.Errorf("the kubeconfig file is not there any more: %w", err)
	}
	return application.Get().Env.OpenFileManager(kc.File, true)
}

// lookup resolves a context ID against the last scan. It is unexported so it
// stays out of the generated bindings; only the other services use it.
func (s *KubeconfigService) lookup(id string) (kube.Context, bool) {
	s.mu.RLock()
	ctx, ok := s.index[id]
	s.mu.RUnlock()
	if ok {
		return ctx, true
	}

	// The context may come from a file added since the last scan (or the app
	// may have just started), so it is worth one rescan before giving up.
	s.Sync()
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, ok = s.index[id]
	return ctx, ok
}
