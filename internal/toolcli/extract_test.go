package toolcli

import (
	"strings"
	"testing"
)

const field = "machine.install.image"

// What `talosctl get machineconfig -o yaml` prints for one machine: the spec
// is the config's text, a stream of documents.
const oneNode = `node: 10.0.0.2
metadata:
    namespace: config
    type: MachineConfigs.config.talos.dev
    id: v1alpha1
    version: 3
spec: |
    version: v1alpha1
    machine:
        type: controlplane
        token: secret-token
        ca:
            crt: LS0tLS1CRUdJTi
            key: LS0tLS1CRUdJTi1QUklWQVRF
        install:
            disk: /dev/vda
            image: factory.talos.dev/nocloud-installer/88d1f7a5:v1.13.0
            wipe: false
    cluster:
        secret: also-secret
    ---
    apiVersion: v1alpha1
    kind: ExtensionServiceConfig
    name: tailscale
    environment:
        - TS_AUTHKEY=tskey-secret
`

func TestExtractReadsOnlyTheNamedScalars(t *testing.T) {
	got, err := Extract(FormatYAMLDocuments, oneNode, []string{field, "machine.install.wipe", "machine.install.missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Node != "10.0.0.2" || got[0].Error != "" {
		t.Fatalf("got %+v", got)
	}
	if v := got[0].Fields[field]; v != "factory.talos.dev/nocloud-installer/88d1f7a5:v1.13.0" {
		t.Errorf("%s = %v", field, v)
	}
	if v, ok := got[0].Fields["machine.install.wipe"].(bool); !ok || v {
		t.Errorf("wipe = %#v", got[0].Fields["machine.install.wipe"])
	}
	if _, ok := got[0].Fields["machine.install.missing"]; ok {
		t.Error("a missing field should be absent")
	}
	if len(got[0].Fields) != 2 {
		t.Errorf("fields = %v", got[0].Fields)
	}
}

func TestExtractRefusesWhatIsNotASingleValue(t *testing.T) {
	got, err := Extract(FormatYAMLDocuments, oneNode, []string{"machine.ca", "machine.install", field})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Fields["machine.ca"]; ok {
		t.Error("a mapping must not be handed back")
	}
	if !strings.Contains(got[0].Error, "machine.ca is not a single value") || !strings.Contains(got[0].Error, "machine.install is not") {
		t.Errorf("error = %q", got[0].Error)
	}
	if strings.Contains(got[0].Error, "LS0t") || strings.Contains(got[0].Error, "secret") {
		t.Errorf("the error carries the config: %q", got[0].Error)
	}
	if got[0].Fields[field] == nil {
		t.Error("the fields that are scalars are still read")
	}
	// A list, too, and a later document's field.
	got, _ = Extract(FormatYAMLDocuments, oneNode, []string{"environment", "kind"})
	if _, ok := got[0].Fields["environment"]; ok || got[0].Fields["kind"] != "ExtensionServiceConfig" {
		t.Errorf("got %+v", got[0])
	}
}

func TestExtractSeveralNodesAndSpecsAsObjects(t *testing.T) {
	out := oneNode + "---\n" + strings.ReplaceAll(oneNode, "10.0.0.2", "10.0.0.3") + `---
node: 10.0.0.4
metadata:
    id: v1alpha1
spec:
    machine:
        install:
            image: ghcr.io/siderolabs/installer:v1.12.0
---
node: 10.0.0.5
metadata:
    id: v1alpha1
spec:
    machine:
        type: worker
`
	got, err := Extract(FormatYAMLDocuments, out, []string{field})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d resources: %+v", len(got), got)
	}
	want := map[string]any{
		"10.0.0.2": "factory.talos.dev/nocloud-installer/88d1f7a5:v1.13.0",
		"10.0.0.3": "factory.talos.dev/nocloud-installer/88d1f7a5:v1.13.0",
		"10.0.0.4": "ghcr.io/siderolabs/installer:v1.12.0",
		"10.0.0.5": nil,
	}
	for _, r := range got {
		if r.Fields[field] != want[r.Node] {
			t.Errorf("%s: %v, want %v", r.Node, r.Fields[field], want[r.Node])
		}
	}
}

func TestExtractFirstDocumentWins(t *testing.T) {
	out := "node: a\nspec: |\n    kind: First\n    machine: {install: {image: one}}\n    ---\n    machine: {install: {image: two}}\n"
	got, err := Extract(FormatYAMLDocuments, out, []string{field})
	if err != nil || got[0].Fields[field] != "one" {
		t.Errorf("got %+v, %v", got, err)
	}
	// A null is as good as absent: the next document is asked.
	out = "node: a\nspec: |\n    machine: {install: {image: null}}\n    ---\n    machine: {install: {image: two}}\n"
	if got, _ := Extract(FormatYAMLDocuments, out, []string{field}); got[0].Fields[field] != "two" {
		t.Errorf("got %+v", got)
	}
}

func TestExtractGarbage(t *testing.T) {
	for name, out := range map[string]string{
		"text":     "NODE  NAMESPACE  TYPE\n10.0.0.2  config  MachineConfig: {",
		"bad yaml": "a: [\n",
		"unclosed": "spec: \"machine",
	} {
		if _, err := Extract(FormatYAMLDocuments, out, []string{field}); err == nil {
			t.Errorf("%s should be an error", name)
		}
	}
	// A parse error inside a spec is said in the app's words, not the parser's.
	got, err := Extract(FormatYAMLDocuments, "node: a\nspec: \"secret: [\"\n", []string{field})
	if err != nil || !strings.Contains(got[0].Error, "not YAML") || strings.Contains(got[0].Error, "secret") {
		t.Errorf("got %+v, %v", got, err)
	}
	// Documents without a spec are not resources; empty output is no answer.
	for _, out := range []string{"", "just text", "node: a\nmetadata: {}\n", "- a\n- b\n", "{}"} {
		got, err := Extract(FormatYAMLDocuments, out, []string{field})
		if err != nil || len(got) != 0 {
			t.Errorf("%q: %+v, %v", out, got, err)
		}
	}
	if _, err := Extract("json", oneNode, []string{field}); err == nil {
		t.Error("an unknown format should be an error")
	}
	if _, err := Extract(FormatYAMLDocuments, strings.Repeat("a", maxExtractInput+1), []string{field}); err == nil {
		t.Error("too much output should be an error")
	}
	long := "node: a\nspec: {machine: {install: {image: " + strings.Repeat("x", maxValueLength+1) + "}}}\n"
	if got, _ := Extract(FormatYAMLDocuments, long, []string{field}); got[0].Fields[field] != nil || got[0].Error == "" {
		t.Errorf("a long value should not be handed back: %+v", got[0].Error)
	}
}

func TestExtractFollowsAliasesButStaysScalar(t *testing.T) {
	out := "node: a\nspec:\n  base: &b {image: x}\n  machine:\n    install: *b\n    other: &s y\n    alias: *s\n"
	got, err := Extract(FormatYAMLDocuments, out, []string{field, "machine.alias", "machine.install"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Fields[field] != "x" || got[0].Fields["machine.alias"] != "y" || got[0].Fields["machine.install"] != nil {
		t.Errorf("got %+v", got[0])
	}
}
