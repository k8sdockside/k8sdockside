package plugins

// The command line tools a plugin's own views may run on the user's machine.
//
// Some solutions are not managed through the Kubernetes API at all -- a
// machine OS with its own API, a storage system with its own CLI -- and the
// tool that speaks to them is already on the user's machine, set up with the
// user's own credentials file. A view has no network and no files, so the app
// runs the tool for it: the manifest names the binary, the files it needs and
// exactly which commands may be run, and the user sees all of that on the
// plugin's card before any of it can happen.
//
// Three lists, three levels of trust:
//
//   - read: run whenever a view asks, its output handed back. For commands
//     that only look. A read entry may instead be an extraction -- an object
//     naming a command and the fields of its output the view may have (see
//     UIToolExtract): the app runs it, parses the output itself, and hands
//     back only those fields, never the output.
//   - run: shown to the user as the exact command line, and run in a console
//     in the dock only if they say yes. For commands that change something.
//   - interactive: opened in the user's own terminal, for a command that draws
//     a full screen. Opening a window is itself something the user sees.
//
// A pattern is words: a literal word must be that argument, `*` is any one
// argument that is not a flag, and `**`, only last, is whatever follows.

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// UITool is one command line tool a plugin's views may run.
type UITool struct {
	// ID is what a view names the tool by.
	ID string `json:"id"`
	// Label is how the app names it; defaults to the command.
	Label string `json:"label,omitzero"`
	// Command is the program's name, looked for on PATH and where package
	// managers put it. A name, never a path.
	Command string `json:"command"`
	// Version are the arguments that make the tool say its version, e.g.
	// ["version", "--client"]. The first vX.Y.Z in the output is shown.
	Version []string `json:"version,omitzero"`
	// Files are the files the tool reads, chosen by the user per cluster.
	Files []UIToolFile `json:"files,omitzero"`
	// Read, Run and Interactive are the patterns of what may be run, and how.
	// Read holds the plain patterns only: the manifest's read entries that
	// are objects are Extract.
	Read        []string `json:"read,omitzero"`
	Run         []string `json:"run,omitzero"`
	Interactive []string `json:"interactive,omitzero"`
	// Extract are the read entries that are extractions: commands whose
	// output the app parses itself, handing a view only the named fields.
	// Written in the manifest among "read"; kept apart here so nothing that
	// reads Read can mistake one for a plain pattern.
	Extract []UIToolExtract `json:"extract,omitzero"`
}

// UIToolExtract is a read entry whose output never reaches a view: the app
// runs the command, parses what it wrote, and hands back only the scalar
// values at the named field paths. For a command whose output holds secrets
// next to the one thing a view needs -- a machine config, say, and the
// installer image in it.
//
//	{ "command": "get machineconfig **",
//	  "extract": { "format": "yaml-documents", "output": ["-o", "yaml"],
//	               "fields": ["machine.install.image"] } }
//
// A command matching an extraction may not be run any other way: a plain
// pattern that could match the same command is refused when the manifest is
// read, and at run time the extraction wins over every other list.
type UIToolExtract struct {
	// Command is a pattern, as in the other lists.
	Command string `json:"command"`
	// Extract is what is taken from the output.
	Extract UIToolExtraction `json:"extract"`
}

// UIToolExtraction is how an extraction reads a command's output.
type UIToolExtraction struct {
	// Format is how the output is parsed: ExtractYAMLDocuments.
	Format string `json:"format"`
	// Output are arguments the app adds after the view's own, to fix the
	// format the tool prints -- ["-o", "yaml"]. The view may not give these
	// flags itself, nor "--", and coming last they are the tool's last word.
	Output []string `json:"output,omitzero"`
	// Fields are dotted paths, e.g. "machine.install.image". Only a scalar
	// -- a string, number or true/false -- is ever handed back.
	Fields []string `json:"fields"`
}

// ExtractYAMLDocuments is output that is one or more YAML documents, each a
// resource with a "spec": a document (a mapping), or text that is itself a
// stream of YAML documents -- `talosctl get ... -o yaml`. The fields are
// looked for in each resource's spec, the first document holding one wins,
// and the answer is one entry per resource, by its "node".
const ExtractYAMLDocuments = "yaml-documents"

// uiToolManifest is UITool as a manifest writes it: read entries are
// patterns or extractions.
type uiToolManifest struct {
	ID          string          `json:"id"`
	Label       string          `json:"label,omitzero"`
	Command     string          `json:"command"`
	Version     []string        `json:"version,omitzero"`
	Files       []UIToolFile    `json:"files,omitzero"`
	Read        []toolReadEntry `json:"read,omitzero"`
	Run         []string        `json:"run,omitzero"`
	Interactive []string        `json:"interactive,omitzero"`
}

// toolReadEntry is one entry of a manifest's "read": a pattern or an
// extraction.
type toolReadEntry struct {
	pattern string
	extract *UIToolExtract
}

// UnmarshalJSONFrom reads a pattern string or an extraction object.
func (e *toolReadEntry) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case '"':
		return json.UnmarshalDecode(dec, &e.pattern)
	case '{':
		e.extract = &UIToolExtract{}
		return json.UnmarshalDecode(dec, e.extract)
	}
	return errors.New(`a read entry is a pattern, or { "command": ..., "extract": { ... } }`)
}

// UnmarshalJSONFrom reads a tool as a manifest writes it, sorting its read
// entries into Read and Extract. The decoder's options -- refusing unknown
// members -- carry on into it.
func (t *UITool) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var m uiToolManifest
	if err := json.UnmarshalDecode(dec, &m); err != nil {
		return err
	}
	*t = UITool{
		ID: m.ID, Label: m.Label, Command: m.Command, Version: m.Version, Files: m.Files,
		Run: m.Run, Interactive: m.Interactive,
	}
	for _, e := range m.Read {
		if e.extract != nil {
			t.Extract = append(t.Extract, *e.extract)
		} else {
			t.Read = append(t.Read, e.pattern)
		}
	}
	return nil
}

// UIToolFile is a file a tool reads -- a credentials file, typically -- which
// the user chooses per cluster and the app passes to every command. The views
// never see where it is, and may not pass the flag themselves.
type UIToolFile struct {
	ID    string `json:"id"`
	Label string `json:"label,omitzero"`
	// Flag passes the file to the tool, e.g. "--config-file".
	Flag string `json:"flag,omitzero"`
	// Env passes it in the environment as well, e.g. "TOOL_CONFIG".
	Env string `json:"env,omitzero"`
	// Default is where it is when the user has not said; ~ is their home.
	Default string `json:"default,omitzero"`
}

// How a command is run, by which list allowed it.
const (
	ToolRead        = "read"
	ToolRun         = "run"
	ToolInteractive = "interactive"
	// ToolExtract is a read entry that is an extraction (UIToolExtract).
	ToolExtract = "extract"
)

var (
	toolIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	toolCommandPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	toolFlagPattern    = regexp.MustCompile(`^--?[A-Za-z0-9][A-Za-z0-9-]*$`)
	toolEnvPattern     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// maxToolPatterns bounds each list: a manifest that needs more is one that
// should be allowing less.
const maxToolPatterns = 128

// Bounds on extractions: each is a line on the plugin's card.
const (
	maxExtracts     = 16
	maxExtractField = 16
	maxFieldDepth   = 8
	maxFieldLength  = 200
	maxOutputArgs   = 8
)

// fieldSegment is one step of a field path: an identifier, as YAML and JSON
// keys in configs are written.
var fieldSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// CheckFieldPath refuses a field path that is not dotted identifiers.
func CheckFieldPath(path string) error {
	if path == "" || len(path) > maxFieldLength {
		return fmt.Errorf("a field path is 1 to %d characters", maxFieldLength)
	}
	parts := strings.Split(path, ".")
	if len(parts) > maxFieldDepth {
		return fmt.Errorf("a field path is at most %d steps deep", maxFieldDepth)
	}
	for _, p := range parts {
		if !fieldSegment.MatchString(p) {
			return fmt.Errorf("%q is not a step of a field path; use dotted names like machine.install.image", p)
		}
	}
	return nil
}

func validateExtract(t UITool, e UIToolExtract) (UIToolExtract, error) {
	e.Command = strings.Join(strings.Fields(e.Command), " ")
	if err := checkToolPattern(e.Command); err != nil {
		return e, fmt.Errorf("extract command %q: %w", e.Command, err)
	}
	if e.Extract.Format != ExtractYAMLDocuments {
		return e, fmt.Errorf("extract command %q: the format %q is not one the app reads; use %q", e.Command, e.Extract.Format, ExtractYAMLDocuments)
	}
	if len(e.Extract.Output) > maxOutputArgs {
		return e, fmt.Errorf("extract command %q: at most %d output arguments", e.Command, maxOutputArgs)
	}
	for i, a := range e.Extract.Output {
		if a == "" || a == "--" || strings.ContainsAny(a, " \t\n") || (i == 0 && !strings.HasPrefix(a, "-")) {
			return e, fmt.Errorf("extract command %q: output %q should be flags and their values, e.g. [\"-o\", \"yaml\"]", e.Command, e.Extract.Output)
		}
	}
	if err := t.PassesNoFile(e.Extract.Output); err != nil {
		return e, fmt.Errorf("extract command %q: %w", e.Command, err)
	}
	if len(e.Extract.Fields) == 0 || len(e.Extract.Fields) > maxExtractField {
		return e, fmt.Errorf("extract command %q: name 1 to %d fields", e.Command, maxExtractField)
	}
	seen := map[string]bool{}
	for _, f := range e.Extract.Fields {
		if err := CheckFieldPath(f); err != nil {
			return e, fmt.Errorf("extract command %q: field %q: %w", e.Command, f, err)
		}
		if seen[f] {
			return e, fmt.Errorf("extract command %q names the field %q twice", e.Command, f)
		}
		seen[f] = true
	}
	// A plain pattern that could match the same command would hand back the
	// whole output the extraction exists to keep from the view.
	for _, list := range []string{ToolRead, ToolRun, ToolInteractive} {
		for _, p := range t.Patterns(list) {
			if patternsOverlap(strings.Fields(p), strings.Fields(e.Command)) {
				return e, fmt.Errorf("the %s pattern %q could run %q, which may only be read for its fields; narrow it so the two cannot match the same command", list, p, e.Command)
			}
		}
	}
	return e, nil
}

// patternsOverlap reports whether some arguments match both patterns.
func patternsOverlap(a, b []string) bool {
	for i := 0; ; i++ {
		if (i < len(a) && a[i] == "**") || (i < len(b) && b[i] == "**") {
			return true
		}
		if i >= len(a) || i >= len(b) {
			return len(a) == len(b)
		}
		x, y := a[i], b[i]
		switch {
		case x == "*" && y == "*":
		case x == "*":
			if y == "" || strings.HasPrefix(y, "-") {
				return false
			}
		case y == "*":
			if x == "" || strings.HasPrefix(x, "-") {
				return false
			}
		case x != y:
			return false
		}
	}
}

func validateTools(pluginID string, tools []UITool) ([]UITool, error) {
	if len(tools) > 4 {
		return nil, fmt.Errorf("plugin %q declares %d tools; at most 4", pluginID, len(tools))
	}
	seen := map[string]bool{}
	for i, t := range tools {
		t.ID = strings.TrimSpace(t.ID)
		t.Command = strings.TrimSpace(t.Command)
		if !toolIDPattern.MatchString(t.ID) {
			return nil, fmt.Errorf("plugin %q has a tool id %q; use lowercase letters, digits and dashes", pluginID, t.ID)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("plugin %q declares the tool %q twice", pluginID, t.ID)
		}
		seen[t.ID] = true
		if !toolCommandPattern.MatchString(t.Command) {
			return nil, fmt.Errorf("plugin %q, tool %q: the command %q must be a program's name, not a path", pluginID, t.ID, t.Command)
		}
		if t.Label = strings.TrimSpace(t.Label); t.Label == "" {
			t.Label = t.Command
		}
		files := map[string]bool{}
		for j, f := range t.Files {
			f.ID, f.Flag, f.Env = strings.TrimSpace(f.ID), strings.TrimSpace(f.Flag), strings.TrimSpace(f.Env)
			if !toolIDPattern.MatchString(f.ID) || files[f.ID] {
				return nil, fmt.Errorf("plugin %q, tool %q: file id %q is not valid or is repeated", pluginID, t.ID, f.ID)
			}
			files[f.ID] = true
			if f.Flag == "" && f.Env == "" {
				return nil, fmt.Errorf("plugin %q, tool %q: file %q needs a flag or an env to pass it by", pluginID, t.ID, f.ID)
			}
			if f.Flag != "" && !toolFlagPattern.MatchString(f.Flag) {
				return nil, fmt.Errorf("plugin %q, tool %q: %q is not a flag", pluginID, t.ID, f.Flag)
			}
			if f.Env != "" && !toolEnvPattern.MatchString(f.Env) {
				return nil, fmt.Errorf("plugin %q, tool %q: %q is not an environment variable name", pluginID, t.ID, f.Env)
			}
			if f.Label = strings.TrimSpace(f.Label); f.Label == "" {
				f.Label = f.ID
			}
			t.Files[j] = f
		}
		for _, list := range []struct {
			name     string
			patterns []string
		}{{ToolRead, t.Read}, {ToolRun, t.Run}, {ToolInteractive, t.Interactive}} {
			if len(list.patterns) > maxToolPatterns {
				return nil, fmt.Errorf("plugin %q, tool %q: more than %d %s patterns", pluginID, t.ID, maxToolPatterns, list.name)
			}
			for _, p := range list.patterns {
				if err := checkToolPattern(p); err != nil {
					return nil, fmt.Errorf("plugin %q, tool %q: %s pattern %q: %w", pluginID, t.ID, list.name, p, err)
				}
			}
		}
		if len(t.Extract) > maxExtracts {
			return nil, fmt.Errorf("plugin %q, tool %q: more than %d extract entries", pluginID, t.ID, maxExtracts)
		}
		for j, e := range t.Extract {
			e, err := validateExtract(t, e)
			if err != nil {
				return nil, fmt.Errorf("plugin %q, tool %q: %w", pluginID, t.ID, err)
			}
			t.Extract[j] = e
		}
		if len(t.Read)+len(t.Run)+len(t.Interactive)+len(t.Extract) == 0 {
			return nil, fmt.Errorf("plugin %q, tool %q allows no commands; give read, run or interactive patterns", pluginID, t.ID)
		}
		tools[i] = t
	}
	return tools, nil
}

func checkToolPattern(pattern string) error {
	words := strings.Fields(pattern)
	if len(words) == 0 {
		return fmt.Errorf("it is empty")
	}
	// The first word is the command itself: a pattern that begins with a
	// wildcard would allow every command the tool has.
	if words[0] == "*" || words[0] == "**" {
		return fmt.Errorf("it must begin with a command, not a wildcard")
	}
	for i, w := range words {
		if w == "**" && i != len(words)-1 {
			return fmt.Errorf("** may only come last")
		}
	}
	return nil
}

// Tool finds one of the plugin's tools by id.
func (p Plugin) Tool(id string) (UITool, bool) {
	if p.UI == nil {
		return UITool{}, false
	}
	i := slices.IndexFunc(p.UI.Tools, func(t UITool) bool { return t.ID == id })
	if i < 0 {
		return UITool{}, false
	}
	return p.UI.Tools[i], true
}

// Patterns are the tool's patterns for one way of running.
func (t UITool) Patterns(how string) []string {
	switch how {
	case ToolRead:
		return t.Read
	case ToolRun:
		return t.Run
	case ToolInteractive:
		return t.Interactive
	case ToolExtract:
		out := make([]string, len(t.Extract))
		for i, e := range t.Extract {
			out[i] = e.Command
		}
		return out
	}
	return nil
}

// Allows reports whether the arguments may be run this way. Arguments that
// would pass one of the tool's files are refused whatever the patterns say:
// which file is used is the user's choice, not the view's.
func (t UITool) Allows(how string, args []string) error {
	if err := t.PassesNoFile(args); err != nil {
		return err
	}
	// An extraction wins over every other list: its command's output never
	// reaches a view, nor a console a view opened.
	if e, ok := t.extractFor(args); ok {
		if how != ToolExtract {
			return fmt.Errorf("%s %s may only be read for %s, never as it is", t.Command, strings.Join(args, " "), strings.Join(e.Extract.Fields, ", "))
		}
		return nil
	}
	for _, p := range t.Patterns(how) {
		if matchTool(strings.Fields(p), args) {
			return nil
		}
	}
	return fmt.Errorf("%s %s is not among the commands this plugin may %s", t.Command, strings.Join(args, " "), verb(how))
}

// extractFor finds the extraction whose command the arguments match.
func (t UITool) extractFor(args []string) (UIToolExtract, bool) {
	for _, e := range t.Extract {
		if matchTool(strings.Fields(e.Command), args) {
			return e, true
		}
	}
	return UIToolExtract{}, false
}

// Extraction checks arguments a view asks to read for their fields, and says
// how: the extraction they match. Beyond Allows, the view may not end the
// flags ("--") nor give the flags the extraction adds itself.
func (t UITool) Extraction(args []string) (UIToolExtraction, error) {
	if err := t.Allows(ToolExtract, args); err != nil {
		return UIToolExtraction{}, err
	}
	e, _ := t.extractFor(args)
	for _, a := range args {
		if a == "--" {
			return UIToolExtraction{}, fmt.Errorf("-- may not be given to a command read for its fields")
		}
		for _, f := range e.Extract.Output {
			if !strings.HasPrefix(f, "-") {
				continue
			}
			short := len(f) == 2 && f[1] != '-'
			if a == f || strings.HasPrefix(a, f+"=") || (short && strings.HasPrefix(a, f)) {
				return UIToolExtraction{}, fmt.Errorf("%s is the app's to pass for a command read for its fields", f)
			}
		}
	}
	return e.Extract, nil
}

// PassesNoFile refuses arguments that would pass one of the tool's files.
func (t UITool) PassesNoFile(args []string) error {
	for _, a := range args {
		for _, f := range t.Files {
			if f.Flag != "" && (a == f.Flag || strings.HasPrefix(a, f.Flag+"=")) {
				return fmt.Errorf("%s is the app's to pass, not a view's", f.Flag)
			}
		}
	}
	return nil
}

func verb(how string) string {
	switch how {
	case ToolRun:
		return "ask to run"
	case ToolInteractive:
		return "open in a terminal"
	case ToolExtract:
		return "read for named fields"
	}
	return "run"
}

func matchTool(pattern, args []string) bool {
	for i, w := range pattern {
		if w == "**" {
			return true
		}
		if i >= len(args) {
			return false
		}
		switch w {
		case "*":
			if args[i] == "" || strings.HasPrefix(args[i], "-") {
				return false
			}
		default:
			if args[i] != w {
				return false
			}
		}
	}
	return len(args) == len(pattern)
}
