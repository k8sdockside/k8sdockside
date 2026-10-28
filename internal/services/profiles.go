package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Profiles is what the profile switcher shows: the one this window runs on,
// empty for the default settings, and every profile there is.
type Profiles struct {
	Current  string   `json:"current"`
	Profiles []string `json:"profiles"`
}

// Profiles lists the settings profiles. See appconfig.OpenProfile.
func (s *SettingsService) Profiles() (Profiles, error) {
	list, err := appconfig.Profiles()
	if err != nil {
		return Profiles{Current: s.store.Profile(), Profiles: []string{}}, err
	}
	return Profiles{Current: s.store.Profile(), Profiles: list}, nil
}

// OpenProfile starts another copy of the app on a profile -- empty for the
// default settings, a new name for a profile that does not exist yet. With
// replace, this copy then quits, which is what switching is; without, the two
// run side by side, each on its own settings.
//
// A profile is chosen when the app starts and cannot change under a running
// one: the plugins, the themes and every service read their settings once.
// So a switch is a relaunch, and the new copy is started before this one goes
// so that a launch that fails leaves the user where they were.
func (s *SettingsService) OpenProfile(name string, replace bool) error {
	if s.server {
		return errDesktopOnly
	}
	name = strings.TrimSpace(name)
	if name != "" {
		if err := appconfig.ValidProfile(name); err != nil {
			return err
		}
	}
	if replace && name == s.store.Profile() {
		return nil
	}
	if err := launchProfile(name); err != nil {
		return fmt.Errorf("starting K8s Dockside on %s: %w", profileLabel(name), err)
	}
	if replace {
		// A moment for the new window to appear before this one goes, so the
		// screen is never empty in between. Quitting saves this window's
		// place and layout as it always does.
		go func() {
			time.Sleep(700 * time.Millisecond)
			if app := application.Get(); app != nil {
				app.Quit()
			}
		}()
	}
	return nil
}

func profileLabel(name string) string {
	if name == "" {
		return "the default profile"
	}
	return "profile " + name
}

// launchProfile starts this program again on a profile, detached from this
// one. The profile goes on the command line, which wins over the environment
// this process passes on -- "--profile=" included, which is the default.
//
// An installed macOS app is started through `open -n`, so the new copy is an
// application of its own -- a Dock icon, its own menu -- rather than a child
// process of this one; anything else, a development build included, runs the
// executable itself.
func launchProfile(name string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	flag := "--profile=" + name

	var cmd *exec.Cmd
	if bundle := macBundle(exe); bundle != "" {
		cmd = exec.Command("open", "-n", "-a", bundle, "--args", flag) // #nosec G204 -- our own bundle, and a name ValidProfile has checked
	} else {
		cmd = exec.Command(exe, flag) // #nosec G204 -- our own executable, and a name ValidProfile has checked
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	// Not waited on: the new copy outlives this one when switching.
	return cmd.Process.Release()
}

// macBundle is the .app an executable is inside, or empty when it is not in
// one -- not on macOS, or a development build run from bin/.
func macBundle(exe string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	const marker = ".app/Contents/MacOS/"
	i := strings.LastIndex(exe, marker)
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}
