package toolcli

// The console: a terminal in the dock that runs one tool's commands, already
// given the files the user chose for the cluster.
//
// It is not a shell. It reads a line, splits it the way a shell would split
// words (and nothing more: no pipes, no variables, no globbing), and runs the
// tool with those words. The prompt does its own echoing and line editing.
//
// Each command runs behind a real pseudo-terminal (internal/localpty), so a
// tool that draws a full screen -- `talosctl dashboard`, say -- works here as
// it does in a terminal emulator: it sees a terminal of the dock's size, every
// keystroke reaches it while it runs, and Ctrl-C is the terminal's interrupt
// rather than this console's. Where no pseudo-terminal can be opened the
// command runs on pipes instead, as it always used to: output only, with
// Ctrl-C stopping it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/k8sdockside/k8sdockside/internal/localpty"
	"github.com/k8sdockside/k8sdockside/internal/procattr"
)

// drainWait is how long a command that has exited is given for the last of
// its output to arrive before its terminal is closed under it.
const drainWait = 500 * time.Millisecond

// Console runs one tool's commands typed into a terminal.
type Console struct {
	// Path is the tool; Name is what the prompt calls it.
	Path string
	Name string
	// Always are arguments added to every command: the files' flags.
	Always []string
	// Defaults are flag and value pairs added to a command that does not give
	// that flag itself -- which machines to ask, say.
	Defaults []string
	// Env is added to the tool's environment.
	Env []string
	// Banner is said once, before the first prompt.
	Banner string

	// NoPTY runs commands on pipes even where a pseudo-terminal could be
	// opened. For tests.
	NoPTY bool

	out io.Writer
	// raw is out without the newline translation: a pseudo-terminal does
	// its own.
	raw     io.Writer
	history []string
	// ahead is what was typed while a command ran.
	ahead []byte

	// mu guards the window size and the command running in it, which
	// Resize reaches from another goroutine.
	mu      sync.Mutex
	cols    uint16
	rows    uint16
	running *localpty.Process
	// sized is closed by the first Resize, for a command that should not
	// start before the window's size is known. Made on first use.
	sized chan struct{}
}

// firstSizeWait is how long a command behind a pseudo-terminal waits for the
// window to say how big it is. The dock reports its size as soon as the
// session exists, which is usually well inside this; a command started at the
// 80x24 default instead draws a full screen into a corner of the window, and
// one that only reads the size at start -- talosctl dashboard -- stays there.
const firstSizeWait = 500 * time.Millisecond

// Resize records the window's size, and passes it on to the command running
// now, if any. Safe to call from any goroutine.
func (c *Console) Resize(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	c.mu.Lock()
	c.cols, c.rows = cols, rows
	running := c.running
	sized := c.sizedLocked()
	c.mu.Unlock()
	select {
	case <-sized:
	default:
		close(sized)
	}
	if running != nil {
		_ = running.Resize(cols, rows)
	}
}

func (c *Console) size() (uint16, uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cols, c.rows
}

// sizedLocked returns the channel the first Resize closes. c.mu must be held.
func (c *Console) sizedLocked() chan struct{} {
	if c.sized == nil {
		c.sized = make(chan struct{})
	}
	return c.sized
}

// waitForSize gives the window up to firstSizeWait to report its size, and
// returns whatever is known by then -- zero for both when nothing came, which
// localpty takes as its default.
func (c *Console) waitForSize(ctx context.Context) (uint16, uint16) {
	c.mu.Lock()
	sized := c.sizedLocked()
	c.mu.Unlock()
	timer := time.NewTimer(firstSizeWait)
	defer timer.Stop()
	select {
	case <-sized:
	case <-timer.C:
	case <-ctx.Done():
	}
	return c.size()
}

func (c *Console) setRunning(p *localpty.Process) {
	c.mu.Lock()
	c.running = p
	c.mu.Unlock()
}

// Run reads keystrokes from in until it closes, the context ends, or the user
// types exit. initial, when given, is run first, as if typed.
func (c *Console) Run(ctx context.Context, in io.Reader, out io.Writer, initial []string) error {
	c.raw = out
	c.out = crlf{out}
	keys := make(chan []byte)
	go func() {
		defer close(keys)
		buf := make([]byte, 1024)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				chunk := slices.Clone(buf[:n])
				select {
				case keys <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	if c.Banner != "" {
		c.say(c.Banner)
	}
	if len(initial) > 0 {
		c.prompt()
		c.write(Display(c.Name, initial) + "\n")
		c.exec(ctx, keys, append(slices.Clone(initial), c.Always...))
	}

	var line []rune
	var escape []byte
	recall := len(c.history)
	c.prompt()
	for {
		// What was typed while a command ran comes first.
		chunk := c.ahead
		c.ahead = nil
		if len(chunk) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case k, ok := <-keys:
				if !ok {
					return nil
				}
				chunk = k
			}
		}
		for len(chunk) > 0 {
			// An escape sequence -- an arrow key -- arrives as several bytes,
			// possibly across reads.
			if len(escape) > 0 || chunk[0] == 0x1b {
				escape = append(escape, chunk[0])
				chunk = chunk[1:]
				if !escapeDone(escape) {
					continue
				}
				switch string(escape) {
				case "\x1b[A", "\x1bOA":
					if recall > 0 {
						recall--
						line = c.replace(line, c.history[recall])
					}
				case "\x1b[B", "\x1bOB":
					if recall < len(c.history)-1 {
						recall++
						line = c.replace(line, c.history[recall])
					} else {
						recall = len(c.history)
						line = c.replace(line, "")
					}
				}
				escape = nil
				continue
			}
			r, size := utf8.DecodeRune(chunk)
			chunk = chunk[size:]
			switch r {
			case '\r', '\n':
				c.write("\n")
				typed := strings.TrimSpace(string(line))
				line = nil
				if typed != "" {
					if len(c.history) == 0 || c.history[len(c.history)-1] != typed {
						c.history = append(c.history, typed)
					}
					// The rest of this chunk was typed before anything
					// the command saw, so it goes ahead of it: to the
					// command, when it runs in a terminal and so can
					// read it; to the next prompt otherwise.
					c.ahead = append(slices.Clone(chunk), c.ahead...)
					chunk = nil
					if c.typed(ctx, keys, typed) {
						return nil
					}
				}
				recall = len(c.history)
				c.prompt()
			case 0x7f, 0x08:
				if len(line) > 0 {
					line = line[:len(line)-1]
					c.write("\b \b")
				}
			case 0x03: // Ctrl-C
				c.write("^C\n")
				line = nil
				c.prompt()
			case 0x04: // Ctrl-D
				if len(line) == 0 {
					c.write("\n")
					return nil
				}
			case 0x0c: // Ctrl-L
				c.write("\x1b[2J\x1b[H")
				c.prompt()
				c.write(string(line))
			case 0x15: // Ctrl-U
				line = c.replace(line, "")
			default:
				if r >= 0x20 && r != utf8.RuneError {
					line = append(line, r)
					c.write(string(r))
				}
			}
		}
	}
}

// escapeDone reports whether an escape sequence is complete: ESC and one more
// byte, or a CSI/SS3 sequence (ESC [ or ESC O) up to its final byte.
func escapeDone(seq []byte) bool {
	if len(seq) < 2 {
		return false
	}
	if seq[1] != '[' && seq[1] != 'O' {
		return true
	}
	if len(seq) < 3 {
		return false
	}
	last := seq[len(seq)-1]
	return (last >= 0x40 && last <= 0x7e) || len(seq) > 16
}

// replace swaps what is on the line for another text, on screen too.
func (c *Console) replace(line []rune, with string) []rune {
	c.write(strings.Repeat("\b \b", len(line)) + with)
	return []rune(with)
}

// typed handles one line, returning true when the console should close.
func (c *Console) typed(ctx context.Context, keys <-chan []byte, typed string) bool {
	words, err := Split(typed)
	if err != nil {
		c.fail(err.Error())
		return false
	}
	if len(words) > 0 && words[0] == c.Name {
		words = words[1:]
	}
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "exit", "quit", "logout":
		return true
	case "clear":
		c.write("\x1b[2J\x1b[H")
		return false
	case "?":
		c.help()
		return false
	}
	c.exec(ctx, keys, c.args(words))
	return false
}

// args adds what a typed command did not give itself.
func (c *Console) args(words []string) []string {
	has := func(flag string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return w == flag || strings.HasPrefix(w, flag+"=") })
	}
	out := slices.Clone(words)
	for i := 0; i+1 < len(c.Defaults); i += 2 {
		if !has(c.Defaults[i]) {
			out = append(out, c.Defaults[i], c.Defaults[i+1])
		}
	}
	return append(out, c.Always...)
}

// exec runs one command until it ends: behind a pseudo-terminal where one can
// be opened, on pipes where not.
func (c *Console) exec(ctx context.Context, keys <-chan []byte, args []string) {
	if !c.NoPTY && localpty.Supported() {
		c.execPTY(ctx, keys, args)
		return
	}
	c.execPiped(ctx, keys, args)
}

// execPTY runs one command behind a pseudo-terminal. Everything typed goes to
// it, Ctrl-C included, until it exits; the window's size follows it.
//
// procattr.Hide is not used here, and must not be: on Windows it asks for
// CREATE_NO_WINDOW, which gives the process a hidden console of its own
// instead of the pseudo console, and nothing it writes would reach the dock.
// localpty starts the process itself; see localpty_windows.go.
func (c *Console) execPTY(ctx context.Context, keys <-chan []byte, args []string) {
	cols, rows := c.waitForSize(ctx)
	env := append(os.Environ(), c.Env...)
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	p, err := localpty.Start(localpty.Options{Path: c.Path, Args: args, Env: env, Cols: cols, Rows: rows})
	if err != nil {
		c.fail("could not run " + c.Name + ": " + err.Error())
		return
	}
	c.setRunning(p)
	// A Resize that came between reading the size and setRunning found no
	// command to pass it to; pass it on now.
	if nowCols, nowRows := c.size(); nowCols != 0 && (nowCols != cols || nowRows != rows) {
		_ = p.Resize(nowCols, nowRows)
	}
	defer c.setRunning(nil)
	defer func() { _ = p.Close() }()
	// What was typed after the command's line is the command's to read, as
	// it would be in a terminal.
	if len(c.ahead) > 0 {
		_, _ = p.Write(c.ahead)
		c.ahead = nil
	}

	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(c.raw, p)
		close(copied)
	}()
	type result struct {
		code int
		err  error
	}
	exited := make(chan result, 1)
	go func() {
		code, err := p.Wait()
		exited <- result{code, err}
	}()

	stopped := false
	closing := ctx.Done()
	for {
		select {
		case r := <-exited:
			select {
			case <-copied:
			case <-time.After(drainWait):
				_ = p.Close()
				<-copied
			}
			switch {
			case ctx.Err() != nil:
			case stopped:
				c.say("Stopped.")
			case r.err != nil:
				c.fail(r.err.Error())
			case r.code != 0:
				c.say(fmt.Sprintf("%s exited with status %d", c.Name, r.code))
			}
			return
		case <-closing:
			// The console is closing: end the command and wait for it.
			closing = nil
			stopped = true
			_ = p.Close()
		case k, ok := <-keys:
			if !ok {
				// The terminal closed: stop the command and wait for it.
				keys = nil
				stopped = true
				_ = p.Close()
				continue
			}
			// Everything typed is the command's -- an interactive tool
			// reads its own keys, and Ctrl-C is its terminal's interrupt.
			_, _ = p.Write(k)
		}
	}
}

// execPiped runs one command on pipes, streaming its output, until it ends or
// the user presses Ctrl-C.
func (c *Console) execPiped(ctx context.Context, keys <-chan []byte, args []string) {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	// #nosec G204 -- the declared tool, with the words the user typed into
	// their own terminal as its arguments. No shell.
	cmd := exec.CommandContext(run, c.Path, args...)
	// No pseudo-terminal here, so the process gets a console of its own on
	// Windows; Hide keeps that console's window from flashing up.
	procattr.Hide(cmd)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdout, cmd.Stderr = c.out, c.out
	if err := cmd.Start(); err != nil {
		c.fail("could not run " + c.Name + ": " + err.Error())
		return
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err := <-done:
			var exit *exec.ExitError
			switch {
			case run.Err() != nil && ctx.Err() == nil:
				c.say("Stopped.")
			case errors.As(err, &exit):
				c.say(fmt.Sprintf("%s exited with status %d", c.Name, exit.ExitCode()))
			case err != nil:
				c.fail(err.Error())
			}
			return
		case k, ok := <-keys:
			if !ok {
				// The terminal closed: stop the command and wait for it.
				keys = nil
				cancel()
				continue
			}
			// Ctrl-C stops the command; anything else is typed ahead, for
			// the prompt that follows -- the tool reads no input here.
			if i := slices.Index(k, 0x03); i >= 0 {
				c.write("^C\n")
				c.ahead = nil
				cancel()
				continue
			}
			c.ahead = append(c.ahead, k...)
		}
	}
}

func (c *Console) prompt() {
	c.write("\x1b[1;36m" + c.Name + "\x1b[0m › ")
}

func (c *Console) help() {
	lines := []string{"Type a " + c.Name + " command, with or without \"" + c.Name + "\" in front."}
	if len(c.Always) > 0 || len(c.Defaults) > 0 {
		lines = append(lines, "Added for you: "+strings.Join(append(slices.Clone(c.Defaults), c.Always...), " ")+" -- give a flag yourself to override a default.")
	}
	lines = append(lines, "  clear  clear the screen    ↑ ↓  history    Ctrl-C  stop a command    exit  close")
	c.say(strings.Join(lines, "\n"))
}

func (c *Console) write(text string) { _, _ = io.WriteString(c.out, text) }

func (c *Console) say(text string) { c.write("\x1b[2m" + text + "\x1b[0m\n") }

func (c *Console) fail(text string) { c.write("\x1b[31m" + text + "\x1b[0m\n") }

// crlf turns a bare newline into the carriage return and newline a terminal
// needs: without a line discipline to do it, each line would start where the
// last one ended.
type crlf struct{ w io.Writer }

func (c crlf) Write(p []byte) (int, error) {
	var b strings.Builder
	prev := byte(0)
	for _, ch := range p {
		if ch == '\n' && prev != '\r' {
			b.WriteByte('\r')
		}
		b.WriteByte(ch)
		prev = ch
	}
	if _, err := io.WriteString(c.w, b.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Split cuts a line into words the way a shell would -- whitespace between,
// single quotes literal, double quotes with backslash escapes -- and does
// nothing else a shell does.
func Split(line string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				word.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}
