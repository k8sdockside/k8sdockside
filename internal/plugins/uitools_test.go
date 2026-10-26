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

func talosLike() UITool {
	return UITool{
		ID: "talosctl", Command: "talosctl",
		Files: []UIToolFile{{ID: "config", Flag: "--talosconfig"}},
		Read:  []string{"get machinestatus **", "version **"},
		Run:   []string{"upgrade **"},
		Extract: []UIToolExtract{{
			Command: "get machineconfig **",
			Extract: UIToolExtraction{Format: ExtractYAMLDocuments, Output: []string{"-o", "yaml"}, Fields: []string{"machine.install.image"}},
		}},
	}
}

// A manifest writes an extraction among its read patterns; the app keeps the
// two apart, and refuses what it does not know inside one.
func TestReadEntriesArePatternsOrExtractions(t *testing.T) {
	raw := `{"id":"talosctl","command":"talosctl","read":["version **",
		{"command":"get machineconfig **","extract":{"format":"yaml-documents","output":["-o","yaml"],"fields":["machine.install.image"]}}]}`
	var tool UITool
	if err := decodeStrict([]byte(raw), &tool); err != nil {
		t.Fatal(err)
	}
	if len(tool.Read) != 1 || tool.Read[0] != "version **" {
		t.Errorf("Read = %q", tool.Read)
	}
	if len(tool.Extract) != 1 || tool.Extract[0].Command != "get machineconfig **" || tool.Extract[0].Extract.Fields[0] != "machine.install.image" {
		t.Errorf("Extract = %+v", tool.Extract)
	}
	for name, bad := range map[string]string{
		"unknown member": `{"id":"x","command":"x","read":[{"command":"a","extract":{"format":"yaml-documents","fields":["a"],"all":true}}]}`,
		"a number":       `{"id":"x","command":"x","read":[1]}`,
		"top-level":      `{"id":"x","command":"x","extract":[]}`,
	} {
		var tool UITool
		if err := decodeStrict([]byte(bad), &tool); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestExtractionIsTheOnlyWayToItsCommand(t *testing.T) {
	tool := talosLike()
	args := []string{"get", "machineconfig", "--nodes", "10.0.0.2"}
	for _, how := range []string{ToolRead, ToolRun, ToolInteractive} {
		if err := tool.Allows(how, args); err == nil || !strings.Contains(err.Error(), "machine.install.image") {
			t.Errorf("%s of an extract command: %v", how, err)
		}
	}
	ex, err := tool.Extraction(args)
	if err != nil || ex.Fields[0] != "machine.install.image" {
		t.Fatalf("Extraction = %+v, %v", ex, err)
	}
	refused := map[string][]string{
		"another command":     {"get", "machinestatus"},
		"the file flag":       {"get", "machineconfig", "--talosconfig", "/x"},
		"the output flag":     {"get", "machineconfig", "-o", "jsonpath={.spec}"},
		"output attached":     {"get", "machineconfig", "-ojson"},
		"output with =":       {"get", "machineconfig", "-o=json"},
		"end of flags":        {"get", "machineconfig", "--", "-o"},
		"no arguments at all": {},
	}
	for name, args := range refused {
		if _, err := tool.Extraction(args); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	// Even a tool whose plain patterns were never validated: the extraction wins.
	wide := tool
	wide.Read = append(wide.Read, "get **")
	if err := wide.Allows(ToolRead, args); err == nil {
		t.Error("a broader read pattern must not reach an extract command")
	}
	if err := wide.Allows(ToolRead, []string{"get", "links"}); err != nil {
		t.Errorf("get links: %v", err)
	}
}

func TestValidateExtractions(t *testing.T) {
	if out, err := validateTools("p", []UITool{talosLike()}); err != nil || len(out[0].Extract) != 1 {
		t.Fatalf("validateTools = %+v, %v", out, err)
	}
	onlyExtract := talosLike()
	onlyExtract.Read, onlyExtract.Run = nil, nil
	if _, err := validateTools("p", []UITool{onlyExtract}); err != nil {
		t.Errorf("a tool with only an extraction: %v", err)
	}
	for _, good := range []string{"machine.install.image", "a", "cluster.network.cni.name", "x_y.z-w"} {
		if err := CheckFieldPath(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", ".", "a..b", "a.", ".a", "a[0]", "a.*", "a b", "1a", "a.b.c.d.e.f.g.h.i", strings.Repeat("a", 300)} {
		if err := CheckFieldPath(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	with := func(change func(*UITool)) UITool {
		tool := talosLike()
		tool.Extract = []UIToolExtract{tool.Extract[0]}
		tool.Extract[0].Extract.Fields = []string{"machine.install.image"}
		change(&tool)
		return tool
	}
	bad := map[string]UITool{
		"a broader read":       with(func(t *UITool) { t.Read = append(t.Read, "get **") }),
		"a wildcard read":      with(func(t *UITool) { t.Read = append(t.Read, "get * **") }),
		"a two-word read":      with(func(t *UITool) { t.Read = append(t.Read, "get *") }),
		"the same read":        with(func(t *UITool) { t.Read = append(t.Read, "get machineconfig") }),
		"a run":                with(func(t *UITool) { t.Run = append(t.Run, "get machineconfig **") }),
		"an interactive":       with(func(t *UITool) { t.Interactive = append(t.Interactive, "** ") }),
		"an unknown format":    with(func(t *UITool) { t.Extract[0].Extract.Format = "text" }),
		"no fields":            with(func(t *UITool) { t.Extract[0].Extract.Fields = nil }),
		"a bad field":          with(func(t *UITool) { t.Extract[0].Extract.Fields = []string{"machine.*"} }),
		"a field twice":        with(func(t *UITool) { t.Extract[0].Extract.Fields = []string{"a", "a"} }),
		"too many fields":      with(func(t *UITool) { t.Extract[0].Extract.Fields = strings.Fields(strings.Repeat("f ", 17)) }),
		"output not a flag":    with(func(t *UITool) { t.Extract[0].Extract.Output = []string{"yaml"} }),
		"output passes a file": with(func(t *UITool) { t.Extract[0].Extract.Output = []string{"--talosconfig", "/x"} }),
		"a wildcard command":   with(func(t *UITool) { t.Extract[0].Command = "** " }),
		"an inner ** command":  with(func(t *UITool) { t.Extract[0].Command = "get ** x" }),
	}
	for name, tool := range bad {
		if _, err := validateTools("p", []UITool{tool}); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	// Patterns that cannot match the same command stand side by side.
	fine := with(func(t *UITool) { t.Read = append(t.Read, "get machinestatus **", "get -o *", "logs * **") })
	if _, err := validateTools("p", []UITool{fine}); err != nil {
		t.Errorf("non-overlapping patterns: %v", err)
	}
}

func TestPatternsOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"get **", "get machineconfig **", true},
		{"get *", "get machineconfig **", true},
		{"get *", "get machineconfig", true},
		{"get * x", "get machineconfig **", true},
		{"get machinestatus **", "get machineconfig **", false},
		{"get", "get machineconfig **", false},
		{"get", "get **", true},
		{"get * -o", "get * **", true},
		{"get -o", "get * **", false},
		{"logs * **", "get machineconfig **", false},
		{"get a b", "get a", false},
	} {
		if got := patternsOverlap(strings.Fields(tc.a), strings.Fields(tc.b)); got != tc.want {
			t.Errorf("overlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := patternsOverlap(strings.Fields(tc.b), strings.Fields(tc.a)); got != tc.want {
			t.Errorf("overlap(%q, %q) = %v, want %v", tc.b, tc.a, got, tc.want)
		}
	}
}
