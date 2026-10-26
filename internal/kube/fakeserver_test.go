package kube

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeServer stands in for an API server well enough for a client to discover
// it, list pods and the definitions, and hold a watch open -- and counts what
// it was asked, which is what the tests below are about. Ping's tests need
// only /version and use a bare handler; these need the rest.
type fakeServer struct {
	*httptest.Server

	mu    sync.Mutex
	asked map[string]int

	// served are the custom resources discovery reports, as plural.group.
	served []string
	// defined are the definitions the server holds, as plural.group. Usually
	// the same as served; a definition discovery has not heard of is one that
	// is defined and not served.
	defined []string
}

// newFakeServer starts a server holding one custom resource, widgets in
// example.com, served and defined.
func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{
		asked:   map[string]int{},
		served:  []string{"widgets.example.com"},
		defined: []string{"widgets.example.com"},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

// count reports how often a request was made, by its key -- the path, with
// "?watch" or "?table" appended for those.
func (f *fakeServer) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[key]
}

func (f *fakeServer) note(key string) {
	f.mu.Lock()
	f.asked[key]++
	f.mu.Unlock()
}

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	query := r.URL.Query()

	key := path
	switch {
	case query.Get("watch") == "true" || query.Get("watch") == "1":
		key += "?watch"
	case strings.Contains(r.Header.Get("Accept"), "as=Table"):
		key += "?table"
	}
	f.note(key)

	switch {
	case path == "/version":
		writeJSON(w, map[string]any{"major": "1", "minor": "31", "gitVersion": "v1.31.0"})
	case path == "/api":
		writeJSON(w, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	case path == "/apis":
		groups := []map[string]any{apiGroup("apiextensions.k8s.io")}
		for _, group := range f.groups() {
			groups = append(groups, apiGroup(group))
		}
		writeJSON(w, map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": groups})
	case path == "/api/v1":
		writeJSON(w, resourceList("v1",
			apiResource("pods", "Pod", true),
			apiResource("namespaces", "Namespace", false),
		))
	case path == "/apis/apiextensions.k8s.io/v1" && r.Method == http.MethodGet:
		writeJSON(w, resourceList("apiextensions.k8s.io/v1",
			apiResource("customresourcedefinitions", "CustomResourceDefinition", false),
		))
	case path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
		f.definitions(w, r)
	case path == "/api/v1/pods":
		f.pods(w, r)
	case strings.HasPrefix(path, "/apis/") && strings.Count(path, "/") == 3:
		// /apis/<group>/v1: whatever custom resources that group serves.
		group := strings.Split(path, "/")[2]
		var resources []map[string]any
		for _, name := range f.served {
			plural, g, _ := strings.Cut(name, ".")
			if g == group {
				resources = append(resources, apiResource(plural, titleKind(plural), true))
			}
		}
		writeJSON(w, resourceList(group+"/v1", resources...))
	default:
		http.NotFound(w, r)
	}
}

// groups are the API groups the served custom resources live in.
func (f *fakeServer) groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range f.served {
		_, group, _ := strings.Cut(name, ".")
		if !seen[group] {
			seen[group] = true
			out = append(out, group)
		}
	}
	return out
}

// definitions answers a list of the definitions, printed as a table when that
// is what was asked for.
func (f *fakeServer) definitions(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "as=Table") {
		rows := []map[string]any{}
		for _, name := range f.defined {
			rows = append(rows, map[string]any{"cells": []any{name, "2024-01-01T00:00:00Z"}})
		}
		writeJSON(w, map[string]any{
			"kind":       "Table",
			"apiVersion": "meta.k8s.io/v1",
			"columnDefinitions": []map[string]any{
				{"name": "Name", "type": "string", "format": "name"},
				{"name": "Created At", "type": "date"},
			},
			"rows": rows,
		})
		return
	}
	items := []map[string]any{}
	for _, name := range f.defined {
		plural, group, _ := strings.Cut(name, ".")
		items = append(items, map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"group": group,
				"scope": "Namespaced",
				"names": map[string]any{"plural": plural, "kind": titleKind(plural)},
			},
		})
	}
	writeJSON(w, map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinitionList",
		"metadata":   map[string]any{"resourceVersion": "1"},
		"items":      items,
	})
}

// pods answers a list of one pod, and holds a watch open until the client
// lets go of it. A watch that asks for its initial events streamed is refused,
// which is what an API server without that feature does, so the client falls
// back to listing.
func (f *fakeServer) pods(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("watch") == "true" || query.Get("watch") == "1" {
		if query.Get("sendInitialEvents") == "true" {
			http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":400,"reason":"BadRequest"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
		return
	}
	writeJSON(w, map[string]any{
		"apiVersion": "v1",
		"kind":       "PodList",
		"metadata":   map[string]any{"resourceVersion": "1"},
		"items": []map[string]any{{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]any{"name": "web", "namespace": "default", "resourceVersion": "1"},
			"status":     map[string]any{"phase": "Running"},
		}},
	})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func apiGroup(name string) map[string]any {
	version := map[string]any{"groupVersion": name + "/v1", "version": "v1"}
	return map[string]any{"name": name, "versions": []any{version}, "preferredVersion": version}
}

func resourceList(groupVersion string, resources ...map[string]any) map[string]any {
	if resources == nil {
		resources = []map[string]any{}
	}
	return map[string]any{"kind": "APIResourceList", "groupVersion": groupVersion, "resources": resources}
}

func apiResource(plural, kind string, namespaced bool) map[string]any {
	return map[string]any{
		"name":         plural,
		"singularName": "",
		"namespaced":   namespaced,
		"kind":         kind,
		"verbs":        []string{"get", "list", "watch"},
	}
}

// titleKind makes a kind out of a plural, well enough for "widgets".
func titleKind(plural string) string {
	singular := strings.TrimSuffix(plural, "s")
	return strings.ToUpper(singular[:1]) + singular[1:]
}
