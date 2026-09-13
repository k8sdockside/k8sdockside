package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Finding a cluster's Prometheus, and saying where it was found.
//
// The default is to look rather than to ask. A Prometheus installed by the
// Operator or by the kube-prometheus-stack chart is findable by label, and going
// through the API server's own service proxy means it is reachable with the
// credentials the app already has -- no port-forward, no second address to
// configure, nothing to set up before the first chart appears. A cluster
// without one but with a VictoriaMetrics is found the same way: it answers the
// same API and the same PromQL.
//
// The override exists because that covers the common case and not every case:
// Thanos, Mimir, a Prometheus in a namespace nothing labels, or one outside the
// cluster entirely.

// Endpoint is a resolved Prometheus: where it is and how we are reaching it.
type Endpoint struct {
	// Service is set when it is reached through the API server's service proxy,
	// which is the usual way.
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      string `json:"port"`
	// Path goes in front of every API path, for a service that serves the
	// Prometheus API below its root: a VictoriaMetrics vmselect serves it
	// under /select/0/prometheus. Empty for a Prometheus.
	Path string `json:"path"`
	// URL is set instead when the user has pointed us at an address directly.
	URL string `json:"url"`
	// Source says how this was decided: "discovered" or "configured". Shown in
	// the UI, because "no charts" has a very different cause depending on which
	// of those it is.
	Source string `json:"source"`
}

// Direct reports whether this endpoint is a URL rather than a service.
func (e Endpoint) Direct() bool { return e.URL != "" }

// Zero reports whether nothing was found or configured.
func (e Endpoint) Zero() bool { return e.URL == "" && e.Service == "" }

// Describe is how the endpoint reads in the UI.
func (e Endpoint) Describe() string {
	switch {
	case e.URL != "":
		return e.URL
	case e.Service != "":
		return e.Namespace + "/" + e.Service + ":" + e.Port + e.Path
	default:
		return ""
	}
}

// The values Endpoint.Source may take.
const (
	Discovered = "discovered"
	Configured = "configured"
)

// ServiceCandidate is one Service the cluster has, as discovery sees it.
type ServiceCandidate struct {
	Namespace string
	Name      string
	Labels    map[string]string
	// Ports by name, with the number each carries. A Service exposing 9090 with
	// no name has an entry keyed by the empty string.
	Ports map[string]int32
}

// wantedPorts are the port names a Prometheus is served on, best first. The
// Operator names it "web"; the community chart names it "http-web".
// VictoriaMetrics names its port "http" everywhere.
var wantedPorts = []string{"web", "http-web", "http", "api"}

// prometheusNumbers and victoriaNumbers are the port numbers each is served
// on by default, for a Service that names its port something else or nothing.
var (
	prometheusNumbers = []int32{9090}
	// 8428 is a single server from the Helm chart, 8429 one from the operator,
	// 8481 a cluster's vmselect.
	victoriaNumbers = []int32{8428, 8429, 8481}
)

// vmselectPath is where a VictoriaMetrics cluster's vmselect serves the
// Prometheus API: tenant 0, the default.
const vmselectPath = "/select/0/prometheus"

// wantedNames are Service names a Prometheus is commonly installed under, best
// first. Reached for only when nothing is labelled, since a name is a much
// weaker signal than a label.
var wantedNames = []string{
	"prometheus-operated",
	"prometheus-k8s",
	"kube-prometheus-stack-prometheus",
	"prometheus-server",
	"prometheus",
}

// Discover picks a Prometheus out of a cluster's services.
//
// Labels first and names second, and within each the order above: a cluster can
// easily have several things called prometheus-something -- a pushgateway, an
// alertmanager, a Thanos sidecar -- and picking the wrong one gives empty charts
// with no clue as to why. A labelled VictoriaMetrics comes after a labelled
// Prometheus and before any guess by name. Returning nothing is a normal
// outcome and not an error: most clusters have no Prometheus.
func Discover(candidates []ServiceCandidate) Endpoint {
	type scored struct {
		endpoint Endpoint
		rank     int
	}
	var best *scored

	consider := func(candidate ServiceCandidate, rank int, numbers []int32, path string) {
		port, ok := servicePort(candidate, numbers)
		if !ok {
			return
		}
		if best != nil && best.rank <= rank {
			return
		}
		best = &scored{
			endpoint: Endpoint{
				Namespace: candidate.Namespace,
				Service:   candidate.Name,
				Port:      port,
				Path:      path,
				Source:    Discovered,
			},
			rank: rank,
		}
	}

	for _, candidate := range candidates {
		// The Operator and the community chart both set this, and it is the
		// only signal that actually means "this is a Prometheus".
		if candidate.Labels["app.kubernetes.io/name"] == "prometheus" {
			consider(candidate, 0, prometheusNumbers, "")
			continue
		}
		if candidate.Labels["app"] == "prometheus" {
			consider(candidate, 1, prometheusNumbers, "")
			continue
		}
		if rank, path, ok := victoriaMetrics(candidate); ok {
			consider(candidate, rank, victoriaNumbers, path)
			continue
		}
		for i, name := range wantedNames {
			if candidate.Name == name {
				consider(candidate, 4+i, prometheusNumbers, "")
				break
			}
		}
	}

	if best == nil {
		return Endpoint{}
	}
	return best.endpoint
}

// victoriaMetrics recognises the VictoriaMetrics services that answer queries,
// by the labels its operator and its Helm charts set: a single server, ranked
// above a cluster's vmselect, which needs its path. vminsert, vmstorage,
// vmagent and vmalert are left alone -- none of them answers a query.
func victoriaMetrics(candidate ServiceCandidate) (rank int, path string, ok bool) {
	name := candidate.Labels["app.kubernetes.io/name"]
	switch {
	case name == "vmsingle" || name == "victoria-metrics-single":
		return 2, "", true
	case name == "vmselect",
		name == "victoria-metrics-cluster" && (candidate.Labels["app"] == "vmselect" || strings.Contains(candidate.Name, "vmselect")):
		return 3, vmselectPath, true
	}
	return 0, "", false
}

// servicePort picks the port to talk to, preferring the names a Prometheus
// is served on and falling back to the default numbers wherever they turn up.
func servicePort(candidate ServiceCandidate, numbers []int32) (string, bool) {
	for _, name := range wantedPorts {
		if _, ok := candidate.Ports[name]; ok {
			return name, true
		}
	}
	for _, want := range numbers {
		for name, number := range candidate.Ports {
			if number == want {
				if name != "" {
					return name, true
				}
				return strconv.Itoa(int(number)), true
			}
		}
	}
	return "", false
}

// serviceRef matches the `namespace/service:port[/path]` form the override accepts.
var serviceRef = regexp.MustCompile(`^([a-z0-9-]+)/([a-z0-9.-]+):([a-zA-Z0-9-]+)(/[A-Za-z0-9._~/-]*)?$`)

// ParseEndpoint reads the override a user typed.
//
// Two forms, because there are two genuinely different situations: a Prometheus
// inside the cluster that discovery simply did not recognise, which should still
// go through the API server proxy and its credentials; and one outside the
// cluster, which cannot. Either may carry a path, for an API served below the
// root (a vmselect's /select/0/prometheus).
//
//	monitoring/prometheus-operated:9090             through the API server's service proxy
//	vm/vmselect-main:8481/select/0/prometheus       the same, below a path
//	https://thanos.example.com                      straight at the address
func ParseEndpoint(value string) (Endpoint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Endpoint{}, nil
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return Endpoint{URL: strings.TrimRight(value, "/"), Source: Configured}, nil
	}
	if match := serviceRef.FindStringSubmatch(value); match != nil {
		return Endpoint{
			Namespace: match[1],
			Service:   match[2],
			Port:      match[3],
			Path:      strings.TrimRight(match[4], "/"),
			Source:    Configured,
		}, nil
	}
	// A bare host:port is the mistake people actually make, so it is worth its
	// own sentence rather than the generic one.
	if strings.Contains(value, ":") && !strings.Contains(value, "/") {
		return Endpoint{}, fmt.Errorf("%q looks like an address without a scheme -- write http://%s", value, value)
	}
	return Endpoint{}, fmt.Errorf("%q is neither namespace/service:port nor an http(s):// address", value)
}

// PortNumber is the port as a number when the override gave one, for callers
// that need it that way. Named ports return 0 and false.
func (e Endpoint) PortNumber() (int, bool) {
	n, err := strconv.Atoi(e.Port)
	if err != nil {
		return 0, false
	}
	return n, true
}
