package kube

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/metadata"
)

// Searching a cluster for objects by name, across every kind it serves.
//
// A search reads metadata and nothing else. It goes through the metadata
// client rather than the dynamic one, so what crosses the wire for each object
// is its name, namespace, labels and timestamps -- not a ConfigMap's contents,
// and never a Secret's values. That is what makes "every kind" affordable, and
// it is also what makes it safe to include Secrets at all.
//
// It is one-shot, not watched: a search answers "where is it", and a result
// that goes on changing under the pointer is not an answer anybody can click.

// searchParallel is how many kinds of one cluster are listed at once. A cluster
// serves a hundred kinds or more, and one at a time would take as long as the
// slowest of them each; all at once would be a burst the API server's priority
// and fairness would rightly queue.
const searchParallel = 8

// searchPage is how many objects one LIST asks for, so a kind with fifty
// thousand of something arrives in pages rather than in one response the size
// of the cluster.
const searchPage = 500

// searchScanLimit bounds how many objects of one kind are looked through. A
// search is for finding something, and a cluster with a hundred thousand
// events is not going to be read to the end to find it.
const searchScanLimit = 20000

// SearchKindLimit is how many hits one kind may contribute. Past it the kind is
// matching nearly everything, and the reader needs a narrower query rather than
// more rows.
const SearchKindLimit = 200

// quietKinds are left out of a search that does not name its kinds. Events are
// named after the objects they are about -- every pod called web-7f9c has a
// dozen web-7f9c.17a3... events beside it -- so including them would bury the
// object under its own history. Naming them, kind:events, still finds them.
var quietKinds = map[string]bool{KindEvents: true}

// skippedGroups are API groups whose resources are other resources seen again:
// the metrics API serves a PodMetrics for every pod, by the pod's own name, and
// events.k8s.io serves the same Events the core group does.
var skippedGroups = map[string]bool{
	"metrics.k8s.io":        true,
	"events.k8s.io":         true,
	"authorization.k8s.io":  true,
	"authentication.k8s.io": true,
}

// SearchQuery is what a search looks for, as parsed from what was typed.
type SearchQuery struct {
	// Terms must each appear in an object's name. A term with a slash in it is
	// matched against namespace/name instead, and a * in a term is a wildcard
	// that anchors the rest of it -- see matchTerm.
	Terms []string
	// Kinds narrows the kinds searched; empty is every kind the cluster lists.
	// Each is matched against a resource's plural, singular, short names and
	// Kind, so "po", "pod", "pods" and "Pod" all mean the same thing.
	Kinds []string
	// Namespaces keeps objects in these namespaces; empty is all of them.
	// A cluster-scoped kind has no namespace, and is left out when this is set.
	Namespaces []string
	// Selector is a label selector, which the API server evaluates.
	Selector string
}

// ParseSearch reads a search as typed: words to find in names, and filters
// written as key:value.
//
//	nginx prod               both words in the name
//	api-*-worker             a wildcard
//	kube-system/coredns      a slash matches the namespace as well
//	kind:pod,svc             only these kinds (also k: and type:)
//	ns:default               only this namespace (also n: and namespace:)
//	label:app=web            a label selector (also l: and labels:)
//
// Case is ignored everywhere but in a label selector, where the API server
// decides.
func ParseSearch(raw string) SearchQuery {
	var q SearchQuery
	var selectors []string
	for field := range strings.FieldsSeq(raw) {
		key, value, found := strings.Cut(field, ":")
		if found && value != "" {
			switch strings.ToLower(key) {
			case "kind", "k", "type":
				q.Kinds = append(q.Kinds, splitList(strings.ToLower(value))...)
				continue
			case "ns", "n", "namespace":
				q.Namespaces = append(q.Namespaces, splitList(value)...)
				continue
			case "label", "labels", "l":
				selectors = append(selectors, value)
				continue
			}
		}
		// A colon that is not one of the filters is part of a name: a
		// ClusterRole is called system:controller:... more often than not.
		q.Terms = append(q.Terms, strings.ToLower(field))
	}
	q.Selector = strings.Join(selectors, ",")
	return q
}

// Empty reports whether the query asks for nothing, which would otherwise mean
// listing every object in every cluster.
func (q SearchQuery) Empty() bool {
	return len(q.Terms) == 0 && len(q.Kinds) == 0 && len(q.Namespaces) == 0 && q.Selector == ""
}

// Validate reports a query the API server would refuse, before any cluster is
// asked -- a malformed selector would otherwise fail once per kind per cluster.
func (q SearchQuery) Validate() error {
	if q.Selector == "" {
		return nil
	}
	if _, err := labels.Parse(q.Selector); err != nil {
		return fmt.Errorf("label selector %q: %w", q.Selector, err)
	}
	return nil
}

func splitList(value string) []string {
	var out []string
	for part := range strings.SplitSeq(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// matches reports whether one object answers the query's names and namespaces.
// Labels are not checked here: the selector went to the server with the LIST.
func (q SearchQuery) matches(namespace, name string) bool {
	if len(q.Namespaces) > 0 && !slices.Contains(q.Namespaces, namespace) {
		return false
	}
	lower := strings.ToLower(name)
	var full string
	for _, term := range q.Terms {
		hay := lower
		if strings.Contains(term, "/") {
			if full == "" {
				full = strings.ToLower(namespace + "/" + name)
			}
			hay = full
		}
		if !matchTerm(hay, term) {
			return false
		}
	}
	return true
}

// matchTerm is a substring match, unless the term has a * in it, when it is a
// wildcard match of the whole name: "api-*" is a prefix, "*-worker" a suffix,
// and "api-*-worker" both.
func matchTerm(s, term string) bool {
	if !strings.Contains(term, "*") {
		return strings.Contains(s, term)
	}
	parts := strings.Split(term, "*")
	first, last := parts[0], parts[len(parts)-1]
	if !strings.HasPrefix(s, first) {
		return false
	}
	s = s[len(first):]
	for _, middle := range parts[1 : len(parts)-1] {
		at := strings.Index(s, middle)
		if at < 0 {
			return false
		}
		s = s[at+len(middle):]
	}
	return strings.HasSuffix(s, last)
}

// SearchHit is one object a search found.
type SearchHit struct {
	ContextID string `json:"contextId"`
	// Kind is the app's name for the kind -- a built-in such as "pods", or
	// "crd:<plural>.<group>" -- which is what a tab, the describe panel and a
	// plugin view are all opened with.
	Kind string `json:"kind"`
	// APIKind is the kind as the API spells it, "Pod", for the reader.
	APIKind    string `json:"apiKind"`
	Group      string `json:"group"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Namespaced bool   `json:"namespaced"`
	Age        string `json:"age"`
}

// SearchStep is how far one cluster's search has got, in kinds.
type SearchStep struct {
	Done  int `json:"done"`
	Total int `json:"total"`
	// Failed counts the kinds that could not be listed -- most often because
	// the caller may not, which on a cluster with narrow RBAC is most of them.
	Failed int `json:"failed"`
}

// searchable is one kind a cluster lists, with every name it may be asked for
// by.
type searchable struct {
	gvr        schema.GroupVersionResource
	kind       string
	apiKind    string
	namespaced bool
	names      []string
}

// Search looks through one cluster for objects matching the query. Hits are
// handed to found as each kind answers, and the running count of kinds to step
// after each one, so the caller can show a search that is still going. It
// returns once every kind has been listed, when ctx is cancelled, or with the
// reason the cluster could not be searched at all.
//
// found and step are called from several goroutines at once.
func (w *Watcher) Search(ctx context.Context, kc Context, q SearchQuery, found func([]SearchHit), step func(SearchStep)) error {
	return w.withClient(kc, func(c *clusterClient) error {
		lists, err := preferredResources(ctx, c.disco)
		if err != nil {
			return err
		}
		targets := chooseSearchable(searchablesIn(lists), q.Kinds)
		total := len(targets)
		step(SearchStep{Total: total})
		if total == 0 {
			return nil
		}

		client, err := metadata.NewForConfig(c.cfg)
		if err != nil {
			return fmt.Errorf("metadata client for context %q: %w", kc.Name, err)
		}

		var done, failed atomic.Int64
		var wg sync.WaitGroup
		slots := make(chan struct{}, searchParallel)
	loop:
		for _, target := range targets {
			select {
			case <-ctx.Done():
				break loop
			case slots <- struct{}{}:
			}
			wg.Add(1)
			go func(t searchable) {
				defer wg.Done()
				defer func() { <-slots }()
				hits, err := searchKind(ctx, client, kc.ID, t, q)
				if len(hits) > 0 {
					found(hits)
				}
				if err != nil && ctx.Err() == nil {
					failed.Add(1)
				}
				step(SearchStep{Done: int(done.Add(1)), Total: total, Failed: int(failed.Load())})
			}(target)
		}
		wg.Wait()
		return ctx.Err()
	})
}

// preferredResources lists what the cluster serves, at the version it
// prefers, giving up when ctx is cancelled.
//
// Discovery takes no context, so the call is left to finish on its own when
// the search is called off; the client it runs on stays valid, and the cached
// listing it fills is the next search's head start. As in serverResources, a
// listing missing an aggregated API that is down is used rather than
// discarded.
func preferredResources(ctx context.Context, d discovery.CachedDiscoveryInterface) ([]*metav1.APIResourceList, error) {
	type answer struct {
		lists []*metav1.APIResourceList
		err   error
	}
	ch := make(chan answer, 1)
	go func() {
		lists, err := d.ServerPreferredResources()
		ch <- answer{lists, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-ch:
		if got.err != nil && (!discovery.IsGroupDiscoveryFailedError(got.err) || len(got.lists) == 0) {
			return nil, got.err
		}
		return got.lists, nil
	}
}

// builtinByGroupKind is builtinKinds turned round, so a resource found by
// discovery can be named the way the sidebar names it.
var builtinByGroupKind = func() map[schema.GroupKind]string {
	out := make(map[schema.GroupKind]string, len(builtinKinds))
	for kind, gk := range builtinKinds {
		out[gk] = kind
	}
	return out
}()

// searchablesIn picks the kinds worth searching out of a discovery listing:
// collections that can be listed, named the way the app opens them.
//
// A core-group resource the app has no name for is left out, because a hit on
// one could be neither described nor opened -- "crd:" needs a group. That is
// the handful nobody searches for: bindings and component statuses.
func searchablesIn(lists []*metav1.APIResourceList) []searchable {
	seen := map[string]bool{}
	var out []searchable
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil || skippedGroups[gv.Group] {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") || !slices.Contains(r.Verbs, "list") {
				continue
			}
			kind, known := builtinByGroupKind[schema.GroupKind{Group: gv.Group, Kind: r.Kind}]
			if !known {
				if gv.Group == "" {
					continue
				}
				kind = CustomKind(r.Name, gv.Group)
			}
			if seen[kind] {
				continue
			}
			seen[kind] = true

			names := []string{kind, r.Name, strings.ToLower(r.Kind), r.Name + "." + gv.Group, strings.ToLower(r.Kind) + "." + gv.Group}
			if r.SingularName != "" {
				names = append(names, r.SingularName)
			}
			for _, short := range r.ShortNames {
				names = append(names, strings.ToLower(short))
			}
			out = append(out, searchable{
				gvr:        gv.WithResource(r.Name),
				kind:       kind,
				apiKind:    r.Kind,
				namespaced: r.Namespaced,
				names:      names,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].kind < out[j].kind })
	return out
}

// chooseSearchable narrows the kinds to the ones asked for, or with none asked
// for, leaves out only the quiet ones.
func chooseSearchable(all []searchable, wanted []string) []searchable {
	var out []searchable
	for _, s := range all {
		if len(wanted) == 0 {
			if !quietKinds[s.kind] {
				out = append(out, s)
			}
			continue
		}
		if slices.ContainsFunc(wanted, func(w string) bool { return slices.Contains(s.names, strings.ToLower(w)) }) {
			out = append(out, s)
		}
	}
	return out
}

// searchKind lists one kind page by page and keeps what matches.
//
// One namespace asked for is asked of the server, which then sends only that
// namespace's objects; several are filtered here, from a list of all of them,
// because a LIST takes one namespace or every one.
func searchKind(ctx context.Context, client metadata.Interface, contextID string, t searchable, q SearchQuery) ([]SearchHit, error) {
	if len(q.Namespaces) > 0 && !t.namespaced {
		return nil, nil
	}
	var from metadata.ResourceInterface = client.Resource(t.gvr)
	if t.namespaced && len(q.Namespaces) == 1 {
		from = client.Resource(t.gvr).Namespace(q.Namespaces[0])
	}

	var hits []SearchHit
	opts := metav1.ListOptions{LabelSelector: q.Selector, Limit: searchPage}
	scanned := 0
	for {
		page, err := listPage(ctx, from, opts)
		if err != nil {
			return hits, err
		}
		for i := range page.Items {
			m := &page.Items[i]
			if !q.matches(m.Namespace, m.Name) {
				continue
			}
			hits = append(hits, SearchHit{
				ContextID:  contextID,
				Kind:       t.kind,
				APIKind:    t.apiKind,
				Group:      t.gvr.Group,
				Namespace:  m.Namespace,
				Name:       m.Name,
				Namespaced: t.namespaced,
				Age:        ageSince(m.CreationTimestamp),
			})
			if len(hits) >= SearchKindLimit {
				return hits, nil
			}
		}
		scanned += len(page.Items)
		if page.Continue == "" || scanned >= searchScanLimit {
			return hits, nil
		}
		opts.Continue = page.Continue
	}
}

// listPage is one LIST, bounded like every other one-shot call.
func listPage(ctx context.Context, from metadata.ResourceInterface, opts metav1.ListOptions) (*metav1.PartialObjectMetadataList, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return from.List(ctx, opts)
}

func ageSince(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return age(int(time.Since(t.Time).Minutes()))
}
