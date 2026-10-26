// Package localshell finds the shells on this machine that the dock's local
// terminal can run, picks the default, and resolves the user's choice --
// detected or typed in by hand -- into a command line.
//
// Everything it asks of the machine goes through System, so the detection can
// be tested against a made-up filesystem and environment for any OS.
package localshell

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

// Shell is one shell that can be chosen.
type Shell struct {
	// Path is the program, and what the setting stores.
	Path string `json:"path"`
	// Name is what the settings list calls it.
	Name string `json:"name"`
	// Args are what it is started with: -l for a login shell, so a desktop
	// app launched from a dock or a start menu gets the PATH the user's
	// profile sets up.
	Args []string `json:"args"`
	// Default marks the one used when nothing is chosen.
	Default bool `json:"default"`
}

// System is what detection asks of the machine.
type System struct {
	// GOOS is the operating system to detect for.
	GOOS string
	// Getenv reads an environment variable.
	Getenv func(string) string
	// Executable reports whether a path is a program that can be run: a
	// regular file, and executable where that is a thing.
	Executable func(string) bool
	// ReadFile reads a file, for /etc/shells.
	ReadFile func(string) ([]byte, error)
	// LookPath finds a bare command name on PATH.
	LookPath func(string) (string, error)
}

// Host is this machine.
func Host() System {
	return System{
		GOOS:   runtime.GOOS,
		Getenv: os.Getenv,
		Executable: func(p string) bool {
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() {
				return false
			}
			return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
		},
		ReadFile: os.ReadFile,
		LookPath: exec.LookPath,
	}
}

func (s System) windows() bool { return s.GOOS == "windows" }

// join is filepath.Join for the OS being detected for, which in a test is not
// necessarily this one.
func (s System) join(elem ...string) string {
	if s.windows() {
		return strings.Join(elem, `\`)
	}
	return path.Join(elem...)
}

func (s System) base(p string) string {
	if s.windows() {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return path.Base(p)
}

// Detect lists the shells on this machine, the default first among equals
// and marked. On Windows, Command Prompt is always in the list: it is the
// fallback that is always there.
func Detect(s System) []Shell {
	var shells []Shell
	if s.windows() {
		shells = detectWindows(s)
	} else {
		shells = detectUnix(s)
	}
	if len(shells) > 0 {
		def := 0
		for i, sh := range shells {
			if sh.Default {
				def = i
			}
		}
		shells[def].Default = true
	}
	return shells
}

// loginArgs are what a shell is given to start as a login shell, by its name.
// Only shells known to take -l get it: anything else is started bare rather
// than with a flag it might read as a script name.
func loginArgs(name string) []string {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	switch name {
	case "bash", "zsh", "sh", "dash", "ksh", "mksh", "oksh", "loksh", "yash", "fish", "tcsh", "csh", "nu":
		return []string{"-l"}
	}
	return []string{}
}

func detectUnix(s System) []Shell {
	var shells []Shell
	seen := map[string]bool{}
	add := func(p, name string, def bool) {
		if p == "" || seen[p] || !s.Executable(p) {
			return
		}
		seen[p] = true
		shells = append(shells, Shell{Path: p, Name: name, Args: loginArgs(s.base(p)), Default: def})
	}

	if login := strings.TrimSpace(s.Getenv("SHELL")); login != "" {
		add(login, fmt.Sprintf("Login shell (%s)", s.base(login)), true)
	}
	if data, err := s.ReadFile("/etc/shells"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if !strings.HasPrefix(line, "/") {
				continue
			}
			add(line, s.base(line)+" ("+line+")", false)
		}
	}
	// A machine with neither $SHELL nor /etc/shells -- a minimal container --
	// still almost always has these.
	for _, p := range []string{"/bin/bash", "/bin/sh"} {
		add(p, s.base(p)+" ("+p+")", false)
	}
	return shells
}

func detectWindows(s System) []Shell {
	var shells []Shell
	seen := map[string]bool{}
	add := func(p, name string, args []string) bool {
		key := strings.ToLower(p)
		if p == "" || seen[key] || !s.Executable(p) {
			return false
		}
		seen[key] = true
		shells = append(shells, Shell{Path: p, Name: name, Args: args})
		return true
	}
	firstOf := func(name string, args []string, candidates ...string) bool {
		for _, c := range candidates {
			if add(c, name, args) {
				return true
			}
		}
		return false
	}
	look := func(cmd string) string {
		if s.LookPath == nil {
			return ""
		}
		p, err := s.LookPath(cmd)
		if err != nil {
			return ""
		}
		return p
	}

	root := s.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	system32 := s.join(root, "System32")
	programFiles := s.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}

	var pwsh []string
	pwsh = append(pwsh, look("pwsh.exe"), s.join(programFiles, "PowerShell", "7", "pwsh.exe"))
	if local := s.Getenv("LOCALAPPDATA"); local != "" {
		pwsh = append(pwsh, s.join(local, "Microsoft", "WindowsApps", "pwsh.exe"))
	}
	firstOf("PowerShell 7", []string{"-NoLogo"}, pwsh...)

	firstOf("Windows PowerShell", []string{"-NoLogo"},
		s.join(system32, "WindowsPowerShell", "v1.0", "powershell.exe"), look("powershell.exe"))

	// Command Prompt is listed whether or not it could be seen: it is the
	// fallback, and a machine without it is not one this app runs on.
	cmd := strings.TrimSpace(s.Getenv("COMSPEC"))
	if cmd == "" || !s.Executable(cmd) {
		cmd = s.join(system32, "cmd.exe")
	}
	if !seen[strings.ToLower(cmd)] {
		seen[strings.ToLower(cmd)] = true
		shells = append(shells, Shell{Path: cmd, Name: "Command Prompt", Args: []string{}})
	}

	var gitBash []string
	gitBash = append(gitBash, s.join(programFiles, "Git", "bin", "bash.exe"))
	if x86 := s.Getenv("ProgramFiles(x86)"); x86 != "" {
		gitBash = append(gitBash, s.join(x86, "Git", "bin", "bash.exe"))
	}
	if local := s.Getenv("LOCALAPPDATA"); local != "" {
		gitBash = append(gitBash, s.join(local, "Programs", "Git", "bin", "bash.exe"))
	}
	firstOf("Git Bash", []string{"--login", "-i"}, gitBash...)

	firstOf("WSL", []string{}, s.join(system32, "wsl.exe"))

	// The default: PowerShell 7, then Windows PowerShell, then cmd.
	for _, want := range []string{"PowerShell 7", "Windows PowerShell", "Command Prompt"} {
		for i := range shells {
			if shells[i].Name == want {
				shells[i].Default = true
				return shells
			}
		}
	}
	return shells
}

// NotFoundError is a chosen shell that could not be run, with everything that
// was tried.
type NotFoundError struct {
	Choice string
	Tried  []string
}

func (e *NotFoundError) Error() string {
	what := "No shell could be found"
	if e.Choice != "" {
		what = fmt.Sprintf("The shell %q was not found", e.Choice)
	}
	tried := "nothing"
	if len(e.Tried) > 0 {
		tried = strings.Join(e.Tried, ", ")
	}
	return fmt.Sprintf("%s (tried: %s). Choose another shell in Settings → Terminal → Local shell.", what, tried)
}

// Resolve turns the setting into the shell to run. Empty means the default.
// Anything else is the path of a detected shell, or a path or command name the
// user typed in -- run as a login shell when it is one known to take -l.
func Resolve(s System, choice string) (Shell, error) {
	choice = strings.TrimSpace(choice)
	shells := Detect(s)

	if choice == "" {
		for _, sh := range shells {
			if sh.Default {
				return sh, nil
			}
		}
		var tried []string
		if login := strings.TrimSpace(s.Getenv("SHELL")); login != "" {
			tried = append(tried, login+" ($SHELL)")
		}
		tried = append(tried, "/etc/shells", "/bin/bash", "/bin/sh")
		return Shell{}, &NotFoundError{Tried: tried}
	}

	for _, sh := range shells {
		if sameFile(s, sh.Path, choice) {
			return sh, nil
		}
	}

	tried := []string{choice}
	if s.Executable(choice) {
		return custom(s, choice), nil
	}
	// A bare name, "zsh" or "pwsh", is looked for on PATH.
	if !strings.ContainsAny(choice, `/\`) && s.LookPath != nil {
		tried = append(tried, choice+" on PATH")
		if p, err := s.LookPath(choice); err == nil && s.Executable(p) {
			return custom(s, p), nil
		}
	}
	return Shell{}, &NotFoundError{Choice: choice, Tried: tried}
}

func custom(s System, p string) Shell {
	args := loginArgs(s.base(p))
	if s.windows() {
		switch strings.ToLower(s.base(p)) {
		case "bash.exe":
			args = []string{"--login", "-i"}
		case "pwsh.exe", "powershell.exe":
			args = []string{"-NoLogo"}
		default:
			args = []string{}
		}
	}
	return Shell{Path: p, Name: s.base(p), Args: args}
}

func sameFile(s System, a, b string) bool {
	if s.windows() {
		return strings.EqualFold(a, b)
	}
	return a == b
}
