package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlugin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestASoundPluginPasses(t *testing.T) {
	dir := writePlugin(t, `{"id": "acme", "version": "1.0.0", "minAppVersion": "0.0.15", "views": [{"id": "pods", "kind": "pods"}]}`)
	var out bytes.Buffer
	if code := run([]string{dir}, &out, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	for _, want := range []string{"ok  acme 1.0.0", "1 view", "needs K8s Dockside 0.0.15 or newer"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// What a plugin's pages can reach beyond the cluster's objects is said, so a
// reviewer reads it before anyone installs the plugin.
func TestWhatThePagesReachIsSaid(t *testing.T) {
	dir := writePlugin(t, `{"id": "flows", "minAppVersion": "0.0.27",
		"ui": {"registries": true, "services": [{"id": "whisker", "label": "Whisker", "namespace": "calico-system", "name": "whisker", "port": 8081, "paths": ["/whisker-backend/flows"]}]},
		"views": [{"id": "pods", "kind": "pods"}]}`)
	if err := os.Mkdir(filepath.Join(dir, "ui"), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run([]string{dir}, &out, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	for _, want := range []string{
		"its pages read pods",
		"its pages ask registries",
		"its pages call Whisker (calico-system/whisker:8081) at /whisker-backend/flows",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestABrokenPluginFailsWithEveryReason(t *testing.T) {
	dir := writePlugin(t, `{"id": "acme", "icon": "rocketship", "views": [{"id": "pods", "kind": "widgets"}]}`)
	var out bytes.Buffer
	if code := run([]string{dir}, &out, &out); code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out.String())
	}
	for _, want := range []string{"FAIL", `"rocketship"`, `"widgets"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestTheAppVersionCanBeChosen(t *testing.T) {
	dir := writePlugin(t, `{"id": "acme", "minAppVersion": "0.0.15", "views": [{"id": "pods", "kind": "pods"}]}`)
	var out bytes.Buffer
	if code := run([]string{"-app", "0.0.14", dir}, &out, &out); code != 1 {
		t.Fatalf("exit %d, want 1 for an app older than the plugin asks for:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "needs K8s Dockside 0.0.15 or newer, and this is 0.0.14") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestAFolderWithNoPluginFails(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{t.TempDir()}, &out, &out); code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out.String())
	}
}
