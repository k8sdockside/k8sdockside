//go:build unix

package localpty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const supported = true

// killGrace is how long a hung-up process group is given to leave before it
// is killed outright.
const killGrace = 2 * time.Second

type unixPTY struct {
	cmd *exec.Cmd
	f   *os.File

	done     chan struct{}
	exitCode int
	exitErr  error

	closeOnce sync.Once
}

func start(opts Options) (impl, error) {
	// #nosec G204 -- the shell or tool the user chose, run in their own
	// terminal on their own machine. No shell is put in between.
	cmd := exec.Command(opts.Path, opts.Args...)
	cmd.Env = opts.Env
	cmd.Dir = opts.Dir
	// SysProcAttr is left nil on purpose: pty.StartWithSize then makes the
	// process the leader of a new session (Setsid) with the pty as its
	// controlling terminal (Setctty). Being a session leader makes its pid
	// the process group id, which is what close() signals to reach every
	// process it started.
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: opts.Cols, Rows: opts.Rows})
	if err != nil {
		return nil, err
	}
	u := &unixPTY{cmd: cmd, f: f, done: make(chan struct{})}
	go u.reap()
	return u, nil
}

func (u *unixPTY) reap() {
	err := u.cmd.Wait()
	u.exitCode = 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			u.exitCode = exit.ExitCode()
			// Killed by a signal: report it the way a shell would.
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				u.exitCode = 128 + int(status.Signal())
			}
		} else {
			u.exitCode = -1
			u.exitErr = err
		}
	}
	close(u.done)
}

func (u *unixPTY) Read(b []byte) (int, error) {
	n, err := u.f.Read(b)
	// Linux answers a read of a pty whose other side has gone with EIO rather
	// than EOF. Either way it is the end.
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		err = io.EOF
	}
	return n, err
}

func (u *unixPTY) Write(b []byte) (int, error) { return u.f.Write(b) }

func (u *unixPTY) resize(cols, rows uint16) error {
	return pty.Setsize(u.f, &pty.Winsize{Cols: cols, Rows: rows})
}

func (u *unixPTY) close() error {
	u.closeOnce.Do(func() {
		pgid := u.cmd.Process.Pid
		// Hang up on the whole group first, which is what closing a terminal
		// window does: a shell saves its history and passes the hang-up on to
		// its jobs.
		_ = syscall.Kill(-pgid, syscall.SIGHUP)
		_ = u.f.Close()
		go func() {
			select {
			case <-u.done:
			case <-time.After(killGrace):
			}
			// Whatever is left in the group -- the shell, if it ignored the
			// hang-up, or a job it started -- is killed. Harmless when the
			// group is already empty.
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}()
	})
	return nil
}

func (u *unixPTY) wait() (int, error) {
	<-u.done
	return u.exitCode, u.exitErr
}

func (u *unixPTY) pid() int { return u.cmd.Process.Pid }
