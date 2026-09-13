package metrics

import (
	"strings"
	"testing"
)

func svc(ns, name string, labels map[string]string, ports map[string]int32) ServiceCandidate {
	return ServiceCandidate{Namespace: ns, Name: name, Labels: labels, Ports: ports}
}

// A cluster can easily have several things called prometheus-something -- a
// pushgateway, an alertmanager, a Thanos sidecar -- and picking the wrong one
// gives empty charts with no clue as to why.
func TestDiscoverPrefersTheLabelledService(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("monitoring", "prometheus-pushgateway", nil, map[string]int32{"http": 9091}),
		svc("default", "prometheus", nil, map[string]int32{"http": 9090}),
		svc("monitoring", "prometheus-operated",
			map[string]string{"app.kubernetes.io/name": "prometheus"},
			map[string]int32{"web": 9090}),
	})

	if got.Service != "prometheus-operated" || got.Namespace != "monitoring" {
		t.Fatalf("found %s/%s, want the labelled one", got.Namespace, got.Service)
	}
	if got.Port != "web" {
		t.Errorf("port = %q, want the named port", got.Port)
	}
	if got.Source != Discovered {
		t.Errorf("source = %q", got.Source)
	}
}

func TestDiscoverFallsBackToKnownNames(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("kube-system", "kubelet", nil, map[string]int32{"https-metrics": 10250}),
		svc("monitoring", "prometheus-k8s", nil, map[string]int32{"web": 9090}),
	})
	if got.Service != "prometheus-k8s" {
		t.Fatalf("found %q", got.Service)
	}
}

// Most clusters have no Prometheus, which is an ordinary answer.
func TestDiscoverFindsNothingQuietly(t *testing.T) {
	got := Discover([]ServiceCandidate{svc("default", "web", nil, map[string]int32{"http": 80})})
	if !got.Zero() {
		t.Errorf("found %+v, want nothing", got)
	}
}

// A service that is called prometheus but serves nothing on a port we could
// query is not a usable answer.
func TestDiscoverSkipsServicesWithNoUsablePort(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("monitoring", "prometheus", map[string]string{"app": "prometheus"}, map[string]int32{"grpc": 10901}),
	})
	if !got.Zero() {
		t.Errorf("found %+v, want nothing", got)
	}
}

func TestDiscoverTakesAnUnnamed9090(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("obs", "prometheus", map[string]string{"app": "prometheus"}, map[string]int32{"": 9090}),
	})
	if got.Port != "9090" {
		t.Errorf("port = %q, want the number", got.Port)
	}
}

func TestParseEndpoint(t *testing.T) {
	service, err := ParseEndpoint("monitoring/prometheus-operated:9090")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if service.Namespace != "monitoring" || service.Service != "prometheus-operated" || service.Port != "9090" {
		t.Errorf("parsed as %+v", service)
	}
	if service.Direct() {
		t.Error("a service reference should not be reached directly")
	}
	if service.Source != Configured {
		t.Errorf("source = %q", service.Source)
	}

	direct, err := ParseEndpoint("https://thanos.example.com/")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if !direct.Direct() || direct.URL != "https://thanos.example.com" {
		t.Errorf("parsed as %+v, want the trailing slash trimmed", direct)
	}

	// Empty is not an error: it is how the override is cleared.
	empty, err := ParseEndpoint("  ")
	if err != nil || !empty.Zero() {
		t.Errorf("empty parsed as %+v, %v", empty, err)
	}
}

// The mistake people actually make, so it gets its own sentence rather than the
// generic one.
func TestParseEndpointExplainsAMissingScheme(t *testing.T) {
	_, err := ParseEndpoint("prometheus.example.com:9090")
	if err == nil {
		t.Fatal("a bare host:port was accepted")
	}
	if want := "http://prometheus.example.com:9090"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not suggest %q", err, want)
	}
}

func TestParseEndpointRefusesNonsense(t *testing.T) {
	for _, value := range []string{"ftp://x", "just some words", "/leading", "ns/svc"} {
		if _, err := ParseEndpoint(value); err == nil {
			t.Errorf("ParseEndpoint accepted %q", value)
		}
	}
}

// A cluster with no Prometheus but a VictoriaMetrics single server: it answers
// the Prometheus API at its root.
func TestDiscoverFindsVictoriaMetricsSingle(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("default", "web", nil, map[string]int32{"http": 80}),
		svc("vm", "vmagent-k8s-stack", map[string]string{"app.kubernetes.io/name": "vmagent"}, map[string]int32{"http": 8429}),
		svc("vm", "vmsingle-k8s-stack", map[string]string{"app.kubernetes.io/name": "vmsingle"}, map[string]int32{"http": 8429}),
	})
	if got.Service != "vmsingle-k8s-stack" || got.Port != "http" || got.Path != "" {
		t.Fatalf("found %+v, want the vmsingle at its root", got)
	}
}

// A VictoriaMetrics cluster's vmselect serves the Prometheus API under a prefix.
func TestDiscoverFindsVMSelectUnderItsPrefix(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("vm", "vminsert-main", map[string]string{"app.kubernetes.io/name": "vminsert"}, map[string]int32{"": 8480}),
		svc("vm", "vmselect-main", map[string]string{"app.kubernetes.io/name": "vmselect"}, map[string]int32{"": 8481}),
	})
	if got.Service != "vmselect-main" || got.Port != "8481" || got.Path != "/select/0/prometheus" {
		t.Fatalf("found %+v, want vmselect on 8481 under /select/0/prometheus", got)
	}
	if want := "vm/vmselect-main:8481/select/0/prometheus"; got.Describe() != want {
		t.Errorf("Describe() = %q, want %q", got.Describe(), want)
	}
}

// The Helm cluster chart labels every component the same; the vmselect one is
// told apart by its own label or its name.
func TestDiscoverFindsTheHelmClusterChartsVMSelect(t *testing.T) {
	chart := map[string]string{"app.kubernetes.io/name": "victoria-metrics-cluster"}
	got := Discover([]ServiceCandidate{
		svc("vm", "vm-victoria-metrics-cluster-vmstorage", chart, map[string]int32{"http": 8482}),
		svc("vm", "vm-victoria-metrics-cluster-vmselect", chart, map[string]int32{"http": 8481}),
	})
	if got.Service != "vm-victoria-metrics-cluster-vmselect" || got.Path != "/select/0/prometheus" {
		t.Fatalf("found %+v, want the vmselect", got)
	}
}

// Both installed: the Prometheus is what the charts were written against.
func TestDiscoverPrefersPrometheusOverVictoriaMetrics(t *testing.T) {
	got := Discover([]ServiceCandidate{
		svc("vm", "vmsingle-main", map[string]string{"app.kubernetes.io/name": "vmsingle"}, map[string]int32{"http": 8429}),
		svc("monitoring", "prometheus-operated", map[string]string{"app.kubernetes.io/name": "prometheus"}, map[string]int32{"web": 9090}),
	})
	if got.Service != "prometheus-operated" {
		t.Fatalf("found %q, want the Prometheus", got.Service)
	}
}

func TestParseEndpointTakesAPath(t *testing.T) {
	got, err := ParseEndpoint("vm/vmselect-main:8481/select/0/prometheus/")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if got.Service != "vmselect-main" || got.Port != "8481" || got.Path != "/select/0/prometheus" {
		t.Errorf("parsed as %+v, want the path without its trailing slash", got)
	}
}

func TestDescribe(t *testing.T) {
	if got := (Endpoint{Namespace: "m", Service: "p", Port: "web"}).Describe(); got != "m/p:web" {
		t.Errorf("got %q", got)
	}
	if got := (Endpoint{URL: "http://x"}).Describe(); got != "http://x" {
		t.Errorf("got %q", got)
	}
	if got := (Endpoint{}).Describe(); got != "" {
		t.Errorf("got %q", got)
	}
}
