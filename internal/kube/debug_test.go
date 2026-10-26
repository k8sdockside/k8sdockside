package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"
)

func runningPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "web-7d9f",
			Namespace:   "shop",
			Labels:      map[string]string{"app": "web"},
			Annotations: map[string]string{"owner": "team-a"},
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-01",
			Containers: []corev1.Container{
				{
					Name:           "app",
					Image:          "shop/web:1.2",
					LivenessProbe:  &corev1.Probe{},
					ReadinessProbe: &corev1.Probe{},
					StartupProbe:   &corev1.Probe{},
				},
				{Name: "sidecar", Image: "envoy:1"},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func mustPlan(t *testing.T, req DebugRequest) DebugPlan {
	t.Helper()
	plan, err := req.Plan()
	if err != nil {
		t.Fatalf("Plan(%+v): %v", req, err)
	}
	return plan
}

func TestPlanFillsInWhatTheFormLeftOut(t *testing.T) {
	plan := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web-7d9f"})

	// The image is left for the container to decide -- a copy's existing
	// container keeps its own -- with the default to fall back on.
	if plan.Image != "" || plan.FallbackImage != DefaultDebugImage || plan.Profile != DefaultDebugProfile {
		t.Errorf("defaults = %q/%q/%q", plan.Image, plan.FallbackImage, plan.Profile)
	}
	if !plan.Attaches() {
		t.Error("a request that does not say is not interactive")
	}
	if plan.Pod != "web-7d9f" {
		t.Errorf("an ephemeral container attaches to %q, want the pod itself", plan.Pod)
	}
	if !strings.HasPrefix(plan.Container, "debugger-") || len(plan.Container) != len("debugger-")+5 {
		t.Errorf("container is named %q", plan.Container)
	}
	if plan.Creates() {
		t.Error("an ephemeral container is reported as creating a pod")
	}

	other := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web-7d9f"})
	if other.Container == plan.Container {
		t.Error("two sessions got the same container name")
	}
}

func TestPlanRefusesWhatCannotWork(t *testing.T) {
	for name, req := range map[string]DebugRequest{
		"unknown mode":       {Mode: "sideways", Namespace: "shop", Name: "web"},
		"unknown profile":    {Mode: DebugEphemeral, Namespace: "shop", Name: "web", Profile: "root"},
		"no name":            {Mode: DebugEphemeral, Namespace: "shop"},
		"pod no namespace":   {Mode: DebugEphemeral, Name: "web"},
		"copy onto itself":   {Mode: DebugCopy, Namespace: "shop", Name: "web", CopyTo: "web"},
		"copy invalid name":  {Mode: DebugCopy, Namespace: "shop", Name: "web", CopyTo: "Not_A_Name"},
		"profile wrong case": {Mode: DebugNode, Name: "worker-01", Profile: "General"},
		"bad pull policy":    {Mode: DebugNode, Name: "worker-01", ImagePullPolicy: "Sometimes"},
		"env without equals": {Mode: DebugNode, Name: "worker-01", Env: []string{"DEBUG"}},
		"env without key":    {Mode: DebugNode, Name: "worker-01", Env: []string{"=1"}},
		"bad container name": {Mode: DebugNode, Name: "worker-01", Container: "Not_A_Name"},
		"custom not a spec":  {Mode: DebugNode, Name: "worker-01", Custom: "image: [unclosed"},
		"custom wrong shape": {Mode: DebugNode, Name: "worker-01", Custom: `{"env": "not a list"}`},
		"set-image no image": {Mode: DebugCopy, Namespace: "shop", Name: "web", SetImages: []string{"app="}},
		"set-image no pair":  {Mode: DebugCopy, Namespace: "shop", Name: "web", SetImages: []string{"busybox"}},
		"replace onto self":  {Mode: DebugCopy, Namespace: "shop", Name: "web", CopyTo: "web", Replace: true},
	} {
		if _, err := req.Plan(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPlanNamesTheCopyAndTheNodePod(t *testing.T) {
	copied := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web-7d9f", Keep: true})
	if copied.Pod != "web-7d9f-debug" || copied.CopyTo != "web-7d9f-debug" {
		t.Errorf("the copy is named %q/%q", copied.Pod, copied.CopyTo)
	}
	if !copied.Creates() || !copied.Keep {
		t.Error("a kept copy is not reported as one")
	}

	node := mustPlan(t, DebugRequest{Mode: DebugNode, Name: "worker-01"})
	if node.Namespace != DefaultNodeShellNamespace {
		t.Errorf("node pod namespace = %q", node.Namespace)
	}
	if !strings.HasPrefix(node.Pod, "k8sdockside-debug-worker-01-") {
		t.Errorf("node pod is named %q", node.Pod)
	}

	ephemeral := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web", Keep: true})
	if ephemeral.Keep {
		t.Error("an ephemeral session claims to keep something it never made")
	}

	named := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web", Container: "probe"})
	if named.Container != "probe" {
		t.Errorf("-c was ignored: %q", named.Container)
	}

	// Nothing attaches, so deleting the pod the moment it exists would be
	// pointless: it is kept.
	no := false
	detached := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web", Interactive: &no})
	if detached.Attaches() || !detached.Keep {
		t.Errorf("a detached copy: attaches=%v keep=%v", detached.Attaches(), detached.Keep)
	}
}

func TestNodeDebugPodNameFitsAPodName(t *testing.T) {
	long := strings.Repeat("a", 300) + ".example.com"
	name := nodeDebugPodName(long, "abcde")
	if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
		t.Errorf("%q is not a pod name: %v", name, problems)
	}
}

func TestProfileSecurityMatchesKubectl(t *testing.T) {
	if profileSecurity(ProfileLegacy) != nil || profileSecurity(ProfileBaseline) != nil {
		t.Error("legacy and baseline should leave the container's security context alone")
	}

	general := profileSecurity(ProfileGeneral)
	if general == nil || len(general.Capabilities.Add) != 1 || general.Capabilities.Add[0] != "SYS_PTRACE" {
		t.Errorf("general = %+v, want SYS_PTRACE", general)
	}

	netadmin := profileSecurity(ProfileNetadmin)
	if netadmin == nil || len(netadmin.Capabilities.Add) != 2 {
		t.Errorf("netadmin = %+v, want NET_ADMIN and NET_RAW", netadmin)
	}

	sysadmin := profileSecurity(ProfileSysadmin)
	if sysadmin == nil || sysadmin.Privileged == nil || !*sysadmin.Privileged {
		t.Errorf("sysadmin = %+v, want privileged", sysadmin)
	}

	restricted := profileSecurity(ProfileRestricted)
	if restricted == nil || restricted.RunAsNonRoot == nil || !*restricted.RunAsNonRoot ||
		restricted.AllowPrivilegeEscalation == nil || *restricted.AllowPrivilegeEscalation ||
		len(restricted.Capabilities.Drop) != 1 || restricted.Capabilities.Drop[0] != "ALL" ||
		restricted.SeccompProfile == nil || restricted.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("restricted = %+v", restricted)
	}
}

func TestEphemeralContainerTargetsAndAttaches(t *testing.T) {
	plan := mustPlan(t, DebugRequest{
		Mode: DebugEphemeral, Namespace: "shop", Name: "web", Image: "nicolaka/netshoot",
		Target: "app", Profile: ProfileNetadmin, Command: []string{"", "tcpdump", "-i", "any"},
		ImagePullPolicy: "Always", Env: []string{"A=1", "B=x=y"},
	})
	ec, err := ephemeralDebugContainer(plan)
	if err != nil {
		t.Fatal(err)
	}
	if ec.ImagePullPolicy != corev1.PullAlways {
		t.Errorf("pull policy = %q", ec.ImagePullPolicy)
	}
	if len(ec.Env) != 2 || ec.Env[1].Name != "B" || ec.Env[1].Value != "x=y" {
		t.Errorf("env = %+v", ec.Env)
	}

	if ec.TargetContainerName != "app" {
		t.Errorf("target = %q", ec.TargetContainerName)
	}
	if !ec.Stdin || !ec.TTY {
		t.Error("the container would not take a terminal")
	}
	if ec.Image != "nicolaka/netshoot" || strings.Join(ec.Command, " ") != "tcpdump -i any" {
		t.Errorf("runs %s %v", ec.Image, ec.Command)
	}
	if ec.SecurityContext == nil || ec.SecurityContext.Capabilities == nil {
		t.Error("the profile was not applied")
	}
}

func TestPodCopyLeavesBehindWhatWouldGetInTheWay(t *testing.T) {
	original := runningPod()
	original.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "old"}}}
	original.Spec.InitContainers = []corev1.Container{{Name: "migrate"}}
	no := false
	plan := mustPlan(t, DebugRequest{
		Mode: DebugCopy, Namespace: "shop", Name: original.Name, SetImages: []string{"app=shop/web:debug"},
		Profile: ProfileLegacy, ShareProcesses: &no,
	})
	copied, err := podCopy(original, plan)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Annotations["owner"] != "" {
		t.Error("the copy kept the original's annotations without being asked")
	}
	if len(copied.Spec.InitContainers) != 1 {
		t.Error("the copy dropped its init containers, which kubectl keeps by default")
	}

	if copied.Name != "web-7d9f-debug" || copied.Namespace != "shop" {
		t.Errorf("copy is %s/%s", copied.Namespace, copied.Name)
	}
	if copied.Labels["app"] != "" {
		t.Error("the copy kept the original's labels, so a Service would send it traffic")
	}
	if !isDebugPod(copied.Labels) || copied.Labels[debugOfLabel] != original.Name {
		t.Errorf("the copy does not say what it is: %v", copied.Labels)
	}
	if copied.Spec.NodeName != "" {
		t.Error("the copy is pinned to the original's node without being asked")
	}
	if len(copied.Spec.EphemeralContainers) != 0 {
		t.Error("the copy carries ephemeral containers, which the API refuses on create")
	}
	if len(copied.Spec.Containers) != 3 {
		t.Fatalf("copy has %d containers, want the two plus the debugger", len(copied.Spec.Containers))
	}
	app := copied.Spec.Containers[0]
	if app.LivenessProbe != nil || app.ReadinessProbe != nil || app.StartupProbe != nil {
		t.Error("the copy kept its probes")
	}
	if app.Image != "shop/web:debug" {
		t.Errorf("the target's image is %q, want the replacement", app.Image)
	}
	if copied.Spec.Containers[1].Image != "envoy:1" {
		t.Error("an image was replaced in a container that was not the target")
	}
	if copied.Spec.ShareProcessNamespace != nil {
		t.Error("legacy without --share-processes shared the process namespace anyway")
	}

	// The original is untouched: the copy is made from a deep copy.
	if original.Spec.Containers[0].LivenessProbe == nil || len(original.Spec.Containers) != 2 {
		t.Error("making the copy changed the original")
	}
}

func TestPodCopySharesProcessesForEveryProfileButLegacy(t *testing.T) {
	no, yes := false, true
	for _, profile := range DebugProfiles {
		plan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web", Profile: profile, ShareProcesses: &no})
		copied, _ := podCopy(runningPod(), plan)
		shared := copied.Spec.ShareProcessNamespace != nil && *copied.Spec.ShareProcessNamespace
		if shared != (profile != ProfileLegacy) {
			t.Errorf("%s: shares processes = %v", profile, shared)
		}
	}

	plan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web", Profile: ProfileLegacy, ShareProcesses: &yes, SameNode: true})
	copied, _ := podCopy(runningPod(), plan)
	if copied.Spec.ShareProcessNamespace == nil || !*copied.Spec.ShareProcessNamespace {
		t.Error("legacy with --share-processes did not share")
	}
	if copied.Spec.NodeName != "worker-01" {
		t.Error("same node was asked for and not given")
	}
}

func TestSetImageWithoutATargetChangesEveryContainer(t *testing.T) {
	plan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web", SetImages: []string{"*=busybox", "sidecar=envoy:2"}})
	copied, _ := podCopy(runningPod(), plan)
	if copied.Spec.Containers[0].Image != "busybox" {
		t.Errorf("app runs %s, want the * image", copied.Spec.Containers[0].Image)
	}
	// A container named outright wins over *.
	if copied.Spec.Containers[1].Image != "envoy:2" {
		t.Errorf("sidecar runs %s", copied.Spec.Containers[1].Image)
	}
}

func TestNodeDebugPodFollowsTheProfile(t *testing.T) {
	cases := []struct {
		profile    string
		namespaces bool
		hostRoot   bool
	}{
		{ProfileLegacy, true, true},
		{ProfileGeneral, true, true},
		{ProfileBaseline, false, false},
		{ProfileNetadmin, true, false},
		{ProfileRestricted, false, false},
		{ProfileSysadmin, true, true},
	}
	for _, tc := range cases {
		plan := mustPlan(t, DebugRequest{Mode: DebugNode, Name: "worker-01", Namespace: "debug", Profile: tc.profile})
		pod, err := nodeDebugPod(plan)
		if err != nil {
			t.Fatal(err)
		}

		if pod.Spec.NodeName != "worker-01" || pod.Namespace != "debug" {
			t.Errorf("%s: pod lands on %s in %s", tc.profile, pod.Spec.NodeName, pod.Namespace)
		}
		if got := pod.Spec.HostPID && pod.Spec.HostNetwork && pod.Spec.HostIPC; got != tc.namespaces {
			t.Errorf("%s: host namespaces = %v, want %v", tc.profile, got, tc.namespaces)
		}
		mounted := len(pod.Spec.Containers[0].VolumeMounts) == 1 && pod.Spec.Containers[0].VolumeMounts[0].MountPath == "/host"
		if mounted != tc.hostRoot {
			t.Errorf("%s: host root at /host = %v, want %v", tc.profile, mounted, tc.hostRoot)
		}
		if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
			t.Errorf("%s: restart policy %q", tc.profile, pod.Spec.RestartPolicy)
		}
		if len(pod.Spec.Tolerations) != 1 || pod.Spec.Tolerations[0].Operator != corev1.TolerationOpExists {
			t.Errorf("%s: tolerations %+v", tc.profile, pod.Spec.Tolerations)
		}
		if pod.Spec.ActiveDeadlineSeconds == nil {
			t.Errorf("%s: no deadline", tc.profile)
		}
		if !isDebugPod(pod.Labels) || pod.Labels[DebugLabel] != DebugNode {
			t.Errorf("%s: labels %v", tc.profile, pod.Labels)
		}
	}
}

func TestDebugLabelsStayValidForALongNodeName(t *testing.T) {
	labels := debugLabels(DebugNode, strings.Repeat("n", 100)+".example.com")
	for key, value := range labels {
		if problems := validation.IsValidLabelValue(value); len(problems) > 0 {
			t.Errorf("%s=%q: %v", key, value, problems)
		}
	}
}

func TestDebugContainerState(t *testing.T) {
	status := func(state corev1.ContainerState) []corev1.ContainerStatus {
		return []corev1.ContainerStatus{{Name: "other"}, {Name: "debugger-x", State: state}}
	}

	running := &corev1.Pod{Status: corev1.PodStatus{EphemeralContainerStatuses: status(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})}}
	if ok, err := debugContainerState(running, "debugger-x", true); !ok || err != nil {
		t.Errorf("running ephemeral = %v, %v", ok, err)
	}
	// The same status read from the wrong list is not there yet.
	if ok, err := debugContainerState(running, "debugger-x", false); ok || err != nil {
		t.Errorf("ephemeral status read as a container's = %v, %v", ok, err)
	}

	pulling := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: status(corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
	})}}
	if ok, err := debugContainerState(pulling, "debugger-x", false); ok || err != nil {
		t.Errorf("creating = %v, %v; want still waiting", ok, err)
	}

	missing := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: status(corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "not found"},
	})}}
	if _, err := debugContainerState(missing, "debugger-x", false); err == nil || !strings.Contains(err.Error(), "ErrImagePull") {
		t.Errorf("a failed pull = %v", err)
	}

	exited := &corev1.Pod{Status: corev1.PodStatus{EphemeralContainerStatuses: status(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 127, Reason: "Error"},
	})}}
	if _, err := debugContainerState(exited, "debugger-x", true); err == nil || !strings.Contains(err.Error(), "127") {
		t.Errorf("an exited container = %v", err)
	}

	failed := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}}
	if _, err := debugContainerState(failed, "debugger-x", false); err == nil {
		t.Error("a failed pod was not reported")
	}
}

func TestCreateDebugTargetAddsAnEphemeralContainer(t *testing.T) {
	client := fake.NewClientset(runningPod())
	c := &clusterClient{typed: client}
	plan := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web-7d9f", Target: "app"})

	if err := c.createDebugTarget(context.Background(), plan); err != nil {
		t.Fatalf("createDebugTarget: %v", err)
	}

	var sawUpdate bool
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetSubresource() == "ephemeralcontainers" {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Errorf("no update of the ephemeralcontainers subresource in %v", client.Actions())
	}
}

func TestCreateDebugTargetRefusesWhatCannotWork(t *testing.T) {
	pending := runningPod()
	pending.Status.Phase = corev1.PodPending
	c := &clusterClient{typed: fake.NewClientset(pending)}

	plan := mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web-7d9f"})
	if err := c.createDebugTarget(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "copy") {
		t.Errorf("a pending pod = %v, want a pointer to debugging a copy", err)
	}

	c = &clusterClient{typed: fake.NewClientset(runningPod())}
	plan = mustPlan(t, DebugRequest{Mode: DebugEphemeral, Namespace: "shop", Name: "web-7d9f", Target: "nope"})
	if err := c.createDebugTarget(context.Background(), plan); err == nil {
		t.Error("a target that is not in the pod was accepted")
	}
}

func TestCreateDebugTargetMakesTheCopyAndTheNodePod(t *testing.T) {
	client := fake.NewClientset(runningPod())
	c := &clusterClient{typed: client}

	copyPlan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web-7d9f"})
	if err := c.createDebugTarget(context.Background(), copyPlan); err != nil {
		t.Fatalf("copy: %v", err)
	}
	made, err := client.CoreV1().Pods("shop").Get(context.Background(), "web-7d9f-debug", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the copy is not there: %v", err)
	}
	if !isDebugPod(made.Labels) {
		t.Errorf("copy labels %v", made.Labels)
	}

	nodePlan := mustPlan(t, DebugRequest{Mode: DebugNode, Name: "worker-01", Namespace: "debug"})
	if err := c.createDebugTarget(context.Background(), nodePlan); err != nil {
		t.Fatalf("node: %v", err)
	}
	if _, err := client.CoreV1().Pods("debug").Get(context.Background(), nodePlan.Pod, metav1.GetOptions{}); err != nil {
		t.Fatalf("the node pod is not there: %v", err)
	}
}

func TestIsDebugPodNeedsBothLabels(t *testing.T) {
	if isDebugPod(map[string]string{ManagedByLabel: ManagedBy}) {
		t.Error("the node shell's pod, or anything else this app made, counts as a debug pod")
	}
	if isDebugPod(map[string]string{DebugLabel: DebugCopy}) {
		t.Error("a pod somebody else labelled counts as ours")
	}
	if !isDebugPod(map[string]string{ManagedByLabel: ManagedBy, DebugLabel: DebugCopy}) {
		t.Error("a debug pod is not recognised")
	}
}

func TestPodCopyKeepsWhatItIsAskedTo(t *testing.T) {
	original := runningPod()
	original.Spec.InitContainers = []corev1.Container{{Name: "migrate"}}
	no := false
	plan := mustPlan(t, DebugRequest{
		Mode: DebugCopy, Namespace: "shop", Name: original.Name,
		KeepLabels: true, KeepAnnotations: true, KeepLiveness: true, KeepReadiness: true, KeepStartup: true,
		KeepInitContainers: &no,
	})
	copied, err := podCopy(original, plan)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Labels["app"] != "web" || !isDebugPod(copied.Labels) {
		t.Errorf("labels = %v, want the original's and the debug ones", copied.Labels)
	}
	if copied.Annotations["owner"] != "team-a" || copied.Annotations["k8sdockside.io/description"] == "" {
		t.Errorf("annotations = %v", copied.Annotations)
	}
	app := copied.Spec.Containers[0]
	if app.LivenessProbe == nil || app.ReadinessProbe == nil || app.StartupProbe == nil {
		t.Error("probes were dropped although kept")
	}
	if len(copied.Spec.InitContainers) != 0 {
		t.Error("init containers were kept although dropped")
	}
}

func TestPodCopyChangesAnExistingContainerWhenNamed(t *testing.T) {
	plan := mustPlan(t, DebugRequest{
		Mode: DebugCopy, Namespace: "shop", Name: "web-7d9f", Container: "app", Command: []string{"sh"},
	})
	plan.FallbackImage = "busybox"
	copied, err := podCopy(runningPod(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied.Spec.Containers) != 2 {
		t.Fatalf("%d containers, want the existing one changed rather than a third", len(copied.Spec.Containers))
	}
	app := copied.Spec.Containers[0]
	if strings.Join(app.Command, " ") != "sh" || !app.Stdin || !app.TTY {
		t.Errorf("app = %v stdin=%v tty=%v", app.Command, app.Stdin, app.TTY)
	}
	// No image was asked for, so it keeps its own -- the fallback is for a
	// new container only.
	if app.Image != "shop/web:1.2" {
		t.Errorf("app runs %s, want its own image", app.Image)
	}
}

func TestArgumentsOnlyPassesArgsToTheEntrypoint(t *testing.T) {
	plan := mustPlan(t, DebugRequest{Mode: DebugNode, Name: "worker-01", Command: []string{"-c", "ls"}, ArgumentsOnly: true})
	c, err := buildDebugContainer(plan, corev1.Container{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Command) != 0 || strings.Join(c.Args, " ") != "-c ls" {
		t.Errorf("command=%v args=%v", c.Command, c.Args)
	}
	if c.Image != DefaultDebugImage {
		t.Errorf("a new container runs %q, want the fallback", c.Image)
	}
}

func TestCustomSpecIsMergedOverTheProfile(t *testing.T) {
	plan := mustPlan(t, DebugRequest{
		Mode: DebugNode, Name: "worker-01", Profile: ProfileGeneral, Env: []string{"A=1"},
		Custom: "name: renamed\nenv:\n- name: B\n  value: \"2\"\nsecurityContext:\n  runAsUser: 1000\n",
	})
	c, err := buildDebugContainer(plan, corev1.Container{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != plan.Container {
		t.Errorf("the custom spec renamed the container to %q", c.Name)
	}
	if len(c.Env) != 2 {
		t.Errorf("env = %+v, want A from the form and B from the custom spec", c.Env)
	}
	sc := c.SecurityContext
	if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser != 1000 || sc.Capabilities == nil || len(sc.Capabilities.Add) != 1 {
		t.Errorf("security context = %+v, want the profile's capability and the custom user", sc)
	}
}

func TestNonInteractiveContainerHasNoTerminal(t *testing.T) {
	no := false
	plan := mustPlan(t, DebugRequest{Mode: DebugNode, Name: "worker-01", Interactive: &no})
	c, err := buildDebugContainer(plan, corev1.Container{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Stdin || c.TTY {
		t.Error("a non-interactive container was given a terminal")
	}
}

func TestCreateDebugTargetReplacesTheOriginal(t *testing.T) {
	client := fake.NewClientset(runningPod())
	c := &clusterClient{typed: client}
	plan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web-7d9f", Replace: true})

	if err := c.createDebugTarget(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("shop").Get(context.Background(), "web-7d9f", metav1.GetOptions{}); err == nil {
		t.Error("the original is still there")
	}
	if _, err := client.CoreV1().Pods("shop").Get(context.Background(), "web-7d9f-debug", metav1.GetOptions{}); err != nil {
		t.Errorf("the copy is not there: %v", err)
	}
}

func TestCreateDebugTargetRefusesASetImageForAMissingContainer(t *testing.T) {
	c := &clusterClient{typed: fake.NewClientset(runningPod())}
	plan := mustPlan(t, DebugRequest{Mode: DebugCopy, Namespace: "shop", Name: "web-7d9f", SetImages: []string{"nope=busybox"}})
	if err := c.createDebugTarget(context.Background(), plan); err == nil {
		t.Error("a set-image for a container the pod does not have was accepted")
	}
}

func TestDebugInfoSaysWhenThePodRunsAsNonRoot(t *testing.T) {
	pod := runningPod()
	if info := debugInfoOf(pod); info.NonRoot || len(info.Containers) != 2 {
		t.Errorf("info = %+v", info)
	}

	user := int64(1000)
	pod.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsUser: &user}
	if !debugInfoOf(pod).NonRoot {
		t.Error("runAsUser 1000 not reported")
	}

	root := int64(0)
	yes := true
	pod.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsUser: &root}
	if debugInfoOf(pod).NonRoot {
		t.Error("runAsUser 0 reported as non-root")
	}
	pod.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: &yes}
	if !debugInfoOf(pod).NonRoot {
		t.Error("runAsNonRoot not reported")
	}
}
