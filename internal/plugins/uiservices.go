package plugins

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/rogerwesterbo/k8sdockside/internal/addons"
	"k8s.io/apimachinery/pkg/labels"
)

// Services a plugin's own views may call.
//
// A view has no network of its own, and most of what a product knows lives in
// the cluster's API. Some of it does not: flow logs, a query API, a health
// page, served by the product's own Service. So a plugin may name those
// Services in its manifest, and its views ask the app to make GET requests to
// them -- through the API server's service proxy, with the credentials the app
// already has, exactly as the charts reach a Prometheus.
//
// What is declared is where a request may go, and the page only picks from
// that: which of the declared services, and a path under one of its declared
// prefixes. The page can never name a namespace, a Service, a port or a host
// of its own, and the declaration is on the plugin's card in Settings before
// any view is opened. GET only: a request that could change something would
// need the confirmation every other change a view asks for gets.

// UIService is one in-cluster Service a plugin's views may call.
type UIService struct {
	// ID is what a view names the service by.
	ID string `json:"id"`
	// Label is how the app names it on the plugin's card and in errors.
	// Defaults to the service name, or the id.
	Label string `json:"label,omitzero"`
	// Namespace pins where the Service is. Required with Name; with Selector
	// it narrows the search, and without it every namespace is searched.
	Namespace string `json:"namespace,omitzero"`
	// Name is the Service's own name. Exactly one of Name and Selector.
	Name string `json:"name,omitzero"`
	// Selector finds the Service by its labels, for a product whose install
	// namespace or release name varies. The first match that has Port is
	// used, in namespace and name order.
	Selector string `json:"selector,omitzero"`
	// Port is the Service port: its name, or its number.
	Port ServicePort `json:"port"`
	// Scheme is http, the default, or https. The API server makes the
	// connection, so whether it checks the certificate is the API server's
	// doing, not the app's.
	Scheme string `json:"scheme,omitzero"`
	// Paths are the path prefixes a view may request, each a whole number of
	// path segments: "/api" allows /api and /api/flows, not /apikeys.
	Paths []string `json:"paths"`
}

// ServicePort is a Service port as a manifest gives it: a name, or a number
// written with or without quotes.
type ServicePort string

// UnmarshalJSON accepts "http", "8081" and 8081 alike.
func (p *ServicePort) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		*p = ServicePort(strings.TrimSpace(text))
		return nil
	}
	var number int
	if err := json.Unmarshal(raw, &number); err != nil {
		return errors.New("a port is a name or a number")
	}
	*p = ServicePort(strconv.Itoa(number))
	return nil
}

// Number is the port as a number, when it was given as one.
func (p ServicePort) Number() (int, bool) {
	n, err := strconv.Atoi(string(p))
	return n, err == nil
}

// maxServices bounds the declarations: each one is a line on the plugin's card.
const maxServices = 8

// Scheme values a service may be reached with.
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Describe is where the service is, as the card and the errors say it.
func (s UIService) Describe() string {
	where := s.Name
	if where == "" {
		where = "the service labelled " + s.Selector
	}
	if s.Namespace != "" {
		where = s.Namespace + "/" + where
	}
	return where + ":" + string(s.Port)
}

// validateServices checks the services a plugin's views may call.
func validateServices(pluginID string, list []UIService) ([]UIService, error) {
	if len(list) > maxServices {
		return list, fmt.Errorf("plugin %q lets its views call %d services; at most %d may be declared", pluginID, len(list), maxServices)
	}
	var errs []error
	seen := map[string]bool{}
	for i, s := range list {
		s, err := validateService(pluginID, s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[s.ID] {
			errs = append(errs, fmt.Errorf("plugin %q declares two services with id %q", pluginID, s.ID))
			continue
		}
		seen[s.ID] = true
		list[i] = s
	}
	return list, errors.Join(errs...)
}

func validateService(pluginID string, s UIService) (UIService, error) {
	s.ID = strings.TrimSpace(s.ID)
	s.Label = strings.TrimSpace(s.Label)
	s.Namespace = strings.TrimSpace(s.Namespace)
	s.Name = strings.TrimSpace(s.Name)
	s.Selector = strings.TrimSpace(s.Selector)
	s.Scheme = strings.ToLower(strings.TrimSpace(s.Scheme))

	if !addons.ValidID(s.ID) {
		return s, fmt.Errorf("plugin %q has a service with id %q; use lowercase letters, digits and dashes", pluginID, s.ID)
	}
	fail := func(format string, args ...any) (UIService, error) {
		return s, fmt.Errorf("plugin %q, service %q: %s", pluginID, s.ID, fmt.Sprintf(format, args...))
	}

	switch {
	case s.Name == "" && s.Selector == "":
		return fail("name the Service, or give a selector that finds it")
	case s.Name != "" && s.Selector != "":
		return fail("give a name or a selector, not both")
	case s.Name != "" && s.Namespace == "":
		return fail("a Service named %q needs its namespace; use a selector to find it wherever it is installed", s.Name)
	}
	if s.Name != "" && !dnsLabel(s.Name) {
		return fail("%q is not a Service name", s.Name)
	}
	if s.Namespace != "" && !dnsLabel(s.Namespace) {
		return fail("%q is not a namespace name", s.Namespace)
	}
	if s.Selector != "" {
		selector, err := labels.Parse(s.Selector)
		if err != nil {
			return fail("label selector %q: %v", s.Selector, err)
		}
		if selector.Empty() {
			return fail("the selector matches every Service; narrow it")
		}
	}

	if err := checkPort(s.Port); err != nil {
		return fail("%v", err)
	}
	switch s.Scheme {
	case "":
		s.Scheme = SchemeHTTP
	case SchemeHTTP, SchemeHTTPS:
	default:
		return fail("scheme %q is neither http nor https", s.Scheme)
	}

	if len(s.Paths) == 0 {
		return fail("list the paths its views may request, such as \"/api/\"")
	}
	for i, prefix := range s.Paths {
		prefix = strings.TrimSpace(prefix)
		if !cleanRequestPath(prefix) {
			return fail("path %q is not a clean absolute path -- no .., //, %%, ? or #", prefix)
		}
		s.Paths[i] = prefix
	}

	if s.Label == "" {
		s.Label = s.Name
	}
	if s.Label == "" {
		s.Label = s.ID
	}
	if len(s.Label) > 60 {
		return fail("the label is %d characters; keep it to 60", len(s.Label))
	}
	return s, nil
}

// checkPort holds a port to what a Service port can be: a number, or an
// IANA-style name.
func checkPort(p ServicePort) error {
	if p == "" {
		return errors.New("name the port, by its name or its number")
	}
	if n, ok := p.Number(); ok {
		if n < 1 || n > 65535 {
			return fmt.Errorf("port %d is out of range", n)
		}
		return nil
	}
	name := string(p)
	if len(name) > 15 || !dnsLabel(name) || !strings.ContainsFunc(name, unicode.IsLetter) {
		return fmt.Errorf("%q is not a port name", name)
	}
	return nil
}

// cleanRequestPath reports whether a path is one a request may carry as it
// is: absolute, already clean, and free of anything that would be read
// differently on the way through a proxy -- a dot segment, an escape, a query
// or a fragment. A trailing slash is kept; some APIs care.
func cleanRequestPath(p string) bool {
	if !strings.HasPrefix(p, "/") || len(p) > 1024 {
		return false
	}
	if strings.Contains(p, "//") || strings.ContainsAny(p, `%?#\`) || strings.ContainsFunc(p, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return false
	}
	trimmed := p
	if len(p) > 1 {
		trimmed = strings.TrimSuffix(p, "/")
	}
	return path.Clean(trimmed) == trimmed
}

// Allows reports whether a view may request a path from this service.
func (s UIService) Allows(p string) bool {
	if !cleanRequestPath(p) {
		return false
	}
	return slices.ContainsFunc(s.Paths, func(prefix string) bool {
		switch {
		case p == prefix, prefix == "/":
			return true
		case strings.HasSuffix(prefix, "/"):
			return strings.HasPrefix(p, prefix)
		default:
			return strings.HasPrefix(p, prefix+"/")
		}
	})
}

// Service returns the service a plugin's views may call by that id.
func (p Plugin) Service(id string) (UIService, bool) {
	if p.UI == nil {
		return UIService{}, false
	}
	for _, s := range p.UI.Services {
		if s.ID == id {
			return s, true
		}
	}
	return UIService{}, false
}
