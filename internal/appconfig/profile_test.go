package appconfig

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProfileFromFlagThenEnvironment(t *testing.T) {
	env := func(v string) func(string) string {
		return func(string) string { return v }
	}
	cases := []struct {
		args []string
		env  string
		want string
	}{
		{nil, "", ""},
		{nil, "dev", "dev"},
		{[]string{"--profile", "dev"}, "", "dev"},
		{[]string{"--profile=dev"}, "other", "dev"},
		// macOS hands an app arguments of its own; they are not ours to refuse.
		{[]string{"-psn_0_12345", "-profile=work"}, "", "work"},
		{[]string{"--profile"}, "fallback", "fallback"},
	}
	for _, c := range cases {
		if got := ProfileFrom(c.args, env(c.env)); got != c.want {
			t.Errorf("ProfileFrom(%q, %q) = %q, want %q", c.args, c.env, got, c.want)
		}
	}
}

// A profile is a directory of its own, and a name that could leave the
// profiles folder is not one.
func TestOpenProfileRefusesANameThatIsNotADirectoryName(t *testing.T) {
	sandboxHome(t)
	for _, name := range []string{"../escape", "Dev", "a/b", ".hidden"} {
		if _, err := OpenProfile(name); err == nil {
			t.Errorf("OpenProfile(%q) succeeded", name)
		}
	}
}

// First use starts from the default settings; after that the two are separate
// and the default file is never written.
func TestOpenProfileStartsFromACopyAndKeepsItsOwn(t *testing.T) {
	sandboxHome(t)
	stable, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stable.AddPluginFolder("/stable/plugins"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(stable.Path())
	if err != nil {
		t.Fatal(err)
	}

	dev, err := OpenProfile("dev")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Profile() != "dev" {
		t.Errorf("Profile() = %q", dev.Profile())
	}
	if want := filepath.Join(filepath.Dir(stable.Path()), "profiles", "dev", settingsFile); dev.Path() != want {
		t.Errorf("Path() = %q, want %q", dev.Path(), want)
	}
	if dev.PluginsDir() == stable.PluginsDir() {
		t.Error("a profile shares the default plugins folder")
	}
	if !slices.Contains(dev.PluginFolders(), "/stable/plugins") {
		t.Errorf("the profile did not start from the default settings: %v", dev.PluginFolders())
	}

	if _, err := dev.RemovePluginFolder("/stable/plugins"); err != nil {
		t.Fatal(err)
	}
	if _, err := dev.AddPluginFolder("/work/plugins"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(stable.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("changing the profile wrote the default settings file")
	}

	again, err := OpenProfile("dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.PluginFolders(); !slices.Equal(got, []string{"/work/plugins"}) {
		t.Errorf("reopened profile has %v, want its own folders", got)
	}
}

func TestProfilesListsTheFoldersThatAreProfiles(t *testing.T) {
	sandboxHome(t)
	if got, err := Profiles(); err != nil || len(got) != 0 {
		t.Fatalf("Profiles() before any = %v, %v", got, err)
	}
	stable, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stable.AddPluginFolder("/x"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work", "dev"} {
		if _, err := OpenProfile(name); err != nil {
			t.Fatal(err)
		}
	}
	// Something else in the folder is not a profile.
	if err := os.MkdirAll(filepath.Join(filepath.Dir(stable.Path()), "profiles", "Not A Profile"), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"dev", "work"}) {
		t.Errorf("Profiles() = %v, want [dev work]", got)
	}
}
