package localshell

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// fake is a made-up machine: the files that exist, the environment, and
// /etc/shells.
type fake struct {
	goos  string
	files []string
	env   map[string]string
	etc   string
	path  map[string]string
}

func (f fake) system() System {
	return System{
		GOOS:   f.goos,
		Getenv: func(k string) string { return f.env[k] },
		Executable: func(p string) bool {
			if f.goos == "windows" {
				return slices.ContainsFunc(f.files, func(x string) bool { return strings.EqualFold(x, p) })
			}
			return slices.Contains(f.files, p)
		},
		ReadFile: func(p string) ([]byte, error) {
			if p == "/etc/shells" && f.etc != "" {
				return []byte(f.etc), nil
			}
			return nil, errors.New("no such file")
		},
		LookPath: func(name string) (string, error) {
			if p, ok := f.path[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
	}
}

func paths(shells []Shell) []string {
	out := make([]string, len(shells))
	for i, s := range shells {
		out[i] = s.Path
	}
	return out
}

func defaultOf(shells []Shell) string {
	for _, s := range shells {
		if s.Default {
			return s.Path
		}
	}
	return ""
}

func TestDetect(t *testing.T) {
	const macShells = `# List of acceptable shells for chpass(1).
/bin/bash
/bin/csh
/bin/zsh  # comment
/opt/homebrew/bin/fish

/usr/local/bin/missing
`
	winEnv := map[string]string{
		"SystemRoot":        `C:\Windows`,
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"COMSPEC":           `C:\Windows\system32\cmd.exe`,
		"LOCALAPPDATA":      `C:\Users\me\AppData\Local`,
	}

	tests := []struct {
		name        string
		machine     fake
		wantPaths   []string
		wantDefault string
	}{
		{
			name: "macOS: $SHELL first and default, then /etc/shells that exist",
			machine: fake{
				goos:  "darwin",
				env:   map[string]string{"SHELL": "/bin/zsh"},
				etc:   macShells,
				files: []string{"/bin/bash", "/bin/csh", "/bin/zsh", "/opt/homebrew/bin/fish", "/bin/sh"},
			},
			wantPaths:   []string{"/bin/zsh", "/bin/bash", "/bin/csh", "/opt/homebrew/bin/fish", "/bin/sh"},
			wantDefault: "/bin/zsh",
		},
		{
			name: "Linux: $SHELL that does not exist is skipped, first found is default",
			machine: fake{
				goos:  "linux",
				env:   map[string]string{"SHELL": "/usr/bin/gone"},
				etc:   "/bin/sh\n/bin/bash\n",
				files: []string{"/bin/sh", "/bin/bash"},
			},
			wantPaths:   []string{"/bin/sh", "/bin/bash"},
			wantDefault: "/bin/sh",
		},
		{
			name: "Linux without /etc/shells or $SHELL still finds /bin/sh",
			machine: fake{
				goos:  "linux",
				files: []string{"/bin/sh"},
			},
			wantPaths:   []string{"/bin/sh"},
			wantDefault: "/bin/sh",
		},
		{
			name:        "Unix with nothing at all",
			machine:     fake{goos: "freebsd"},
			wantPaths:   []string{},
			wantDefault: "",
		},
		{
			name: "Windows: everything installed, pwsh is the default",
			machine: fake{
				goos: "windows",
				env:  winEnv,
				path: map[string]string{"pwsh.exe": `C:\Program Files\PowerShell\7\pwsh.exe`},
				files: []string{
					`C:\Program Files\PowerShell\7\pwsh.exe`,
					`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
					`C:\Windows\system32\cmd.exe`,
					`C:\Program Files\Git\bin\bash.exe`,
					`C:\Windows\System32\wsl.exe`,
				},
			},
			wantPaths: []string{
				`C:\Program Files\PowerShell\7\pwsh.exe`,
				`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				`C:\Windows\system32\cmd.exe`,
				`C:\Program Files\Git\bin\bash.exe`,
				`C:\Windows\System32\wsl.exe`,
			},
			wantDefault: `C:\Program Files\PowerShell\7\pwsh.exe`,
		},
		{
			name: "Windows without pwsh defaults to Windows PowerShell",
			machine: fake{
				goos: "windows",
				env:  winEnv,
				files: []string{
					`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
					`C:\Windows\system32\cmd.exe`,
					`C:\Program Files (x86)\Git\bin\bash.exe`,
				},
			},
			wantPaths: []string{
				`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				`C:\Windows\system32\cmd.exe`,
				`C:\Program Files (x86)\Git\bin\bash.exe`,
			},
			wantDefault: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		},
		{
			name: "Windows where nothing can be seen still offers cmd as the default",
			machine: fake{
				goos: "windows",
				env:  map[string]string{},
			},
			wantPaths:   []string{`C:\Windows\System32\cmd.exe`},
			wantDefault: `C:\Windows\System32\cmd.exe`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.machine.system())
			if !slices.Equal(paths(got), tt.wantPaths) {
				t.Fatalf("paths = %q, want %q", paths(got), tt.wantPaths)
			}
			if d := defaultOf(got); d != tt.wantDefault {
				t.Fatalf("default = %q, want %q", d, tt.wantDefault)
			}
			defaults := 0
			for _, s := range got {
				if s.Default {
					defaults++
				}
				if s.Args == nil {
					t.Fatalf("%s has nil args; want an empty list for JSON", s.Path)
				}
			}
			if len(got) > 0 && defaults != 1 {
				t.Fatalf("%d defaults, want exactly one", defaults)
			}
		})
	}
}

func TestDetectArgs(t *testing.T) {
	unix := fake{
		goos:  "linux",
		env:   map[string]string{"SHELL": "/usr/bin/zsh"},
		etc:   "/usr/bin/zsh\n/usr/bin/elvish\n",
		files: []string{"/usr/bin/zsh", "/usr/bin/elvish"},
	}.system()
	got := Detect(unix)
	if !slices.Equal(got[0].Args, []string{"-l"}) {
		t.Fatalf("zsh args = %q, want -l", got[0].Args)
	}
	if len(got[1].Args) != 0 {
		t.Fatalf("elvish args = %q, want none: it is not known to take -l", got[1].Args)
	}

	win := fake{
		goos:  "windows",
		env:   map[string]string{"ProgramFiles": `C:\Program Files`},
		files: []string{`C:\Program Files\Git\bin\bash.exe`},
	}.system()
	for _, s := range Detect(win) {
		if s.Name == "Git Bash" && !slices.Equal(s.Args, []string{"--login", "-i"}) {
			t.Fatalf("Git Bash args = %q", s.Args)
		}
	}
}

func TestResolve(t *testing.T) {
	machine := fake{
		goos:  "linux",
		env:   map[string]string{"SHELL": "/bin/bash"},
		etc:   "/bin/bash\n/usr/bin/fish\n",
		files: []string{"/bin/bash", "/usr/bin/fish", "/opt/custom/xonsh", "/home/me/bin/zsh"},
		path:  map[string]string{"zsh": "/home/me/bin/zsh"},
	}.system()

	tests := []struct {
		name     string
		choice   string
		wantPath string
		wantArgs []string
		wantErr  string
	}{
		{name: "empty is the default", choice: "", wantPath: "/bin/bash", wantArgs: []string{"-l"}},
		{name: "a detected shell", choice: "/usr/bin/fish", wantPath: "/usr/bin/fish", wantArgs: []string{"-l"}},
		{name: "a custom path", choice: " /opt/custom/xonsh ", wantPath: "/opt/custom/xonsh", wantArgs: []string{}},
		{name: "a bare name on PATH", choice: "zsh", wantPath: "/home/me/bin/zsh", wantArgs: []string{"-l"}},
		{name: "a path that is not there", choice: "/nope/shell", wantErr: `"/nope/shell" was not found (tried: /nope/shell)`},
		{name: "a name that is not on PATH", choice: "nush", wantErr: "tried: nush, nush on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(machine, tt.choice)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "Settings") {
					t.Fatalf("err = %v, want it to point at Settings", err)
				}
				var nf *NotFoundError
				if !errors.As(err, &nf) {
					t.Fatalf("err = %T, want *NotFoundError", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != tt.wantPath || !slices.Equal(got.Args, tt.wantArgs) {
				t.Fatalf("got %s %q, want %s %q", got.Path, got.Args, tt.wantPath, tt.wantArgs)
			}
		})
	}
}

func TestResolveWithNothingInstalled(t *testing.T) {
	machine := fake{goos: "linux", env: map[string]string{"SHELL": "/bin/zsh"}}.system()
	_, err := Resolve(machine, "")
	if err == nil || !strings.Contains(err.Error(), "/bin/zsh ($SHELL)") || !strings.Contains(err.Error(), "/bin/sh") {
		t.Fatalf("err = %v, want it to list what was tried", err)
	}
}

func TestResolveWindowsIsCaseInsensitive(t *testing.T) {
	machine := fake{
		goos:  "windows",
		env:   map[string]string{"SystemRoot": `C:\Windows`},
		files: []string{`C:\Windows\System32\cmd.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
	}.system()
	got, err := Resolve(machine, `c:\windows\system32\WINDOWSPOWERSHELL\v1.0\powershell.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Windows PowerShell" || !slices.Equal(got.Args, []string{"-NoLogo"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestHostDetectsSomethingHere(t *testing.T) {
	// Not a check of any particular machine: only that the real System is
	// wired up and the default, if any, resolves.
	shells := Detect(Host())
	if len(shells) == 0 {
		t.Skip("no shells on this machine")
	}
	if _, err := Resolve(Host(), ""); err != nil {
		t.Fatal(err)
	}
}
