package kube

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// Counting objects by one of their own fields, which is what a solution
// plugin's overview is made of: 42 Applications, 38 of them Healthy.
//
// It is a one-shot list rather than a watch. An overview is a page you open and
// read, not a table you sit in front of, and holding an informer open per card
// would mean a plugin's landing page quietly costing more than the tab it
// links to.

// FieldPath addresses one value inside an object, in the two shapes a
// Kubernetes status is actually written in:
//
//	status.health.status          a plain dotted path
//	status.conditions[Ready]      the status of the condition of that type
//
// The second form exists because conditions are the near-universal Kubernetes
// idiom and a dotted path cannot reach into a list. It is deliberately not a
// general query language: a plugin file comes from outside the app, and "an
// address" is a much smaller thing to accept than "an expression".
type FieldPath string

// conditionOf splits `status.conditions[Ready]` into the path of the list and
// the condition type wanted. ok is false for a plain dotted path.
func (p FieldPath) conditionOf() (path []string, want string, ok bool) {
	open := strings.IndexByte(string(p), '[')
	if open < 0 || !strings.HasSuffix(string(p), "]") {
		return nil, "", false
	}
	want = string(p)[open+1 : len(p)-1]
	if want == "" {
		return nil, "", false
	}
	return strings.Split(string(p)[:open], "."), want, true
}

// mapKeyOf splits `status.capacity{nvidia.com/gpu}` into the path of the map
// and the key wanted. It is the one way to reach a key with dots or slashes in
// it -- an extended resource, a label -- which a dotted path would cut apart.
// ok is false for any other shape.
func (p FieldPath) mapKeyOf() (path []string, key string, ok bool) {
	open := strings.IndexByte(string(p), '{')
	if open <= 0 || !strings.HasSuffix(string(p), "}") {
		return nil, "", false
	}
	key = string(p)[open+1 : len(p)-1]
	if key == "" {
		return nil, "", false
	}
	return strings.Split(string(p)[:open], "."), key, true
}

// Value reads this path out of one object, or "" when the object does not have
// it -- which is an ordinary answer: a resource that has not been reconciled
// yet has no status at all.
func (p FieldPath) Value(u *unstructured.Unstructured) string {
	if path, key, ok := p.mapKeyOf(); ok {
		return nestedString(u, append(path, key)...)
	}
	if path, want, ok := p.conditionOf(); ok {
		for _, raw := range nestedSlice(u, path...) {
			cond := asMap(raw)
			if strings.EqualFold(mapString(cond, "type"), want) {
				return mapString(cond, "status")
			}
		}
		return ""
	}
	if p == "" {
		return ""
	}
	return nestedString(u, strings.Split(string(p), ".")...)
}

// Valid reports whether a path is one this app is willing to evaluate. It is
// checked when a plugin is loaded rather than when a card is drawn, so a typo
// is reported next to the plugin instead of as an empty tile.
func (p FieldPath) Valid() bool {
	segments := strings.Split(string(p), ".")
	if path, want, ok := p.conditionOf(); ok {
		if !identifier(want) {
			return false
		}
		segments = path
	}
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if !identifier(segment) {
			return false
		}
	}
	return true
}

// ValidLookup reports whether a path is one a workload probe may use: any path
// Valid accepts, or a dotted path ending in a `{key}` lookup whose key is what
// Kubernetes allows in an extended resource or label name. Only probes take
// the lookup form -- cards keep to the two shapes their reference documents.
func (p FieldPath) ValidLookup() bool {
	path, key, ok := p.mapKeyOf()
	if !ok {
		return p.Valid()
	}
	for _, segment := range path {
		if !identifier(segment) {
			return false
		}
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
		default:
			return false
		}
	}
	return true
}

// identifier is one segment of a field path: a JSON object key as Kubernetes
// writes them.
func identifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// NoSelector is the label selector meaning "every object of this kind", which
// is what all but a plugin's own views want.
const NoSelector = ""

// Tally is how many objects of a kind there are, and how they divide up by one
// of their fields.
type Tally struct {
	Total int `json:"total"`
	// Counts by the value of the field asked for. An object whose field is
	// absent is counted under "" -- distinct from having no objects, and the
	// caller decides how to word it.
	Counts map[string]int `json:"counts"`
}

// CountBy lists one kind and tallies it by a field.
//
// namespace and selector narrow what is counted, matching what the same
// plugin's table view would show; either may be empty. An empty path counts
// without dividing.
func (w *Watcher) CountBy(kc Context, kind, namespace, selector string, path FieldPath) (Tally, error) {
	tally := Tally{Counts: map[string]int{}}

	chosen, err := labels.Parse(selector)
	if err != nil {
		return tally, fmt.Errorf("label selector %q: %w", selector, err)
	}

	err = w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		// The selector goes to the server, which is both faster and less to
		// carry back. The namespace is filtered here instead: List takes it
		// through a namespaced client rather than through ListOptions, and the
		// one-shot path deliberately does not build one.
		items, _, err := c.list(ctx, kind, metav1.ListOptions{LabelSelector: chosen.String()})
		if err != nil {
			return err
		}
		for i := range items {
			if namespace != AllNamespaces && items[i].GetNamespace() != namespace {
				continue
			}
			tally.Total++
			tally.Counts[path.Value(&items[i])]++
		}
		return nil
	})
	return tally, err
}

// IsKnownKind reports whether a kind names something this app can open: one of
// the built-in kinds, or a well-formed custom resource.
//
// It is exported for the plugin loader, which validates a plugin's views when
// the file is read rather than leaving a typo to surface later as a tab that
// will not open.
func IsKnownKind(kind string) bool {
	if _, _, ok := ParseCustomKind(kind); ok {
		return true
	}
	if kind == KindHelmReleases {
		return true
	}
	_, ok := builtinKinds[kind]
	return ok
}
