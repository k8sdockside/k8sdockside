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
//     that only look.
//   - run: shown to the user as the exact command line, and run in a console
//     in the dock only if they say yes. For commands that change something.
//   - interactive: opened in the user's own terminal, for a command that draws
//     a full screen. Opening a window is itself something the user sees.
//
// A pattern is words: a literal word must be that argument, `*` is any one
// argument that is not a flag, and `**`, only last, is whatever follows.

import (
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
	Read        []string `json:"read,omitzero"`
	Run         []string `json:"run,omitzero"`
	Interactive []string `json:"interactive,omitzero"`
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
		if len(t.Read)+len(t.Run)+len(t.Interactive) == 0 {
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
	for _, p := range t.Patterns(how) {
		if matchTool(strings.Fields(p), args) {
			return nil
		}
	}
	return fmt.Errorf("%s %s is not among the commands this plugin may %s", t.Command, strings.Join(args, " "), verb(how))
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
