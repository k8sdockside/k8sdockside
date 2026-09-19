package services

import (
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

func TestConnectArgsNameTheContextExplicitly(t *testing.T) {
	// The app's whole premise is that several clusters are open at once. A
	// terminal that relied on the current-context would open on whichever
	// cluster the kubeconfig happened to be pointing at, which is the worst
	// kind of bug this feature could have.
	got := connectArgs(kube.Context{File: "/home/dev/.kube/prod", Name: "prod-eu"})
	want := []string{"--kubeconfig", "/home/dev/.kube/prod", "--context", "prod-eu"}

	if len(got) != len(want) {
		t.Fatalf("connectArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("connectArgs() = %v, want %v", got, want)
		}
	}
}

func TestShellChainTriesEveryShellInOrder(t *testing.T) {
	got := shellChain([]string{"zsh", "bash", "sh"})

	// The loop is run by the last candidate, which is the one most likely to
	// exist: an external kubectl gets one command, so the trying has to happen
	// inside the container.
	if got[0] != "sh" || got[1] != "-c" {
		t.Fatalf("the chain is run by %v, want sh -c", got[:2])
	}

	script := got[2]
	for _, shell := range []string{"zsh", "bash", "sh"} {
		if !strings.Contains(script, "'"+shell+"'") {
			t.Errorf("the chain does not try %s: %s", shell, script)
		}
	}
	// Ordered as the user wrote them, not as the loop happens to reach them.
	if strings.Index(script, "'zsh'") > strings.Index(script, "'bash'") {
		t.Errorf("the chain tries bash before zsh: %s", script)
	}
	// A container with none of them says so rather than exiting silently.
	if !strings.Contains(script, "exit 127") {
		t.Errorf("the chain does not fail when nothing runs: %s", script)
	}
}

func TestShellChainFallsBackWhenNothingWasChosen(t *testing.T) {
	got := shellChain(nil)
	if len(got) != 3 || got[0] != kube.DefaultShells[len(kube.DefaultShells)-1] {
		t.Fatalf("an empty list produced %v", got)
	}
}

func TestShellChainQuotesAShellNameThatWouldEndTheString(t *testing.T) {
	// The name goes into a single-quoted shell string. A settings file is a
	// file somebody can edit, and what is in it must not become script.
	got := shellChain([]string{"sh'; rm -rf /; echo '"})
	if strings.Contains(got[2], "; rm -rf /; echo ") && !strings.Contains(got[2], `'\''`) {
		t.Fatalf("a shell name escaped its quoting: %s", got[2])
	}
}

func TestChunkerHandsOverWhatGathered(t *testing.T) {
	var got [][]byte
	batch := newChunker(func(data []byte) { got = append(got, append([]byte{}, data...)) })

	if _, err := batch.Write([]byte("hello ")); err != nil {
		t.Fatalf("writing: %v", err)
	}
	// Nothing yet: a terminal printing a large file writes far faster than a
	// window can be told about it, so output gathers until there is a screenful
	// or the tick comes round.
	if len(got) != 0 {
		t.Fatalf("a small write was sent straight through: %v", got)
	}

	if _, err := batch.Write([]byte("world")); err != nil {
		t.Fatalf("writing: %v", err)
	}
	batch.flush()

	if len(got) != 1 || string(got[0]) != "hello world" {
		t.Fatalf("the batch came out as %q", got)
	}

	// An empty flush is a wake-up for no reason.
	batch.flush()
	if len(got) != 1 {
		t.Fatalf("an empty flush emitted something: %v", got)
	}
}

func TestChunkerHandsOverAsSoonAsThereIsAScreenful(t *testing.T) {
	var batches int
	batch := newChunker(func([]byte) { batches++ })

	if _, err := batch.Write(make([]byte, terminalBatchBytes+1)); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if batches != 1 {
		t.Fatalf("a full buffer waited for the tick: %d batches", batches)
	}
}
