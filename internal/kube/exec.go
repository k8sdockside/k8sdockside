package kube

// Opening a shell in a container.
//
// An exec is not a request that returns an answer: it is a request that turns
// into a pair of streams and stays open until one end hangs up. That shape is
// why it lives here rather than beside the one-shot actions -- what it needs
// from a cluster is not a client but the connection under one, because the
// dialer that upgrades the request carries the credentials, the TLS and the
// proxy settings, and none of those can be recovered from a built client.
//
// Two things are worth knowing about what is below:
//
//   - The shell is chosen by trying. A container knows what it has; we do not,
//     and a container image is free to have bash, only sh, or neither. So the
//     candidates are attempted in order and a "no such executable" is taken as
//     "try the next one" rather than as the end of it.
//   - Websocket first, SPDY second. Modern API servers speak the websocket
//     protocol; SPDY is what everything before 1.29 speaks, and some proxies in
//     between will only pass one of the two. client-go's fallback executor
//     tries the first and quietly uses the second when the upgrade is refused,
//     which is what kubectl itself does.

import (
	"context"
	"fmt"
	"io"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
	"k8s.io/streaming/pkg/httpstream"
)

// DefaultShells is what a terminal tries, in order, when the user has not said.
// bash for the comfort of it, sh because a container that has anything has sh.
var DefaultShells = []string{"bash", "sh"}

// ExecTarget is the one container a terminal is attached to. A pod is one of
// these on its own; a workload resolves to one of its pods, because "exec into
// a Deployment" means "exec into one of the things it is running".
type ExecTarget struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

// TerminalSize is a window's size in cells, as the frontend measures it. It is
// its own type rather than remotecommand's so that nothing above this package
// has to import client-go to say how big a terminal is.
type TerminalSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// TerminalChunk is what crosses to the frontend: some output, or the news that
// the session has ended.
//
// Data is base64 because a terminal's output is bytes and not text. A chunk
// boundary falls wherever the network put it, which is regularly in the middle
// of a UTF-8 sequence and occasionally in the middle of an escape sequence --
// so the bytes are carried whole and reassembled by the decoder at the far end,
// rather than being mangled into valid JSON on the way.
type TerminalChunk struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
	Error     string `json:"error"`
	Done      bool   `json:"done"`
	// Exited is set on the last chunk of a local terminal whose program
	// ended by itself, with ExitCode its status -- which the tab shows,
	// since a shell that exited non-zero is not an error to report but is
	// worth knowing about.
	Exited   bool `json:"exited"`
	ExitCode int  `json:"exitCode"`
}

// sizeQueue feeds window sizes to client-go, which asks for the next one and
// blocks until there is one. A nil answer ends the resize stream, which is what
// the context being cancelled means here.
type sizeQueue struct {
	ctx context.Context
	in  <-chan TerminalSize
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case <-q.ctx.Done():
		return nil
	case size, ok := <-q.in:
		if !ok {
			return nil
		}
		return &remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
	}
}

// counted is a writer that remembers whether anything has gone through it. The
// shell fallback below needs to know: a failure after output has appeared is a
// shell that ran and then died, and retrying it with a different shell would
// scroll a second prompt into a session the user is already reading.
type counted struct {
	to      io.Writer
	written atomic.Bool
}

func (c *counted) Write(p []byte) (int, error) {
	if len(p) > 0 {
		c.written.Store(true)
	}
	return c.to.Write(p)
}

// missingExecutable reports whether an exec failed because what it was asked to
// run is not in the image.
//
// The runtime says so in two ways depending on which one it is: an exit status
// -- 127 for "not found", 126 for "found but not executable" -- or a plain
// error naming the file. Both are matched, because a shell that is not there is
// the ordinary case this whole fallback exists for and must not be reported to
// the user as though the cluster refused them.
func missingExecutable(err error) bool {
	if err == nil {
		return false
	}
	var coded utilexec.CodeExitError
	if ok := asCodeExit(err, &coded); ok && (coded.Code == 126 || coded.Code == 127) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, mark := range []string{
		"executable file not found",
		"no such file or directory",
		"exec format error",
		"exit code 127",
		"exit code 126",
	} {
		if strings.Contains(text, mark) {
			return true
		}
	}
	return false
}

// asCodeExit unwraps to client-go's exit error, which carries the status of the
// process rather than of the request.
func asCodeExit(err error, out *utilexec.CodeExitError) bool {
	for e := err; e != nil; {
		if coded, ok := e.(utilexec.CodeExitError); ok { //nolint:errorlint // CodeExitError is a value type; the chain is walked by hand below.
			*out = coded
			return true
		}
		unwrapped, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = unwrapped.Unwrap()
	}
	return false
}

// exec runs one command in a container with a terminal attached, and blocks
// until it ends.
func (c *clusterClient) exec(
	ctx context.Context,
	target ExecTarget,
	command []string,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
) error {
	req := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(target.Pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: target.Container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			// Deliberately off: with a TTY the two streams are the same stream,
			// and asking for stderr as well is rejected by the API server.
			Stderr: false,
			TTY:    true,
		}, scheme.ParameterCodec)

	return c.stream(ctx, req.URL(), stdin, stdout, sizes)
}

// stream upgrades one exec or attach request into a terminal, and blocks until
// it ends. Websocket first and SPDY second -- see the top of this file.
func (c *clusterClient) stream(
	ctx context.Context,
	url *neturl.URL,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
) error {
	websocket, err := remotecommand.NewWebSocketExecutor(c.cfg, "GET", url.String())
	if err != nil {
		return err
	}
	spdy, err := remotecommand.NewSPDYExecutor(c.cfg, "POST", url)
	if err != nil {
		return err
	}
	stream, err := remotecommand.NewFallbackExecutor(websocket, spdy, httpstream.IsUpgradeFailure)
	if err != nil {
		return err
	}

	return stream.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Tty:               true,
		TerminalSizeQueue: sizeQueue{ctx: ctx, in: sizes},
	})
}

// ---- handing the user's typing to the attempt that is live -----------------
//
// Trying a second shell after the first turns out not to be in the image is
// only safe if the first one lets go of the keyboard on its way out, and it
// does not: client-go copies stdin on a goroutine of its own, which sits
// blocked in Read on whatever reader it was given. A failed attempt leaves that
// goroutine there, and the *next* thing the user types is delivered to it --
// read from the shared reader, written to a closed stream, and gone. The shell
// on screen sees nothing and appears to have hung.
//
// So neither the reader nor the size channel is handed to an attempt directly.
// One pump owns each, and delivers to whichever attempt is currently live;
// ending an attempt closes its end, which unblocks its copier for good.

// stdinCarry is the most that is held for an attempt that has not started yet.
// A fallback takes milliseconds, so this only ever holds a keystroke or two --
// the cap is here so that a paste into a terminal that is failing to open
// cannot grow without limit.
const stdinCarry = 64 << 10

// stdinRelay carries what the user types to the attempt that is live.
type stdinRelay struct {
	mu      sync.Mutex
	to      *io.PipeWriter
	pending []byte
}

// newStdinRelay starts reading the user's input. The pump ends when the reader
// does, which is when the session's own pipe is closed.
func newStdinRelay(ctx context.Context, from io.Reader) *stdinRelay {
	relay := &stdinRelay{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := from.Read(buf)
			if n > 0 {
				relay.deliver(buf[:n])
			}
			if err != nil || ctx.Err() != nil {
				return
			}
		}
	}()
	return relay
}

// attach gives the next attempt a reader of its own, starting with anything
// typed while nothing was listening.
func (r *stdinRelay) attach() io.Reader {
	reader, writer := io.Pipe()

	r.mu.Lock()
	r.to = writer
	carried := r.pending
	r.pending = nil
	r.mu.Unlock()

	if len(carried) > 0 {
		// On its own goroutine: a pipe write blocks until the far end reads,
		// and the far end is the exec that has not been started yet.
		go func() { _, _ = writer.Write(carried) }()
	}
	return reader
}

// detach ends an attempt's reader, unblocking the copier that was on it.
func (r *stdinRelay) detach() {
	r.mu.Lock()
	writer := r.to
	r.to = nil
	r.mu.Unlock()

	if writer != nil {
		_ = writer.Close()
	}
}

// deliver hands one read to the live attempt, or holds it for the next.
func (r *stdinRelay) deliver(p []byte) {
	r.mu.Lock()
	writer := r.to
	r.mu.Unlock()

	if writer != nil {
		if _, err := writer.Write(p); err == nil {
			return
		}
		// The attempt it was meant for has gone. Fall through and hold it.
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending)+len(p) <= stdinCarry {
		r.pending = append(r.pending, p...)
	}
}

// sizeRelay does the same for window sizes, with one difference: only the
// latest one matters, so a new attempt is told the size straight away rather
// than being left at the 80x24 a shell assumes.
type sizeRelay struct {
	mu   sync.Mutex
	to   chan TerminalSize
	last TerminalSize
	seen bool
}

func newSizeRelay(ctx context.Context, from <-chan TerminalSize) *sizeRelay {
	relay := &sizeRelay{}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case size, ok := <-from:
				if !ok {
					return
				}
				relay.deliver(size)
			}
		}
	}()
	return relay
}

func (r *sizeRelay) deliver(size TerminalSize) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.last, r.seen = size, true
	if r.to == nil {
		return
	}
	// Latest wins: a queue of sizes would replay every intermediate width of a
	// window somebody dragged.
	select {
	case <-r.to:
	default:
	}
	select {
	case r.to <- size:
	default:
	}
}

func (r *sizeRelay) attach() chan TerminalSize {
	sizes := make(chan TerminalSize, 1)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.to = sizes
	if r.seen {
		sizes <- r.last
	}
	return sizes
}

// detach closes an attempt's channel, which is how client-go's resize loop is
// told there will be no more of them.
func (r *sizeRelay) detach() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.to != nil {
		close(r.to)
		r.to = nil
	}
}

// shell opens a terminal on a container, trying each candidate in turn.
//
// `prefix` is what the command is wrapped in -- nothing for a container, and
// `chroot /host` for a node shell, where the point of the pod is to be a way
// into the filesystem underneath it.
//
// Retrying with the next shell is safe only before anything has been read from
// the user or written to the screen. Both hold in the case this is for: a shell
// that is not in the image fails when the runtime tries to start it, which is
// before the terminal has drawn a single character.
func (c *clusterClient) shell(
	ctx context.Context,
	target ExecTarget,
	prefix []string,
	shells []string,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
) error {
	if len(shells) == 0 {
		shells = DefaultShells
	}

	typed := newStdinRelay(ctx, stdin)
	defer typed.detach()
	resizes := newSizeRelay(ctx, sizes)
	defer resizes.detach()

	screen := &counted{to: stdout}
	var last error
	for _, sh := range shells {
		if ctx.Err() != nil {
			return nil
		}
		command := append(append([]string{}, prefix...), sh)

		err := c.exec(ctx, target, command, typed.attach(), screen, resizes.attach())
		// Whether it worked or not, this attempt is over: its copier has to be
		// let go before the next one is given the keyboard.
		typed.detach()
		resizes.detach()

		if err == nil {
			return nil
		}
		last = err
		if screen.written.Load() || !missingExecutable(err) {
			return err
		}
	}

	// Every candidate was missing. Say so in terms of the thing the user can
	// actually change, rather than repeating the runtime's last complaint.
	return fmt.Errorf(
		"none of %s is in this container -- name one that is under Settings → Terminal (%w)",
		strings.Join(shells, ", "), last,
	)
}

// runningPodFor picks one pod of a workload to attach to.
//
// Running rather than merely existing: a pod that is pending has no container
// to exec into, and one that has finished would take the terminal with it. The
// first match is taken, in the API server's own order, so two people opening a
// shell on the same Deployment land on the same pod.
func (c *clusterClient) runningPodFor(ctx context.Context, kind, namespace, name string) (*unstructured.Unstructured, error) {
	obj, _, err := c.get(ctx, kind, namespace, name)
	if err != nil {
		return nil, err
	}
	if kind == KindPods {
		return obj, nil
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
	for i := range pods.Items {
		phase, _, _ := unstructured.NestedString(pods.Items[i].Object, "status", "phase")
		if phase == "Running" {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf("%s %s has no running pod to open a shell in", kind, name)
}

// ExecTarget resolves what a terminal opened on an object attaches to: which
// pod, and which container in it.
//
// An empty container means "whichever it would be by default" -- the first one
// the pod declares, matching what `kubectl exec` picks without -c. Init
// containers are not candidates: by the time a pod is running they have
// finished, and a terminal in one would have nothing to attach to.
func (w *Watcher) ExecTarget(kc Context, kind, namespace, name, container string) (ExecTarget, error) {
	var out ExecTarget
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		pod, err := c.runningPodFor(ctx, kind, namespace, name)
		if err != nil {
			return err
		}

		out = ExecTarget{Namespace: pod.GetNamespace(), Pod: pod.GetName(), Container: container}
		if out.Container != "" {
			return nil
		}
		for _, raw := range nestedSlice(pod, "spec", "containers") {
			if first := mapString(asMap(raw), "name"); first != "" {
				out.Container = first
				return nil
			}
		}
		return fmt.Errorf("pod %s declares no containers", pod.GetName())
	})
	return out, err
}

// Shell attaches a terminal to one container and blocks until it closes.
//
// It blocks for the same reason Logs does: the caller runs it on its own
// goroutine and cancels the context to hang up. Holding the cluster for the
// session's whole length is deliberate -- it is one connection, already open,
// and releasing it under a live stream would be releasing something in use.
func (w *Watcher) Shell(
	ctx context.Context,
	kc Context,
	target ExecTarget,
	shells []string,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
) error {
	return w.withClient(kc, func(c *clusterClient) error {
		return c.shell(ctx, target, nil, shells, stdin, stdout, sizes)
	})
}
