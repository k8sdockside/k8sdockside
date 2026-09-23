package plugins

import (
	json "encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const vmKind = "crd:virtualmachines.kubevirt.io"

func parsePlugin(t *testing.T, raw string) (Plugin, error) {
	t.Helper()
	var p Plugin
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return validate(p)
}

func TestActionsLoadWithTheirDefaults(t *testing.T) {
	p, err := parsePlugin(t, `{
        "id": "vms",
        "actions": [
            { "id": "pause", "kind": "`+vmKind+`",
              "request": { "type": "subresource", "subresource": "pause", "version": "v1",
                           "apiGroup": "subresources.kubevirt.io", "resource": "virtualmachineinstances" } },
            { "id": "stop", "label": "Stop", "kind": "`+vmKind+`", "tone": "danger",
              "request": { "type": "patch", "patch": { "spec": { "runStrategy": "Halted" } } } }
        ]
    }`)
	if err != nil {
		t.Fatal(err)
	}
	pause, _ := p.Action("pause")
	if pause.Label != "pause" || pause.Icon != "puzzle" || pause.Request.Method != "PUT" {
		t.Errorf("pause = %+v, want its defaults filled in", pause)
	}
	if got := pause.Request.SubresourcePath("ns", "web"); got != "/apis/subresources.kubevirt.io/v1/namespaces/ns/virtualmachineinstances/web/pause" {
		t.Errorf("path = %s", got)
	}
	// Actions alone are enough to be worth loading, and need no ui folder.
	if p.UI != nil {
		t.Errorf("ui = %+v, want none for a plugin with only actions", p.UI)
	}
}

func TestActionsAreRefusedWhereTheyWouldReachTooFar(t *testing.T) {
	cases := map[string]string{
		"a group outside the kind's": `{ "id": "a", "kind": "` + vmKind + `",
            "request": { "type": "subresource", "subresource": "x", "version": "v1", "apiGroup": "apps" } }`,
		"a subresource on a built-in kind": `{ "id": "a", "kind": "pods",
            "request": { "type": "subresource", "subresource": "eviction", "version": "v1" } }`,
		"patching a secret": `{ "id": "a", "kind": "secrets",
            "request": { "type": "patch", "patch": { "data": {} } } }`,
		"creating a binding": `{ "id": "a", "kind": "` + vmKind + `",
            "request": { "type": "create", "kind": "clusterrolebindings",
                         "object": { "apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding" } } }`,
		"a DELETE": `{ "id": "a", "kind": "` + vmKind + `",
            "request": { "type": "subresource", "subresource": "x", "version": "v1", "method": "DELETE" } }`,
		"a path in the version": `{ "id": "a", "kind": "` + vmKind + `",
            "request": { "type": "subresource", "subresource": "x", "version": "v1/../../api" } }`,
		"deleting a secret": `{ "id": "a", "kind": "secrets", "request": { "type": "delete" } }`,
		"deleting a CRD":    `{ "id": "a", "kind": "crd:roles.rbac.authorization.k8s.io", "request": { "type": "delete" } }`,
		"no request":        `{ "id": "a", "kind": "` + vmKind + `" }`,
		"a bad field":       `{ "id": "a", "kind": "` + vmKind + `", "when": [{ "field": "status..x" }], "request": { "type": "patch", "patch": { "a": 1 } } }`,
	}
	for name, action := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePlugin(t, `{ "id": "p", "actions": [`+action+`] }`); err == nil {
				t.Error("loaded, want it refused")
			}
		})
	}
}

func TestADeleteActionLoadsAndSaysSoInItsOffer(t *testing.T) {
	p, err := parsePlugin(t, `{
        "id": "vms",
        "actions": [
            { "id": "delete-vm", "label": "Delete VM", "kind": "`+vmKind+`", "tone": "danger",
              "confirm": "Delete {namespace}/{name}?", "request": { "type": "delete" } }
        ]
    }`)
	if err != nil {
		t.Fatal(err)
	}
	del, ok := p.Action("delete-vm")
	if !ok || del.Request.Type != RequestDelete {
		t.Fatalf("delete-vm = %+v", del)
	}
	offer := del.Offer(p, "ns", "web")
	if offer.Type != RequestDelete || offer.Confirm != "Delete ns/web?" {
		t.Errorf("offer = %+v", offer)
	}
}

func TestAnActionIsOfferedOnlyWhenItsConditionsHold(t *testing.T) {
	action := Action{When: []Condition{
		{Field: "status.printableStatus", In: []string{"Running"}},
		{Field: "status.conditions[LiveMigratable]", NotIn: []string{"False"}},
	}}
	vm := func(status, migratable string) *unstructured.Unstructured {
		conds := []any{}
		if migratable != "" {
			conds = append(conds, map[string]any{"type": "LiveMigratable", "status": migratable})
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"status": map[string]any{"printableStatus": status, "conditions": conds},
		}}
	}
	for _, tc := range []struct {
		status, migratable string
		want               bool
	}{
		{"Running", "True", true},
		{"Running", "", true}, // not said either way: offered
		{"Running", "False", false},
		{"Stopped", "True", false},
	} {
		if got := action.Offers(vm(tc.status, tc.migratable)); got != tc.want {
			t.Errorf("Offers(%s, %q) = %v, want %v", tc.status, tc.migratable, got, tc.want)
		}
	}
}

func TestExpandFillsOnlyStrings(t *testing.T) {
	got := Expand(map[string]any{
		"metadata": map[string]any{"generateName": "{name}-", "labels": map[string]any{"{name}": "x"}},
		"spec":     map[string]any{"vmiName": "{name}", "replicas": 2.0, "list": []any{"{namespace}"}},
	}, Vars("ns", "web")).(map[string]any)

	spec := got["spec"].(map[string]any)
	if spec["vmiName"] != "web" || spec["replicas"] != 2.0 || spec["list"].([]any)[0] != "ns" {
		t.Errorf("spec = %v", spec)
	}
	labels := got["metadata"].(map[string]any)["labels"].(map[string]any)
	if _, kept := labels["{name}"]; !kept {
		t.Errorf("labels = %v, want the key left as written", labels)
	}
}

func TestSectionsNeedAFolderAndGiveTheirKindToTheReadableList(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "vms")
	write(t, pluginDir, "plugin.json", `{
        "id": "vms",
        "sections": [{ "id": "console", "label": "Console", "kind": "`+vmKind+`", "entry": "vm.html" }]
    }`)
	write(t, filepath.Join(pluginDir, "ui"), "vm.html", "<p>vm</p>")

	cat := Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems: %v", cat.Problems)
	}
	p, _ := cat.Find("vms")
	if len(p.Sections) != 1 || p.Sections[0].Height != DefaultSectionHeight {
		t.Fatalf("sections = %+v", p.Sections)
	}
	if !p.CanRead(vmKind) {
		t.Errorf("readable = %v, want the section's kind in it", p.UI.Readable)
	}
}

func TestRepoFolder(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/acme/k8sdockside-kubevirt.git": "k8sdockside-kubevirt",
		"git@github.com:acme/KubeVirt_UI.git":              "kubevirt-ui",
		"https://example.com/acme/plugin/":                 "plugin",
	} {
		if got, err := RepoFolder(url); err != nil || got != want {
			t.Errorf("RepoFolder(%s) = %q, %v; want %q", url, got, err, want)
		}
	}
}

func TestValidGitURL(t *testing.T) {
	for url, want := range map[string]bool{
		"https://github.com/acme/plugin.git": true,
		"git@github.com:acme/plugin.git":     true,
		"ssh://git@example.com/plugin.git":   true,
		"file:///etc":                        false,
		"ext::sh -c touch% /tmp/x":           false,
		"--upload-pack=touch /tmp/x":         false,
		"/home/me/plugin":                    false,
	} {
		if got := ValidGitURL(url); got != want {
			t.Errorf("ValidGitURL(%q) = %v, want %v", url, got, want)
		}
	}
	if _, err := Clone(t.TempDir(), "file:///etc"); err == nil || !strings.Contains(err.Error(), "not a repository address") {
		t.Errorf("Clone(file://) = %v, want it refused before git is run", err)
	}
}
