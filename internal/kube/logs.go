package kube

// Following logs.
//
// One stream per container, gathered into one view. A pod's containers are its
// own; a workload's are every container of every pod its selector matches, so
// watching a rollout is one view rather than a tab per pod.
//
// Lines do not go to the frontend as they arrive. A single chatty container
// emits thousands a second, and one event per line would drown the bridge to
// the window -- so they gather in a batcher and cross in groups.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// tailLines is how much history a stream opens with. Enough to see what led up
// to now without pulling a week of logs through the bridge.
const tailLines int64 = 1000

// batchSize and batchEvery are what the batcher hands over on: whichever comes
// first. A tenth of a second is under what reads as a delay, and 200 lines is
// more than a window shows at once.
const batchSize = 200
const batchEvery = 100 * time.Millisecond

// rescanEvery is how often a workload's pods are looked at again, so a rollout
// picks up the pods it creates. A re-list rather than an informer: the informer
// machinery serves the tables, and borrowing it here would tie two things
// together that change for different reasons.
const rescanEvery = 5 * time.Second

// ContainerRef names one stream: a container, in a pod.
type ContainerRef struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	// Init marks a container that runs to completion before the others start.
	Init bool `json:"init"`
}

// LogLine is one line, tagged with where it came from -- which is the whole
// point of a merged view.
type LogLine struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Text      string `json:"text"`
}

// LogBatch is what crosses to the frontend: the lines that gathered since the
// last one, or the news that a stream has ended.
type LogBatch struct {
	StreamID string    `json:"streamId"`
	Lines    []LogLine `json:"lines"`
	Error    string    `json:"error"`
	Done     bool      `json:"done"`
}

// containersOf names every container a pod runs, init containers first -- the
// order the table's rectangles use, so the two agree wherever both are shown.
func containersOf(pod *unstructured.Unstructured) []ContainerRef {
	name := pod.GetName()
	var out []ContainerRef
	for _, field := range []struct {
		path string
		init bool
	}{{"initContainers", true}, {"containers", false}} {
		for _, raw := range nestedSlice(pod, "spec", field.path) {
			if c := mapString(asMap(raw), "name"); c != "" {
				out = append(out, ContainerRef{Pod: name, Container: c, Init: field.init})
			}
		}
	}
	return out
}

// selectorFor reads the label selector a workload uses to find its pods.
//
// It goes through apimachinery's own converter rather than reading matchLabels
// by hand, so that matchExpressions work and the string is the one the API
// server would build for the same selector.
func selectorFor(obj *unstructured.Unstructured) (string, error) {
	raw, found, err := unstructured.NestedMap(obj.Object, "spec", "selector")
	if err != nil || !found || len(raw) == 0 {
		return "", fmt.Errorf("%s %s selects no pods, so it has no logs to follow", obj.GetKind(), obj.GetName())
	}

	var selector metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector); err != nil {
		return "", fmt.Errorf("reading the pod selector: %w", err)
	}
	parsed, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return "", err
	}
	if parsed.Empty() {
		return "", fmt.Errorf("%s %s selects no pods, so it has no logs to follow", obj.GetKind(), obj.GetName())
	}
	return parsed.String(), nil
}

// ---- the batcher -----------------------------------------------------------

// batcher gathers lines from every container in a view and hands them over in
// groups: when there are `limit` of them, or on a tick, whichever comes first.
//
// Every container streams on its own goroutine into this one, so it is the
// place the lines from different containers are interleaved -- in the order
// they actually arrived, which is the best any merged view can offer.
type batcher struct {
	limit int
	emit  func([]LogLine)

	mu    sync.Mutex
	lines []LogLine
}

func newBatcher(limit int, emit func([]LogLine)) *batcher {
	return &batcher{limit: limit, emit: emit}
}

// add takes one line, handing the batch over if that filled it.
func (b *batcher) add(line LogLine) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	full := len(b.lines) >= b.limit
	b.mu.Unlock()

	if full {
		b.flush()
	}
}

// flush hands over whatever has gathered. Nothing gathered means nothing is
// sent: an empty event is a wake-up for no reason.
func (b *batcher) flush() {
	b.mu.Lock()
	lines := b.lines
	b.lines = nil
	b.mu.Unlock()

	if len(lines) > 0 {
		b.emit(lines)
	}
}

// run hands the batch over on every tick until the context ends, so a quiet
// container's single line does not wait for a batch that will never fill.
//
// The ticker is passed in rather than made here: it is the one part of this
// that a test cannot wait for.
func (b *batcher) run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			b.flush()
			return
		case <-tick:
			b.flush()
		}
	}
}

// ---- streaming -------------------------------------------------------------

// logTargets resolves what a log view covers: the containers of one pod, or of
// every pod a workload currently has.
func (c *clusterClient) logTargets(ctx context.Context, kind, namespace, name string) ([]ContainerRef, error) {
	obj, _, err := c.get(ctx, kind, namespace, name)
	if err != nil {
		return nil, err
	}
	if kind == KindPods {
		return containersOf(obj), nil
	}

	selector, err := selectorFor(obj)
	if err != nil {
		return nil, err
	}
	mapping, err := c.mappingForKind(KindPods)
	if err != nil {
		return nil, err
	}
	pods, err := resourceFor(c.dynamic, mapping, namespace).
		List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	var out []ContainerRef
	for i := range pods.Items {
		out = append(out, containersOf(&pods.Items[i])...)
	}
	return out, nil
}

// notYetRunning reports whether the API server refused a log stream because
// the container has not started yet, rather than because something is wrong.
//
// There is no typed error for it: the API server phrases it in the message,
// always as "is waiting to start: <reason>", where the reason is whatever the
// kubelet is doing -- pulling the image, running the init containers, backing
// off after a crash. Every one of those ends by itself, which is what makes
// this worth waiting through rather than reporting.
func notYetRunning(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "is waiting to start") ||
		strings.Contains(text, "ContainerCreating") ||
		strings.Contains(text, "PodInitializing")
}

// startingReason is the kubelet's word for what a container is waiting on --
// "ContainerCreating", "PodInitializing" -- pulled out of the message so the
// view can say what it is waiting for, and say it again only when it changes.
func startingReason(err error) string {
	_, reason, found := strings.Cut(err.Error(), "is waiting to start: ")
	if !found {
		return "starting"
	}
	return strings.TrimSpace(reason)
}

// retryFirst and retryMax bound the wait between attempts at a container that
// is not up yet. Short to begin with, because most containers are a second or
// two away; capped, because a pod stuck on a bad image would otherwise be
// asked about forever at that rate.
const retryFirst = 500 * time.Millisecond
const retryMax = 5 * time.Second

// follow streams one container into the batcher until the context ends.
//
// A container that is not running yet is waited for rather than reported: a
// log opened the moment a pod is created -- which is exactly when someone
// opens one -- would otherwise come back empty and stay empty, and the only
// way on was to ask for it again. Waiting is for a view that follows; one
// asking for what is there now is answered with what is there now.
//
// Any other failure is reported and dropped rather than taking the view down
// with it: one container of twenty going away -- a pod finishing a rollout, a
// container restarting -- is the ordinary case, and the other nineteen are
// still worth reading.
func (c *clusterClient) follow(ctx context.Context, namespace string, ref ContainerRef, follow bool, into *batcher) {
	open := func(ctx context.Context) (io.ReadCloser, error) {
		return c.typed.CoreV1().Pods(namespace).GetLogs(ref.Pod, &corev1.PodLogOptions{
			Container: ref.Container,
			Follow:    follow,
			TailLines: ptr(tailLines),
		}).Stream(ctx)
	}
	streamContainer(ctx, ref, follow, open, into)
}

// logOpener asks the cluster for one container's stream. Named so the waiting
// around it can be tested without one.
type logOpener func(context.Context) (io.ReadCloser, error)

// streamContainer is follow without the cluster: open, and if the container is
// not up yet, wait and open again.
func streamContainer(ctx context.Context, ref ContainerRef, follow bool, open logOpener, into *batcher) {
	wait := retryFirst
	said := ""
	for {
		stream, err := open(ctx)
		if err == nil {
			readLog(ctx, stream, ref, into)
			_ = stream.Close()
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !follow || !notYetRunning(err) {
			into.add(LogLine{Pod: ref.Pod, Container: ref.Container, Text: "— cannot read: " + err.Error()})
			return
		}
		// Said once per reason, not once per attempt: the view is meant to
		// show why it is empty, not to fill with the same line every second.
		if reason := startingReason(err); reason != said {
			said = reason
			into.add(LogLine{Pod: ref.Pod, Container: ref.Container, Text: "— waiting for the container to start: " + reason})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait *= 2; wait > retryMax {
			wait = retryMax
		}
	}
}

// readLog turns one open stream into lines until it ends.
func readLog(ctx context.Context, stream io.ReadCloser, ref ContainerRef, into *batcher) {
	reader := bufio.NewScanner(stream)
	// A single log line can be far longer than bufio's default 64K -- a stack
	// trace on one line, or JSON logging -- and the default would end the
	// stream on it rather than return it.
	reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for reader.Scan() {
		if ctx.Err() != nil {
			return
		}
		into.add(LogLine{Pod: ref.Pod, Container: ref.Container, Text: reader.Text()})
	}
	if err := reader.Err(); err != nil && ctx.Err() == nil && err != io.EOF {
		into.add(LogLine{Pod: ref.Pod, Container: ref.Container, Text: "— stream ended: " + err.Error()})
	}
}

func ptr[T any](v T) *T { return &v }

// LogContainers names what a log view could follow, for the picker.
func (w *Watcher) LogContainers(kc Context, kind, namespace, name string) ([]ContainerRef, error) {
	out := []ContainerRef{}
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		found, err := c.logTargets(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		out = found
		return nil
	})
	return out, err
}

// Logs follows every chosen container of an object until the context ends.
//
// It blocks; the caller runs it in a goroutine. An empty `containers` follows
// all of them, which is what a view opens on.
//
// A workload is re-listed while it is followed, so a rollout's new pods join
// the view as they appear. A pod is not: a pod's containers are settled when it
// is created, and re-listing one would be a request per five seconds for an
// answer that cannot change.
func (w *Watcher) Logs(
	ctx context.Context,
	kc Context,
	kind, namespace, name string,
	containers []string,
	follow bool,
	emit func([]LogLine),
) error {
	return w.withClient(kc, func(c *clusterClient) error {
		into := newBatcher(batchSize, emit)
		ticker := time.NewTicker(batchEvery)
		defer ticker.Stop()
		go into.run(ctx, ticker.C)

		wanted := map[string]bool{}
		for _, want := range containers {
			wanted[want] = true
		}

		// Which containers already have a stream, so a re-list adds the new
		// pods rather than starting a second stream on every old one.
		streaming := map[ContainerRef]bool{}
		var running sync.WaitGroup

		attach := func() error {
			targets, err := c.logTargets(ctx, kind, namespace, name)
			if err != nil {
				return err
			}
			for _, ref := range targets {
				if len(wanted) > 0 && !wanted[ref.Container] {
					continue
				}
				if streaming[ref] {
					continue
				}
				streaming[ref] = true
				running.Add(1)
				go func(ref ContainerRef) {
					defer running.Done()
					c.follow(ctx, namespace, ref, follow, into)
				}(ref)
			}
			return nil
		}

		// The first attach is the one whose failure matters: nothing is on
		// screen yet, so there is a view waiting to be told why it is empty.
		if err := attach(); err != nil {
			return err
		}

		if follow && kind != KindPods {
			rescan := time.NewTicker(rescanEvery)
			defer rescan.Stop()
			for {
				select {
				case <-ctx.Done():
					running.Wait()
					into.flush()
					return nil
				case <-rescan.C:
					// A failed re-list is not fatal: the streams already open
					// go on, and the next scan tries again.
					_ = attach()
				}
			}
		}

		running.Wait()
		into.flush()
		return nil
	})
}
