package services

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogerwesterbo/k8sdockside/internal/plugins"
	"github.com/rogerwesterbo/k8sdockside/internal/registry"
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
