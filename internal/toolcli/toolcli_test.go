package toolcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/localpty"
)

func TestSplit(t *testing.T) {
	cases := map[string][]string{
		`get members`:                     {"get", "members"},
		`  logs   kubelet  -f `:           {"logs", "kubelet", "-f"},
		`upgrade --image 'a b' "c\"d"`:    {"upgrade", "--image", "a b", `c"d`},
		`get rd ''`:                       {"get", "rd", ""},
		`a\ b`:                            {"a b"},
		`get members; rm -rf / && reboot`: {"get", "members;", "rm", "-rf", "/", "&&", "reboot"},
	}
	for line, want := range cases {
		got, err := Split(line)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Split(%q) = %q, %v; want %q", line, got, err, want)
		}
	}
	if _, err := Split(`get 'members`); err == nil {
		t.Error("an unclosed quote should be an error")
	}
}

func TestConsoleArgs(t *testing.T) {
	c := &Console{Name: "tool", Always: []string{"--config", "/cfg"}, Defaults: []string{"--nodes", "10.0.0.1", "--context", "lab"}}
	cases := map[string][]string{
		"get members":                       {"get", "members", "--nodes", "10.0.0.1", "--context", "lab", "--config", "/cfg"},
		"get members -n x --nodes=10.0.0.9": {"get", "members", "-n", "x", "--nodes=10.0.0.9", "--context", "lab", "--config", "/cfg"},
	}
	for line, want := range cases {
		words, _ := Split(line)
		if got := c.args(words); !slices.Equal(got, want) {
			t.Errorf("args(%q) = %q; want %q", line, got, want)
		}
	}
}

func TestCRLF(t *testing.T) {
	var out bytes.Buffer
	_, _ = crlf{&out}.Write([]byte("a\nb\r\nc\n"))
	if out.String() != "a\r\nb\r\nc\r\n" {
		t.Errorf("crlf = %q", out.String())
	}
}

func TestDisplay(t *testing.T) {
	if got := Display("tool", []string{"get", "a b", "it's"}); got != `tool get 'a b' 'it'\''s'` {
		t.Errorf("Display = %s", got)
	}
}

// fakeTool is a script that prints its arguments, one per line.
func fakeTool(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the tool")
	}
	path := filepath.Join(t.TempDir(), "tool")
	script := "#!/bin/sh\n" +
		"[ \"$1\" = tty ] && { test -t 0 && test -t 1 && echo istty; stty size; exit 0; }\n" +
		"[ \"$1\" = read ] && { read x; echo \"got:$x\"; exit 0; }\n" +
		"for a in \"$@\"; do echo \"arg:$a\"; done\n[ \"$1\" = fail ] && exit 3\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun(t *testing.T) {
	path := fakeTool(t)
	out, err := Run(context.Background(), path, []string{"get", "x y"}, nil)
	if err != nil || out.Code != 0 || out.Stdout != "arg:get\narg:x y\n" {
		t.Fatalf("Run = %+v, %v", out, err)
	}
	out, err = Run(context.Background(), path, []string{"fail"}, nil)
	if err != nil || out.Code != 3 {
		t.Fatalf("a failing command = %+v, %v; want code 3 and no error", out, err)
	}
}

func TestConsoleRun(t *testing.T) {
	// On pipes: what is typed while a command runs waits for the prompt.
	c := &Console{Path: fakeTool(t), Name: "tool", Always: []string{"--config", "/cfg"}, NoPTY: true}
	in, typing := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background(), in, &out, []string{"service", "kubelet", "restart"}) }()

	// Typed ahead while the first command runs, with a correction, then
	// recalled from history with the up arrow.
	_, _ = io.WriteString(typing, "get membx\x7fers\r")
	_, _ = io.WriteString(typing, "\x1b[A\r")
	_, _ = io.WriteString(typing, "exit\r")

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the console did not close on exit")
	}
	got := out.String()
	if !strings.Contains(got, "arg:service\r\narg:kubelet\r\narg:restart\r\narg:--config\r\narg:/cfg\r\n") {
		t.Errorf("the initial command did not run with the files' flags:\n%s", got)
	}
	if n := strings.Count(got, "arg:members"); n != 2 {
		t.Errorf("get members ran %d times, want 2 (typed, then recalled):\n%s", n, got)
	}
}

func TestConsoleRunsCommandsInATerminal(t *testing.T) {
	c := &Console{Path: fakeTool(t), Name: "tool"}
	c.Resize(100, 30)
	in, typing := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background(), in, &out, []string{"tty"}) }()

	waitFor := func(text string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(out.String(), text) {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %q in:\n%s", text, out.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// The command sees a terminal, of the size the window said.
	waitFor("istty")
	waitFor("30 100")

	// A command that reads gets what is typed while it runs.
	_, _ = io.WriteString(typing, "read\r")
	time.Sleep(200 * time.Millisecond)
	_, _ = io.WriteString(typing, "hello\r")
	waitFor("got:hello")

	// A failing command's status is said.
	_, _ = io.WriteString(typing, "fail\r")
	waitFor("tool exited with status 3")

	_, _ = io.WriteString(typing, "exit\r")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the console did not close on exit")
	}
}

// The dock reports its size only once the session exists, after the initial
// command has been handed over. That command must still start at the
// window's size, not at 80x24 -- talosctl dashboard reads it only at start.
func TestConsoleInitialCommandWaitsForTheWindowSize(t *testing.T) {
	if !localpty.Supported() {
		t.Skip("no pseudo-terminal on this OS")
	}
	c := &Console{Path: fakeTool(t), Name: "tool"}
	in, typing := io.Pipe()
	defer func() { _ = typing.Close() }()
	var out syncBuffer
	go func() { _ = c.Run(context.Background(), in, &out, []string{"tty"}) }()

	time.Sleep(50 * time.Millisecond)
	c.Resize(132, 40)

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "40 132") {
		if time.Now().After(deadline) {
			t.Fatalf("the initial command did not see the window's size:\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = io.WriteString(typing, "exit\r")
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
