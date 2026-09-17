package kube

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
)

// GET requests to a Service, through the API server's service proxy, for a
// plugin's own views. What may be asked is decided in internal/plugins and
// the plugin service; this only makes the request and reports the answer.
//
// The answer is the Service's own: its status and its body, whatever they
// are, so a view can read a 404 from its API as the API meant it. What the
// API server says on its own account -- the proxy is forbidden, the Service is
// missing or has nothing behind it -- is an error instead, worded for the
// person who can fix it.

// ServiceTarget is one Service port to call.
type ServiceTarget struct {
	Namespace string
	Name      string
	// Port is the Service port's name or number.
	Port   string
	Scheme string
}

// Describe is how the target reads in an answer and an error.
func (t ServiceTarget) Describe() string {
	return t.Namespace + "/" + t.Name + ":" + t.Port
}

// ServiceAnswer is what a Service said.
type ServiceAnswer struct {
	// Service is the Service that answered, as namespace/name:port.
	Service     string `json:"service"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	// Body is the answer as text. The views read JSON and text; anything else
	// arrives mangled rather than refused, and says so in ContentType.
	Body string `json:"body"`
}

// ServicePort is one port of a Service, as FindServices lists it.
type ServicePort struct {
	Name   string
	Number int32
}

// ServiceFound is a Service a selector found.
type ServiceFound struct {
	Namespace string
	Name      string
	Ports     []ServicePort
}

const (
	// serviceTimeout bounds one request. A view is waiting on it, and a slow
	// request holds an API server connection open behind it.
	serviceTimeout = 15 * time.Second
	// maxServiceBody bounds one answer: the app holds it in memory and hands
	// it through the webview's bridge in one message.
	maxServiceBody = 8 << 20 // 8 MiB
)

// FindServices lists the Services a selector matches, in one namespace or in
// all of them, in namespace and name order.
func (w *Watcher) FindServices(kc Context, namespace, selector string) ([]ServiceFound, error) {
	var out []ServiceFound
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		list, err := c.typed.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, svc := range list.Items {
			found := ServiceFound{Namespace: svc.Namespace, Name: svc.Name}
			for _, port := range svc.Spec.Ports {
				found.Ports = append(found.Ports, ServicePort{Name: port.Name, Number: port.Port})
			}
			out = append(out, found)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, err
}

// ServiceGet makes one GET request to a Service through the API server. path
// is sent as it is, so it must already have been checked; query is encoded
// here.
func (w *Watcher) ServiceGet(ctx context.Context, kc Context, target ServiceTarget, path string, query url.Values) (ServiceAnswer, error) {
	answer := ServiceAnswer{Service: target.Describe()}
	err := w.withClient(kc, func(c *clusterClient) error {
		client, err := c.proxyClient()
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, serviceTimeout)
		defer cancel()

		// The request builder gives the proxy's address; the path is added by
		// hand, because the builder cleans what it is given and would drop a
		// trailing slash an API may want.
		address := c.typed.CoreV1().RESTClient().Get().
			Namespace(target.Namespace).
			Resource("services").
			Name(utilnet.JoinSchemeNamePort(target.Scheme, target.Name, target.Port)).
			SubResource("proxy").
			URL()
		address.Path = strings.TrimSuffix(address.Path, "/") + path
		address.RawPath = ""
		address.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(call, http.MethodGet, address.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.5")
		resp, err := client.Do(req)
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s did not answer within %s -- if a network policy covers its pods, it has to let the API server in", target.Describe(), serviceTimeout)
		}
		if err != nil {
			return fmt.Errorf("reaching %s: %w", target.Describe(), err)
		}
		// Closed for its side effect of releasing the connection.
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxServiceBody+1))
		if err != nil {
			return fmt.Errorf("reading %s's answer: %w", target.Describe(), err)
		}
		if len(body) > maxServiceBody {
			return fmt.Errorf("%s answered with more than %d MiB; ask for less", target.Describe(), maxServiceBody>>20)
		}
		if err := proxyRefusal(target, resp, body); err != nil {
			return err
		}
		answer.Status = resp.StatusCode
		answer.ContentType = resp.Header.Get("Content-Type")
		answer.Body = strings.ToValidUTF8(string(body), string(utf8.RuneError))
		return nil
	})
	return answer, err
}

// proxyClient is the HTTP client the service proxy is called through: the
// connection's own credentials and TLS, built once per connection.
func (c *clusterClient) proxyClient() (*http.Client, error) {
	c.proxyOnce.Do(func() {
		c.proxyHTTP, c.proxyErr = rest.HTTPClientFor(c.cfg)
	})
	return c.proxyHTTP, c.proxyErr
}

// proxyRefusal turns what the API server said on its own account into an
// error, and leaves the Service's own answers alone. The API server speaks in
// Status objects; a Service that happens to answer with one is taken at its
// word only when the API server could not have said it.
func proxyRefusal(target ServiceTarget, resp *http.Response, body []byte) error {
	if resp.StatusCode < 400 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	var status apiStatus
	if json.Unmarshal(body, &status) != nil || status.Kind != "Status" || status.APIVersion != "v1" {
		return nil
	}
	switch {
	case status.Reason == metav1.StatusReasonForbidden:
		return fmt.Errorf("not allowed to reach %s through the API server -- calling a service needs the services/proxy permission", target.Describe())
	case status.Reason == metav1.StatusReasonNotFound && status.Details.Kind == "services":
		return fmt.Errorf("no service %s/%s in this cluster", target.Namespace, target.Name)
	case resp.StatusCode == http.StatusServiceUnavailable && strings.Contains(status.Message, "no endpoints available"):
		return fmt.Errorf("%s has no ready pod behind it", target.Describe())
	case resp.StatusCode == http.StatusServiceUnavailable && strings.HasPrefix(status.Message, "no service port"):
		return fmt.Errorf("service %s/%s has no port %s", target.Namespace, target.Name, target.Port)
	case resp.StatusCode == http.StatusServiceUnavailable && strings.HasPrefix(status.Message, "error trying to reach service"):
		// The API server's own dial or TLS failure, with its reason after
		// the colon: a refused connection, a timeout, a certificate.
		return fmt.Errorf("the API server could not reach %s: %s", target.Describe(), strings.TrimSpace(strings.TrimPrefix(status.Message, "error trying to reach service:")))
	case status.Reason == metav1.StatusReasonUnauthorized:
		return fmt.Errorf("the cluster refused the app's credentials while reaching %s", target.Describe())
	}
	return nil
}

// apiStatus is the part of a metav1.Status proxyRefusal reads.
type apiStatus struct {
	Kind       string              `json:"kind"`
	APIVersion string              `json:"apiVersion"`
	Message    string              `json:"message"`
	Reason     metav1.StatusReason `json:"reason"`
	Details    struct {
		Kind string `json:"kind"`
	} `json:"details"`
}
