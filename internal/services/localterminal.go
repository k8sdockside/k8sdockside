package services

// The local terminal: a shell on this machine, in the dock, behind a real
// pseudo-terminal (internal/localpty). Opened for a cluster, it is given a
// kubeconfig of its own holding only that cluster's context, so kubectl, helm
// and the rest talk to the cluster the tab is for without the user's own
// kubeconfig being touched. Opened plainly, it is just a shell.
//
// Desktop only. In server mode a "local" shell would be a shell on the server,
// as the server's user, for anyone who can sign in -- so it is refused here,
// whatever the window asks, and the window does not offer it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/localpty"
	"github.com/k8sdockside/k8sdockside/internal/localshell"
)

// errLocalTerminalServer is what server mode answers a local terminal with.
var errLocalTerminalServer = errors.New("the local terminal is turned off in server mode: it would be a shell on the server, for everyone who can sign in")

// localDrain is how long a shell that has exited is given for the last of its
// output to be read before its terminal is closed under it. Only reached when
// something the shell started is still holding the terminal open.
const localDrain = 500 * time.Millisecond

// LocalShells is what the settings view offers for the local terminal.
type LocalShells struct {
	// Shells are those found on this machine, the default marked.
	Shells []localshell.Shell `json:"shells"`
	// Supported is false where no local terminal can be opened at all: in
	// server mode, or on a platform without a pseudo-terminal.
	Supported bool `json:"supported"`
	// Reason says why, when it is not.
	Reason string `json:"reason"`
}

func (s *TerminalService) shellSystem() localshell.System {
	if s.shells != nil {
		return *s.shells
	}
	return localshell.Host()
}

// kubeconfigDir is where temporary kubeconfigs go: under the user's cache
// directory, which is theirs alone, rather than a shared /tmp.
func (s *TerminalService) kubeconfigDir() string {
	if s.scratch != "" {
		return s.scratch
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "k8sdockside", "terminal")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("k8sdockside-terminal-%d", os.Getuid()))
}

// LocalShells lists the shells the local terminal can run, for Settings.
func (s *TerminalService) LocalShells() LocalShells {
	if s.server {
		return LocalShells{Shells: []localshell.Shell{}, Reason: errLocalTerminalServer.Error()}
	}
	if !localpty.Supported() {
		return LocalShells{Shells: []localshell.Shell{}, Reason: localpty.ErrUnsupported.Error()}
	}
	shells := localshell.Detect(s.shellSystem())
	if shells == nil {
		shells = []localshell.Shell{}
	}
	return LocalShells{Shells: shells, Supported: true}
}

// OpenLocal starts a shell on this machine in the dock. With a context ID, the
// shell's KUBECONFIG is a temporary file holding only that context, removed
// when the terminal closes; with none, the shell gets the environment the app
// has.
//
// A shell that cannot be found is an error returned here, naming what was
// tried, rather than a terminal that opens onto nothing.
func (s *TerminalService) OpenLocal(ctx context.Context, contextID string) (TerminalSession, error) {
	if s.server {
		return TerminalSession{}, errLocalTerminalServer
	}
	if !localpty.Supported() {
		return TerminalSession{}, localpty.ErrUnsupported
	}

	shell, err := localshell.Resolve(s.shellSystem(), s.store.Get().Preferences.Terminal.LocalShell)
	if err != nil {
		return TerminalSession{}, err
	}

	var kubeconfig, contextName string
	if contextID != "" {
		kc, err := s.resolve(contextID)
		if err != nil {
			return TerminalSession{}, err
		}
		kubeconfig, err = kube.WriteScopedKubeconfig(kc, s.kubeconfigDir())
		if err != nil {
			return TerminalSession{}, fmt.Errorf("could not prepare a kubeconfig for %s: %w", kc.Name, err)
		}
		contextName = kc.Name
	}
	removeKubeconfig := func() {
		if kubeconfig != "" {
			_ = os.Remove(kubeconfig)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	proc, err := localpty.Start(localpty.Options{
		Path: shell.Path,
		Args: shell.Args,
		Env:  localEnv(os.Environ(), runtime.GOOS, kubeconfig, contextName),
		Dir:  home,
	})
	if err != nil {
		removeKubeconfig()
		return TerminalSession{}, fmt.Errorf("could not start %s: %w. Choose another shell in Settings → Terminal → Local shell", shell.Path, err)
	}

	id, sess := s.register(ctx)
	// Closing the tab, or quitting, ends the process tree and removes the
	// kubeconfig there and then.
	sess.atStop(func() {
		_ = proc.Close()
		removeKubeconfig()
	})

	go s.runLocal(id, sess, proc)

	return TerminalSession{ID: id, Pod: shell.Path}, nil
}

// runLocal connects a local process to its session until it ends.
func (s *TerminalService) runLocal(id string, sess *live, proc *localpty.Process) {
	defer s.finished(id)

	// Keystrokes in; the copy ends when the session's pipe is closed.
	go func() { _, _ = io.Copy(proc, sess.in) }()
	// Window sizes.
	go func() {
		for {
			select {
			case <-sess.ctx.Done():
				return
			case size := <-sess.sizes:
				_ = proc.Resize(size.Cols, size.Rows)
			}
		}
	}()

	// Output out, until the terminal reaches its end -- which is when the
	// shell and everything it started have let go of it.
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(sess.batch, proc)
		close(copied)
	}()

	code, waitErr := proc.Wait()
	select {
	case <-copied:
	case <-time.After(localDrain):
		// Something the shell started still holds the terminal. The shell
		// is gone, so the tab is done: close it under them.
		_ = proc.Close()
		<-copied
	}

	sess.batch.flush()
	done := kube.TerminalChunk{SessionID: id, Done: true}
	if sess.ctx.Err() == nil {
		if waitErr != nil {
			done.Error = waitErr.Error()
		} else {
			done.Exited = true
			done.ExitCode = code
		}
	}
	sess.stop()
	s.push(done)
}

// localEnv is the environment a local shell starts with: the app's own, told
// it is in a 256-colour, true-colour terminal, and -- for a cluster's tab --
// pointed at that cluster's kubeconfig alone.
func localEnv(base []string, goos, kubeconfig, contextName string) []string {
	drop := map[string]bool{"TERM": true, "COLORTERM": true}
	if kubeconfig != "" {
		drop["KUBECONFIG"] = true
		drop["K8SDOCKSIDE_CONTEXT"] = true
	}
	name := func(kv string) string {
		key, _, _ := strings.Cut(kv, "=")
		if goos == "windows" {
			key = strings.ToUpper(key)
		}
		return key
	}

	out := make([]string, 0, len(base)+5)
	hasLocale := false
	for _, kv := range base {
		key := name(kv)
		if drop[key] {
			continue
		}
		if key == "LANG" || key == "LC_ALL" || key == "LC_CTYPE" {
			hasLocale = true
		}
		out = append(out, kv)
	}
	out = append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
	// An app opened from Finder has no locale in its environment, and a
	// shell without one cannot show anything but ASCII. Terminal.app sets
	// one for the same reason. Not on Linux, where naming a locale that is
	// not generated makes every program complain about it.
	if goos == "darwin" && !hasLocale {
		out = append(out, "LANG=en_US.UTF-8")
	}
	if kubeconfig != "" {
		out = append(out, "KUBECONFIG="+kubeconfig, "K8SDOCKSIDE_CONTEXT="+contextName)
	}
	return out
}
