package services

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// debugService is a TerminalService over a settings file in a temp directory.
func debugService(t *testing.T) (*TerminalService, *appconfig.Store) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	store, err := appconfig.Open()
	if err != nil {
		t.Fatal(err)
	}
	return NewTerminalService(nil, nil, store), store
}

func TestPlanDebugTakesWhatTheFormLeavesFromTheSettings(t *testing.T) {
	s, store := debugService(t)
	prefs := store.Get().Preferences
	prefs.Debug = appconfig.Debug{Image: "nicolaka/netshoot", Profile: "netadmin"}
	prefs.Terminal.NodeNamespace = "debugging"
	if _, err := store.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}

	plan, err := s.planDebug(kube.DebugRequest{Mode: kube.DebugNode, Name: "worker-01"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.FallbackImage != "nicolaka/netshoot" || plan.Profile != "netadmin" || plan.Namespace != "debugging" {
		t.Errorf("plan = %s/%s in %s", plan.FallbackImage, plan.Profile, plan.Namespace)
	}

	// What the form does say wins over the settings.
	plan, err = s.planDebug(kube.DebugRequest{Mode: kube.DebugEphemeral, Namespace: "shop", Name: "web", Image: "alpine", Profile: "sysadmin"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Image != "alpine" || plan.Profile != "sysadmin" {
		t.Errorf("the form's choice was overridden: %s/%s", plan.Image, plan.Profile)
	}
}

func TestDebugArgsForAnEphemeralContainer(t *testing.T) {
	plan, err := kube.DebugRequest{
		Mode: kube.DebugEphemeral, Namespace: "shop", Name: "web", Image: "busybox",
		Profile: "general", Target: "app",
	}.Plan()
	if err != nil {
		t.Fatal(err)
	}
	got := debugArgs(plan)
	want := []string{
		"debug", "web", "-it", "-n", "shop", "--profile=general", "--image=busybox",
		"--container=" + plan.Container, "--target=app",
	}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q\nwant   %q", got, want)
	}
}

func TestDebugArgsForACopy(t *testing.T) {
	plan, err := kube.DebugRequest{
		Mode: kube.DebugCopy, Namespace: "shop", Name: "web", Image: "busybox", Profile: "legacy",
		CopyTo: "web-copy", SameNode: true, SetImages: []string{"*=web:debug"}, Replace: true,
		KeepLabels: true, KeepStartup: true, KeepInitContainers: new(false), ArgumentsOnly: true,
		Env: []string{"A=1"}, ImagePullPolicy: "Never",
		Command: []string{"sh", "-l"},
	}.Plan()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(debugArgs(plan), " ")
	for _, part := range []string{
		"--copy-to=web-copy", "--share-processes=true", "--same-node", "--set-image=*=web:debug",
		"--replace", "--keep-labels", "--keep-startup", "--keep-init-containers=false",
		"--env=A=1", "--image-pull-policy=Never", "--arguments-only -- sh -l",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("%q is missing %q", got, part)
		}
	}
}

func TestDebugArgsForANode(t *testing.T) {
	plan, err := kube.DebugRequest{Mode: kube.DebugNode, Namespace: "default", Name: "worker-01", Image: "busybox", Profile: "sysadmin"}.Plan()
	if err != nil {
		t.Fatal(err)
	}
	got := debugArgs(plan)
	want := []string{"debug", "node/worker-01", "-it", "-n", "default", "--profile=sysadmin", "--image=busybox"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q\nwant   %q", got, want)
	}
}

func TestDescribeDebugSaysWhatIsMadeAndWhatHappensToIt(t *testing.T) {
	copyPlan, _ := kube.DebugRequest{Mode: kube.DebugCopy, Namespace: "shop", Name: "web"}.Plan()
	if line := describeDebug(copyPlan); !strings.Contains(line, "web-debug") || !strings.Contains(line, "deleted") {
		t.Errorf("copy: %q", line)
	}
	copyPlan.Keep = true
	if line := describeDebug(copyPlan); !strings.Contains(line, "kept") {
		t.Errorf("kept copy: %q", line)
	}

	nodePlan, _ := kube.DebugRequest{Mode: kube.DebugNode, Name: "worker-01"}.Plan()
	if line := describeDebug(nodePlan); !strings.Contains(line, "worker-01") || !strings.Contains(line, nodePlan.Pod) {
		t.Errorf("node: %q", line)
	}

	ephemeral, _ := kube.DebugRequest{Mode: kube.DebugEphemeral, Namespace: "shop", Name: "web", Target: "app"}.Plan()
	if line := describeDebug(ephemeral); !strings.Contains(line, "ephemeral") || !strings.Contains(line, "app") {
		t.Errorf("ephemeral: %q", line)
	}
}

func TestLaunchDebugIsDesktopOnly(t *testing.T) {
	s, _ := debugService(t)
	s.server = true
	if err := s.LaunchDebug("ctx", kube.DebugRequest{}); !errors.Is(err, errDesktopOnly) {
		t.Errorf("err = %v, want errDesktopOnly", err)
	}
}

func TestDebugArgsWithoutATerminalDoNotAttach(t *testing.T) {
	plan, err := kube.DebugRequest{Mode: kube.DebugNode, Name: "worker-01", Interactive: new(false)}.Plan()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(debugArgs(plan), " ")
	if strings.Contains(got, "-it") || !strings.Contains(got, "--attach=false") {
		t.Errorf("args = %q", got)
	}
}

func TestDebugArgsKeepAnExistingContainersImage(t *testing.T) {
	plan, err := kube.DebugRequest{Mode: kube.DebugCopy, Namespace: "shop", Name: "web", Container: "app", Command: []string{"sh"}}.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(debugArgs(plan), " "); strings.Contains(got, "--image") {
		t.Errorf("args = %q, want no image so the container keeps its own", got)
	}
}
