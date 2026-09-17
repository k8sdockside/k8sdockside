// Command plugincheck reads a K8s Dockside plugin's folder exactly as the app
// would once it is installed, and says whether it loads -- and if not, every
// reason why. It is meant for a plugin's own repository, run by hand or in CI:
//
//	go run github.com/rogerwesterbo/k8sdockside/cmd/plugincheck@latest .
//
// It checks what the app checks on load and nothing less: the JSON, every
// field's name and type, the kinds, the queries, the actions' requests, the
// icons, the links, the versions, and that every page a view, panel or
// overview opens is in the ui folder. What it cannot check is what only a
// cluster can answer -- whether the kinds are served there.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rogerwesterbo/k8sdockside/internal/plugins"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plugincheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	appVersion := flags.String("app", "", "check as this release of the app would, e.g. 0.0.15 (default: as a development build, which admits any minAppVersion)")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: plugincheck [-app version] [folder ...]")
		_, _ = fmt.Fprintln(stderr, "Checks each folder (default: the current one) the way K8s Dockside reads a plugin installed from it.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	folders := flags.Args()
	if len(folders) == 0 {
		folders = []string{"."}
	}

	failed := false
	for _, folder := range folders {
		loaded, problems := plugins.CheckFolder(*appVersion, folder)
		for _, p := range loaded {
			_, _ = fmt.Fprintf(stdout, "ok  %s\n", describe(p))
			for _, line := range details(p) {
				_, _ = fmt.Fprintf(stdout, "    %s\n", line)
			}
		}
		for _, problem := range problems {
			failed = true
			_, _ = fmt.Fprintf(stdout, "FAIL %s\n", problem.Path)
			for line := range strings.SplitSeq(problem.Message, "\n") {
				_, _ = fmt.Fprintf(stdout, "    %s\n", line)
			}
		}
	}
	if failed {
		return 1
	}
	return 0
}

// describe is the plugin's one-line summary: its id, name and version.
func describe(p plugins.Plugin) string {
	s := p.ID
	if p.Version != "" {
		s += " " + p.Version
	}
	if p.Name != p.ID {
		s += " (" + p.Name + ")"
	}
	return s
}

// details are what the plugin adds, in the words the settings view uses.
func details(p plugins.Plugin) []string {
	var out []string
	var parts []string
	custom := 0
	for _, v := range p.Views {
		if v.Type == plugins.ViewCustom {
			custom++
		}
	}
	parts = append(parts, plural(len(p.Views), "view"))
	if custom > 0 {
		parts = append(parts, fmt.Sprintf("%d of its own", custom))
	}
	if len(p.Cards) > 0 {
		parts = append(parts, plural(len(p.Cards), "card"))
	}
	if len(p.Charts) > 0 {
		parts = append(parts, plural(len(p.Charts), "chart"))
	}
	if len(p.Actions) > 0 {
		parts = append(parts, plural(len(p.Actions), "action"))
	}
	if len(p.Sections) > 0 {
		parts = append(parts, plural(len(p.Sections), "panel"))
	}
	if p.Overview != nil {
		parts = append(parts, "its own overview ("+p.Overview.Entry+")")
	}
	out = append(out, strings.Join(parts, ", "))

	if p.UI != nil {
		line := "its pages read no kinds"
		if len(p.UI.Readable) > 0 {
			line = fmt.Sprintf("its pages read %s", strings.Join(p.UI.Readable, ", "))
		}
		if p.UI.Write {
			line += ", and may ask to change them"
		}
		out = append(out, line)
		if p.UI.Registries {
			out = append(out, "its pages ask registries about the images the cluster runs")
		}
		for _, svc := range p.UI.Services {
			out = append(out, fmt.Sprintf("its pages call %s (%s) at %s", svc.Label, svc.Describe(), strings.Join(svc.Paths, ", ")))
		}
	}
	if p.MinAppVersion != "" {
		out = append(out, "needs K8s Dockside "+strings.TrimPrefix(p.MinAppVersion, "v")+" or newer")
	} else {
		out = append(out, "no minAppVersion: an older app will try to load it, and may refuse it field by field")
	}
	for _, builtin := range plugins.Builtin() {
		if builtin.ID == p.ID {
			out = append(out, "replaces the built-in "+builtin.Name+" plugin when installed")
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
