package kube

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// Snapshot is one push to the frontend: the whole current contents of a
// subscribed collection.
//
// Deltas would be cheaper on the wire, but a Row is a *projection* of an object
// and the frontend cannot apply a delta without redoing that projection. The
// informer keeps a local cache, so rebuilding the entire table costs no API
// traffic at all -- only the projection itself, which is why sending the whole
// thing is affordable.
type Snapshot struct {
	SubscriptionID string `json:"subscriptionId"`
	Table          Table  `json:"table"`
}

// coalesce is how long a subscription waits after a change before pushing. A
// rollout produces a burst of pod events; without this the UI would repaint for
// every one of them.
const coalesce = 150 * time.Millisecond

// resync is the informer's relist interval. Watches are meant to be reliable,
// so this is a backstop against a missed event rather than the primary path.
const resync = 10 * time.Minute

// idleGrace is how long a cluster's client is kept after its last user lets
// go of it.
//
// A dashboard polls every thirty seconds and holds no informer in between; a
// tab switch closes one subscription just before opening the next; a ping
// from the sidebar borrows the client for a single request. Dropping the
// client at zero references made each of those build it again -- kubeconfig,
// credential plugin, TLS, a cold discovery cache and the full discovery
// download that refills it -- to make one LIST call. What lingers is a config
// and that cache, not a connection: client-go pools connections per config on
// its own, whether or not this struct is still around.
const idleGrace = 2 * time.Minute

// Watcher owns every live connection: one client and one set of informers per
// kubeconfig context, shared by all the tabs looking at that context.
//
// Lifetime is reference-counted from both ends. Three tabs on the same kind
// share one watch; the informer stops when the last of them closes. The
// cluster's client outlives its last user by idleGrace rather than going with
// it -- see releaseCluster.
type Watcher struct {
	emit func(Snapshot)

	mu       sync.Mutex
	clusters map[string]*cluster
	subs     map[string]*subscription
	nextID   atomic.Uint64
}

// NewWatcher returns a watcher that pushes snapshots through emit. emit is
// called from background goroutines and must be safe for concurrent use.
func NewWatcher(emit func(Snapshot)) *Watcher {
	return &Watcher{
		emit:     emit,
		clusters: map[string]*cluster{},
		subs:     map[string]*subscription{},
	}
}

// informerKey identifies a watch within a cluster.
//
// The field selector is part of the identity, not an afterthought: the Helm
// view watches the same resource as a Secrets tab but narrowed to release
// records, and the two must not be handed the same informer -- they hold
// different objects and strip them differently. Everything else watches a
// whole collection and so keys on an empty selector.
type informerKey struct {
	gvr   schema.GroupVersionResource
	field string
}

// cluster is one context's live connection plus the informers opened against it.
type cluster struct {
	refs      int
	informers map[informerKey]*liveInformer

	// ready closes once client/err are set. Building a client can run an exec
	// credential plugin, which is slow, so it happens off the watcher's lock
	// with other callers waiting here rather than on the mutex.
	ready  chan struct{}
	client *clusterClient
	err    error

	// evict is armed when the last reference goes and disarmed by the next
	// one; it fires only if the cluster stayed idle for the whole idleGrace.
	evict *time.Timer
}

// liveInformer is one watch: shared by every subscription to the same resource
// in the same cluster, regardless of the namespace each of them is filtering to.
type liveInformer struct {
	refs     int
	key      informerKey
	informer cache.SharedIndexInformer
	lister   cache.GenericLister
	scope    meta.RESTScopeName
	stop     chan struct{}
}

// subscription is one open tab. It holds the namespace filter, because the
// informer it reads from is cluster-scoped and shared.
type subscription struct {
	id        string
	contextID string
	kind      string
	// namespaces is the set of namespaces the tab shows, nil for every one of
	// them. A set rather than one name: a table showing a team's namespace
	// beside kube-system is one table, not two.
	namespaces map[string]bool
	// selector narrows the rows to objects carrying certain labels, which is
	// how a solution plugin ships a view of "the parts of this cluster that
	// belong to Argo CD" rather than of a whole kind.
	//
	// It is applied here rather than passed to the informer on purpose. The
	// informer is shared by every tab on the same resource, so a selector on it
	// would either need an informer per selector -- a second watch, a second
	// cache of the same objects -- or would narrow what the other tabs see.
	// Filtering the cache on the way out costs a label match per row and keeps
	// one watch per kind, which is the same trade the namespace filter makes.
	selector labels.Selector
	columns  []column
	live     *liveInformer

	// kc is the context this subscription reads from, kept only for the views
	// whose rows are read live rather than projected from the informer's cache.
	// Every other subscription needs nothing beyond contextID.
	kc Context
	// live rows rather than cached ones: the informer says *that* something
	// changed and the rows are fetched again. Helm releases are the only view
	// that works this way -- see SubscribeHelm.
	reread bool

	dirty chan struct{} // buffered(1): a pending "something changed"
	done  chan struct{}
}

// Subscribe opens a live view of one kind and returns its subscription ID. It
// returns as soon as the watch is started; the first snapshot arrives through
// emit once the informer's cache has synced, so the caller never blocks on the
// network.
//
// namespaces narrows the rows to those namespaces; none means every one.
// selector is an optional label selector narrowing them further; empty means
// every object of the kind. See subscription.selector for why it is not pushed
// down to the informer.
func (w *Watcher) Subscribe(kc Context, kind string, namespaces []string, selector string) (string, error) {
	return w.SubscribeFor(kc, kind, namespaces, selector, nil)
}

// SubscribeFor is Subscribe with a claim: claim, when given, is called with the
// new subscription's ID before anything can be pushed under it. The web
// version routes each snapshot to whoever opened the subscription, and a first
// snapshot that raced ahead of the claim would have nobody to go to.
func (w *Watcher) SubscribeFor(kc Context, kind string, namespaces []string, selector string, claim func(id string)) (string, error) {
	// Left nil when there is no selector, rather than labels.Everything(): the
	// overwhelming majority of tabs have none, and nil is what lets project()
	// skip the match entirely instead of asking a selector that always says yes.
	var chosen labels.Selector
	if strings.TrimSpace(selector) != "" {
		parsed, err := labels.Parse(selector)
		if err != nil {
			return "", fmt.Errorf("label selector %q: %w", selector, err)
		}
		chosen = parsed
	}

	cl, err := w.clusterFor(kc)
	if err != nil {
		return "", err
	}

	mapping, err := cl.client.mappingForKind(kind)
	if err != nil {
		w.releaseCluster(kc.ID)
		return "", err
	}

	namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
	// Columns are resolved per subscription, not per informer: two kinds can
	// map to the same resource (a Gateway is both "gateways" and a custom
	// resource) and they do not have to be rendered the same way.
	cols, err := cl.client.columnsFor(kind, namespaced)
	if err != nil {
		w.releaseCluster(kc.ID)
		return "", err
	}

	live := w.informerFor(cl, mapping, "", stripBulk)

	sub := &subscription{
		id:         fmt.Sprintf("sub-%d", w.nextID.Add(1)),
		contextID:  kc.ID,
		kind:       kind,
		namespaces: namespaceFilter(namespaces),
		selector:   chosen,
		columns:    cols,
		live:       live,
		dirty:      make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	if claim != nil {
		claim(sub.id)
	}

	w.mu.Lock()
	w.subs[sub.id] = sub
	w.mu.Unlock()

	go w.pump(sub)
	go w.firstSnapshot(sub)

	return sub.id, nil
}

// Unsubscribe closes one tab's view, stopping the underlying watch if no other
// tab is using it.
func (w *Watcher) Unsubscribe(id string) {
	w.mu.Lock()
	sub, ok := w.subs[id]
	if !ok {
		w.mu.Unlock()
		return
	}
	delete(w.subs, id)

	live := sub.live
	live.refs--
	stopInformer := live.refs == 0
	if stopInformer {
		if cl, ok := w.clusters[sub.contextID]; ok {
			delete(cl.informers, live.key)
		}
	}
	w.mu.Unlock()

	close(sub.done)
	if stopInformer {
		close(live.stop)
		w.releaseCluster(sub.contextID)
	}
}

// SetNamespaces re-points an existing subscription at other namespaces. The
// informer is cluster-scoped, so this is a filter change and a repaint -- no
// watch is torn down or reopened.
func (w *Watcher) SetNamespaces(id string, namespaces []string) {
	w.mu.Lock()
	sub, ok := w.subs[id]
	if ok {
		sub.namespaces = namespaceFilter(namespaces)
	}
	w.mu.Unlock()
	if ok {
		sub.markDirty()
	}
}

// Close tears down every subscription and connection. Called when the app quits.
func (w *Watcher) Close() {
	w.mu.Lock()
	ids := make([]string, 0, len(w.subs))
	for id := range w.subs {
		ids = append(ids, id)
	}
	w.mu.Unlock()

	for _, id := range ids {
		w.Unsubscribe(id)
	}

	// Nothing is coming back for the idle clients now, so they go too, and
	// their timers with them. One still borrowed by a call in flight is left
	// for that call to release, which arms a timer that finds nothing to do.
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, cl := range w.clusters {
		if cl.refs > 0 {
			continue
		}
		if cl.evict != nil {
			cl.evict.Stop()
		}
		delete(w.clusters, id)
	}
}

// clusterFor returns the live client for a context, building it if this is the
// first subscription against it, and takes a reference on it.
func (w *Watcher) clusterFor(kc Context) (*cluster, error) {
	w.mu.Lock()
	cl, ok := w.clusters[kc.ID]
	if !ok {
		cl = &cluster{informers: map[informerKey]*liveInformer{}, ready: make(chan struct{})}
		w.clusters[kc.ID] = cl
	} else if cl.evict != nil {
		// Idle, and about to be forgotten: this caller is exactly what the
		// grace was waiting for.
		cl.evict.Stop()
		cl.evict = nil
	}
	cl.refs++
	w.mu.Unlock()

	if !ok {
		// First caller builds the client, off the lock: an exec credential
		// plugin can take seconds and must not stall other contexts.
		cl.client, cl.err = newClusterClient(kc)
		close(cl.ready)
	}
	<-cl.ready

	if cl.err != nil {
		w.releaseCluster(kc.ID)
		return nil, cl.err
	}
	return cl, nil
}

// releaseCluster drops one reference. At zero the cluster is not forgotten but
// left to idle for idleGrace, so that the next caller -- a poll thirty seconds
// away, the tab being opened after the one just closed -- finds the client and
// its discovery cache warm instead of building both again.
//
// A client that could not be built is the exception and goes at once: the next
// caller should try again, with whatever has been fixed in the meantime.
func (w *Watcher) releaseCluster(contextID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	cl, ok := w.clusters[contextID]
	if !ok {
		return
	}
	cl.refs--
	if cl.refs > 0 {
		return
	}
	if cl.err != nil {
		delete(w.clusters, contextID)
		return
	}
	cl.evict = time.AfterFunc(idleGrace, func() { w.evictIdle(contextID, cl) })
}

// evictIdle forgets a cluster whose grace ran out unused. It checks the entry
// by identity and by count, because a timer can fire just as a caller takes
// the cluster back: clusterFor stops the timer, but one already on its way
// here still arrives, and must find nothing to do.
func (w *Watcher) evictIdle(contextID string, cl *cluster) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.clusters[contextID] != cl || cl.refs > 0 {
		return
	}
	delete(w.clusters, contextID)
}

// informerFor returns the shared informer for a resource, starting it if this
// is the first subscription, and takes a reference on it.
//
// field narrows the watch itself, which is different from every other filter
// here: the namespace and label filters run over the cache so that changing
// them repaints without reopening anything, while a field selector decides what
// the cache is allowed to contain at all. Only the Helm view uses one.
//
// transform is what each object is put through before it is cached, and belongs
// to the key rather than to the caller: two subscriptions sharing an informer
// share its contents, so a second caller asking for the same resource and
// selector gets the first one's transform. Passing it here rather than reaching
// for stripBulk inside means a narrowed watch can strip more than a whole one.
func (w *Watcher) informerFor(cl *cluster, mapping *meta.RESTMapping, field string, transform cache.TransformFunc) *liveInformer {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := informerKey{gvr: mapping.Resource, field: field}
	if live, ok := cl.informers[key]; ok {
		live.refs++
		return live
	}

	var tweak dynamicinformer.TweakListOptionsFunc
	if field != "" {
		tweak = func(opts *metav1.ListOptions) { opts.FieldSelector = field }
	}

	// Cluster-scoped, with namespace filtering done against the cache. That
	// matches how the tables have always filtered, and it means changing the
	// namespace dropdown is instant instead of reopening a watch.
	gi := dynamicinformer.NewFilteredDynamicInformer(
		cl.client.dynamic, mapping.Resource, "", resync, cache.Indexers{}, tweak,
	)
	inf := gi.Informer()

	// An informer caches the whole collection. Without this, opening a Secrets
	// tab would pull every secret value in the cluster into the app's memory;
	// managed fields are simply bulk we never render.
	_ = inf.SetTransform(transform)

	live := &liveInformer{
		refs:     1,
		key:      key,
		informer: inf,
		lister:   gi.Lister(),
		scope:    mapping.Scope.Name(),
		stop:     make(chan struct{}),
	}
	cl.informers[key] = live

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { w.markKind(live) },
		UpdateFunc: func(any, any) { w.markKind(live) },
		DeleteFunc: func(any) { w.markKind(live) },
	}
	_, _ = inf.AddEventHandler(handler)

	// A watch that cannot be established -- unreachable cluster, expired
	// credentials, no RBAC for this kind -- is the failure the user most needs
	// to see, and it arrives here rather than from Subscribe.
	_ = inf.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
		w.reportError(live, err)
	})

	go inf.Run(live.stop)
	return live
}

// markKind flags every subscription reading from an informer as needing a push.
func (w *Watcher) markKind(live *liveInformer) {
	w.mu.Lock()
	subs := w.subsFor(live)
	w.mu.Unlock()
	for _, s := range subs {
		s.markDirty()
	}
}

// reportError pushes a failure to every tab watching the affected kind.
func (w *Watcher) reportError(live *liveInformer, err error) {
	w.mu.Lock()
	subs := w.subsFor(live)
	w.mu.Unlock()
	for _, s := range subs {
		w.emit(Snapshot{
			SubscriptionID: s.id,
			Table:          Table{Kind: s.kind, Columns: []string{}, Rows: []Row{}, Error: err.Error()},
		})
	}
}

// subsFor lists the subscriptions reading from an informer. The caller holds
// the lock.
func (w *Watcher) subsFor(live *liveInformer) []*subscription {
	var out []*subscription
	for _, s := range w.subs {
		if s.live == live {
			out = append(out, s)
		}
	}
	return out
}

// firstSnapshot pushes once the cache has synced, so that a collection which is
// simply empty still resolves the tab's loading state.
func (w *Watcher) firstSnapshot(sub *subscription) {
	if cache.WaitForCacheSync(sub.done, sub.live.informer.HasSynced) {
		sub.markDirty()
	}
}

// pump is one subscription's push loop: it waits for a change, lets the burst
// settle, then sends the current contents.
func (w *Watcher) pump(sub *subscription) {
	for {
		select {
		case <-sub.done:
			return
		case <-sub.dirty:
		}

		select {
		case <-sub.done:
			return
		case <-time.After(coalesce):
		}

		w.mu.Lock()
		keep := sub.namespaces
		w.mu.Unlock()

		w.emit(Snapshot{SubscriptionID: sub.id, Table: w.project(sub, keep)})
	}
}

// namespaceFilter turns the namespaces a tab asked for into the set its rows
// are checked against. Nil means every namespace, which is what an empty
// choice means: a picker with nothing ticked shows the whole cluster, not an
// empty table.
func namespaceFilter(names []string) map[string]bool {
	var keep map[string]bool
	for _, name := range names {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if keep == nil {
			keep = map[string]bool{}
		}
		keep[name] = true
	}
	return keep
}

// project turns the informer's cache into the table the UI renders, keeping
// the rows in the given namespaces -- all of them when keep is nil.
func (w *Watcher) project(sub *subscription, keep map[string]bool) Table {
	// A view whose rows are read rather than cached. The informer has already
	// done its job by waking the pump; what it holds is not the answer.
	if sub.reread {
		table, err := w.helmReleases(sub.kc, keep)
		if err != nil {
			return Table{Kind: sub.kind, Columns: []string{}, Rows: []Row{}, Error: err.Error()}
		}
		return table
	}

	objs, err := sub.live.lister.List(labels.Everything())
	if err != nil {
		return Table{Kind: sub.kind, Columns: []string{}, Rows: []Row{}, Error: err.Error()}
	}

	items := make([]*unstructured.Unstructured, 0, len(objs))
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		if keep != nil && !keep[u.GetNamespace()] {
			continue
		}
		if sub.selector != nil && !sub.selector.Matches(labels.Set(u.GetLabels())) {
			continue
		}
		items = append(items, u)
	}

	return buildLiveTable(sub.kind, sub.columns, sub.live.scope == meta.RESTScopeNameNamespace, items)
}

// markDirty records that this subscription has something new to send. The
// channel is buffered to one, so a burst collapses into a single wake-up.
func (s *subscription) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// stripBulk drops the parts of an object we never render before it is cached.
func stripBulk(obj any) (any, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil
	}
	u.SetManagedFields(nil)
	// Secret values must not sit in a desktop app's memory just because a tab
	// listing their names happens to be open. The count of keys is all the
	// table shows, so keep the keys and drop what they point at.
	if u.GetKind() == "Secret" {
		if data, found, _ := unstructured.NestedMap(u.Object, "data"); found {
			redacted := make(map[string]any, len(data))
			for k := range data {
				redacted[k] = ""
			}
			_ = unstructured.SetNestedMap(u.Object, redacted, "data")
		}
	}
	return u, nil
}

var _ runtime.Object = (*unstructured.Unstructured)(nil)
