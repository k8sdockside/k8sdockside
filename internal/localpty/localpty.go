// Package localpty runs a program on this machine behind a pseudo-terminal, so
// that a shell, an editor or a full-screen tool behaves in the dock exactly as
// it does in a terminal emulator: it sees a terminal, it is told the window's
// size, Ctrl-C is a signal rather than a byte, and it can draw a whole screen.
//
// One API on every platform, with one implementation per family:
//
//   - Unix (Linux, macOS, the BSDs, Solaris/illumos, AIX) uses
//     github.com/creack/pty, which opens a pty pair and starts the process as
//     the leader of a new session with the pty as its controlling terminal.
//   - Windows uses ConPTY -- CreatePseudoConsole and friends -- directly through
//     golang.org/x/sys/windows. See localpty_windows.go for why the process is
//     started by hand rather than through os/exec.
//   - Anything else gets Start returning ErrUnsupported, so a caller can fall
//     back or say so rather than fail to build.
package localpty

import (
	"errors"
	"io"
)

// ErrUnsupported is returned by Start on a platform with no pseudo-terminal
// this package knows how to open.
var ErrUnsupported = errors.New("a local terminal is not supported on this operating system")

// Options is what to run, and how big its window is to begin with.
type Options struct {
	// Path is the program; Args are its arguments, not including the program
	// itself.
	Path string
	Args []string
	// Env is the whole environment, as KEY=value pairs. Nil means this
	// process's own.
	Env []string
	// Dir is the working directory. Empty means this process's own.
	Dir string
	// Cols and Rows are the initial size. Zero means 80x24.
	Cols, Rows uint16
}

// impl is what each platform provides.
type impl interface {
	io.ReadWriter
	resize(cols, rows uint16) error
	// close ends the process, and everything it started, and releases the
	// terminal. Safe to call more than once.
	close() error
	// wait blocks until the process has exited and returns its exit code.
	wait() (int, error)
	pid() int
}

// Process is a program running behind a pseudo-terminal. Read gives what it
// writes to its terminal; Write is what is typed into it.
type Process struct {
	p impl
}

// Start runs a program in a new pseudo-terminal.
func Start(opts Options) (*Process, error) {
	if opts.Path == "" {
		return nil, errors.New("no program to run")
	}
	if opts.Cols == 0 || opts.Rows == 0 {
		opts.Cols, opts.Rows = 80, 24
	}
	p, err := start(opts)
	if err != nil {
		return nil, err
	}
	return &Process{p: p}, nil
}

// Supported reports whether Start can work on this platform at all.
func Supported() bool { return supported }

// Read reads what the program wrote to its terminal. It returns io.EOF once
// the program, and everything holding the terminal open, has gone.
func (p *Process) Read(b []byte) (int, error) { return p.p.Read(b) }

// Write types into the program's terminal.
func (p *Process) Write(b []byte) (int, error) { return p.p.Write(b) }

// Resize tells the terminal its window is now cols by rows. A zero dimension
// is ignored.
func (p *Process) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	return p.p.resize(cols, rows)
}

// Close ends the program and every process it started, and releases the
// terminal. Read returns io.EOF after it. Safe to call more than once, and
// from any goroutine.
func (p *Process) Close() error { return p.p.close() }

// Wait blocks until the program exits and returns its exit code. It may be
// called more than once, and from several goroutines; all get the same answer.
func (p *Process) Wait() (int, error) { return p.p.wait() }

// Pid is the program's process id.
func (p *Process) Pid() int { return p.p.pid() }
