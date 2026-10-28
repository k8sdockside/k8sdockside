package services

import (
	"runtime"
	"testing"
)

func TestMacBundleIsTheAppAroundTheExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		if got := macBundle("/Applications/K8s Dockside.app/Contents/MacOS/k8sdockside"); got != "" {
			t.Errorf("macBundle off macOS = %q, want empty", got)
		}
		return
	}
	cases := map[string]string{
		"/Applications/K8s Dockside.app/Contents/MacOS/k8sdockside": "/Applications/K8s Dockside.app",
		// A development build runs from bin/, not from a bundle.
		"/Users/me/k8sdockside/bin/k8sdockside": "",
	}
	for exe, want := range cases {
		if got := macBundle(exe); got != want {
			t.Errorf("macBundle(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestOpenProfileIsDesktopOnly(t *testing.T) {
	if err := (&SettingsService{server: true}).OpenProfile("dev", false); err == nil {
		t.Error("the web version started a profile")
	}
}
