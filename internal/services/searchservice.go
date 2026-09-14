package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SearchEvent is the event a search's results and progress arrive on. One
// event is one report about one cluster's part of one search; the payload
// names both.
const SearchEvent = "search:update"

// Registering the event gives the binding generator the payload's type, the
// same way SnapshotEvent does in resourceservice.go.
func init() {
	application.RegisterEvent[SearchUpdate](SearchEvent)
}

// The phases one cluster's part of a search goes through. A cluster the
// search has not reached yet has had no update at all.
const (
	// SearchConnecting: building the client, which may run a credential
	// plugin, and reading what kinds the cluster serves.
	SearchConnecting = "connecting"
	// SearchSearching: listing kinds; the update's step says how far.
	SearchSearching = "searching"
	// SearchDone: every kind listed, or the search called off.
	SearchDone = "done"
	// SearchFailed: the cluster could not be searched at all. Kinds that
	// could not be listed are not this -- they are counted in the step.
	SearchFailed = "error"
)

// searchHitLimit bounds a whole search, across every cluster in it. A query
// finding more than this is too broad to be read, and the reader is told so
// rather than handed a list nobody will scroll.
const searchHitLimit = 1000

// searchClusters is how many clusters are searched at once. Each already
// lists several kinds at a time; this keeps a search of every context in a
// long kubeconfig from being every connection at once.
const searchClusters = 6

// searchTick is the least time between two updates that carry only progress,
// per cluster. A cluster answers a hundred kinds in a second or two, and
// repainting the count for each would be a hundred repaints to say "still
// going".
const searchTick = 120 * time.Millisecond

// SearchRequest is one search, as the search box sends it.
type SearchRequest struct {
	// ID is chosen by the caller, so that no update can arrive before the
	// caller knows which search it belongs to.
	ID string `json:"id"`
	// Query is what was typed, filters and all -- see kube.ParseSearch.
	Query string `json:"query"`
	// Kinds narrows the kinds searched beyond any kind: in the query, from the
	// search box's own options. Empty leaves it to the query.
	Kinds []string `json:"kinds"`
	// ContextIDs are the clusters to search.
	ContextIDs []string `json:"contextIds"`
}

// SearchUpdate is one report from a search in progress.
type SearchUpdate struct {
	SearchID string `json:"searchId"`
	// ContextID is the cluster this is about. Empty on the last update of the
	// whole search, which is about all of them.
	ContextID string          `json:"contextId"`
	Phase     string          `json:"phase"`
	Step      kube.SearchStep `json:"step"`
	// Hits found since the last update. They add to what came before.
	Hits  []kube.SearchHit `json:"hits"`
	Error string           `json:"error"`
	// Finished marks the last update of the whole search.
	Finished bool `json:"finished"`
	// Truncated says the search stopped at searchHitLimit rather than at the
	// end of what it had to look through.
	Truncated bool `json:"truncated"`
}

// SearchService finds objects by name across clusters: every kind each one
// serves, several clusters at once, the results arriving as they are found.
//
// It borrows the resource service's watcher, like every other service that
// talks to a cluster, so a context already open in a tab is searched through
// the connection it already has -- and a context that was not open is left
// with a warm client for the tab the reader is about to open on what they
// found.
type SearchService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	// emit sends one update to the frontend. A field so a test can listen.
	emit func(SearchUpdate)

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

// NewSearchService wires the service to the kubeconfig cache it resolves
// context IDs against, and to the watcher whose clients it borrows.
func NewSearchService(configs *KubeconfigService, watcher *kube.Watcher) *SearchService {
	return &SearchService{
		configs: configs,
		watcher: watcher,
		emit: func(u SearchUpdate) {
			if app := application.Get(); app != nil {
				app.Event.Emit(SearchEvent, u)
			}
		},
		running: map[string]context.CancelFunc{},
	}
}

// ServiceShutdown calls off every search still running when the app quits.
func (s *SearchService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.running {
		cancel()
		delete(s.running, id)
	}
	return nil
}

// Start begins a search and returns at once. What it finds arrives as
// SearchEvents carrying the request's ID, cluster by cluster, and the last one
// is marked Finished.
//
// It refuses, before asking any cluster, a query that asks for nothing or
// carries a selector the API server would refuse.
func (s *SearchService) Start(req SearchRequest) error {
	if req.ID == "" {
		return errors.New("a search needs an id")
	}
	q := kube.ParseSearch(req.Query)
	q.Kinds = append(q.Kinds, req.Kinds...)
	if q.Empty() {
		return errors.New("type something to search for")
	}
	if err := q.Validate(); err != nil {
		return err
	}

	var targets []kube.Context
	for _, id := range slices.Compact(slices.Sorted(slices.Values(req.ContextIDs))) {
		if kc, ok := s.configs.lookup(id); ok {
			targets = append(targets, kc)
		}
	}
	if len(targets) == 0 {
		return errors.New("no contexts to search -- they may have been removed from the kubeconfig")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if _, taken := s.running[req.ID]; taken {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("a search called %q is already running", req.ID)
	}
	s.running[req.ID] = cancel
	s.mu.Unlock()

	go s.run(ctx, cancel, req.ID, q, targets)
	return nil
}

// Cancel calls a search off. What it has already found stays found; the
// clusters still going stop, and report themselves done.
func (s *SearchService) Cancel(searchID string) {
	s.mu.Lock()
	cancel, ok := s.running[searchID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

// run searches every cluster, a few at a time, and says when it has finished.
func (s *SearchService) run(ctx context.Context, cancel context.CancelFunc, id string, q kube.SearchQuery, targets []kube.Context) {
	defer s.forget(id)
	budget := &hitBudget{left: searchHitLimit, stop: cancel}

	var wg sync.WaitGroup
	slots := make(chan struct{}, searchClusters)
	for _, kc := range targets {
		wg.Add(1)
		go func(kc kube.Context) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchDone})
				return
			}
			defer func() { <-slots }()
			s.searchOne(ctx, id, kc, q, budget)
		}(kc)
	}
	wg.Wait()
	s.emit(SearchUpdate{SearchID: id, Phase: SearchDone, Finished: true, Truncated: budget.cut()})
}

// searchOne searches one cluster, reporting as it goes.
func (s *SearchService) searchOne(ctx context.Context, id string, kc kube.Context, q kube.SearchQuery, budget *hitBudget) {
	s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchConnecting})

	var mu sync.Mutex
	var step kube.SearchStep
	var last time.Time

	found := func(hits []kube.SearchHit) {
		hits = budget.take(hits)
		if len(hits) == 0 {
			return
		}
		mu.Lock()
		current := step
		last = time.Now()
		mu.Unlock()
		s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchSearching, Step: current, Hits: hits})
	}
	progress := func(next kube.SearchStep) {
		mu.Lock()
		// Kinds finish on several goroutines, so their counts can arrive out
		// of order; the furthest one is the truth.
		if next.Done < step.Done {
			mu.Unlock()
			return
		}
		step = next
		due := next.Done == 0 || next.Done == next.Total || time.Since(last) >= searchTick
		if due {
			last = time.Now()
		}
		mu.Unlock()
		if due {
			s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchSearching, Step: next})
		}
	}

	err := s.watcher.Search(ctx, kc, q, found, progress)

	mu.Lock()
	final := step
	mu.Unlock()
	if err != nil && ctx.Err() == nil {
		s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchFailed, Step: final, Error: err.Error()})
		return
	}
	s.emit(SearchUpdate{SearchID: id, ContextID: kc.ID, Phase: SearchDone, Step: final})
}

// forget drops a search that has finished, however it finished.
func (s *SearchService) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.running[id]; ok {
		cancel()
		delete(s.running, id)
	}
}

// hitBudget is how many more hits a search may report, shared by every
// cluster in it. The search is called off once it is spent.
type hitBudget struct {
	mu        sync.Mutex
	left      int
	truncated bool
	stop      context.CancelFunc
}

// take hands back as many of the hits as the budget has room for.
func (b *hitBudget) take(hits []kube.SearchHit) []kube.SearchHit {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(hits) > b.left {
		hits = hits[:b.left]
		b.truncated = true
		b.stop()
	}
	b.left -= len(hits)
	return hits
}

// cut reports whether hits were left out for want of room.
func (b *hitBudget) cut() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
