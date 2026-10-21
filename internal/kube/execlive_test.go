package kube

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The live checks for the two things in this package that are streams rather
// than requests. They run against a real cluster, so they are opt-in in the
// same way the rest of the live tests are -- see liveContext -- and they need
// to be told what to open:
//
//	K8SDOCKSIDE_TEST_KUBECONFIG=~/.kube/config \
//	K8SDOCKSIDE_TEST_CONTEXT=admin@hrw \
//	K8SDOCKSIDE_TEST_POD=argocd/argocd-redis-... \
//	K8SDOCKSIDE_TEST_SERVICE=argocd/argocd-server \
//	go test ./internal/kube/ -run Live -v
//
// They are the only check that an exec and a port-forward actually work: both
// go through a protocol upgrade that nothing in a unit test can stand in for,
// and both have failure modes -- a shell that is not in the image, a service
// port that lands on a different port on the pod -- that only appear against a
// real API server.
func liveTarget(t *testing.T, variable string) (namespace, name string) {
	t.Helper()

	value := os.Getenv(variable)
	if value == "" {
		t.Skipf("set %s to <namespace>/<name> to run this", variable)
	}
	namespace, name, found := strings.Cut(value, "/")
	if !found || namespace == "" || name == "" {
		t.Fatalf("%s = %q, want <namespace>/<name>", variable, value)
	}
	return namespace, name
}

func TestLiveShellRunsWhatIsTypedIntoIt(t *testing.T) {
	kc := liveContext(t)
	namespace, name := liveTarget(t, "K8SDOCKSIDE_TEST_POD")

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	target, err := w.ExecTarget(kc, KindPods, namespace, name, "")
	if err != nil {
		t.Fatalf("ExecTarget: %v", err)
	}
	t.Logf("attached to %s/%s in %s", target.Pod, target.Container, target.Namespace)

	stdin, typing := io.Pipe()
	var screen strings.Builder
	sizes := make(chan TerminalSize, 1)
	sizes <- TerminalSize{Cols: 100, Rows: 30}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ended := make(chan error, 1)
	go func() { ended <- w.Shell(ctx, kc, target, DefaultShells, stdin, &screen, sizes) }()

	// Long enough for the shell to have drawn a prompt -- including, on an
	// image without bash, for the fallback to the next candidate.
	time.Sleep(3 * time.Second)
	if _, err := typing.Write([]byte("echo k8sdockside-live-check; exit\n")); err != nil {
		t.Fatalf("typing: %v", err)
	}

	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("Shell: %v\nwhat was on screen:\n%s", err, screen.String())
		}
	case <-ctx.Done():
		t.Fatalf("the shell never ended; what was on screen:\n%s", screen.String())
	}

	t.Logf("terminal:\n%s", screen.String())
	// The point of the check: what the user typed reached the shell that is
	// live, and not the copier of an attempt that already failed.
	if !strings.Contains(screen.String(), "k8sdockside-live-check") {
		t.Fatal("the shell did not run what was typed into it")
	}
}

func TestLiveForwardCarriesTraffic(t *testing.T) {
	kc := liveContext(t)
	namespace, name := liveTarget(t, "K8SDOCKSIDE_TEST_SERVICE")

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	options, err := w.ForwardablePorts(kc, KindServices, namespace, name)
	if err != nil {
		t.Fatalf("ForwardablePorts: %v", err)
	}
	if len(options) == 0 {
		t.Fatalf("service %s/%s offers no ports", namespace, name)
	}
	t.Logf("ports: %+v", options)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listening := make(chan int32, 1)
	ended := make(chan error, 1)
	go func() {
		ended <- w.Forward(ctx, kc, KindServices, namespace, name, options[0].Port, 0,
			func(local int32, at ForwardEndpoint) {
				t.Logf("listening on %d, reaching %s/%s:%d", local, at.Namespace, at.Pod, at.Port)
				listening <- local
			},
			func(note string) { t.Logf("forwarder: %s", note) },
		)
	}()

	var local int32
	select {
	case local = <-listening:
	case err := <-ended:
		t.Fatalf("Forward: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the forward never came up")
	}
	// A local port of 0 means "any free one", and the answer is the port it
	// actually got -- which is what the link beside a forward uses.
	if local == 0 {
		t.Fatal("the forward came up on no port")
	}

	address := net.JoinHostPort("localhost", strconv.Itoa(int(local)))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatalf("connecting through the tunnel: %v", err)
	}
	_ = conn.Close()

	// Cancelling is how the app disconnects one, and it must actually stop.
	cancel()
	select {
	case err := <-ended:
		// Being cancelled may or may not surface as an error; either way the
		// forward stopped, which is all this checks.
		t.Logf("forward stopped: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the forward did not stop when it was cancelled")
	}

	if _, err := net.DialTimeout("tcp", address, 2*time.Second); err == nil {
		t.Fatal("the listener is still there after the forward was stopped")
	}
}

func TestLiveForwardReportsAServiceItCannotReach(t *testing.T) {
	kc := liveContext(t)

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A service that does not exist is the ordinary way this fails, and the
	// row that reports it should say what the API server said.
	err := w.Forward(ctx, kc, KindServices, "default", "k8sdockside-does-not-exist", 80, 0, nil, nil)
	if err == nil {
		t.Fatal("forwarding to a service that does not exist succeeded")
	}
	t.Logf("refused with: %v", err)
}
