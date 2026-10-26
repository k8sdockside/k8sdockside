package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/localshell"
	"k8s.io/client-go/tools/clientcmd"
)

const localKubeconfig = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: dev-cluster
  cluster:
    server: https://dev.example:6443
- name: prod-cluster
  cluster:
    server: https://prod.example:6443
contexts:
- name: dev
  context:
    cluster: dev-cluster
    user: dev-user
- name: prod
  context:
    cluster: prod-cluster
    user: prod-user
users:
- name: dev-user
  user:
    token: dev-token
- name: prod-user
  user:
    token: prod-token
`

// onlySh is a machine whose one shell is /bin/sh, so a test does not depend on
// what the person running it has set as theirs.
func onlySh() *localshell.System {
	sys := localshell.Host()
	sys.Getenv = func(k string) string {
		if k == "SHELL" {
			return "/bin/sh"
		}
		return os.Getenv(k)
	}
	sys.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	return &sys
}

// localService is a TerminalService with a kubeconfig holding two contexts,
// returning it and the ID of the "dev" one.
func localService(t *testing.T) (*TerminalService, string) {
	t.Helper()
	s, store := debugService(t)
	home, _ := os.UserHomeDir()
	file := filepath.Join(home, ".kube", "config")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(localKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", file)
	s.configs = NewKubeconfigService(store)
	s.scratch = filepath.Join(t.TempDir(), "kubeconfigs")
	s.shells = onlySh()
	for _, f := range s.configs.Sync() {
		for _, c := range f.Contexts {
			if c.Name == "dev" {
				return s, c.ID
			}
		}
	}
	t.Fatal("the dev context was not discovered")
	return nil, ""
}

func TestOpenLocalIsRefusedInServerMode(t *testing.T) {
	s, _ := debugService(t)
	s.server = true
	if _, err := s.OpenLocal(context.Background(), ""); !errors.Is(err, errLocalTerminalServer) {
		t.Fatalf("err = %v, want errLocalTerminalServer", err)
	}
	if got := s.LocalShells(); got.Supported || len(got.Shells) != 0 || got.Reason == "" {
		t.Fatalf("LocalShells in server mode = %+v, want nothing offered and a reason", got)
	}
}

func TestOpenLocalSaysWhichShellWasNotFound(t *testing.T) {
	s, store := debugService(t)
	s.shells = onlySh()
	prefs := store.Get().Preferences
	prefs.Terminal.LocalShell = "/no/such/shell"
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	_, err := s.OpenLocal(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "/no/such/shell") || !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("err = %v, want it to name the shell and point at Settings", err)
	}
}

func TestOpenLocalScopesAndRemovesTheKubeconfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs /bin/sh")
	}
	s, devID := localService(t)

	session, err := s.OpenLocal(context.Background(), devID)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" || session.Pod != "/bin/sh" {
		t.Fatalf("session = %+v", session)
	}

	entries, err := os.ReadDir(s.scratch)
	if err != nil || len(entries) != 1 {
		t.Fatalf("scratch holds %v (%v), want one kubeconfig", entries, err)
	}
	path := filepath.Join(s.scratch, entries[0].Name())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 0600", perm)
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "dev" || len(cfg.Contexts) != 1 || len(cfg.Clusters) != 1 || len(cfg.AuthInfos) != 1 {
		t.Fatalf("kubeconfig = %+v, want the dev context alone", cfg)
	}

	s.Close(context.Background(), session.ID)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the kubeconfig is still there after Close: %v", err)
	}
	// The session is dropped once its shell has gone.
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		_, open := s.sessions[session.ID]
		s.mu.Unlock()
		if !open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session outlived Close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestShutdownRemovesEveryKubeconfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs /bin/sh")
	}
	s, devID := localService(t)
	for range 2 {
		if _, err := s.OpenLocal(context.Background(), devID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.OpenLocal(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(s.scratch); len(entries) != 2 {
		t.Fatalf("scratch holds %d files, want 2: a plain terminal writes none", len(entries))
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(s.scratch); len(entries) != 0 {
		t.Fatalf("left behind after shutdown: %v", entries)
	}
}

func TestLocalEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "KUBECONFIG=/home/me/.kube/config", "TERM=dumb", "HOME=/home/me"}

	scoped := localEnv(base, "linux", "/tmp/k.yaml", "dev")
	want := []string{"PATH=/usr/bin", "HOME=/home/me", "TERM=xterm-256color", "COLORTERM=truecolor", "KUBECONFIG=/tmp/k.yaml", "K8SDOCKSIDE_CONTEXT=dev"}
	if !slices.Equal(scoped, want) {
		t.Fatalf("scoped env = %q, want %q", scoped, want)
	}

	plain := localEnv(base, "linux", "", "")
	if !slices.Contains(plain, "KUBECONFIG=/home/me/.kube/config") {
		t.Fatalf("a plain terminal lost the user's KUBECONFIG: %q", plain)
	}

	mac := localEnv([]string{"PATH=/usr/bin"}, "darwin", "", "")
	if !slices.Contains(mac, "LANG=en_US.UTF-8") {
		t.Fatalf("macOS without a locale should get one: %q", mac)
	}
	if got := localEnv([]string{"LANG=nb_NO.UTF-8"}, "darwin", "", ""); slices.Contains(got, "LANG=en_US.UTF-8") {
		t.Fatalf("a locale that was set was overridden: %q", got)
	}

	win := localEnv([]string{"Path=C:\\Windows", "KubeConfig=C:\\k"}, "windows", "C:\\t.yaml", "dev")
	if slices.Contains(win, "KubeConfig=C:\\k") || !slices.Contains(win, "KUBECONFIG=C:\\t.yaml") {
		t.Fatalf("windows env names are case-insensitive: %q", win)
	}
}
