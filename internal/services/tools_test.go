package services

import (
	"context"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/toolcli"
)

func extractingPlugin() *PluginService {
	talos := plugins.Plugin{
		ID: "talos", Name: "Talos",
		UI: &plugins.UI{Tools: []plugins.UITool{{
			ID: "talosctl", Label: "talosctl", Command: "talosctl",
			Files: []plugins.UIToolFile{{ID: "config", Flag: "--talosconfig"}},
			Read:  []string{"version **"},
			Extract: []plugins.UIToolExtract{{
				Command: "get machineconfig **",
				Extract: plugins.UIToolExtraction{Format: plugins.ExtractYAMLDocuments, Output: []string{"-o", "yaml"}, Fields: []string{"machine.install.image"}},
			}},
		}}},
	}
	return &PluginService{cached: &plugins.Catalogue{Plugins: []plugins.Plugin{talos}}, found: map[string]foundService{}}
}

// A command read for its fields cannot be run for its output: not by exec,
// and not by asking to run it. Each refusal comes before anything runs.
func TestAnExtractCommandIsNeverRunForItsOutput(t *testing.T) {
	s := extractingPlugin()
	args := []string{"get", "machineconfig", "--nodes", "10.0.0.2", "-o", "yaml"}
	if _, err := s.ToolExec(context.Background(), "ctx", "talos", "talosctl", args); err == nil || !strings.Contains(err.Error(), "may only be read for machine.install.image") {
		t.Errorf("ToolExec: %v", err)
	}
	for _, how := range []string{plugins.ToolRun, plugins.ToolInteractive} {
		if _, err := s.ToolPlan("ctx", "talos", "talosctl", how, args); err == nil {
			t.Errorf("ToolPlan %s should be refused", how)
		}
	}
	for name, args := range map[string][]string{
		"the output flag": {"get", "machineconfig", "-o", "jsonpath={.spec}"},
		"end of flags":    {"get", "machineconfig", "--"},
		"the file":        {"get", "machineconfig", "--talosconfig", "/elsewhere"},
		"another command": {"get", "secrets"},
		"a plain read":    {"version"},
	} {
		if _, err := s.ToolExtract(context.Background(), "ctx", "talos", "talosctl", args); err == nil {
			t.Errorf("ToolExtract %s should be refused", name)
		}
	}
	if _, err := s.ToolExtract(context.Background(), "ctx", "acme", "talosctl", []string{"get", "machineconfig"}); err == nil {
		t.Error("another plugin's tool should be refused")
	}
}

// What a failed command said on stderr is not passed on.
func TestExtractFromSaysOnlyTheExitCode(t *testing.T) {
	tool := plugins.UITool{Label: "talosctl"}
	ex := plugins.UIToolExtraction{Format: plugins.ExtractYAMLDocuments, Fields: []string{"machine.install.image"}}
	_, err := extractFrom(tool, toolcli.Output{Stdout: "", Stderr: "rpc error: secret-stuff", Code: 1}, ex)
	if err == nil || !strings.Contains(err.Error(), "code 1") || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v", err)
	}
	_, err = extractFrom(tool, toolcli.Output{Stdout: "a: [", Stderr: "secret", Code: 1}, ex)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v", err)
	}
	if _, err := extractFrom(tool, toolcli.Output{Truncated: true}, ex); err == nil {
		t.Error("cut output should be an error")
	}
	got, err := extractFrom(tool, toolcli.Output{Stdout: "node: a\nspec: {machine: {install: {image: x}}}\n", Code: 1}, ex)
	if err != nil || len(got) != 1 || got[0].Fields["machine.install.image"] != "x" {
		t.Errorf("a partial answer is still an answer: %+v, %v", got, err)
	}
	got, err = extractFrom(tool, toolcli.Output{}, ex)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("no output is no resources: %#v, %v", got, err)
	}
}
