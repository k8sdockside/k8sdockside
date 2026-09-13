package kube

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rogerwesterbo/k8sdockside/internal/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reaching a cluster's Prometheus.
//
// The default route is the API server's own service proxy. That is worth
// spelling out because it is what makes charts work with no setup: the request
// goes to the same API server the app is already authenticated against, which
// forwards it to the Service. No port-forward to manage, no second credential,
// and it works from anywhere the kubeconfig works -- including through a bastion
// or a VPN that only exposes the API server.

// promTimeout bounds one Prometheus query. Shorter than callTimeout: a chart
// that has not answered in this long has already lost its reader, and a slow
// query left running holds an API server connection open behind it.
const promTimeout = 12 * time.Second

// PrometheusServices lists the Services a Prometheus might be, for discovery.
//
// Every Service in the cluster, rather than a labelled subset, because the
// label that identifies a Prometheus is one of several and a field selector
// cannot express "any of these". The list is small -- a few hundred at most --
// and it is read once when a context's charts are first drawn.
func (w *Watcher) PrometheusServices(kc Context) ([]metrics.ServiceCandidate, error) {
	var out []metrics.ServiceCandidate
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		list, err := c.typed.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, svc := range list.Items {
			ports := make(map[string]int32, len(svc.Spec.Ports))
			for _, port := range svc.Spec.Ports {
				ports[port.Name] = port.Port
			}
			out = append(out, metrics.ServiceCandidate{
				Namespace: svc.Namespace,
				Name:      svc.Name,
				Labels:    svc.Labels,
				Ports:     ports,
			})
		}
		return nil
	})
	return out, err
}

// PrometheusFetch returns the Fetch the metrics package queries through.
//
// Which of the two routes it takes is decided by the endpoint, not here: a
// service reference goes through the API server proxy with the kubeconfig's
// credentials, and a URL goes straight out with none.
func (w *Watcher) PrometheusFetch(kc Context, endpoint metrics.Endpoint) metrics.Fetch {
	if endpoint.Direct() {
		return directFetch(endpoint.URL)
	}
	return w.proxyFetch(kc, endpoint)
}

// proxyFetch queries through the API server's service proxy.
func (w *Watcher) proxyFetch(kc Context, endpoint metrics.Endpoint) metrics.Fetch {
	return func(ctx context.Context, path string, params map[string]string) ([]byte, error) {
		var body []byte
		err := w.withClient(kc, func(c *clusterClient) error {
			call, cancel := context.WithTimeout(ctx, promTimeout)
			defer cancel()

			raw, err := c.typed.CoreV1().
				Services(endpoint.Namespace).
				ProxyGet("http", endpoint.Service, endpoint.Port, endpoint.Path+path, params).
				DoRaw(call)
			if err != nil {
				return proxyError(endpoint, err)
			}
			body = raw
			return nil
		})
		return body, err
	}
}

// proxyError explains a failed proxy call in terms of the thing the reader can
// act on. The wire error names a URL inside the API server that means nothing
// to anybody, so the Service is named instead.
func proxyError(endpoint metrics.Endpoint, err error) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "not found"):
		return fmt.Errorf("no service %s/%s in this cluster", endpoint.Namespace, endpoint.Service)
	case strings.Contains(text, "forbidden"), strings.Contains(text, "Forbidden"):
		return fmt.Errorf("not allowed to reach %s/%s through the API server -- proxying a service needs the services/proxy permission",
			endpoint.Namespace, endpoint.Service)
	default:
		return fmt.Errorf("reaching %s: %w", endpoint.Describe(), err)
	}
}

// directFetch queries a URL the user configured, without credentials.
//
// Deliberately no authentication and no client certificate: this is an address
// somebody typed into a settings field, and quietly presenting the cluster's
// credentials to it would be a way to leak them somewhere the kubeconfig never
// pointed. A Prometheus behind auth wants a proxy in front of it, or the service
// form above.
func directFetch(base string) metrics.Fetch {
	client := &http.Client{
		Timeout: promTimeout,
		Transport: &http.Transport{
			// The default, spelled out: a configured HTTPS endpoint is verified
			// like any other. Nothing here turns verification off.
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	return func(ctx context.Context, path string, params map[string]string) ([]byte, error) {
		target, err := url.Parse(base + path)
		if err != nil {
			return nil, fmt.Errorf("%s is not a usable address: %w", base, err)
		}
		query := target.Query()
		for key, value := range params {
			query.Set(key, value)
		}
		target.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("reaching %s: %w", base, err)
		}
		// Closed for its side effect of releasing the connection; a failure to
		// close a response we have finished reading is nothing the caller can
		// act on.
		defer func() { _ = resp.Body.Close() }()

		// Bounded: this is an address outside the cluster answering, and an
		// unbounded read is a way for it to spend all the memory it likes.
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPromBody))
		if err != nil {
			return nil, err
		}
		// A Prometheus reports a bad query as 400 with the reason in the body,
		// which the parser reads; anything else is worth reporting as a status.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s answered %s", base, resp.Status)
		}
		return body, nil
	}
}

// maxPromBody bounds one Prometheus answer. A range query capped at 120 steps
// is a few tens of kilobytes even with many series.
const maxPromBody = 8 << 20 // 8 MiB
