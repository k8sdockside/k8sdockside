package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameRepositoryIgnoresHowTheAddressIsSpelt(t *testing.T) {
	same := [][2]string{
		{"https://github.com/rogerwesterbo/k8sdockside-certmanager", "https://github.com/rogerwesterbo/k8sdockside-certmanager.git"},
		{"https://github.com/Acme/Plugin.git/", "https://github.com/acme/plugin"},
		{"git@github.com:acme/plugin.git", "https://github.com/acme/plugin"},
		{"ssh://git@github.com/acme/plugin.git", "git@github.com:acme/plugin"},
	}
	for _, pair := range same {
		if !SameRepository(pair[0], pair[1]) {
			t.Errorf("%s and %s were taken for different repositories", pair[0], pair[1])
		}
	}
	different := [][2]string{
		{"https://github.com/acme/plugin", "https://github.com/acme/plugin-two"},
		{"https://github.com/acme/plugin", "https://gitlab.com/acme/plugin"},
		{"https://github.com/acme/plugin", ""},
		{"", ""},
	}
	for _, pair := range different {
		if SameRepository(pair[0], pair[1]) {
			t.Errorf("%q and %q were taken for the same repository", pair[0], pair[1])
		}
	}
}

// git runs git for a test, with an identity and no signing whatever the
// machine's own configuration says.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// upstreamWithClone makes a repository and a clone of it in a plugins folder
// under the folder name the address gives, with the clone's origin set to
// that address and git told to fetch it from the local repository instead --
// so the update path runs without a network.
func upstreamWithClone(t *testing.T, address string) (upstream, plugins, clone string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	upstream = t.TempDir()
	git(t, upstream, "init", "-q")
	write(t, upstream, "README.md", "a plugin, not yet pushed\n")
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-q", "-m", "Initial commit")

	plugins = t.TempDir()
	folder, err := RepoFolder(address)
	if err != nil {
		t.Fatal(err)
	}
	clone = filepath.Join(plugins, folder)
	git(t, plugins, "clone", "-q", upstream, folder)
	git(t, clone, "remote", "set-url", "origin", address)
	git(t, clone, "config", "url."+upstream+".insteadOf", address)
	return upstream, plugins, clone
}

// The Official badge belongs to the repository, not the name: cert-manager
// cloned from the known list's address is official; a plugin calling itself
// cert-manager from anywhere else -- or writing "official": true -- is not.
func TestOfficialIsDecidedByTheRepositoryNotTheName(t *testing.T) {
	known, ok := FindKnown("cert-manager")
	if !ok || !known.Official {
		t.Fatal("this test needs cert-manager to be an official known plugin")
	}
	const manifest = `{"id": "cert-manager", "official": true, "views": [{"id": "pods", "kind": "pods"}]}`

	_, plugins, clone := upstreamWithClone(t, known.Repo)
	write(t, clone, "plugin.json", manifest)
	if p, ok := Load(plugins, nil, nil).Find("cert-manager"); !ok || !p.Official {
		t.Errorf("a clone of %s: found %v, official %v; want it official", known.Repo, ok, p.Official)
	}

	_, elsewhere, fork := upstreamWithClone(t, "https://github.com/someone-else/k8sdockside-certmanager.git")
	write(t, fork, "plugin.json", manifest)
	if p, ok := Load(elsewhere, nil, nil).Find("cert-manager"); !ok || p.Official {
		t.Errorf("a clone of someone else's repository: found %v, official %v; want it not official", ok, p.Official)
	}

	loose := t.TempDir()
	write(t, loose, "cert-manager.json", manifest)
	if p, ok := Load(loose, nil, nil).Find("cert-manager"); !ok || p.Official {
		t.Errorf("a file in the plugins folder: found %v, official %v; want it not official", ok, p.Official)
	}
}

// A repository cloned before its plugin was pushed has no plugin.json, so the
// plugin is offered for installing again. Installing it has to update that
// clone rather than refuse because the folder is taken.
func TestInstallingOverAnEarlierCloneOfTheSameRepositoryUpdatesIt(t *testing.T) {
	const address = "https://github.com/acme/k8sdockside-acme.git"
	upstream, plugins, clone := upstreamWithClone(t, address)

	if got := Load(plugins, nil, nil).Problems; len(got) != 1 || !strings.Contains(got[0].Message, "no plugin.json") {
		t.Fatalf("an empty clone reported %v, want it named as having no plugin.json", got)
	}

	write(t, upstream, "plugin.json", `{"id": "acme", "views": [{"id": "pods", "kind": "pods"}]}`)
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-q", "-m", "The plugin")

	// Spelt differently from the clone's origin, as the known list and a
	// hand-typed address often are.
	dest, err := Install(plugins, "https://github.com/acme/k8sdockside-acme")
	if err != nil {
		t.Fatalf("Install over the earlier clone: %v", err)
	}
	if dest != clone {
		t.Errorf("Install used %s, want the clone already at %s", dest, clone)
	}
	cat := Load(plugins, nil, nil)
	if _, ok := cat.Find("acme"); !ok {
		t.Errorf("the plugin did not load after the clone was updated; problems: %v", cat.Problems)
	}
	if len(cat.Problems) > 0 {
		t.Errorf("problems after the update: %v", cat.Problems)
	}
}

func TestInstallLeavesAFolderFromAnotherRepositoryAlone(t *testing.T) {
	_, plugins, clone := upstreamWithClone(t, "https://github.com/someone-else/k8sdockside-acme.git")
	before, _ := os.ReadDir(clone)

	_, err := Install(plugins, "https://github.com/acme/k8sdockside-acme.git")
	if err == nil || !strings.Contains(err.Error(), "someone-else") {
		t.Fatalf("Install over another repository's clone: %v, want it refused and the other address named", err)
	}
	after, _ := os.ReadDir(clone)
	if len(after) != len(before) {
		t.Error("the other repository's clone was changed")
	}
}
