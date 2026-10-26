package appconfig

import (
	"slices"
	"testing"
)

func TestDebugDefaultsAreWhatKubectlWouldUse(t *testing.T) {
	got := openIn(t).Get().Preferences.Debug

	if got.Image != DefaultDebugImage {
		t.Errorf("image = %q, want %q", got.Image, DefaultDebugImage)
	}
	if got.Profile != DefaultDebugProfile {
		t.Errorf("profile = %q, want %q", got.Profile, DefaultDebugProfile)
	}
	// Empty rather than nil, so the frontend reads a list and not null.
	if got.Images == nil || len(got.Images) != 0 {
		t.Errorf("images = %#v, want an empty list", got.Images)
	}
}

func TestDebugSettingsSurviveAReopen(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	prefs := store.Get().Preferences
	prefs.Debug = Debug{
		Image:   "registry.internal/toolbox:2",
		Images:  []string{"registry.internal/toolbox:2", "ghcr.io/example/debug"},
		Profile: "netadmin",
	}
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}

	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	got := reopened.Get().Preferences.Debug
	if got.Image != "registry.internal/toolbox:2" || got.Profile != "netadmin" {
		t.Errorf("the choice came back as %q/%q", got.Image, got.Profile)
	}
	if !slices.Equal(got.Images, []string{"registry.internal/toolbox:2", "ghcr.io/example/debug"}) {
		t.Errorf("images came back as %v", got.Images)
	}
}

func TestNormaliseDebugTidiesWhatAHandEditedFileSays(t *testing.T) {
	got := normaliseDebug(Debug{
		Image:   "  ",
		Images:  []string{" ghcr.io/a ", "", "ghcr.io/a", "busybox", "nicolaka/netshoot", "ghcr.io/b"},
		Profile: "root-please",
	})

	if got.Image != DefaultDebugImage {
		t.Errorf("an empty image became %q, want the default", got.Image)
	}
	// Trimmed, each once, and no preset: those are offered anyway.
	if !slices.Equal(got.Images, []string{"ghcr.io/a", "ghcr.io/b"}) {
		t.Errorf("images = %v", got.Images)
	}
	if got.Profile != DefaultDebugProfile {
		t.Errorf("an unknown profile was kept as %q", got.Profile)
	}
}

func TestNormaliseDebugKeepsEveryProfileKubectlKnows(t *testing.T) {
	for _, profile := range DebugProfiles {
		if got := normaliseDebug(Debug{Profile: profile}).Profile; got != profile {
			t.Errorf("profile %q became %q", profile, got)
		}
	}
}

func TestDebugPresetsAreACopy(t *testing.T) {
	presets := DebugPresets()
	if len(presets) == 0 {
		t.Fatal("no presets")
	}
	presets[0].Image = "changed"
	if DebugPresets()[0].Image == "changed" {
		t.Error("a caller could change the presets for everyone")
	}
	for _, preset := range DebugPresets() {
		if preset.Image == "" || preset.Label == "" {
			t.Errorf("preset %+v says nothing about itself", preset)
		}
	}
}

func TestCloneDoesNotShareTheDebugImages(t *testing.T) {
	s := Defaults()
	s.Preferences.Debug.Images = []string{"ghcr.io/a"}
	out := clone(s)
	out.Preferences.Debug.Images[0] = "changed"
	if s.Preferences.Debug.Images[0] != "ghcr.io/a" {
		t.Error("the clone shares its debug images with the original")
	}
}
