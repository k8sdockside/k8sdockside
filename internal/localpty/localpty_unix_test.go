//go:build unix

package localpty

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// readAll reads until EOF or the deadline, whichever comes first.
func readAll(t *testing.T, p *Process, within time.Duration) string {
	t.Helper()
	out := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(p)
		out <- data
	}()
	select {
	case data := <-out:
		return string(data)
	case <-time.After(within):
		_ = p.Close()
		t.Fatal("the terminal did not reach end of file in time")
		return ""
	}
}

func TestStartReadsOutputAndExitCode(t *testing.T) {
	p, err := Start(Options{Path: "/bin/sh", Args: []string{"-c", "echo hi; exit 3"}, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	got := readAll(t, p, 10*time.Second)
	if !strings.Contains(got, "hi") {
		t.Fatalf("output = %q, want it to contain hi", got)
	}
	// A terminal turns a newline into CR LF; a pipe would not.
	if !strings.Contains(got, "hi\r\n") {
		t.Fatalf("output = %q, want the CR LF a terminal writes", got)
	}
	code, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	if p.Pid() <= 0 {
		t.Fatalf("pid = %d", p.Pid())
	}
}

func TestStartSeesATerminalOfTheGivenSize(t *testing.T) {
	p, err := Start(Options{
		Path: "/bin/sh",
		Args: []string{"-c", "test -t 0 && echo tty; stty size"},
		Env:  os.Environ(),
		Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	got := readAll(t, p, 10*time.Second)
	if !strings.Contains(got, "tty") {
		t.Fatalf("output = %q: the program did not see a terminal", got)
	}
	if !strings.Contains(got, "30 100") {
		t.Fatalf("output = %q, want stty to report 30 100", got)
	}
}

func TestResizeReachesTheProgram(t *testing.T) {
	// The shell waits for a line, then reports the size it has by then.
	p, err := Start(Options{
		Path: "/bin/sh",
		Args: []string{"-c", "read line; stty size"},
		Env:  os.Environ(),
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	if err := p.Resize(132, 43); err != nil {
		t.Fatal(err)
	}
	if err := p.Resize(0, 10); err != nil {
		t.Fatalf("a zero size should be ignored, got %v", err)
	}
	if _, err := p.Write([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, p, 10*time.Second)
	if !strings.Contains(got, "43 132") {
		t.Fatalf("output = %q, want stty to report 43 132 after the resize", got)
	}
}

func TestCloseEndsTheProcessGroup(t *testing.T) {
	// A shell that ignores the hang-up, with a child of its own: both have to
	// go for the read to end.
	p, err := Start(Options{
		Path: "/bin/sh",
		Args: []string{"-c", "trap '' HUP; sleep 60 & echo started; wait"},
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	var seen bytes.Buffer
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(seen.String(), "started") {
		if time.Now().After(deadline) {
			t.Fatalf("never saw the shell start: %q", seen.String())
		}
		n, err := p.Read(buf)
		seen.Write(buf[:n])
		if err != nil {
			t.Fatalf("read: %v (after %q)", err, seen.String())
		}
	}

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
	done := make(chan int, 1)
	go func() {
		code, _ := p.Wait()
		done <- code
	}()
	select {
	case code := <-done:
		if code == 0 {
			t.Fatalf("exit code = 0, want the shell to have been killed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the shell outlived Close")
	}
}

func TestStartReportsAMissingProgram(t *testing.T) {
	if _, err := Start(Options{Path: "/nonexistent/shell"}); err == nil {
		t.Fatal("want an error for a program that is not there")
	}
	if _, err := Start(Options{}); err == nil {
		t.Fatal("want an error for no program at all")
	}
}

func TestSupported(t *testing.T) {
	if !Supported() {
		t.Fatal("Unix should be supported")
	}
}
