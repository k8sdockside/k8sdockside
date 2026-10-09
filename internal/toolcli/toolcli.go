// Package toolcli runs the command line tools plugins declare in
// "ui": {"tools": [...]} -- finding them, running them, and giving them a
// console in the dock. What may be run is decided in internal/plugins
// (UITool.Allows); this package only runs it.
//
// Every command is a process, never a shell: arguments are passed as
// arguments, so nothing a view sends can become a second command.
package toolcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	probeTimeout = 5 * time.Second
	// RunTimeout bounds one command a view runs. A tool that talks to a
	// machine that is down answers with a dial error long before this.
	RunTimeout = 30 * time.Second
	// maxOutput is as much of one stream as is kept.
	maxOutput = 16 << 20
)

// Tool is where a command is on this machine, and what version it is.
type Tool struct {
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
	// Reason is why there is no usable tool, in words that name the fix.
	Reason string `json:"reason"`
}

// searchPath is where package managers put programs, for a desktop app that
// was not started from a login shell and so does not see the shell's PATH.
func searchPath(command string) []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "windows":
		dirs = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links")}
		command += ".exe"
	default:
		dirs = []string{"/usr/local/bin", "/usr/bin", "/home/linuxbrew/.linuxbrew/bin", "/snap/bin"}
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), filepath.Join(home, "go", "bin"))
	}
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = filepath.Join(d, command)
	}
	return out
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

// Locate finds a command, and asks it its version when versionArgs are given.
func Locate(command string, versionArgs []string) Tool {
	path, err := exec.LookPath(command)
	if err != nil {
		path = ""
		for _, candidate := range searchPath(command) {
			if executable(candidate) {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return Tool{Reason: fmt.Sprintf("%s was not found on your PATH -- install it and restart the app", command)}
	}
	return Tool{Found: true, Path: path, Version: version(path, versionArgs)}
}

var versionPattern = regexp.MustCompile(`v?\d+\.\d+\.\d+[0-9A-Za-z.+-]*`)

func version(path string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	// #nosec G204 -- a binary located above, with the manifest's own arguments.
	out, _ := exec.CommandContext(ctx, path, args...).Output()
	return versionPattern.FindString(string(out))
}

// ExpandHome turns a leading ~ into the user's home.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// Output is what a command wrote, and how it ended. A non-zero exit is not an
// error: a tool that asked several machines and heard from some of them
// exits 1, and what it did hear is still the answer.
type Output struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
	// Truncated says a stream was longer than the app keeps.
	Truncated bool `json:"truncated"`
}

// Run runs one command to the end.
func Run(ctx context.Context, path string, args, env []string) (Output, error) {
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	// #nosec G204 -- a located binary; the arguments were matched against the
	// manifest's patterns by the caller. No shell.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), env...)
	stdout, stderr := &limited{max: maxOutput}, &limited{max: maxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Output{}, fmt.Errorf("%s did not finish within %s", filepath.Base(path), RunTimeout)
	}
	out := Output{Stdout: stdout.String(), Stderr: stderr.String(), Truncated: stdout.cut || stderr.cut}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		out.Code = exit.ExitCode()
	} else if err != nil {
		return Output{}, fmt.Errorf("could not run %s: %w", filepath.Base(path), err)
	}
	return out, nil
}

// limited is a buffer that stops keeping bytes past max.
type limited struct {
	bytes.Buffer
	max int
	cut bool
}

func (l *limited) Write(p []byte) (int, error) {
	room := l.max - l.Len()
	if room < len(p) {
		l.cut = true
	}
	if room > 0 {
		l.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Display writes a command line as a person would type it, quoting what a
// shell would need quoted.
func Display(command string, args []string) string {
	parts := []string{command}
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'$`\\|&;<>()*?!#~") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
