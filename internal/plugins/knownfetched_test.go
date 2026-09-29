package plugins

import (
	"fmt"
	"strings"
	"testing"
)

// A fetched list is read by apps older and newer than the one that wrote it,
// so it is held to what each of them can offer, entry by entry.

const fetchedEntry = `{
	"id": %q, "name": "Blast radius", "tagline": "what fails if this goes away", "category": "platform",
	"description": "d", "repo": %q, "author": "K8s Dockside",
	"links": [{ "label": "Source", "url": "https://github.com/k8sdockside/blastradius" }]%s
}`

func entry(id, repo, extra string) string {
	return fmt.Sprintf(fetchedEntry, id, repo, extra)
}

func TestFetchedListIsReadLeniently(t *testing.T) {
	data := "[" + strings.Join([]string{
		entry("blastradius", "https://github.com/k8sdockside/blastradius.git", `, "someFutureField": {"x": 1}`),
		entry("future", "https://github.com/k8sdockside/future.git", `, "minAppVersion": "9.0.0"`),
		entry("broken", "https://github.com/k8sdockside/broken.git", `, "detect": ["crd:"]`),
		entry("blastradius", "https://github.com/someone/else.git", ""),
	}, ",") + "]"

	list, skipped, err := ParseFetchedKnown([]byte(data), "v0.1.18")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "blastradius" {
		t.Fatalf("kept %v, want blastradius alone", list)
	}
	want := []string{"needs K8s Dockside 9.0.0", "broken:", "listed twice"}
	if len(skipped) != len(want) {
		t.Fatalf("skipped %q", skipped)
	}
	for i, w := range want {
		if !strings.Contains(skipped[i], w) {
			t.Errorf("skipped[%d] = %q, want it to mention %q", i, skipped[i], w)
		}
	}

	// A development build is not a version, so it is offered everything.
	if list, _, _ := ParseFetchedKnown([]byte(data), "development build"); len(list) != 2 {
		t.Errorf("a development build kept %d entries, want 2", len(list))
	}
}

func TestFetchedListThatIsNotAListIsRefused(t *testing.T) {
	for name, data := range map[string]string{
		"an object":      `{"id": "x"}`,
		"an empty list":  `[]`,
		"not JSON":       `<html>rate limited</html>`,
		"nothing usable": `[{"id": "Not An Id"}]`,
	} {
		if _, _, err := ParseFetchedKnown([]byte(data), "v1.0.0"); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestFetchedListMergesOverTheBuiltInOne(t *testing.T) {
	t.Cleanup(func() { UseFetchedKnown(nil) })
	base := embeddedKnown()
	if len(base) == 0 {
		t.Fatal("no built-in list")
	}
	first := base[0]

	// The fetched copy rewrites the first entry's name and tries to move it
	// to another repository and make it official; and adds a new plugin.
	changed := first
	changed.Name = "Renamed"
	changed.Repo = "https://github.com/attacker/evil.git"
	changed.Official = !first.Official
	added := Known{ID: "brandnew", Name: "Brand new", Repo: "https://github.com/k8sdockside/brandnew.git", Author: "K8s Dockside", Official: true}

	UseFetchedKnown([]Known{added, changed})
	got := KnownPlugins()
	if len(got) != len(base)+1 {
		t.Fatalf("merged list has %d entries, want %d", len(got), len(base)+1)
	}
	if got[0].Name != "Renamed" {
		t.Errorf("the fetched name was not taken: %q", got[0].Name)
	}
	if got[0].Repo != first.Repo || got[0].Official != first.Official {
		t.Errorf("a fetched list moved a shipped plugin: repo %q official %v", got[0].Repo, got[0].Official)
	}
	if last := got[len(got)-1]; last.ID != "brandnew" || !last.Official {
		t.Errorf("the new plugin was not appended as given: %+v", last)
	}
	if k, ok := FindKnown("brandnew"); !ok || k.Repo != added.Repo {
		t.Errorf("FindKnown does not see the fetched plugin")
	}

	UseFetchedKnown(nil)
	if len(KnownPlugins()) != len(base) {
		t.Errorf("clearing the fetched list did not go back to the built-in one")
	}
}
