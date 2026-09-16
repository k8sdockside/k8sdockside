package kube

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ping is the reachability check behind the sidebar's connection indicator, so
// unlike the rest of the live layer it has to be testable without a cluster:
// these run against an httptest server standing in for an API server.

// kubeconfigFor writes a single-context kubeconfig pointing at server and
// returns the parsed context.
func kubeconfigFor(t *testing.T, server string) Context {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	body := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: %s
contexts:
  - name: test
    context:
      cluster: test
      user: test
users:
  - name: test
    user: {}
`, server)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing kubeconfig: %v", err)
	}

	file := ParseFile(path, SourceManual)
	if file.Error != "" {
		t.Fatalf("parsing the kubeconfig we just wrote: %s", file.Error)
	}
	if len(file.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(file.Contexts))
	}
	return file.Contexts[0]
}

func TestPingReachesAServerThatAnswers(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"major":"1","minor":"31","gitVersion":"v1.31.0"}`)
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	if err := w.Ping(kubeconfigFor(t, srv.URL)); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if asked != "/version" {
		t.Errorf("probed %q, want /version -- the cheapest call that proves TLS and credentials", asked)
	}
}

func TestPingFailsWhenNothingIsListening(t *testing.T) {
	// A server started and immediately stopped leaves a port nobody is on,
	// which is exactly the "connection refused" the sidebar has to show.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	err := w.Ping(kubeconfigFor(t, url))
	if err == nil {
		t.Fatal("Ping succeeded against a closed port, want an error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want it to carry the underlying connection failure", err)
	}
}

func TestPingReportsAnUnauthorisedServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"kind":"Status","code":401,"message":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	err := w.Ping(kubeconfigFor(t, srv.URL))
	if err == nil {
		t.Fatal("Ping succeeded against a 401, want an error -- reachable is not the same as usable")
	}
}

// brokenKubeconfigFor writes a kubeconfig whose user points at a client
// certificate that does not exist, which is enough for client-go to refuse to
// build a client at all -- no server needed.
func brokenKubeconfigFor(t *testing.T) Context {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	body := `apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: https://127.0.0.1:1
contexts:
  - name: test
    context:
      cluster: test
      user: test
users:
  - name: test
    user:
      client-certificate: /nonexistent/client.crt
      client-key: /nonexistent/client.key
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing kubeconfig: %v", err)
	}
	file := ParseFile(path, SourceManual)
	if file.Error != "" || len(file.Contexts) != 1 {
		t.Fatalf("parsing the kubeconfig we just wrote: %+v", file)
	}
	return file.Contexts[0]
}

// pinged runs one ping against an answering server and returns the watcher and
// the context, for the lifecycle tests below.
func pinged(t *testing.T) (*Watcher, Context) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"major":"1","minor":"31"}`)
	}))
	t.Cleanup(srv.Close)

	w := NewWatcher(func(Snapshot) {})
	t.Cleanup(w.Close)

	kc := kubeconfigFor(t, srv.URL)
	if err := w.Ping(kc); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	return w, kc
}

// A ping gives its reference back but does not throw the client away: the
// context is likely to be opened next, and dropping the client would make that
// build it -- kubeconfig, credential plugin, discovery -- from nothing again.
func TestPingLeavesTheClientForTheNextCaller(t *testing.T) {
	w, kc := pinged(t)

	w.mu.Lock()
	first, ok := w.clusters[kc.ID]
	refs, armed := 0, false
	if ok {
		refs, armed = first.refs, first.evict != nil
	}
	w.mu.Unlock()

	if !ok {
		t.Fatal("the client was dropped right after the ping, want it kept for the next caller")
	}
	if refs != 0 {
		t.Errorf("refs = %d after a ping, want 0: the ping must give its reference back", refs)
	}
	if !armed {
		t.Error("no eviction armed on the idle client, want it forgotten after idleGrace")
	}

	if err := w.Ping(kc); err != nil {
		t.Fatalf("second Ping: %v", err)
	}
	w.mu.Lock()
	second := w.clusters[kc.ID]
	w.mu.Unlock()
	if second != first {
		t.Error("the second ping built a new client, want the idle one reused")
	}
}

// Once the grace runs out unused the client is forgotten, so a context probed
// once from the sidebar does not hold its client for the life of the app.
func TestIdleClusterIsForgottenAfterTheGrace(t *testing.T) {
	w, kc := pinged(t)

	w.mu.Lock()
	cl := w.clusters[kc.ID]
	w.mu.Unlock()
	if cl == nil {
		t.Fatal("no idle client after the ping")
	}

	// The grace is minutes long; do what the timer would.
	cl.evict.Stop()
	w.evictIdle(kc.ID, cl)

	w.mu.Lock()
	held := len(w.clusters)
	w.mu.Unlock()
	if held != 0 {
		t.Errorf("watcher still holds %d cluster(s) after the grace, want 0", held)
	}
}

// A timer that fires just as the cluster is taken back must not take it away
// from whoever now holds it, and letting go again must arm a fresh grace.
func TestEvictionWaitsWhileTheClusterIsInUse(t *testing.T) {
	w, kc := pinged(t)

	w.mu.Lock()
	cl := w.clusters[kc.ID]
	w.mu.Unlock()

	if _, err := w.clusterFor(kc); err != nil {
		t.Fatalf("clusterFor: %v", err)
	}
	w.evictIdle(kc.ID, cl) // the late timer

	w.mu.Lock()
	_, held := w.clusters[kc.ID]
	w.mu.Unlock()
	if !held {
		t.Fatal("evicted a cluster that had a reference on it")
	}

	w.releaseCluster(kc.ID, cl)

	w.mu.Lock()
	armed := cl.evict != nil
	w.mu.Unlock()
	if !armed {
		t.Error("releasing the last reference did not arm the grace again")
	}
}

// Disconnecting forgets the client at once rather than after the grace, and a
// call that borrowed the old one and returns afterwards leaves the new one
// alone.
func TestDisconnectForgetsTheClientAtOnce(t *testing.T) {
	w, kc := pinged(t)

	w.mu.Lock()
	old := w.clusters[kc.ID]
	w.mu.Unlock()
	// A call in flight across the disconnect.
	if _, err := w.clusterFor(kc); err != nil {
		t.Fatalf("clusterFor: %v", err)
	}

	if closed := w.Disconnect(kc.ID); closed != 0 {
		t.Errorf("closed %d subscriptions, want 0: the context had none", closed)
	}
	w.mu.Lock()
	_, held := w.clusters[kc.ID]
	w.mu.Unlock()
	if held {
		t.Fatal("the client is still held after Disconnect")
	}

	if err := w.Ping(kc); err != nil {
		t.Fatalf("Ping after Disconnect: %v", err)
	}
	w.mu.Lock()
	fresh := w.clusters[kc.ID]
	w.mu.Unlock()
	if fresh == nil || fresh == old {
		t.Fatal("the ping after Disconnect did not build a new client")
	}

	// The call in flight returns now.
	w.releaseCluster(kc.ID, old)
	w.mu.Lock()
	still := w.clusters[kc.ID]
	oldArmed := old.evict != nil
	w.mu.Unlock()
	if still != fresh {
		t.Error("releasing the old client disturbed the new one")
	}
	if oldArmed {
		t.Error("the forgotten client armed an eviction")
	}

	// Disconnecting a context nothing is connected to is fine.
	w.Disconnect("nobody")
}

// A client that could not be built is not worth keeping: the next caller
// should try again, with whatever has been fixed since.
func TestAClientThatCouldNotBeBuiltIsNotKept(t *testing.T) {
	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	if err := w.Ping(brokenKubeconfigFor(t)); err == nil {
		t.Fatal("Ping succeeded with a client certificate that does not exist")
	}

	w.mu.Lock()
	held := len(w.clusters)
	w.mu.Unlock()
	if held != 0 {
		t.Errorf("watcher kept %d failed client(s), want 0", held)
	}
}

// Closing the watcher takes the idle clients with it; nothing is coming back
// for them.
func TestCloseDropsIdleClients(t *testing.T) {
	w, _ := pinged(t)

	w.Close()

	w.mu.Lock()
	held := len(w.clusters)
	w.mu.Unlock()
	if held != 0 {
		t.Errorf("watcher still holds %d cluster(s) after Close, want 0", held)
	}
}
