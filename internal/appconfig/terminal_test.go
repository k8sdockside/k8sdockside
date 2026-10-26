package appconfig

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestTerminalDefaultsAreSomethingThatWorks(t *testing.T) {
	got := openIn(t).Get().Preferences.Terminal

	// The built-in terminal is the answer that always works: it needs nothing
	// installed on this machine.
	if got.Mode != TerminalInApp {
		t.Errorf("mode = %q, want %q", got.Mode, TerminalInApp)
	}
	if !slices.Equal(got.Shells, []string{"bash", "sh"}) {
		t.Errorf("shells = %v, want bash then sh", got.Shells)
	}
	if got.NodeImage != DefaultNodeImage || got.NodeNamespace != DefaultNodeNamespace {
		t.Errorf("the node shell would use %s in %s", got.NodeImage, got.NodeNamespace)
	}
	if got.FontSize != DefaultTermFontSize || got.Scrollback != DefaultScrollback {
		t.Errorf("appearance = %d/%d, want %d/%d", got.FontSize, got.Scrollback, DefaultTermFontSize, DefaultScrollback)
	}
}

func TestTerminalSettingsSurviveAReopen(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	prefs := store.Get().Preferences
	prefs.Terminal = Terminal{
		Mode:          TerminalExternal,
		External:      "kitty",
		Shells:        []string{"zsh", "bash", "sh"},
		NodeImage:     "registry.internal/busybox:1.36",
		NodeNamespace: "kube-system",
		FontSize:      14,
		Scrollback:    20000,
		LocalShell:    " /opt/homebrew/bin/fish ",
	}
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}

	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	got := reopened.Get().Preferences.Terminal

	if got.Mode != TerminalExternal || got.External != "kitty" {
		t.Errorf("the choice of terminal came back as %q/%q", got.Mode, got.External)
	}
	if !slices.Equal(got.Shells, []string{"zsh", "bash", "sh"}) {
		t.Errorf("shells came back as %v", got.Shells)
	}
	if got.NodeImage != "registry.internal/busybox:1.36" || got.NodeNamespace != "kube-system" {
		t.Errorf("the node shell came back as %s in %s", got.NodeImage, got.NodeNamespace)
	}
	if got.FontSize != 14 || got.Scrollback != 20000 {
		t.Errorf("appearance came back as %d/%d", got.FontSize, got.Scrollback)
	}
	if got.LocalShell != "/opt/homebrew/bin/fish" {
		t.Errorf("the local shell came back as %q, want it kept and trimmed", got.LocalShell)
	}
}

func TestTheLocalShellDefaultsToTheMachinesOwn(t *testing.T) {
	if got := openIn(t).Get().Preferences.Terminal.LocalShell; got != "" {
		t.Errorf("local shell = %q, want empty: the default is this machine's", got)
	}
}

func TestAnOlderFileGainsAWorkingTerminal(t *testing.T) {
	// A settings file written before this feature existed has no terminal block
	// at all. Somebody upgrading into it should find a terminal that opens in
	// the dock and tries bash then sh, not one that cannot open.
	path := tempSettings(t)
	if err := os.WriteFile(path, []byte(`{"preferences":{"theme":"k8sdockside-dark"}}`), 0o600); err != nil {
		t.Fatalf("writing an old file: %v", err)
	}

	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got := store.Get().Preferences.Terminal
	if got.Mode != TerminalInApp || len(got.Shells) == 0 || got.NodeImage == "" {
		t.Fatalf("an older file produced %+v", got)
	}
}

func TestAHandEditedTerminalIsRepaired(t *testing.T) {
	path := tempSettings(t)
	raw := `{"preferences":{"terminal":{
		"mode":"telepathy",
		"external":"kitty",
		"shells":["  ","bash","bash"," zsh "],
		"nodeImage":"  ",
		"nodeNamespace":"",
		"fontSize":900,
		"scrollback":1
	}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := store.Get().Preferences.Terminal

	if got.Mode != TerminalInApp {
		t.Errorf("an unknown mode became %q, want the default", got.Mode)
	}
	// Trimmed, and each shell kept once: a list with the same shell twice would
	// try it twice and report the second failure.
	if !slices.Equal(got.Shells, []string{"bash", "zsh"}) {
		t.Errorf("shells = %v, want bash then zsh", got.Shells)
	}
	if got.NodeImage != DefaultNodeImage || got.NodeNamespace != DefaultNodeNamespace {
		t.Errorf("the node shell was left unusable: %s in %s", got.NodeImage, got.NodeNamespace)
	}
	if got.FontSize != DefaultTermFontSize || got.Scrollback != DefaultScrollback {
		t.Errorf("appearance was left out of bounds: %d/%d", got.FontSize, got.Scrollback)
	}
	// The one field deliberately left alone: it names a terminal that may not
	// be installed *here*, and blanking it would lose the choice on every sync
	// between two machines.
	if got.External != "kitty" {
		t.Errorf("external = %q, want it kept as written", got.External)
	}
}

func TestAnEmptyShellListIsRefusedRatherThanSaved(t *testing.T) {
	store := openIn(t)
	prefs := store.Get().Preferences
	prefs.Terminal.Shells = []string{}

	got, err := store.SetPreferences(prefs)
	if err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}
	// A terminal with nothing to try is a shell that can never open, which is
	// not a choice anybody makes on purpose.
	if len(got.Preferences.Terminal.Shells) == 0 {
		t.Fatal("an empty shell list was saved as it was")
	}
}

func TestPortForwardsRoundTripAsRequestsRatherThanConnections(t *testing.T) {
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	saved := []PortForward{
		{ID: "pf-1", ContextID: "prod", Kind: "services", Namespace: "web", Name: "api", RemotePort: 80, LocalPort: 51234, Random: true, Browser: true},
		{ID: "pf-2", ContextID: "prod", Kind: "pods", Namespace: "web", Name: "api-7f9", RemotePort: 9090, LocalPort: 9090},
	}
	if _, err := store.SetPortForwards(saved); err != nil {
		t.Fatalf("SetPortForwards: %v", err)
	}

	reopened, err := openAt(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	got := reopened.PortForwards()

	if len(got) != 2 {
		t.Fatalf("%d forwards came back, want 2: %+v", len(got), got)
	}
	// The port a random forward came up on is remembered, so a bookmark still
	// works after a restart -- see PortForward.LocalPort.
	if got[0].LocalPort != 51234 || !got[0].Random {
		t.Errorf("the random forward came back as %+v", got[0])
	}
	if got[1].ID != "pf-2" || got[1].RemotePort != 9090 {
		t.Errorf("the second forward came back as %+v", got[1])
	}
}

func TestPortForwardsAreAListRatherThanNull(t *testing.T) {
	// Serialised straight to JSON, where nil would become null and break the
	// frontend's array handling.
	path := tempSettings(t)
	store, err := openAt(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.SetLayout(store.Get().Layout); err != nil {
		t.Fatalf("SetLayout: %v", err)
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- a file this test just wrote
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if !strings.Contains(string(raw), `"portForwards": []`) {
		t.Fatalf("the file does not hold an empty list:\n%s", raw)
	}
	if store.PortForwards() == nil {
		t.Fatal("PortForwards() returned nil rather than an empty list")
	}
}
