package plugins

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const acmeManifest = `{"id": "acme", "views": [{"id": "pods", "kind": "pods"}]}`

func gone(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

func TestUninstallingACloneDeletesItsFolderAndNothingElse(t *testing.T) {
	_, dir, clone := upstreamWithClone(t, "https://github.com/acme/k8sdockside-acme.git")
	write(t, clone, "plugin.json", acmeManifest)
	neighbour := write(t, dir, "neighbour.json", `{"id": "neighbour", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, nil)
	p, ok := cat.Find("acme")
	if !ok {
		t.Fatalf("acme did not load: %v", cat.Problems)
	}
	removal, err := Uninstall(cat, dir, p)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if removal.Path != clone {
		t.Errorf("deleted %s, want the clone %s", removal.Path, clone)
	}
	if !gone(t, clone) {
		t.Error("the clone is still there")
	}
	if gone(t, neighbour) || gone(t, dir) {
		t.Error("something beside the clone was deleted")
	}
	if _, ok := Load(dir, nil, nil).Find("acme"); ok {
		t.Error("acme still loads after it was uninstalled")
	}
}

func TestUninstallingAFileInThePluginsFolderDeletesOnlyThatFile(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "acme.json", acmeManifest)
	other := write(t, dir, "other.json", `{"id": "other", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, nil)
	p, _ := cat.Find("acme")
	removal, err := Uninstall(cat, dir, p)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if removal.Path != file || !slices.Equal(removal.Plugins, []string{"acme"}) {
		t.Errorf("removal = %+v, want only %s", removal, file)
	}
	if !gone(t, file) || gone(t, other) {
		t.Error("want acme.json deleted and other.json kept")
	}
}

// A pack is one file, so uninstalling one of its plugins takes the rest with
// it -- and the question asked beforehand has to say so.
func TestUninstallingOnePluginOfAPackNamesTheRestOfIt(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "pack.json", `{"name": "Acme Pack", "plugins": [
		{"id": "acme", "views": [{"id": "pods", "kind": "pods"}]},
		{"id": "acme-edge", "views": [{"id": "pods", "kind": "pods"}]}
	]}`)

	cat := Load(dir, nil, nil)
	p, ok := cat.Find("acme-edge")
	if !ok {
		t.Fatalf("the pack did not load: %v", cat.Problems)
	}
	removal, err := Removable(cat, dir, p)
	if err != nil {
		t.Fatalf("Removable: %v", err)
	}
	got := slices.Clone(removal.Plugins)
	slices.Sort(got)
	if removal.Path != file || !slices.Equal(got, []string{"acme", "acme-edge"}) {
		t.Errorf("removal = %+v, want the pack and both its plugins", removal)
	}
	if gone(t, file) {
		t.Error("asking what would be deleted deleted it")
	}
}

// A folder the user added is theirs -- typically the repository the plugin is
// written in -- so nothing in it is deleted.
func TestAPluginFromAWatchedFolderIsNotDeleted(t *testing.T) {
	dir, extra := t.TempDir(), t.TempDir()
	file := write(t, filepath.Join(extra, "mine"), "plugin.json", acmeManifest)

	cat := Load(dir, []string{extra}, nil)
	p, ok := cat.Find("acme")
	if !ok {
		t.Fatalf("acme did not load from the watched folder: %v", cat.Problems)
	}
	if _, err := Uninstall(cat, dir, p); err == nil || !strings.Contains(err.Error(), "only watches") {
		t.Errorf("Uninstall of a watched folder's plugin: %v, want it refused", err)
	}
	if gone(t, file) {
		t.Error("the plugin in the watched folder was deleted")
	}
}

func TestABuiltInCannotBeUninstalled(t *testing.T) {
	dir := t.TempDir()
	cat := Load(dir, nil, nil)
	p, ok := cat.Find("argocd")
	if !ok {
		t.Fatal("this test needs the built-in argocd plugin")
	}
	if _, err := Uninstall(cat, dir, p); err == nil || !strings.Contains(err.Error(), "switch it off") {
		t.Errorf("Uninstall of a built-in: %v, want it refused", err)
	}
}

// A copy read from a watched folder is the user's checkout, not an install:
// the known plugin is still offered, and only a copy in the plugins folder is
// the app's to update or uninstall.
func TestOnlyACopyInThePluginsFolderCountsAsInstalled(t *testing.T) {
	dir, extra := t.TempDir(), t.TempDir()
	const manifest = `{"id": "cert-manager", "views": [{"id": "pods", "kind": "pods"}]}`
	write(t, filepath.Join(extra, "mine"), "plugin.json", manifest)

	offered := func(cat Catalogue) bool {
		for _, o := range cat.Offer() {
			if o.ID == "cert-manager" {
				return o.Installed
			}
		}
		t.Fatal("cert-manager is not on the known list")
		return false
	}

	cat := Load(dir, []string{extra}, nil)
	p, ok := cat.Find("cert-manager")
	if !ok {
		t.Fatalf("the watched copy did not load: %v", cat.Problems)
	}
	if InPluginsDir(dir, p) {
		t.Error("a watched folder's copy counted as in the plugins folder")
	}
	if _, ok := cat.InstalledHere("cert-manager"); ok || offered(cat) {
		t.Error("a watched folder's copy counted as installed")
	}

	write(t, filepath.Join(dir, "k8sdockside-certmanager"), "plugin.json", manifest)
	cat = Load(dir, []string{extra}, nil)
	if q, ok := cat.InstalledHere("cert-manager"); !ok || !InPluginsDir(dir, q) || !offered(cat) {
		t.Errorf("the plugins folder's copy: installed %v, from %s; want it installed", ok, q.Origin)
	}
	if _, ok := cat.InstalledHere("argocd"); !ok {
		t.Error("a built-in did not count as installed")
	}
}

// The case that asked for uninstalling: the same plugin installed from its
// repository and read from the folder it is being written in. The installed
// copy takes the id; uninstalling it lets the other one load.
func TestUninstallingTheInstalledCopyLetsTheWatchedOneLoad(t *testing.T) {
	dir, extra := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "k8sdockside-acme"), "plugin.json", acmeManifest)
	dev := write(t, filepath.Join(extra, "k8sdockside-acme"), "plugin.json", acmeManifest)

	cat := Load(dir, []string{extra}, nil)
	p, _ := cat.Find("acme")
	if p.Origin == dev || len(cat.Problems) != 1 {
		t.Fatalf("want the installed copy loaded and the watched one refused; loaded %s, problems %v", p.Origin, cat.Problems)
	}
	if _, err := Uninstall(cat, dir, p); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	after := Load(dir, []string{extra}, nil)
	if q, ok := after.Find("acme"); !ok || q.Origin != dev || len(after.Problems) != 0 {
		t.Errorf("after uninstalling: loaded %v from %s, problems %v; want the watched copy", ok, q.Origin, after.Problems)
	}
}
