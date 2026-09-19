package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/metrics"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
)

// MetricsService draws the charts a plugin declares, against the cluster's own
// Prometheus.
//
// Finding the Prometheus is the interesting part and is cached per context: it
// costs a list of every Service in the cluster, and the answer changes when
// somebody installs a monitoring stack rather than continuously. Everything
// after that is one HTTP call per chart.
type MetricsService struct {
	store   *appconfig.Store
	configs *KubeconfigService
	watcher *kube.Watcher
	plugins *PluginService

	mu    sync.Mutex
	found map[string]metrics.Endpoint
}

// NewMetricsService wires the service to the cluster connections and the plugin
// catalogue whose charts it draws.
func NewMetricsService(store *appconfig.Store, configs *KubeconfigService, watcher *kube.Watcher, p *PluginService) *MetricsService {
	return &MetricsService{
		store:   store,
		configs: configs,
		watcher: watcher,
		plugins: p,
		found:   map[string]metrics.Endpoint{},
	}
}

// Source is where a context's metrics come from, for the settings view and for
// the message a chart panel shows when there are none.
type Source struct {
	Endpoint metrics.Endpoint `json:"endpoint"`
	// Configured is the override as the user typed it, so the settings field can
	// show it back.
	Configured string `json:"configured"`
	// Available is whether there is anywhere to query at all.
	Available bool `json:"available"`
	// Error is why we could not look, as opposed to having looked and found
	// nothing. A cluster that refused the request and a cluster with no
	// Prometheus need different things said about them.
	Error string `json:"error"`
}

// Source resolves where one context's metrics come from: the override if there
// is one, and otherwise whatever discovery finds.
func (s *MetricsService) Source(contextID string) Source {
	configured := s.store.MetricsEndpoint(contextID)
	out := Source{Configured: configured}

	if configured != "" {
		endpoint, err := metrics.ParseEndpoint(configured)
		if err != nil {
			out.Error = err.Error()
			return out
		}
		out.Endpoint = endpoint
		out.Available = !endpoint.Zero()
		return out
	}

	endpoint, err := s.discover(contextID)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Endpoint = endpoint
	out.Available = !endpoint.Zero()
	return out
}

// discover finds a context's Prometheus, remembering the answer.
func (s *MetricsService) discover(contextID string) (metrics.Endpoint, error) {
	s.mu.Lock()
	cached, known := s.found[contextID]
	s.mu.Unlock()
	if known {
		return cached, nil
	}

	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return metrics.Endpoint{}, fmt.Errorf("unknown context %q", contextID)
	}
	services, err := s.watcher.PrometheusServices(ctx)
	if err != nil {
		// Not cached: an unreachable cluster now may be reachable in a minute,
		// and caching "nothing" here would mean never looking again.
		return metrics.Endpoint{}, err
	}

	endpoint := metrics.Discover(services)
	s.mu.Lock()
	s.found[contextID] = endpoint
	s.mu.Unlock()
	return endpoint, nil
}

// Rediscover forgets what was found for a context, so the next chart looks
// again. Called when the override changes, and offered as a button for the case
// where monitoring was installed while the app was open.
func (s *MetricsService) Rediscover(contextID string) Source {
	s.mu.Lock()
	delete(s.found, contextID)
	s.mu.Unlock()
	return s.Source(contextID)
}

// SetEndpoint records where a context's Prometheus is, overriding discovery. An
// empty value clears the override and goes back to looking.
func (s *MetricsService) SetEndpoint(contextID, value string) (Source, error) {
	if _, err := metrics.ParseEndpoint(value); err != nil {
		return s.Source(contextID), err
	}
	if _, err := s.store.SetMetricsEndpoint(contextID, value); err != nil {
		return s.Source(contextID), err
	}
	return s.Rediscover(contextID), nil
}

// Panel is a surface's worth of charts, plus why there are none if there are
// none.
type Panel struct {
	// Attached is whether any installed plugin draws on this surface at all.
	//
	// It is the difference between "there is nothing to show here" and "there is
	// something to show and it is empty", and only the second is worth drawing a
	// heading for. Without it every pod in a cluster with no charting plugin
	// would carry an empty Metrics section.
	Attached bool `json:"attached"`
	// Source is carried so the panel can say where the numbers came from, and
	// what to do when there are none.
	Source Source                `json:"source"`
	Charts []plugins.ChartResult `json:"charts"`
	Range  int                   `json:"range"`
}

// Charts draws every chart the installed plugins attach to one surface.
//
// `surface` is a resource kind, "dashboard", or one plugin's overview as
// plugins.OverviewSurface names it. `namespace` and `name` identify the object
// for a per-object surface and are ignored for the others. `minutes` is how
// far back to look.
func (s *MetricsService) Charts(contextID, surface, namespace, name string, minutes int) Panel {
	panel := Panel{Range: minutes, Charts: []plugins.ChartResult{}}

	// Enabled rather than every plugin: switching one off has to take its
	// charts with it, or the dashboard would go on drawing for something the
	// user has said they do not want to see. An overview narrows it further,
	// to the one plugin whose page it is.
	attach, installed := plugins.Surface(surface, s.plugins.List().Enabled())
	panel.Attached = plugins.HasChartsFor(installed, attach)
	if !panel.Attached {
		// Nothing to draw here, so the cluster is not asked where its
		// Prometheus is either -- that costs a list of every Service, and a pod
		// in a cluster with no charting plugin should cost nothing at all.
		return panel
	}

	panel.Source = s.Source(contextID)
	if !panel.Source.Available {
		return panel
	}

	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		panel.Source.Error = fmt.Sprintf("unknown context %q", contextID)
		return panel
	}

	fetch := s.watcher.PrometheusFetch(ctx, panel.Source.Endpoint)
	panel.Charts = plugins.ChartsFor(
		installed,
		attach,
		// A node's object name is what its charts call $node as well as $name,
		// so a query may use either and mean the same thing.
		metrics.Variables{Namespace: namespace, Name: name, Node: name},
		metrics.Range{Minutes: minutes},
		&promQuerier{fetch: fetch},
	)
	return panel
}

// Attachments reports which surfaces have any charts at all, so the frontend can
// leave the panel out entirely rather than drawing an empty one and then
// removing it.
func (s *MetricsService) Attachments() []string {
	return s.plugins.List().Attachments()
}

// promQuerier adapts a Fetch to the interface the plugin package draws through.
type promQuerier struct {
	fetch metrics.Fetch
}

func (q *promQuerier) QueryRange(query, legend string, r metrics.Range) (metrics.Result, error) {
	// Bounded a little above the per-request timeout, so a slow chart fails with
	// Prometheus's own reason rather than this deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return metrics.QueryRange(ctx, q.fetch, query, legend, r)
}
