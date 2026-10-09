package plugins

import (
	"strings"
	"testing"
)

func TestToolAllows(t *testing.T) {
	tool := UITool{
		ID: "acmectl", Command: "acmectl",
		Files: []UIToolFile{{ID: "config", Flag: "--acmeconfig"}},
		Read:  []string{"get status **", "logs * **", "version"},
		Run:   []string{"reboot **"},
	}
	allowed := [][]string{
		{"get", "status", "-o", "json", "--nodes", "10.0.0.1"},
		{"logs", "kubelet", "--tail", "20"},
		{"version"},
	}
	for _, args := range allowed {
		if err := tool.Allows(ToolRead, args); err != nil {
			t.Errorf("read %q: %v", args, err)
		}
	}
	refused := [][]string{
		{"get", "machineconfig"},
		{"logs", "--follow"},
		{"version", "--extra"},
		{"reboot"},
		{"get", "status", "--acmeconfig", "/elsewhere"},
		{"get", "status", "--acmeconfig=/elsewhere"},
		{},
	}
	for _, args := range refused {
		if err := tool.Allows(ToolRead, args); err == nil {
			t.Errorf("read %q should be refused", args)
		}
	}
	if err := tool.Allows(ToolRun, []string{"reboot", "--nodes", "x"}); err != nil {
		t.Errorf("run reboot: %v", err)
	}
}

func TestValidateTools(t *testing.T) {
	good := []UITool{{ID: "acmectl", Command: "acmectl", Files: []UIToolFile{{ID: "config", Flag: "--acmeconfig", Env: "ACMECONFIG"}}, Read: []string{"get **"}}}
	if out, err := validateTools("p", good); err != nil || out[0].Label != "acmectl" {
		t.Fatalf("validateTools = %+v, %v", out, err)
	}
	bad := map[string]UITool{
		"a path":         {ID: "x", Command: "/usr/bin/x", Read: []string{"a"}},
		"no patterns":    {ID: "x", Command: "x"},
		"wildcard verb":  {ID: "x", Command: "x", Read: []string{"** "}},
		"inner **":       {ID: "x", Command: "x", Read: []string{"a ** b"}},
		"bad flag":       {ID: "x", Command: "x", Read: []string{"a"}, Files: []UIToolFile{{ID: "f", Flag: "; rm"}}},
		"no way to pass": {ID: "x", Command: "x", Read: []string{"a"}, Files: []UIToolFile{{ID: "f"}}},
	}
	for name, tool := range bad {
		if _, err := validateTools("p", []UITool{tool}); err == nil {
			t.Errorf("%s should be refused", name)
		} else if !strings.Contains(err.Error(), `"p"`) {
			t.Errorf("%s: the error should name the plugin: %v", name, err)
		}
	}
}
