package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogerwesterbo/k8sdockside/internal/appconfig"
	"github.com/rogerwesterbo/k8sdockside/internal/kube"
)

const sampleConfig = `apiVersion: v1
kind: Config
current-context: one
clusters:
- name: one
  cluster:
    server: https://10.0.0.1:6443
contexts:
- name: one
  context:
    cluster: one
    user: admin
users:
- name: admin
`

// service returns a KubeconfigService backed by a settings file in a temp
// directory, with HOME pointed somewhere empty so the real ~/.kube plays no
// part in what the tests see.
func service(t *testing.T) *KubeconfigService {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")

	store, err := appconfig.Open()
	if err != nil {
		t.Fatal(err)
	}
	return NewKubeconfigService(store)
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// realTempDir is t.TempDir with its symlinks resolved. Discovery reports files
// by their real path, and on macOS the temp folder is itself behind a symlink,
// /var to /private/var, so a path built on the unresolved folder never matches.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddFilesKeepsTheGoodOnesWhenOneIsBad(t *testing.T) {
	s := service(t)
	dir := t.TempDir()

	good1 := write(t, dir, "a.config", sampleConfig)
	good2 := write(t, dir, "b.config", sampleConfig)
	bad := write(t, dir, "notes.txt", "not a kubeconfig\n")

	files, err := s.AddFiles(t.Context(), []string{good1, bad, good2})
	if err == nil {
		t.Fatal("want an error naming the file that failed")
	}
	if !strings.Contains(err.Error(), "notes.txt") {
		t.Errorf("error = %q, want it to name notes.txt", err)
	}
	if !strings.Contains(err.Error(), "added 2 of 3") {
		t.Errorf("error = %q, want it to say how many were added", err)
	}

	// The point of the exercise: a bad file among good ones must not throw the
	// good ones away, or the user has to pick them all again.
	if len(files) != 2 {
		t.Fatalf("got %d files, want the 2 good ones: %+v", len(files), files)
	}
}

func TestAddFilesReportsPlainlyWhenNoneWork(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	bad := write(t, dir, "notes.txt", "nope\n")

	files, err := s.AddFiles(t.Context(), []string{bad})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "added") {
		t.Errorf("error = %q, want no partial-success wording when nothing worked", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want none", len(files))
	}
}

func TestAddFolderTakesEveryConfigWhateverItIsNamed(t *testing.T) {
	s := service(t)
	dir := t.TempDir()

	write(t, dir, "hrw.config", sampleConfig)
	write(t, dir, "kubeconfig-test01-af3e", sampleConfig)
	write(t, dir, "readme.md", "# notes\n")

	files, err := s.AddFolder(t.Context(), dir)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(files), files)
	}
	for _, f := range files {
		if f.Source != kube.SourceFolder {
			t.Errorf("%s: source = %q", filepath.Base(f.Path), f.Source)
		}
	}
	if got := s.Folders(); len(got) != 1 || got[0] != dir {
		t.Errorf("Folders() = %v, want [%s]", got, dir)
	}
}

func TestAddFolderRefusesAFolderWithNothingInIt(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "readme.md", "# no configs here\n")

	if _, err := s.AddFolder(t.Context(), dir); err == nil {
		t.Fatal("want an error rather than a folder that silently does nothing")
	}
	if got := s.Folders(); len(got) != 0 {
		t.Errorf("Folders() = %v, want it not to have been stored", got)
	}
}

func TestAddFolderRefusesAFile(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "a.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), path); err == nil {
		t.Fatal("want an error")
	}
}

func TestRemoveFolderTakesItsConfigsWithIt(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "a.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	files, err := s.RemoveFolder(t.Context(), dir)
	if err != nil {
		t.Fatalf("RemoveFolder: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want none: %+v", len(files), files)
	}
	if got := s.Folders(); len(got) != 0 {
		t.Errorf("Folders() = %v, want empty", got)
	}
}

func TestAFileFromAWatchedFolderCanBeRemovedOnItsOwn(t *testing.T) {
	s := service(t)
	dir := realTempDir(t)
	keep := write(t, dir, "a.config", sampleConfig)
	drop := write(t, dir, "b.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}

	files, err := s.RemoveFile(t.Context(), drop)
	if err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}
	if len(files) != 1 || files[0].Path != keep {
		t.Fatalf("got %+v, want only %s", files, keep)
	}
	if got := s.Excluded(); len(got) != 1 || got[0] != drop {
		t.Errorf("Excluded() = %v, want [%s]", got, drop)
	}
}

func TestAnExcludedFileStaysGoneAcrossSyncs(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "a.config", sampleConfig)
	drop := write(t, dir, "b.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFile(t.Context(), drop); err != nil {
		t.Fatal(err)
	}

	// The whole reason exclusions exist: a rescan finds the file on disk again
	// and must not bring it back.
	if files := s.Sync(); len(files) != 1 {
		t.Errorf("got %d files after sync, want 1: %+v", len(files), files)
	}
}

func TestRestoreFileBringsAHiddenConfigBack(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "a.config", sampleConfig)
	drop := write(t, dir, "b.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFile(t.Context(), drop); err != nil {
		t.Fatal(err)
	}

	files, err := s.RestoreFile(t.Context(), drop)
	if err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2: %+v", len(files), files)
	}
	if got := s.Excluded(); len(got) != 0 {
		t.Errorf("Excluded() = %v, want empty", got)
	}
}

func TestRemovingAManualFileForgetsItRatherThanHidingIt(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "a.config", sampleConfig)

	if _, err := s.AddFile(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFile(t.Context(), path); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	// A file the user added is theirs to forget; recording it as an exclusion
	// would leave junk behind that re-adding the same file would trip over.
	if got := s.Excluded(); len(got) != 0 {
		t.Errorf("Excluded() = %v, want empty", got)
	}
	if files := s.Files(); len(files) != 0 {
		t.Errorf("got %d files, want none", len(files))
	}

	// ...and re-adding it works, rather than being silently hidden.
	files, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatalf("re-adding: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("got %d files after re-adding, want 1", len(files))
	}
}

func TestUnwatchingAFolderForgetsWhatWasHiddenInIt(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "a.config", sampleConfig)
	drop := write(t, dir, "b.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFile(t.Context(), drop); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}

	// Re-adding the folder must show everything in it. Keeping the exclusion
	// would mean quietly returning fewer files with nothing explaining why.
	files, err := s.AddFolder(t.Context(), dir)
	if err != nil {
		t.Fatalf("re-adding the folder: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want both back: %+v", len(files), files)
	}
}

func TestWatchedFolderIsRescannedOnSync(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	write(t, dir, "a.config", sampleConfig)

	if _, err := s.AddFolder(t.Context(), dir); err != nil {
		t.Fatal(err)
	}

	// A config dropped in later is the reason folders are stored as folders
	// rather than expanded into their files when they are added.
	write(t, dir, "b.config", sampleConfig)

	if files := s.Sync(); len(files) != 2 {
		t.Errorf("got %d files after sync, want 2: %+v", len(files), files)
	}
}

// twoContexts is a kubeconfig holding two contexts, for removing one of them.
const twoContexts = `apiVersion: v1
kind: Config
current-context: one
clusters:
- name: one
  cluster:
    server: https://10.0.0.1:6443
- name: two
  cluster:
    server: https://10.0.0.2:6443
contexts:
- name: one
  context:
    cluster: one
    user: admin
- name: two
  context:
    cluster: two
    user: admin
users:
- name: admin
`

// idOf finds a context by name in a scan, so tests use the id the app gives it
// -- built from the resolved path -- rather than one built from the path they
// wrote, which differs wherever the temp dir is a symlink, as it is on macOS.
func idOf(t *testing.T, files []kube.File, name string) string {
	t.Helper()
	for _, f := range files {
		for _, c := range f.Contexts {
			if c.Name == name {
				return c.ID
			}
		}
	}
	t.Fatalf("no context %q in %+v", name, files)
	return ""
}

// Removing one context leaves the rest of its file alone, and -- like a hidden
// file -- a rescan must not bring it back. Nothing is written to the kubeconfig.
func TestRemoveContextTakesOneContextOutOfItsFile(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	files, err := s.RemoveContext(t.Context(), idOf(t, added, "two"))
	if err != nil {
		t.Fatalf("RemoveContext: %v", err)
	}

	if len(files) != 1 || len(files[0].Contexts) != 1 || files[0].Contexts[0].Name != "one" {
		t.Errorf("after removing: %+v, want the file with only context one", files)
	}
	if files := s.Sync(); len(files[0].Contexts) != 1 {
		t.Errorf("a sync brought the removed context back: %+v", files[0].Contexts)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the kubeconfig file was changed; a removal must only be remembered in the app's own settings")
	}
}

// A removal lasts as long as the context is in its file. Once the file no
// longer holds it, the removal is forgotten, so the same context added again
// later appears instead of vanishing into a removal nobody can see.
func TestRemovedContextAppearsAgainWhenAddedToItsFileAgain(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	path := write(t, dir, "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveContext(t.Context(), idOf(t, added, "two")); err != nil {
		t.Fatal(err)
	}

	// The context leaves the kubeconfig, as when a cluster is torn down.
	write(t, dir, "both.config", sampleConfig)
	if files := s.Sync(); len(files) != 1 || len(files[0].Contexts) != 1 {
		t.Fatalf("after the context left its file: %+v", files)
	}
	if got := s.store.ExcludedContexts(); len(got) != 0 {
		t.Errorf("removal kept after the context left its file: %v", got)
	}

	// And comes back, as when its credentials are fetched again.
	write(t, dir, "both.config", twoContexts)
	if files := s.Sync(); len(files) != 1 || len(files[0].Contexts) != 2 {
		t.Errorf("the context added again did not appear: %+v", files)
	}
}

// Re-adding the file is the other way to add a context again: everything in
// it comes back, removed or not.
func TestRemovedContextAppearsAgainWithItsFile(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveContext(t.Context(), idOf(t, added, "two")); err != nil {
		t.Fatal(err)
	}
	if files, err := s.RemoveFile(t.Context(), path); err != nil || len(files) != 0 {
		t.Fatalf("RemoveFile: %v, %+v", err, files)
	}

	files, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatalf("re-adding the file: %v", err)
	}
	if len(files) != 1 || len(files[0].Contexts) != 2 {
		t.Errorf("after re-adding the file: %+v, want both contexts", files)
	}
}

// A file that cannot be read says nothing about which contexts it holds, so
// its removals are kept until it can be read again.
func TestRemovalSurvivesTheFileBeingUnreadable(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	path := write(t, dir, "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveContext(t.Context(), idOf(t, added, "two")); err != nil {
		t.Fatal(err)
	}

	write(t, dir, "both.config", "contexts: [not a kubeconfig")
	if files := s.Sync(); len(files) != 1 || files[0].Error == "" {
		t.Fatalf("want the broken file listed with an error: %+v", files)
	}

	write(t, dir, "both.config", twoContexts)
	if files := s.Sync(); len(files) != 1 || len(files[0].Contexts) != 1 {
		t.Errorf("the removal was forgotten while the file was unreadable: %+v", files)
	}
}

// The removal a rescan cannot undo has to be undoable somewhere, or the only
// way back is to take the context out of its kubeconfig, sync, and put it back
// -- which is not a thing anybody would guess.
func TestARemovedContextCanBeRestored(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, added, "two")
	if _, err := s.RemoveContext(t.Context(), id); err != nil {
		t.Fatal(err)
	}

	files, err := s.RestoreContext(t.Context(), id)
	if err != nil {
		t.Fatalf("RestoreContext: %v", err)
	}

	if len(files) != 1 || len(files[0].Contexts) != 2 {
		t.Errorf("after restoring: %+v, want both contexts back", files)
	}
	// And it stays back: the removal is gone from the settings, not merely
	// skipped once.
	if got := s.store.ExcludedContexts(); len(got) != 0 {
		t.Errorf("removals after restoring = %v, want none", got)
	}
	if files := s.Sync(); len(files[0].Contexts) != 2 {
		t.Errorf("a sync hid the restored context again: %+v", files[0].Contexts)
	}
}

// The list is the only place a removal can be seen, so it is the only place it
// can be undone -- the sidebar reads it to offer exactly that.
func TestRemovedContextsAreListedSoTheyCanBeUndone(t *testing.T) {
	s := service(t)
	path := write(t, t.TempDir(), "both.config", twoContexts)
	added, err := s.AddFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, added, "two")
	if _, err := s.RemoveContext(t.Context(), id); err != nil {
		t.Fatal(err)
	}

	got := s.RemovedContexts()

	if len(got) != 1 || got[0] != id {
		t.Errorf("removed contexts = %v, want just %q", got, id)
	}
}

func TestRestoreContextRefusesNothing(t *testing.T) {
	s := service(t)
	if _, err := s.RestoreContext(t.Context(), ""); err == nil {
		t.Error("RestoreContext(\"\") = nil error, want a complaint")
	}
}

func TestRemoveContextRefusesNothing(t *testing.T) {
	s := service(t)
	if _, err := s.RemoveContext(t.Context(), ""); err == nil {
		t.Error("RemoveContext(\"\") = nil error, want a complaint")
	}
}
