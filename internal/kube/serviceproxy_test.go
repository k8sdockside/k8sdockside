package kube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A plugin view's service call, against an httptest server standing in for
// the API server and the Service behind its proxy.

func TestServiceGetGoesThroughTheProxyAndKeepsTheServicesAnswer(t *testing.T) {
	var asked *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL
		if r.Method != http.MethodGet {
			t.Errorf("method %s, want GET", r.Method)
		}
		// The Service's own 404, in its own words: not the API server's.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error":"no such flow"}`)
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	target := ServiceTarget{Namespace: "calico-system", Name: "whisker", Port: "8081", Scheme: "http"}
	answer, err := w.ServiceGet(context.Background(), kubeconfigFor(t, srv.URL), target, "/whisker-backend/flows/", url.Values{"sortBy": {"Time"}, "filter": {"a", "b"}})
	if err != nil {
		t.Fatalf("ServiceGet: %v", err)
	}
	if want := "/api/v1/namespaces/calico-system/services/http:whisker:8081/proxy/whisker-backend/flows/"; asked.Path != want {
		t.Errorf("asked %s, want %s -- the trailing slash included", asked.Path, want)
	}
	if got := asked.Query(); got.Get("sortBy") != "Time" || strings.Join(got["filter"], ",") != "a,b" {
		t.Errorf("query %v, want sortBy and both filters", got)
	}
	if answer.Status != http.StatusNotFound || answer.Body != `{"error":"no such flow"}` || answer.ContentType != "application/json" {
		t.Errorf("answer %+v, want the Service's 404 as it was", answer)
	}
	if answer.Service != "calico-system/whisker:8081" {
		t.Errorf("service %q", answer.Service)
	}
}

func TestServiceGetNamesTheSchemeForHTTPS(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	target := ServiceTarget{Namespace: "kube-system", Name: "hubble-ui", Port: "https", Scheme: "https"}
	if _, err := w.ServiceGet(context.Background(), kubeconfigFor(t, srv.URL), target, "/healthz", nil); err != nil {
		t.Fatalf("ServiceGet: %v", err)
	}
	if want := "/api/v1/namespaces/kube-system/services/https:hubble-ui:https/proxy/healthz"; asked != want {
		t.Errorf("asked %s, want %s", asked, want)
	}
}

// What the API server says on its own account is an error worded for whoever
// can fix it; the same status from the Service is its answer.
func TestServiceGetExplainsTheAPIServersRefusals(t *testing.T) {
	status := func(code int, reason, message, kind string) string {
		return fmt.Sprintf(`{"kind":"Status","apiVersion":"v1","status":"Failure","code":%d,"reason":%q,"message":%q,"details":{"name":"whisker","kind":%q}}`, code, reason, message, kind)
	}
	for name, tc := range map[string]struct {
		code int
		body string
		want string
	}{
		"forbidden": {http.StatusForbidden, status(403, "Forbidden", `services "whisker" is forbidden`, "services"), "services/proxy permission"},
		"missing":   {http.StatusNotFound, status(404, "NotFound", `services "whisker" not found`, "services"), "no service calico-system/whisker"},
		"no pods":   {http.StatusServiceUnavailable, status(503, "ServiceUnavailable", `no endpoints available for service "whisker"`, ""), "no ready pod"},
		"no port":   {http.StatusServiceUnavailable, status(503, "ServiceUnavailable", `no service port 8443 found for service "whisker"`, ""), "has no port http"},
		"unreached": {http.StatusServiceUnavailable, status(503, "ServiceUnavailable", `error trying to reach service: dial tcp 10.0.0.7:8081: i/o timeout`, ""), "could not reach calico-system/whisker:http: dial tcp 10.0.0.7:8081: i/o timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			w := NewWatcher(func(Snapshot) {})
			defer w.Close()

			target := ServiceTarget{Namespace: "calico-system", Name: "whisker", Port: "http", Scheme: "http"}
			_, err := w.ServiceGet(context.Background(), kubeconfigFor(t, srv.URL), target, "/flows", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestServiceGetRefusesAnAnswerTooBigToHold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxServiceBody+1)))
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	target := ServiceTarget{Namespace: "a", Name: "b", Port: "80", Scheme: "http"}
	_, err := w.ServiceGet(context.Background(), kubeconfigFor(t, srv.URL), target, "/big", nil)
	if err == nil || !strings.Contains(err.Error(), "more than 8 MiB") {
		t.Errorf("error %v, want the answer refused as too big", err)
	}
}

func TestFindServicesListsMatchesInOrder(t *testing.T) {
	var asked *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"kind":"ServiceList","apiVersion":"v1","items":[
			{"metadata":{"name":"whisker","namespace":"z-system"},"spec":{"ports":[{"name":"http","port":8081}]}},
			{"metadata":{"name":"whisker","namespace":"calico-system"},"spec":{"ports":[{"port":8081},{"name":"metrics","port":9090}]}}
		]}`)
	}))
	defer srv.Close()

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	found, err := w.FindServices(kubeconfigFor(t, srv.URL), "", "k8s-app=whisker")
	if err != nil {
		t.Fatalf("FindServices: %v", err)
	}
	if asked.Path != "/api/v1/services" || asked.Query().Get("labelSelector") != "k8s-app=whisker" {
		t.Errorf("asked %s, want every namespace with the selector", asked)
	}
	if len(found) != 2 || found[0].Namespace != "calico-system" || found[1].Namespace != "z-system" {
		t.Fatalf("found %+v, want both, calico-system first", found)
	}
	if ports := found[0].Ports; len(ports) != 2 || ports[0] != (ServicePort{Number: 8081}) || ports[1] != (ServicePort{Name: "metrics", Number: 9090}) {
		t.Errorf("ports %+v", ports)
	}
}

// A Service whose pods never answer -- a network policy dropping the API
// server's connection, most often -- is given up on, and said so.
func TestServiceGetGivesUpOnASilentService(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()
	target := ServiceTarget{Namespace: "calico-system", Name: "whisker", Port: "8081", Scheme: "http"}
	_, err := w.ServiceGet(ctx, kubeconfigFor(t, srv.URL), target, "/flows", nil)
	if err == nil || !strings.Contains(err.Error(), "did not answer") || !strings.Contains(err.Error(), "network policy") {
		t.Errorf("error %v, want it to say the service did not answer", err)
	}
}
