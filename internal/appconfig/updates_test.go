package appconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/themes"
)

// The default is on, so a file written before the field existed must not read
// as "off": nil is the answer for "never chosen", and it resolves to yes.
func TestCheckForUpdatesIsOnUntilTheUserChooses(t *testing.T) {
	path := tempSettings(t)
	if err := os.WriteFile(path, []byte(`{"preferences":{"theme":"dockside"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if store.Get().Preferences.CheckForUpdates != nil {
		t.Error("CheckForUpdates should stay nil until chosen, so the frontend can default it to true")
	}
	if !store.CheckForUpdates() {
		t.Error("an older file switched the check off")
	}
}

func TestSwitchingUpdateChecksOffSurvivesAReopen(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	prefs := store.Get().Preferences
	off := false
	prefs.CheckForUpdates = &off
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}
	if store.CheckForUpdates() {
		t.Error("the check is still on after being switched off")
	}

	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.CheckForUpdates() {
		t.Error("an explicit no came back as yes after a reopen")
	}
}

// The same guarantee RestoreTabs has: neither the caller's pointer nor a
// returned one may reach into the store.
func TestCheckForUpdatesCopiesThePointerField(t *testing.T) {
	store := openIn(t)

	on := true
	if _, err := store.SetPreferences(Preferences{
		Theme:           themes.DefaultID,
		Density:         DensityComfortable,
		CheckForUpdates: &on,
	}); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}

	on = false
	if !store.CheckForUpdates() {
		t.Error("mutating the caller's pointer changed what the store holds")
	}

	handed := store.Get().Preferences
	*handed.CheckForUpdates = false
	if !store.CheckForUpdates() {
		t.Error("writing through a returned pointer changed what the store holds")
	}
}

// The point of persisting it: a notice marked as read stays read after a
// restart, rather than ringing the bell again every morning.
func TestMarkUpdateReadSurvivesAReopen(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	saved, err := store.MarkUpdateRead(" v1.2.0\n")
	if err != nil {
		t.Fatalf("MarkUpdateRead: %v", err)
	}
	if saved.Updates.ReadVersion != "v1.2.0" {
		t.Errorf("readVersion = %q, want it tidied to v1.2.0", saved.Updates.ReadVersion)
	}

	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Get().Updates.ReadVersion; got != "v1.2.0" {
		t.Errorf("readVersion = %q after reopen", got)
	}
}

// Nothing read yet is the state every settings file starts in, and it should
// not be spelled out in every one of them.
func TestAnUntouchedUpdatesBlockWritesNothingToTheFile(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.AddManualFile("/home/u/.kube/config"); err != nil {
		t.Fatalf("AddManualFile: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), `"updates"`) {
		t.Errorf("an untouched updates block was written to the file:\n%s", written)
	}
}
