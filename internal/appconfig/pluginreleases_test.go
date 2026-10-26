package appconfig

import (
	"os"
	"testing"
)

// On until chosen, like the update check, and an explicit no survives a reopen.
func TestPluginReleasesIsOnUntilSwitchedOff(t *testing.T) {
	path := tempSettings(t)
	if err := os.WriteFile(path, []byte(`{"preferences":{"theme":"dockside"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if store.Get().Preferences.PluginReleases != nil || !store.PluginReleases() {
		t.Fatal("an older file switched release lists off")
	}

	prefs := store.Get().Preferences
	off := false
	prefs.PluginReleases = &off
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}
	off = true
	if store.PluginReleases() {
		t.Error("still on after being switched off, or the caller's pointer reached the store")
	}
	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.PluginReleases() {
		t.Error("an explicit no came back as yes after a reopen")
	}
}
