// Package helmcli runs the helm command line on behalf of the app.
//
// Reading a release needs nothing from helm: the record is a Secret, and
// internal/kube/helmdetail.go decodes it. Changing one is a different matter.
// An upgrade fetches a chart from a repository or a registry, renders its
// templates, and three-way merges the result against what is in the cluster;
// a rollback and an uninstall have to run the chart's hooks and keep the
// release history straight. That is Helm's job, and the alternative to asking
// helm to do it is either reimplementing it -- with the user's releases as the
// thing that breaks when the reimplementation is wrong -- or linking Helm's own
// library, which brings an OCI registry client, a container image stack and
// some two hundred modules into a binary that otherwise needs none of it.
//
// So: helm is asked. It is not required. A machine without it keeps everything
// in the drawer and loses the four buttons, which is said plainly rather than
// left to fail at the click.
package helmcli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/procattr"
)

// probeTimeout bounds asking helm what version it is. It is a local process
// answering a trivial question, so anything slower than this is a machine in
// trouble rather than a slow answer worth waiting for.
const probeTimeout = 5 * time.Second

// Tool is where helm is on this machine, and what it is.
//
// Found is answered rather than assumed because the interesting case is the
// common one: a Mac app launched from Finder inherits a PATH of four system
// directories, so the helm that works in the user's shell is invisible to it.
// Reason says which of the several ways that can be true this is.
type Tool struct {
	Found bool   `json:"found"`
	Path  string `json:"path"`
	// Version is helm's own `version --short`, e.g. "v3.16.2+g13654a5". Empty
	// when the binary was found but would not say.
	Version string `json:"version"`
	// Configured records that this came from the settings rather than from the
	// search, so the settings view can say which and offer to clear it.
	Configured bool `json:"configured"`
	// Reason is why there is no usable helm, in words that name the fix. Empty
	// when there is one.
	Reason string `json:"reason"`
}

// searchPath is where helm is looked for when it is not on PATH and the user
// has not said.
//
// These are the install locations of the package managers people actually use,
// and they are checked because PATH cannot be relied on: a desktop app is not
// started from a login shell, so on macOS it sees `/usr/bin:/bin:/usr/sbin:/sbin`
// and nothing a package manager has ever written to.
func searchPath() []string {
	home, _ := os.UserHomeDir()

	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/opt/homebrew/bin/helm", // Homebrew on Apple silicon
			"/usr/local/bin/helm",    // Homebrew on Intel, and manual installs
			"/opt/local/bin/helm",    // MacPorts
		}
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("ProgramFiles"), "helm", "helm.exe"),
			filepath.Join(os.Getenv("ChocolateyInstall"), "bin", "helm.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "helm.exe"),
		}
	default:
		candidates = []string{
			"/usr/local/bin/helm",
			"/usr/bin/helm",
			"/snap/bin/helm",
			"/home/linuxbrew/.linuxbrew/bin/helm",
		}
	}

	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "helm"))
		if runtime.GOOS != "windows" {
			candidates = append(candidates, filepath.Join(home, "bin", "helm"))
		}
	}
	return candidates
}

// executable reports whether a path is a file this process could run.
func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	// Windows decides by extension rather than by a mode bit, and Stat's mode
	// there says nothing useful; the file existing is as much as can be known
	// without running it.
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// Locate finds helm, preferring the path the user configured.
//
// A configured path that is not there is an error rather than a reason to fall
// back to the search: somebody who typed a path meant that helm, and quietly
// running a different one is how the wrong cluster gets upgraded.
func Locate(configured string) Tool {
	if path := strings.TrimSpace(configured); path != "" {
		if !executable(path) {
			return Tool{
				Configured: true,
				Path:       path,
				Reason: fmt.Sprintf(
					"helm was not found at %s -- the path in settings points at nothing runnable", path,
				),
			}
		}
		return described(path, true)
	}

	if path, err := exec.LookPath("helm"); err == nil {
		return described(path, false)
	}
	for _, candidate := range searchPath() {
		if executable(candidate) {
			return described(candidate, false)
		}
	}

	return Tool{Reason: notFoundReason()}
}

// notFoundReason says what to do about it, which depends on where the app is
// running: on a Mac the usual cause is not a missing helm at all.
func notFoundReason() string {
	const install = "install it, or set the path to it in Settings › Helm"
	if runtime.GOOS == "darwin" {
		return "helm was not found. An app launched from Finder does not see your shell's PATH, " +
			"so a helm installed by Homebrew is invisible to it -- run `which helm` in a terminal " +
			"and put that path in Settings › Helm, or " + install
	}
	return "helm was not found on your PATH -- " + install
}

// described asks a found helm what it is, so the settings view can show a
// version rather than only a path. A binary that will not answer is still
// reported as found: it may be a wrapper script that only implements the
// subcommands, and refusing to use it over a version string would be worse
// than not knowing the version.
func described(path string, configured bool) Tool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 -- the program is a path the user configured or one this
	// package found itself, and the argument is a constant.
	cmd := exec.CommandContext(ctx, path, "version", "--short")
	procattr.Hide(cmd)
	out, err := cmd.Output()
	version := ""
	if err == nil {
		version = strings.TrimSpace(string(out))
	}

	return Tool{Found: true, Path: path, Version: version, Configured: configured}
}

// Helm is a located helm, ready to be run against a cluster.
type Helm struct {
	path string
}

// New wraps a located helm. It refuses a Tool that names no usable binary, so
// that "helm is missing" is reported once, here, rather than as a failure to
// start a process further down.
func New(tool Tool) (*Helm, error) {
	if !tool.Found || tool.Path == "" {
		reason := tool.Reason
		if reason == "" {
			reason = notFoundReason()
		}
		return nil, fmt.Errorf("%s", reason)
	}
	return &Helm{path: tool.Path}, nil
}

// Path is where this helm is, for messages that name it.
func (h *Helm) Path() string { return h.path }

// validName is Helm's own rule for a release name, from
// chartutil.ValidateReleaseName. It is applied here for two reasons: a name
// helm would refuse is worth refusing before a process is started, and a name
// matching this pattern cannot begin with a dash, so nothing the frontend sends
// can arrive at helm as a flag.
var validName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// The lengths Kubernetes and Helm hold these to.
const (
	maxReleaseNameLen = 53
	maxNamespaceLen   = 253
)

func checkRelease(name string) error {
	if name == "" {
		return fmt.Errorf("no release was named")
	}
	if len(name) > maxReleaseNameLen || !validName.MatchString(name) {
		return fmt.Errorf("%q is not a Helm release name", name)
	}
	return nil
}

func checkNamespace(namespace string) error {
	if namespace == "" {
		return fmt.Errorf("no namespace was named")
	}
	if len(namespace) > maxNamespaceLen || !validName.MatchString(namespace) {
		return fmt.Errorf("%q is not a namespace", namespace)
	}
	return nil
}

// checkChart refuses a chart reference that would reach helm as a flag.
//
// It is deliberately the only rule: a chart is a repo alias and a name, an OCI
// URL, an http URL, a local directory or a packaged .tgz, and a pattern narrow
// enough to be worth writing would refuse one of those. Nothing here goes
// through a shell, so a leading dash is the whole of the risk.
func checkChart(chart string) error {
	if strings.TrimSpace(chart) == "" {
		return fmt.Errorf("no chart was named")
	}
	if strings.HasPrefix(chart, "-") {
		return fmt.Errorf("a chart reference may not begin with a dash")
	}
	return nil
}
