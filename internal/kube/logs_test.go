package kube

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestContainersOfNamesEveryContainerAPodRuns(t *testing.T) {
	pod := podWith(
		map[string]any{
			"initContainers": []any{map[string]any{"name": "wait-for-db"}},
			"containers": []any{
				map[string]any{"name": "app"},
				map[string]any{"name": "sidecar"},
			},
		},
		map[string]any{},
	)

	got := containersOf(pod)

	if len(got) != 3 {
		t.Fatalf("found %d containers, want 3: %+v", len(got), got)
	}
	// Init containers first, as the rectangles draw them, so the two orders
	// agree wherever both are shown.
	if got[0].Container != "wait-for-db" || !got[0].Init {
		t.Errorf("first container = %+v, want the init container", got[0])
	}
	for _, c := range got {
		if c.Pod != "web" {
			t.Errorf("container %q says it is in pod %q, want web", c.Container, c.Pod)
		}
	}
}

func TestSelectorForAWorkloadReadsItsMatchLabels(t *testing.T) {
	deployment := podWith(
		map[string]any{"selector": map[string]any{
			"matchLabels": map[string]any{"app": "web", "tier": "front"},
		}},
		map[string]any{},
	)

	got, err := selectorFor(deployment)

	if err != nil {
		t.Fatalf("selectorFor: %v", err)
	}
	// Both labels, and in a stable order: an unordered map would otherwise make
	// the request text differ run to run.
	if got != "app=web,tier=front" {
		t.Errorf("selector = %q, want app=web,tier=front", got)
	}
}

func TestSelectorForHandlesMatchExpressions(t *testing.T) {
	deployment := podWith(
		map[string]any{"selector": map[string]any{
			"matchExpressions": []any{
				map[string]any{"key": "tier", "operator": "In", "values": []any{"front", "back"}},
			},
		}},
		map[string]any{},
	)

	got, err := selectorFor(deployment)

	if err != nil {
		t.Fatalf("selectorFor: %v", err)
	}
	if !strings.Contains(got, "tier in (back,front)") {
		t.Errorf("selector = %q, which does not carry the expression", got)
	}
}

// A kind with nothing to select by cannot be followed, and saying so is better
// than following every pod in the namespace.
func TestSelectorForSaysWhenThereIsNothingToSelectBy(t *testing.T) {
	if _, err := selectorFor(podWith(map[string]any{}, map[string]any{})); err == nil {
		t.Error("selectorFor accepted an object with no selector")
	}
}

// ---- the batcher -----------------------------------------------------------

/** Collects what the batcher hands over, safely under concurrent adds. */
type handovers struct {
	mu    sync.Mutex
	batch [][]LogLine
}

func (h *handovers) take(lines []LogLine) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.batch = append(h.batch, lines)
}

func (h *handovers) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.batch)
}

func (h *handovers) lines() []LogLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []LogLine
	for _, b := range h.batch {
		out = append(out, b...)
	}
	return out
}

func line(text string) LogLine {
	return LogLine{Pod: "web", Container: "app", Text: text}
}

func TestBatcherHoldsLinesUntilThereAreEnoughOfThem(t *testing.T) {
	got := &handovers{}
	b := newBatcher(3, got.take)

	b.add(line("one"))
	b.add(line("two"))

	if got.count() != 0 {
		t.Errorf("handed over %d batches before the batch was full", got.count())
	}
}

// The reason this exists: a chatty container emits thousands of lines a second,
// and one event per line would drown the bridge to the window.
func TestBatcherHandsOverWhenItIsFull(t *testing.T) {
	got := &handovers{}
	b := newBatcher(3, got.take)

	b.add(line("one"))
	b.add(line("two"))
	b.add(line("three"))

	if got.count() != 1 {
		t.Fatalf("handed over %d batches, want 1", got.count())
	}
	if texts := textsOf(got.lines()); strings.Join(texts, ",") != "one,two,three" {
		t.Errorf("lines = %v, want them in the order they arrived", texts)
	}
}

func TestBatcherStartsAfreshAfterHandingOver(t *testing.T) {
	got := &handovers{}
	b := newBatcher(2, got.take)

	b.add(line("one"))
	b.add(line("two"))
	b.add(line("three"))
	b.flush()

	if got.count() != 2 {
		t.Fatalf("handed over %d batches, want 2", got.count())
	}
	if texts := textsOf(got.lines()); strings.Join(texts, ",") != "one,two,three" {
		t.Errorf("lines = %v, want each handed over once", texts)
	}
}

// A quiet container still has to reach the window: a line that arrives alone
// must not wait for a batch that will never fill.
func TestBatcherHandsOverOnATickEvenWhenNotFull(t *testing.T) {
	got := &handovers{}
	b := newBatcher(100, got.take)
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { b.run(ctx, ticks); close(done) }()

	b.add(line("alone"))
	ticks <- time.Now()
	// A second tick, which cannot be served until the first has been.
	ticks <- time.Now()

	if got.count() == 0 {
		t.Error("a line sat in the batcher through a tick")
	}
	cancel()
	<-done
}

// An empty tick must not send an event carrying nothing.
func TestBatcherSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	got := &handovers{}
	b := newBatcher(100, got.take)
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { b.run(ctx, ticks); close(done) }()

	ticks <- time.Now()
	ticks <- time.Now()

	if got.count() != 0 {
		t.Errorf("handed over %d empty batches", got.count())
	}
	cancel()
	<-done
}

// Every container streams on its own goroutine into one batcher.
func TestBatcherTakesLinesFromManyContainersAtOnce(t *testing.T) {
	got := &handovers{}
	b := newBatcher(10, got.take)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.add(line("x"))
		}()
	}
	wg.Wait()
	b.flush()

	if n := len(got.lines()); n != 20 {
		t.Errorf("kept %d lines of 20", n)
	}
}

func textsOf(lines []LogLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Text)
	}
	return out
}

// ---- waiting for a container that is not up yet ----------------------------

func TestNotYetRunningTellsStartingApartFromBroken(t *testing.T) {
	starting := []string{
		`container "app" in pod "web-0" is waiting to start: ContainerCreating`,
		`container "app" in pod "web-0" is waiting to start: PodInitializing`,
		`container "app" in pod "web-0" is waiting to start: trying and failing to pull image`,
	}
	for _, text := range starting {
		if !notYetRunning(errors.New(text)) {
			t.Errorf("%q was not taken for a container that is still starting", text)
		}
	}
	broken := []string{
		`pods "web-0" not found`,
		`pods "web-0" is forbidden: User "me" cannot get resource "pods/log"`,
		`the server could not find the requested resource`,
	}
	for _, text := range broken {
		if notYetRunning(errors.New(text)) {
			t.Errorf("%q was taken for a container that is still starting", text)
		}
	}
	if notYetRunning(nil) {
		t.Error("no error at all was taken for a container that is still starting")
	}
}

func TestStartingReasonIsTheKubeletsOwnWord(t *testing.T) {
	got := startingReason(errors.New(`container "app" in pod "web-0" is waiting to start: ContainerCreating`))
	if got != "ContainerCreating" {
		t.Errorf("startingReason gave %q, want ContainerCreating", got)
	}
	if got := startingReason(errors.New("something else entirely")); got != "starting" {
		t.Errorf("an unphrased error gave %q, want starting", got)
	}
}

// The bug this covers: a log opened on a pod that was still being created came
// back with one "cannot read" line and ended, and the only way to the log was
// to ask for it again -- and again, until the container happened to be up.
func TestAViewThatFollowsWaitsForAContainerThatIsStillStarting(t *testing.T) {
	var attempts atomic.Int32
	open := func(context.Context) (io.ReadCloser, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New(`container "app" in pod "web-0" is waiting to start: ContainerCreating`)
		}
		return io.NopCloser(strings.NewReader("first\nsecond\n")), nil
	}

	var got []LogLine
	var mu sync.Mutex
	into := newBatcher(1000, func(lines []LogLine) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, lines...)
	})

	streamContainer(t.Context(), ContainerRef{Pod: "web-0", Container: "app"}, true, open, into)
	into.flush()

	if n := attempts.Load(); n != 3 {
		t.Errorf("the stream was opened %d times, want it tried again until the container was up", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("lines: %v", got)
	}
	// One waiting line, said once for the one reason, and then the log.
	if !strings.Contains(got[0].Text, "waiting for the container to start: ContainerCreating") {
		t.Errorf("the first line was %q, want it to say what it is waiting for", got[0].Text)
	}
	if got[1].Text != "first" || got[2].Text != "second" {
		t.Errorf("the log itself did not arrive: %v", got[1:])
	}
}

// A view showing what is there now rather than following is answered now: it
// has no tail to wait for, and a reader waiting on a spinner is worse than a
// reader told the container has not started.
func TestAViewThatDoesNotFollowSaysSoRatherThanWaiting(t *testing.T) {
	var attempts atomic.Int32
	open := func(context.Context) (io.ReadCloser, error) {
		attempts.Add(1)
		return nil, errors.New(`container "app" in pod "web-0" is waiting to start: ContainerCreating`)
	}

	var got []LogLine
	into := newBatcher(1000, func(lines []LogLine) { got = append(got, lines...) })
	streamContainer(t.Context(), ContainerRef{Pod: "web-0", Container: "app"}, false, open, into)
	into.flush()

	if n := attempts.Load(); n != 1 {
		t.Errorf("the stream was opened %d times, want once", n)
	}
	if len(got) != 1 || !strings.Contains(got[0].Text, "cannot read") {
		t.Errorf("lines: %v, want the one line saying it could not be read", got)
	}
}

// Anything that is not a container starting is still reported at once: waiting
// for a pod that does not exist, or that this user may not read, would be
// waiting forever.
func TestAFailureThatIsNotStartingIsReportedAtOnce(t *testing.T) {
	var attempts atomic.Int32
	open := func(context.Context) (io.ReadCloser, error) {
		attempts.Add(1)
		return nil, errors.New(`pods "web-0" is forbidden`)
	}
	var got []LogLine
	into := newBatcher(1000, func(lines []LogLine) { got = append(got, lines...) })
	streamContainer(t.Context(), ContainerRef{Pod: "web-0", Container: "app"}, true, open, into)
	into.flush()

	if n := attempts.Load(); n != 1 {
		t.Errorf("the stream was opened %d times, want once", n)
	}
	if len(got) != 1 || !strings.Contains(got[0].Text, "forbidden") {
		t.Errorf("lines: %v, want the refusal", got)
	}
}

// Closing the view stops the waiting: the goroutine has to go with the tab.
func TestClosingTheViewStopsWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	open := func(context.Context) (io.ReadCloser, error) {
		return nil, errors.New(`container "app" in pod "web-0" is waiting to start: ContainerCreating`)
	}
	into := newBatcher(1000, func([]LogLine) {})

	done := make(chan struct{})
	go func() {
		streamContainer(ctx, ContainerRef{Pod: "web-0", Container: "app"}, true, open, into)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream went on waiting after the view was closed")
	}
}
