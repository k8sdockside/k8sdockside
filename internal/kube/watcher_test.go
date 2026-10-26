package kube

import (
	"sync"
	"testing"
	"time"
)

// snapshotsFor collects what a watcher pushes, so a test can wait for the
// push to one subscription in particular.
type snapshotsFor struct {
	mu   sync.Mutex
	seen map[string]int
}

func (s *snapshotsFor) emit(snap Snapshot) {
	s.mu.Lock()
	if s.seen == nil {
		s.seen = map[string]int{}
	}
	s.seen[snap.SubscriptionID]++
	s.mu.Unlock()
}

func (s *snapshotsFor) waitFor(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := s.seen[id]
		s.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no snapshot for %s within 10s", id)
}

// subscribed opens a pods tab against a fake server and waits for its first
// snapshot.
func subscribed(t *testing.T, w *Watcher, got *snapshotsFor, kc Context) string {
	t.Helper()
	id, err := w.Subscribe(kc, KindPods, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	got.waitFor(t, id)
	return id
}

// Coming back to a tab a moment after leaving it -- which is what switching
// between two clusters' tabs is -- finds its watch still there, and paints
// from it, rather than listing the whole collection again.
func TestAWatchOutlivesItsLastTabForTheGrace(t *testing.T) {
	srv := newFakeServer(t)
	got := &snapshotsFor{}
	w := NewWatcher(got.emit)
	t.Cleanup(w.Close)
	kc := kubeconfigFor(t, srv.URL)

	first := subscribed(t, w, got, kc)
	w.Unsubscribe(first)

	w.mu.Lock()
	cl := w.clusters[kc.ID]
	lingering, armed := len(cl.informers), false
	for _, live := range cl.informers {
		armed = live.linger != nil && live.refs == 0
	}
	w.mu.Unlock()
	if lingering != 1 || !armed {
		t.Fatalf("after the last tab closed: %d watches, grace armed %v; want the one watch kept with its grace running", lingering, armed)
	}

	second := subscribed(t, w, got, kc)
	if lists := srv.count("/api/v1/pods"); lists != 1 {
		t.Errorf("pods listed %d times, want once: the second tab should have been painted from the watch the first left behind", lists)
	}

	w.mu.Lock()
	for _, live := range w.clusters[kc.ID].informers {
		if live.linger != nil || live.refs != 1 {
			t.Errorf("the reused watch has refs %d and grace armed %v, want one reference and no grace", live.refs, live.linger != nil)
		}
	}
	w.mu.Unlock()
	w.Unsubscribe(second)
}

// A watch that runs out its grace stops, and gives back the reference it held
// on its cluster, so the cluster can go idle in its turn.
func TestAnIdleWatchStopsAndLetsItsClusterIdle(t *testing.T) {
	srv := newFakeServer(t)
	got := &snapshotsFor{}
	w := NewWatcher(got.emit)
	t.Cleanup(w.Close)
	kc := kubeconfigFor(t, srv.URL)

	// Two tabs on one watch: each took a reference on the cluster, and both
	// must be given back -- not only the last one's.
	a := subscribed(t, w, got, kc)
	b := subscribed(t, w, got, kc)
	w.Unsubscribe(a)
	w.Unsubscribe(b)

	w.mu.Lock()
	cl := w.clusters[kc.ID]
	refs := cl.refs
	var live *liveInformer
	for _, l := range cl.informers {
		live = l
	}
	w.mu.Unlock()
	if refs != 1 {
		t.Fatalf("cluster refs = %d with both tabs closed, want 1: the lingering watch's own", refs)
	}
	if live == nil {
		t.Fatal("no lingering watch")
	}

	// What the grace's timer does when it fires.
	w.stopIdleInformer(kc.ID, cl, live)

	w.mu.Lock()
	refs, watches, evicting := cl.refs, len(cl.informers), cl.evict != nil
	w.mu.Unlock()
	if refs != 0 || watches != 0 {
		t.Errorf("after the grace: refs %d, watches %d, want none of either", refs, watches)
	}
	if !evicting {
		t.Error("the cluster is not idling, want its own grace armed once the watch let go of it")
	}
	select {
	case <-live.stop:
	default:
		t.Error("the watch was not stopped")
	}
}

// Disconnecting is letting go on purpose, and keeps nothing lingering.
func TestDisconnectStopsLingeringWatches(t *testing.T) {
	srv := newFakeServer(t)
	got := &snapshotsFor{}
	w := NewWatcher(got.emit)
	t.Cleanup(w.Close)
	kc := kubeconfigFor(t, srv.URL)

	id := subscribed(t, w, got, kc)
	w.mu.Lock()
	var live *liveInformer
	for _, l := range w.clusters[kc.ID].informers {
		live = l
	}
	w.mu.Unlock()

	if closed := w.Disconnect(kc.ID); closed != 1 {
		t.Errorf("Disconnect closed %d subscriptions, want 1 (%s)", closed, id)
	}
	select {
	case <-live.stop:
	default:
		t.Error("the watch is still running after Disconnect, want it stopped rather than left to its grace")
	}
	w.mu.Lock()
	held := len(w.clusters)
	w.mu.Unlock()
	if held != 0 {
		t.Errorf("%d clients held after Disconnect, want none", held)
	}
}

// A ping that reaches the cluster leaves its discovery cache filling behind
// it, so the first tab does not wait for the download -- and a burst of pings
// fills it once.
func TestAPingWarmsDiscoveryOnce(t *testing.T) {
	srv := newFakeServer(t)
	w := NewWatcher(func(Snapshot) {})
	t.Cleanup(w.Close)
	kc := kubeconfigFor(t, srv.URL)

	for range 3 {
		if err := w.Ping(kc); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	}

	w.mu.Lock()
	c := w.clusters[kc.ID].client
	w.mu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for !c.disco.Fresh() {
		if time.Now().After(deadline) {
			t.Fatal("the discovery cache was not filled after a ping that reached the cluster")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Let any fill that was going to start, start.
	time.Sleep(50 * time.Millisecond)
	if n := srv.count("/apis"); n != 1 {
		t.Errorf("/apis fetched %d times for three pings, want once", n)
	}
}
