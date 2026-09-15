package kube

import (
	"os"
	"path/filepath"
	"testing"
)

const twoContexts = `apiVersion: v1
kind: Config
current-context: staging
clusters:
- name: staging
  cluster:
    server: https://10.0.0.1:6443
- name: prod
  cluster:
    server: https://10.0.0.2:6443
contexts:
- name: staging
  context:
    cluster: staging
    user: staging-admin
    namespace: apps
- name: prod
  context:
    cluster: prod
    user: prod-admin
users:
- name: staging-admin
- name: prod-admin
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// realTempDir is t.TempDir with its symlinks resolved. Discover reports files
// by their real path -- it is how a symlinked config is told apart from a
// second one -- and on macOS the temp folder is itself behind a symlink, /var
// to /private/var, so a path built on the unresolved folder never matches.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestParseFileFlattensContexts(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config", twoContexts)

	f := ParseFile(path, SourceDefault)
	if f.Error != "" {
		t.Fatalf("unexpected error: %s", f.Error)
	}
	if len(f.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2", len(f.Contexts))
	}

	staging := f.Contexts[0]
	if staging.Name != "staging" {
		t.Errorf("Name = %q, want staging", staging.Name)
	}
	// The server lives on the cluster, not the context: flattening it is the
	// whole point of ParseFile.
	if staging.Server != "https://10.0.0.1:6443" {
		t.Errorf("Server = %q, want the staging cluster's server", staging.Server)
	}
	if staging.Namespace != "apps" {
		t.Errorf("Namespace = %q, want apps", staging.Namespace)
	}
	if !staging.Current {
		t.Error("staging should be marked as the current context")
	}
	if f.Contexts[1].Current {
		t.Error("prod is not the current context")
	}
	if staging.ID != ContextID(path, "staging") {
		t.Errorf("ID = %q, want it derived from path and name", staging.ID)
	}
}

func TestParseFileRejectsNonKubeconfigs(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		content string
	}{
		{"not yaml", "{{{ this is not yaml"},
		{"wrong kind", "apiVersion: v1\nkind: Secret\ndata: {}\n"},
		{"no contexts", "apiVersion: v1\nkind: Config\nclusters: []\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, dir, tc.name+".yaml", tc.content)
			if f := ParseFile(path, SourceManual); f.Error == "" {
				t.Fatalf("expected an error, got %d contexts", len(f.Contexts))
			}
		})
	}
}

func TestParseFileReportsMissingFile(t *testing.T) {
	f := ParseFile(filepath.Join(t.TempDir(), "absent"), SourceManual)
	if f.Error == "" {
		t.Fatal("expected an error for a file that does not exist")
	}
	// Callers render the file even when it is broken, so the path must survive.
	if f.Path == "" {
		t.Error("Path should be preserved on failure")
	}
}

func TestDiscoverFindsEverySource(t *testing.T) {
	home := realTempDir(t)
	elsewhere := realTempDir(t)
	t.Setenv("HOME", home)

	def := writeFile(t, filepath.Join(home, ".kube"), "config", twoContexts)
	scanned := writeFile(t, filepath.Join(home, ".kube"), "extra.yaml", twoContexts)
	fromEnv := writeFile(t, elsewhere, "env-config", twoContexts)
	manual := writeFile(t, elsewhere, "manual.yaml", twoContexts)
	t.Setenv("KUBECONFIG", fromEnv)

	// Directories under ~/.kube must not be opened as config files.
	if err := os.MkdirAll(filepath.Join(home, ".kube", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	sources := map[string]string{}
	for _, f := range Discover(Sources{Files: []string{manual}}) {
		sources[f.Path] = f.Source
	}

	want := map[string]string{
		def:     SourceDefault,
		fromEnv: SourceEnv,
		manual:  SourceManual,
		scanned: SourceScan,
	}
	for path, source := range want {
		if got := sources[path]; got != source {
			t.Errorf("%s: source = %q, want %q", filepath.Base(path), got, source)
		}
	}
	if len(sources) != len(want) {
		t.Errorf("found %d files, want %d: %v", len(sources), len(want), sources)
	}
}

func TestDiscoverDeduplicatesAndPrefersTheFirstSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	def := writeFile(t, filepath.Join(home, ".kube"), "config", twoContexts)
	// The same file named three ways: directly, through $KUBECONFIG, and as a
	// symlink. All three must collapse into one entry, labelled "default".
	link := filepath.Join(home, ".kube", "linked.yaml")
	if err := os.Symlink(def, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", def)

	files := Discover(Sources{Files: []string{link}})
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %v", len(files), files)
	}
	if files[0].Source != SourceDefault {
		t.Errorf("source = %q, want %q", files[0].Source, SourceDefault)
	}
}

func TestDiscoverSkipsJunkInKubeDirButReportsNamedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	writeFile(t, filepath.Join(home, ".kube"), "config", twoContexts)
	// A stray YAML file that is not a kubeconfig: the user never asked for it,
	// so it should be dropped rather than shown as an error.
	writeFile(t, filepath.Join(home, ".kube"), "notes.yaml", "hello: world\n")
	broken := writeFile(t, t.TempDir(), "broken.yaml", "hello: world\n")

	files := Discover(Sources{Files: []string{broken}})

	var scanned, manual int
	for _, f := range files {
		switch f.Source {
		case SourceScan:
			scanned++
		case SourceManual:
			manual++
			if f.Error == "" {
				t.Error("a file the user added should report why it failed")
			}
		}
	}
	if scanned != 0 {
		t.Errorf("got %d scanned files, want 0", scanned)
	}
	if manual != 1 {
		t.Errorf("got %d manual files, want 1", manual)
	}
}

func TestResolveExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := resolve("~/.kube/config"), filepath.Join(home, ".kube", "config"); got != want {
		t.Errorf("resolve(~) = %q, want %q", got, want)
	}
	if resolve("   ") != "" {
		t.Error("a blank path should resolve to nothing")
	}
}

func TestDiscoverIgnoresAMissingDefaultConfig(t *testing.T) {
	home := realTempDir(t)
	elsewhere := realTempDir(t)
	t.Setenv("HOME", home)

	// No ~/.kube/config at all: the user keeps their clusters elsewhere.
	fromEnv := writeFile(t, elsewhere, "env-config", twoContexts)
	t.Setenv("KUBECONFIG", fromEnv)

	files := Discover(Sources{})
	if len(files) != 1 {
		t.Fatalf("got %d files, want only the $KUBECONFIG one: %+v", len(files), files)
	}
	if files[0].Path != fromEnv {
		t.Errorf("path = %q, want %q", files[0].Path, fromEnv)
	}
}

func TestDiscoverStillReportsAnUnreadableDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	// A default config that exists and holds real content, but is not a
	// kubeconfig, is the user's problem to see -- unlike one that is simply
	// absent or has nothing in it.
	writeFile(t, filepath.Join(home, ".kube"), "config", "apiVersion: v1\nkind: Secret\ndata: {}\n")

	files := Discover(Sources{})
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Error == "" {
		t.Error("a malformed ~/.kube/config was silently dropped")
	}
}

func TestScanFolderFindsConfigsWhateverTheyAreNamed(t *testing.T) {
	dir := t.TempDir()

	// A folder of kubeconfigs as people actually keep them: some with an
	// extension, some with none at all.
	writeFile(t, dir, "hrw.config", twoContexts)
	writeFile(t, dir, "kubeconfig-test01-af3e", twoContexts)
	writeFile(t, dir, "plain", twoContexts)

	found := ScanFolder(dir)
	if len(found) != 3 {
		t.Fatalf("found %d configs, want 3: %+v", len(found), found)
	}
	for _, f := range found {
		if f.Source != SourceFolder {
			t.Errorf("%s: source = %q, want %q", filepath.Base(f.Path), f.Source, SourceFolder)
		}
	}
}

func TestScanFolderSkipsWhatIsNotAKubeconfig(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "real.config", twoContexts)
	writeFile(t, dir, "notes.txt", "just some notes\n")
	writeFile(t, dir, "other.yaml", "apiVersion: v1\nkind: Secret\ndata: {}\n")
	writeFile(t, dir, "empty.yaml", "")
	// A binary file must be rejected as such rather than fed to the YAML parser.
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte{0x00, 0x01, 0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Sub-directories are not descended.
	writeFile(t, filepath.Join(dir, "nested"), "deep.config", twoContexts)

	found := ScanFolder(dir)
	if len(found) != 1 {
		t.Fatalf("found %d configs, want only the real one: %+v", len(found), found)
	}
	if filepath.Base(found[0].Path) != "real.config" {
		t.Errorf("found %q", found[0].Path)
	}
}

func TestParseFileRejectsBinaryAsNotText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.p12")
	if err := os.WriteFile(path, []byte{0x30, 0x82, 0x00, 0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}

	f := ParseFile(path, SourceManual)
	if f.Error != "not a text file" {
		t.Errorf("Error = %q, want %q", f.Error, "not a text file")
	}
}

func TestDiscoverIncludesWatchedFolders(t *testing.T) {
	home := realTempDir(t)
	watched := realTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	inFolder := writeFile(t, watched, "cluster-a", twoContexts)
	writeFile(t, watched, "readme.md", "# not a kubeconfig\n")

	files := Discover(Sources{Folders: []string{watched}})
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Path != inFolder {
		t.Errorf("path = %q, want %q", files[0].Path, inFolder)
	}
	if files[0].Source != SourceFolder {
		t.Errorf("source = %q, want %q", files[0].Source, SourceFolder)
	}
}

func TestAFileNamedDirectlyKeepsItsSourceOverAWatchedFolder(t *testing.T) {
	home := t.TempDir()
	watched := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	path := writeFile(t, watched, "cluster-a", twoContexts)

	// The user added this file by hand and also happens to watch its folder.
	// It must appear once, labelled as theirs, so it stays removable.
	files := Discover(Sources{Files: []string{path}, Folders: []string{watched}})
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Source != SourceManual {
		t.Errorf("source = %q, want %q", files[0].Source, SourceManual)
	}
}

// noContexts is a valid kubeconfig that simply has nothing in it yet -- what
// `kubectl config` leaves behind, and what a fresh machine often has.
const noContexts = `apiVersion: v1
kind: Config
clusters: []
contexts: []
users: []
`

func TestDiscoverIgnoresADefaultConfigWithNothingInIt(t *testing.T) {
	// The shapes a ~/.kube/config with no clusters in it actually takes: never
	// written to, written by a tool that only stamped the header, and emptied
	// out by hand. None is a fault -- we look at that path whether or not the
	// user asked us to, so having nothing to show there is the ordinary case,
	// and the sidebar needs no files at all to offer its "add a kubeconfig"
	// empty state instead of a red error.
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"zero bytes", ""},
		{"header only", "apiVersion: v1\nkind: Config\n"},
		{"empty lists", noContexts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("KUBECONFIG", "")

			writeFile(t, filepath.Join(home, ".kube"), "config", tc.content)

			files := Discover(Sources{})
			if len(files) != 0 {
				t.Fatalf("got %d files, want none: %+v", len(files), files)
			}
		})
	}
}

func TestDiscoverStillReportsAnEmptyFileTheUserAdded(t *testing.T) {
	home := t.TempDir()
	elsewhere := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	// The user pointed at this one, so its being empty is worth saying --
	// otherwise adding a file appears to do nothing at all.
	added := writeFile(t, elsewhere, "mine.config", noContexts)

	files := Discover(Sources{Files: []string{added}})
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Error == "" {
		t.Error("an empty file the user added should say why nothing appeared")
	}
}
