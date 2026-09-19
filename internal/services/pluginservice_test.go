package services

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/registry"
)

// After a clone, "installed" followed by nothing appearing is the outcome that
// leaves the user with nowhere to look, so what became of the folder is said.
func TestInstalledSaysWhatBecameOfAClone(t *testing.T) {
	dest := filepath.Join("/plugins", "k8sdockside-metallb")

	loaded := plugins.Catalogue{Plugins: []plugins.Plugin{
		{ID: "argocd", Origin: plugins.BuiltinOrigin},
		{ID: "metallb", Origin: filepath.Join(dest, "plugin.json")},
	}}
	if err := installed(loaded, dest); err != nil {
		t.Errorf("a clone whose plugin loaded reported %v", err)
	}

	refused := plugins.Catalogue{
		Plugins: []plugins.Plugin{{ID: "argocd", Origin: plugins.BuiltinOrigin}},
		Problems: []plugins.Problem{
			{Path: filepath.Join(dest, "plugin.json"), Message: `plugin "metallb" needs K8s Dockside 0.0.15 or newer`},
			// Another folder's trouble is not this clone's.
			{Path: filepath.Join("/plugins", "k8sdockside-metallb-old", "plugin.json"), Message: "unrelated"},
		},
	}
	err := installed(refused, dest)
	if err == nil || !strings.Contains(err.Error(), "would not load") || !strings.Contains(err.Error(), "0.0.15") {
		t.Errorf("a clone that would not load reported %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "unrelated") {
		t.Errorf("a problem in a neighbouring folder was blamed on this clone: %v", err)
	}

	if err := installed(plugins.Catalogue{}, dest); err == nil || !strings.Contains(err.Error(), "no plugin.json") {
		t.Errorf("a clone with nothing in it reported %v", err)
	}
}

// A view may ask a registry only about what the pods run, so what they run is
// read from every kind of container, and a reference is found however it is
// spelled.
func TestImagesInPods(t *testing.T) {
	pod := func(field string, images ...string) map[string]any {
		containers := []any{}
		for _, image := range images {
			containers = append(containers, map[string]any{"name": "c", "image": image})
		}
		return map[string]any{"spec": map[string]any{field: containers}}
	}
	got := imagesIn([]map[string]any{
		pod("containers", "nginx:1.27", "not an image"),
		pod("initContainers", "ghcr.io/org/init@sha256:"+strings.Repeat("a", 64)),
		pod("ephemeralContainers", "busybox"),
		{"spec": map[string]any{}},
	})

	for _, image := range []string{"docker.io/library/nginx:1.27", "docker.io/library/nginx", "ghcr.io/org/init", "docker.io/library/busybox:latest"} {
		if !got[image] {
			t.Errorf("%s is not among %v", image, got)
		}
	}
	for _, image := range []string{"docker.io/library/nginx:1.28", "ghcr.io/org/init:latest"} {
		if got[image] {
			t.Errorf("%s is among what runs", image)
		}
	}
	for _, image := range []string{"nginx:1.27", "index.docker.io/library/nginx:1.27", "busybox"} {
		ref, err := registry.Parse(image)
		if err != nil || !got[ref.Tagged()] {
			t.Errorf("%s was not found as %s (%v)", image, ref.Tagged(), err)
		}
	}
}

// A service found by a selector is the first one with the declared port, which
// may be named or given by number.
func TestPickServiceTakesTheFirstWithThePort(t *testing.T) {
	candidates := []kube.ServiceFound{
		{Namespace: "a", Name: "no-port", Ports: []kube.ServicePort{{Name: "metrics", Number: 9090}}},
		{Namespace: "b", Name: "whisker", Ports: []kube.ServicePort{{Name: "http", Number: 8081}}},
		{Namespace: "c", Name: "whisker", Ports: []kube.ServicePort{{Name: "http", Number: 8081}}},
	}
	for port, want := range map[plugins.ServicePort]string{"http": "b/whisker:http", "8081": "b/whisker:8081", "9090": "a/no-port:9090", "grpc": ""} {
		svc := plugins.UIService{ID: "w", Selector: "k8s-app=whisker", Port: port, Scheme: plugins.SchemeHTTP}
		got, ok := pickService(candidates, svc)
		if want == "" {
			if ok {
				t.Errorf("port %s: picked %s, want nothing", port, got.Describe())
			}
			continue
		}
		if !ok || got.Describe() != want || got.Scheme != plugins.SchemeHTTP {
			t.Errorf("port %s: picked %+v (%v), want %s", port, got, ok, want)
		}
	}
}

func TestServiceQueryIsBounded(t *testing.T) {
	got, err := serviceQuery(map[string][]string{"filter": {"a", "b"}, "limit": {"50"}})
	if err != nil || got.Encode() != "filter=a&filter=b&limit=50" {
		t.Errorf("encoded %q (%v)", got.Encode(), err)
	}
	if _, err := serviceQuery(map[string][]string{"": {"x"}}); err == nil {
		t.Error("a parameter without a name was taken")
	}
	many := map[string][]string{}
	for i := range maxQueryKeys + 1 {
		many[strconv.Itoa(i)] = []string{"x"}
	}
	if _, err := serviceQuery(many); err == nil {
		t.Error("too many parameters were taken")
	}
	if _, err := serviceQuery(map[string][]string{"q": {strings.Repeat("x", maxQueryBytes)}}); err == nil {
		t.Error("an oversized query was taken")
	}
}

// A view gets only what its plugin declares: the service by its id, and a path
// under one of its prefixes. Each refusal comes before the cluster is asked.
func TestServiceGetRefusesWhatIsNotDeclared(t *testing.T) {
	flows := plugins.Plugin{
		ID:   "flows",
		Name: "Flows",
		UI: &plugins.UI{Services: []plugins.UIService{
			{ID: "whisker", Label: "Whisker", Selector: "k8s-app=whisker", Port: "8081", Scheme: plugins.SchemeHTTP, Paths: []string{"/whisker-backend/"}},
		}},
	}
	off := plugins.Plugin{ID: "off", Name: "Off", Disabled: true, UI: flows.UI}
	s := &PluginService{cached: &plugins.Catalogue{Plugins: []plugins.Plugin{flows, off}}, found: map[string]foundService{}}

	for name, tc := range map[string]struct {
		plugin, service, path string
		query                 map[string][]string
		want                  string
	}{
		"another plugin":   {"acme", "whisker", "/whisker-backend/flows", nil, `no plugin called "acme"`},
		"switched off":     {"off", "whisker", "/whisker-backend/flows", nil, "switched off"},
		"undeclared":       {"flows", "prometheus", "/api/v1/query", nil, `does not declare a service "prometheus"`},
		"outside the path": {"flows", "whisker", "/admin", nil, `may not request "/admin" from Whisker`},
		"climbing out":     {"flows", "whisker", "/whisker-backend/../admin", nil, "may not request"},
		"a nameless query": {"flows", "whisker", "/whisker-backend/flows", map[string][]string{"": {"x"}}, "no name"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.ServiceGet(context.Background(), "ctx", tc.plugin, tc.service, tc.path, tc.query)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want one saying %q", err, tc.want)
			}
		})
	}
}
