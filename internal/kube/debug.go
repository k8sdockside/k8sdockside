package kube

// kubectl debug, from the app.
//
// Three ways in, the same three kubectl has:
//
//   - An ephemeral container added to a running pod. It shares the pod's
//     network, and with a target the process namespace of one of its
//     containers, so the tools in the debug image can see what the app is
//     doing without the app's image having to carry them. It cannot be
//     removed again: the API has no such operation, and the container stays in
//     the pod's spec, stopped, until the pod itself goes.
//   - A copy of a pod with a debug container added. For a pod that crashes
//     too fast to attach to, or one whose image should be swapped for a
//     look. Labels and probes are dropped, as kubectl drops them, so the copy
//     is not picked up by a Service or killed by a probe the debugging trips.
//   - A pod on a node, given the host's namespaces and its filesystem at
//     /host as the profile allows.
//
// The debug container runs the image's own command with a terminal attached,
// the way `kubectl debug -it` runs it, rather than a shell exec'd in beside a
// sleep: whatever shell the image has is its entrypoint, and when it exits the
// container is done.
//
// The last two create a pod, and both are labelled as this app's debug pods
// so they can be found and removed. Unless the user asked to keep it, the pod
// is deleted when the terminal closes, the way the node shell's is.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

// The ways a debug session can reach its subject.
const (
	DebugEphemeral = "ephemeral"
	DebugCopy      = "copy"
	DebugNode      = "node"
)

// The profiles kubectl debug knows. See applyProfile for what each one does.
const (
	ProfileLegacy     = "legacy"
	ProfileGeneral    = "general"
	ProfileBaseline   = "baseline"
	ProfileNetadmin   = "netadmin"
	ProfileRestricted = "restricted"
	ProfileSysadmin   = "sysadmin"
)

// DebugProfiles lists the profiles in the order kubectl's help lists them.
var DebugProfiles = []string{ProfileLegacy, ProfileGeneral, ProfileBaseline, ProfileNetadmin, ProfileRestricted, ProfileSysadmin}

// Debug defaults, for a caller that hands nothing in. The same answers the
// settings file starts with.
const (
	DefaultDebugImage   = "busybox"
	DefaultDebugProfile = ProfileGeneral
)

// The labels a debug pod carries. managed-by is what the node shell's pod
// carries too; DebugLabel is what marks it as one of these, and is what
// DeleteDebugPod checks before deleting anything.
const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedBy      = "k8sdockside"
	DebugLabel     = "k8sdockside.io/debug"
	// debugOfLabel names what the pod was made to debug: the pod copied, or
	// the node.
	debugOfLabel = "k8sdockside.io/debug-of"
)

// debugStartup bounds the wait for a debug container to run. Long for the
// reason the node shell's is: pulling a debug image over a slow link takes
// minutes, while a pull that has failed is reported as soon as it does.
const debugStartup = nodeShellStartup

// DebugRequest is what the launch form asks for: kubectl debug's options, by
// the names kubectl gives them.
//
// The three booleans whose kubectl default is true are pointers, so that a
// request which does not mention them means what kubectl would mean rather
// than Go's false.
type DebugRequest struct {
	// Mode is DebugEphemeral, DebugCopy or DebugNode.
	Mode string `json:"mode"`
	// Namespace is the pod's. For a node, it is where the debug pod is made.
	Namespace string `json:"namespace"`
	// Name is the pod's, or the node's.
	Name string `json:"name"`

	// Container names the debug container (-c). Empty means a fresh
	// "debugger-xxxxx". In a copy, naming one of the pod's own containers
	// changes that container -- its command, and its image when one is
	// given -- rather than adding another.
	Container string `json:"container"`
	// Image is what the debug container runs. Empty means the default, except
	// when a copy's existing container is being changed, where it means "keep
	// its image".
	Image string `json:"image"`
	// ImagePullPolicy is Always, IfNotPresent or Never; empty leaves it to
	// the cluster.
	ImagePullPolicy string `json:"imagePullPolicy"`
	// Env are KEY=VALUE pairs set in the debug container.
	Env []string `json:"env"`
	// Command replaces the image's entrypoint -- or, with ArgumentsOnly, is
	// passed to it as arguments.
	Command       []string `json:"command"`
	ArgumentsOnly bool     `json:"argumentsOnly"`
	// Profile is one of DebugProfiles. Empty means the default.
	Profile string `json:"profile"`
	// Custom is a partial container spec, JSON or YAML, merged over the debug
	// container after the profile, as kubectl's --custom is.
	Custom string `json:"custom"`
	// Interactive gives the container a terminal and attaches to it (-it).
	// Nil means yes. Without it the container is only started, and a pod made
	// for it is kept.
	Interactive *bool `json:"interactive"`

	// Target is the container whose process namespace an ephemeral container
	// shares (--target).
	Target string `json:"target"`

	// CopyTo names the copy. Empty means "<pod>-debug".
	CopyTo string `json:"copyTo"`
	// SetImages are name=image pairs changing the copy's containers' images;
	// a name of * changes every one of them.
	SetImages []string `json:"setImages"`
	// ShareProcesses gives a copy one process namespace. Nil means yes.
	ShareProcesses *bool `json:"shareProcesses"`
	// SameNode schedules a copy on the node the original is on.
	SameNode bool `json:"sameNode"`
	// Replace deletes the original pod once the copy exists.
	Replace bool `json:"replace"`
	// What a copy keeps of the original. All but the init containers are
	// dropped unless asked for, as kubectl drops them.
	KeepLabels      bool `json:"keepLabels"`
	KeepAnnotations bool `json:"keepAnnotations"`
	KeepLiveness    bool `json:"keepLiveness"`
	KeepReadiness   bool `json:"keepReadiness"`
	KeepStartup     bool `json:"keepStartup"`
	// KeepInitContainers keeps the copy's init containers. Nil means yes.
	KeepInitContainers *bool `json:"keepInitContainers"`

	// Keep leaves a copy or a node pod in place when the session ends,
	// rather than deleting it.
	Keep bool `json:"keep"`
}

// DebugPlan is a request with everything decided: the names of what will be
// made, so the terminal can say what it is attached to before it is.
type DebugPlan struct {
	DebugRequest
	// Pod is the pod the terminal attaches to: the one debugged, the copy,
	// or the pod on the node.
	Pod string
	// Container is the debug container's name.
	Container string
	// FallbackImage is what a new debug container runs when the request
	// named no image -- the settings' default.
	FallbackImage string
	// custom is Custom, read and turned into JSON.
	custom []byte
	// setImages is SetImages, read.
	setImages map[string]string
}

// Creates reports whether the session makes a pod of its own.
func (p DebugPlan) Creates() bool {
	return p.Mode == DebugCopy || p.Mode == DebugNode
}

// Attaches reports whether the session attaches a terminal at all.
func (p DebugPlan) Attaches() bool {
	return orTrue(p.Interactive)
}

// orTrue reads a boolean whose default is yes.
func orTrue(b *bool) bool {
	return b == nil || *b
}

// Plan checks a request and fills in what it leaves to the app: the default
// image and profile, the copy's name, and fresh names for the debug container
// and the node pod.
func (r DebugRequest) Plan() (DebugPlan, error) {
	r.Namespace = strings.TrimSpace(r.Namespace)
	r.Name = strings.TrimSpace(r.Name)
	r.Container = strings.TrimSpace(r.Container)
	r.Image = strings.TrimSpace(r.Image)
	r.Target = strings.TrimSpace(r.Target)
	r.CopyTo = strings.TrimSpace(r.CopyTo)
	r.Command = nonEmpty(r.Command)
	r.Env = nonEmpty(r.Env)
	r.SetImages = nonEmpty(r.SetImages)

	if r.Profile == "" {
		r.Profile = DefaultDebugProfile
	}
	if !slices.Contains(DebugProfiles, r.Profile) {
		return DebugPlan{}, fmt.Errorf("%q is not a debug profile -- it is one of %s", r.Profile, strings.Join(DebugProfiles, ", "))
	}
	if r.Name == "" {
		return DebugPlan{}, fmt.Errorf("nothing was named to debug")
	}
	if r.Namespace == "" {
		if r.Mode != DebugNode {
			return DebugPlan{}, fmt.Errorf("pod %s has no namespace", r.Name)
		}
		r.Namespace = DefaultNodeShellNamespace
	}
	switch corev1.PullPolicy(r.ImagePullPolicy) {
	case "", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		return DebugPlan{}, fmt.Errorf("%q is not an image pull policy -- it is Always, IfNotPresent or Never", r.ImagePullPolicy)
	}
	for _, pair := range r.Env {
		if key, _, ok := strings.Cut(pair, "="); !ok || strings.TrimSpace(key) == "" {
			return DebugPlan{}, fmt.Errorf("%q is not KEY=VALUE", pair)
		}
	}
	if r.Container != "" {
		if problems := validation.IsDNS1123Label(r.Container); len(problems) > 0 {
			return DebugPlan{}, fmt.Errorf("%q cannot name a container: %s", r.Container, strings.Join(problems, "; "))
		}
	}

	if r.Mode == DebugCopy && r.CopyTo == "" {
		r.CopyTo = r.Name + "-debug"
	}

	suffix := utilrand.String(5)
	plan := DebugPlan{DebugRequest: r, Container: r.Container, FallbackImage: DefaultDebugImage}
	if plan.Container == "" {
		plan.Container = "debugger-" + suffix
	}

	custom, err := readCustom(r.Custom)
	if err != nil {
		return DebugPlan{}, err
	}
	plan.custom = custom

	switch r.Mode {
	case DebugEphemeral:
		plan.Pod = r.Name
		// Nothing is created that could be kept or removed.
		plan.Keep = false
	case DebugCopy:
		// Even with Replace: the original is deleted after the copy is made,
		// and under one name that would be deleting the copy.
		if r.CopyTo == r.Name {
			return DebugPlan{}, fmt.Errorf("the copy needs a name of its own, not %s's", r.Name)
		}
		if problems := validation.IsDNS1123Subdomain(r.CopyTo); len(problems) > 0 {
			return DebugPlan{}, fmt.Errorf("%q cannot name a pod: %s", r.CopyTo, strings.Join(problems, "; "))
		}
		images, err := readSetImages(r.SetImages)
		if err != nil {
			return DebugPlan{}, err
		}
		plan.setImages = images
		plan.Pod = r.CopyTo
	case DebugNode:
		plan.Pod = nodeDebugPodName(r.Name, suffix)
	default:
		return DebugPlan{}, fmt.Errorf("%q is not a way to debug -- it is ephemeral, copy or node", r.Mode)
	}

	// A pod nothing attaches to is a pod the user means to look at some
	// other way, and deleting it the moment it was made would be pointless.
	if plan.Creates() && !plan.Attaches() {
		plan.Keep = true
	}
	return plan, nil
}

// nonEmpty drops the empty strings a form leaves in a list.
func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if strings.TrimSpace(item) != "" {
			out = append(out, item)
		}
	}
	return out
}

// readCustom turns a partial container spec, JSON or YAML, into JSON -- and
// checks that it is one, so a typo is reported before anything is made.
func readCustom(text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	raw, err := yaml.YAMLToJSON([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("the custom container spec cannot be read: %w", err)
	}
	var probe corev1.Container
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("the custom container spec is not a container: %w", err)
	}
	return raw, nil
}

// readSetImages reads name=image pairs into a map.
func readSetImages(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		name, image, ok := strings.Cut(strings.TrimSpace(pair), "=")
		name, image = strings.TrimSpace(name), strings.TrimSpace(image)
		if !ok || name == "" || image == "" {
			return nil, fmt.Errorf("%q is not container=image", pair)
		}
		out[name] = image
	}
	return out, nil
}

// nodeDebugPodName names the pod made on a node: the app's name, the node's,
// and a suffix that keeps two sessions apart. A node name can be a long FQDN,
// so it is cut to keep the whole inside what a pod name may be.
func nodeDebugPodName(node, suffix string) string {
	const prefix = "k8sdockside-debug-"
	room := validation.DNS1123SubdomainMaxLength - len(prefix) - len(suffix) - 1
	if len(node) > room {
		node = strings.TrimRight(node[:room], ".-")
	}
	return prefix + node + "-" + suffix
}

// debugLabels are what a pod made for debugging carries, so that it says what
// made it and can be found again.
func debugLabels(mode, of string) map[string]string {
	return map[string]string{
		ManagedByLabel: ManagedBy,
		DebugLabel:     mode,
		// A label value is at most 63 characters; a node FQDN can be longer.
		debugOfLabel: labelSafe(of),
	}
}

// labelSafe cuts a name down to what a label value may hold.
func labelSafe(value string) string {
	if len(value) > validation.LabelValueMaxLength {
		value = value[:validation.LabelValueMaxLength]
	}
	return strings.TrimRight(value, ".-_")
}

// ---- profiles ---------------------------------------------------------------

// The shape of the pod a profile is applied to. A profile means different
// things to each: host namespaces are for a node, a shared process namespace
// for a copy, and an ephemeral container can only be given a security context.
type debugStyle int

const (
	styleEphemeral debugStyle = iota
	styleCopy
	styleNode
)

// profileSecurity is the security context a profile gives the debug container.
// Nil means the container is given none, which is both legacy (kubectl did not
// touch it) and baseline (which asks for nothing beyond the namespace's own
// defaults).
func profileSecurity(profile string) *corev1.SecurityContext {
	yes, no := true, false
	switch profile {
	case ProfileGeneral:
		// Enough to attach a debugger or strace to a process in the target.
		return &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_PTRACE"}}}
	case ProfileNetadmin:
		return &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN", "NET_RAW"}}}
	case ProfileSysadmin:
		return &corev1.SecurityContext{Privileged: &yes}
	case ProfileRestricted:
		return &corev1.SecurityContext{
			RunAsNonRoot:             &yes,
			AllowPrivilegeEscalation: &no,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	}
	return nil
}

// applyPodProfile makes the pod-level changes a profile asks for: on a node,
// the host's namespaces and its filesystem; on a copy, one process namespace.
func applyPodProfile(pod *corev1.Pod, container string, profile string, style debugStyle, share bool) {
	switch style {
	case styleNode:
		hostNamespaces := profile == ProfileLegacy || profile == ProfileGeneral ||
			profile == ProfileNetadmin || profile == ProfileSysadmin
		hostRoot := profile == ProfileLegacy || profile == ProfileGeneral || profile == ProfileSysadmin
		if hostNamespaces {
			pod.Spec.HostNetwork = true
			pod.Spec.HostPID = true
			pod.Spec.HostIPC = true
		}
		if hostRoot {
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name:         "host-root",
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}},
			})
			for i := range pod.Spec.Containers {
				if pod.Spec.Containers[i].Name == container {
					pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts,
						corev1.VolumeMount{Name: "host-root", MountPath: "/host"})
				}
			}
		}
	case styleCopy:
		// Every profile but legacy shares processes in a copy, whatever was
		// asked: it is what makes the debug container useful beside the app.
		if share || profile != ProfileLegacy {
			yes := true
			pod.Spec.ShareProcessNamespace = &yes
		}
	case styleEphemeral:
		// Nothing at the pod's level: it is already running, and an
		// ephemeral container cannot change it.
	}
}

// ---- what is made -----------------------------------------------------------

// buildDebugContainer makes the debug container from base -- empty for a new
// one, or the copy's own container being changed -- in the order kubectl does
// it: the request's options, then the profile, then the custom spec over the
// lot.
func buildDebugContainer(plan DebugPlan, base corev1.Container) (corev1.Container, error) {
	c := *base.DeepCopy()
	c.Name = plan.Container
	switch {
	case plan.Image != "":
		c.Image = plan.Image
	case c.Image == "":
		c.Image = plan.FallbackImage
	}
	if plan.ImagePullPolicy != "" {
		c.ImagePullPolicy = corev1.PullPolicy(plan.ImagePullPolicy)
	}
	if len(plan.Command) > 0 {
		if plan.ArgumentsOnly {
			c.Args = plan.Command
		} else {
			c.Command = plan.Command
			c.Args = nil
		}
	}
	for _, pair := range plan.Env {
		key, value, _ := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		c.Env = slices.DeleteFunc(c.Env, func(e corev1.EnvVar) bool { return e.Name == key })
		c.Env = append(c.Env, corev1.EnvVar{Name: key, Value: value})
	}
	c.Stdin = plan.Attaches()
	c.TTY = plan.Attaches()
	if c.TerminationMessagePolicy == "" {
		c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
	if security := profileSecurity(plan.Profile); security != nil {
		c.SecurityContext = security
	} else if plan.Profile == ProfileBaseline || plan.Profile == ProfileRestricted {
		c.SecurityContext = nil
	}

	if len(plan.custom) == 0 {
		return c, nil
	}
	current, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	merged, err := strategicpatch.StrategicMergePatch(current, plan.custom, corev1.Container{})
	if err != nil {
		return c, fmt.Errorf("applying the custom container spec: %w", err)
	}
	var out corev1.Container
	if err := json.Unmarshal(merged, &out); err != nil {
		return c, fmt.Errorf("applying the custom container spec: %w", err)
	}
	// The name is what the terminal attaches to, so the custom spec does not
	// get to change it out from under the session.
	out.Name = plan.Container
	return out, nil
}

// ephemeralDebugContainer is the container added to a running pod. The same
// container as the other two modes get, moved into the ephemeral shape, whose
// fields are the same ones.
func ephemeralDebugContainer(plan DebugPlan) (corev1.EphemeralContainer, error) {
	c, err := buildDebugContainer(plan, corev1.Container{})
	if err != nil {
		return corev1.EphemeralContainer{}, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return corev1.EphemeralContainer{}, err
	}
	var common corev1.EphemeralContainerCommon
	if err := json.Unmarshal(raw, &common); err != nil {
		return corev1.EphemeralContainer{}, err
	}
	return corev1.EphemeralContainer{EphemeralContainerCommon: common, TargetContainerName: plan.Target}, nil
}

// podCopy is the copy of a pod a session debugs, as kubectl --copy-to makes
// it: the same spec, less what would get in the way unless asked to keep it.
func podCopy(pod *corev1.Pod, plan DebugPlan) (*corev1.Pod, error) {
	spec := pod.Spec.DeepCopy()

	// Off whatever node it was on, unless asked: the original may be there
	// because that node is the problem, but a copy pinned to a full node
	// never schedules.
	if !plan.SameNode {
		spec.NodeName = ""
	}
	// An ephemeral container cannot be set when a pod is created, and the
	// original's are its own business.
	spec.EphemeralContainers = nil
	if !orTrue(plan.KeepInitContainers) {
		spec.InitContainers = nil
	}

	for i := range spec.Containers {
		c := &spec.Containers[i]
		// Probes are dropped unless kept, as kubectl drops them: a container
		// being debugged is one that will fail them, and the kubelet
		// restarting it mid-session is the opposite of help.
		if !plan.KeepLiveness {
			c.LivenessProbe = nil
		}
		if !plan.KeepReadiness {
			c.ReadinessProbe = nil
		}
		if !plan.KeepStartup {
			c.StartupProbe = nil
		}
		if image, ok := plan.setImages[c.Name]; ok {
			c.Image = image
		} else if image, ok := plan.setImages["*"]; ok {
			c.Image = image
		}
	}

	existing := slices.IndexFunc(spec.Containers, func(c corev1.Container) bool { return c.Name == plan.Container })
	if existing >= 0 {
		changed, err := buildDebugContainer(plan, spec.Containers[existing])
		if err != nil {
			return nil, err
		}
		spec.Containers[existing] = changed
	} else {
		added, err := buildDebugContainer(plan, corev1.Container{})
		if err != nil {
			return nil, err
		}
		spec.Containers = append(spec.Containers, added)
	}

	// The original's labels are left behind unless kept: they are what a
	// Service, a ReplicaSet or a NetworkPolicy selects by, and a copy picked
	// up by any of them would be serving traffic or counted as a replica.
	labels := map[string]string{}
	if plan.KeepLabels {
		maps.Copy(labels, pod.Labels)
	}
	maps.Copy(labels, debugLabels(DebugCopy, pod.Name))
	annotations := map[string]string{}
	if plan.KeepAnnotations {
		maps.Copy(annotations, pod.Annotations)
	}
	annotations["k8sdockside.io/description"] = "Copy of " + pod.Name + " for debugging, created by K8s Dockside."

	copied := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        plan.Pod,
			Namespace:   pod.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: *spec,
	}
	applyPodProfile(copied, plan.Container, plan.Profile, styleCopy, orTrue(plan.ShareProcesses))
	return copied, nil
}

// nodeDebugPod is the pod made on a node, as `kubectl debug node/…` makes it.
func nodeDebugPod(plan DebugPlan) (*corev1.Pod, error) {
	container, err := buildDebugContainer(plan, corev1.Container{})
	if err != nil {
		return nil, err
	}
	deadline := int64(nodeShellLifetime.Seconds())
	grace := int64(0)
	noToken := false

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      plan.Pod,
			Namespace: plan.Namespace,
			Labels:    debugLabels(DebugNode, plan.Name),
			Annotations: map[string]string{
				"k8sdockside.io/description": "Debug pod on " + plan.Name + ", created by K8s Dockside.",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      plan.Name,
			RestartPolicy: corev1.RestartPolicyNever,
			// A node worth debugging is often a tainted one -- a control
			// plane node, or one that is misbehaving.
			Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			// A deadline, as the node shell has, so a pod orphaned by the app
			// being killed does not sit on a node until somebody notices.
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &grace,
			// Nothing here talks to the API server, so it is given nothing
			// to do it with.
			AutomountServiceAccountToken: &noToken,
			Containers:                   []corev1.Container{container},
		},
	}
	applyPodProfile(pod, plan.Container, plan.Profile, styleNode, false)
	return pod, nil
}

// ---- waiting ----------------------------------------------------------------

// debugContainerState reads whether the debug container has started, and when
// it never will, why not.
func debugContainerState(pod *corev1.Pod, container string, ephemeral bool) (bool, error) {
	if !ephemeral {
		switch pod.Status.Phase {
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, fmt.Errorf("the debug pod ended before it could be used: %s", podReason(pod))
		}
	}

	statuses := pod.Status.ContainerStatuses
	if ephemeral {
		statuses = pod.Status.EphemeralContainerStatuses
	}
	for _, status := range statuses {
		if status.Name != container {
			continue
		}
		if status.State.Running != nil {
			return true, nil
		}
		if done := status.State.Terminated; done != nil {
			why := done.Reason
			if done.Message != "" {
				why += ": " + strings.TrimSpace(done.Message)
			}
			return false, fmt.Errorf("the debug container exited (code %d) before it could be attached to: %s", done.ExitCode, why)
		}
		if waiting := status.State.Waiting; waiting != nil {
			switch waiting.Reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
				return false, fmt.Errorf("%s: %s", waiting.Reason, waiting.Message)
			}
		}
	}
	return false, nil
}

// waitDebugContainer blocks until the debug container runs, or says why it
// will not.
func (c *clusterClient) waitDebugContainer(ctx context.Context, plan DebugPlan) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		pod, err := c.typed.CoreV1().Pods(plan.Namespace).Get(ctx, plan.Pod, metav1.GetOptions{})
		if err != nil {
			return err
		}
		running, err := debugContainerState(pod, plan.Container, plan.Mode == DebugEphemeral)
		if err != nil || running {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// ---- the session ------------------------------------------------------------

// attach connects a terminal to a container's own process, the way `kubectl
// attach -it` does, and blocks until it ends.
func (c *clusterClient) attach(
	ctx context.Context,
	target ExecTarget,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
) error {
	req := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(target.Pod).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: target.Container,
			Stdin:     true,
			Stdout:    true,
			// Off for the reason exec has it off: with a TTY the two
			// streams are one.
			Stderr: false,
			TTY:    true,
		}, scheme.ParameterCodec)

	return c.stream(ctx, req.URL(), stdin, stdout, sizes)
}

// createDebugTarget makes whatever the plan debugs in: the ephemeral container
// added to the pod, or the copy, or the pod on the node.
func (c *clusterClient) createDebugTarget(ctx context.Context, plan DebugPlan) error {
	pods := c.typed.CoreV1().Pods(plan.Namespace)

	switch plan.Mode {
	case DebugEphemeral:
		pod, err := pods.Get(ctx, plan.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pod.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("pod %s is %s, and an ephemeral container can only be added to a running pod -- debug a copy of it instead",
				plan.Name, strings.ToLower(string(pod.Status.Phase)))
		}
		if plan.Target != "" && !hasContainer(pod, plan.Target) {
			return fmt.Errorf("pod %s has no container %s to target", plan.Name, plan.Target)
		}
		if hasContainer(pod, plan.Container) || slices.ContainsFunc(pod.Spec.EphemeralContainers,
			func(e corev1.EphemeralContainer) bool { return e.Name == plan.Container }) {
			return fmt.Errorf("pod %s already has a container named %s", plan.Name, plan.Container)
		}
		container, err := ephemeralDebugContainer(plan)
		if err != nil {
			return err
		}
		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, container)
		if _, err := pods.UpdateEphemeralContainers(ctx, plan.Name, pod, metav1.UpdateOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("this cluster does not serve ephemeral containers (%w) -- debug a copy of the pod instead", err)
			}
			return err
		}
		return nil

	case DebugCopy:
		pod, err := pods.Get(ctx, plan.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for name := range plan.setImages {
			if name != "*" && !hasContainer(pod, name) {
				return fmt.Errorf("pod %s has no container %s to change the image of", plan.Name, name)
			}
		}
		copied, err := podCopy(pod, plan)
		if err != nil {
			return err
		}
		if _, err := pods.Create(ctx, copied, metav1.CreateOptions{}); err != nil {
			return err
		}
		if plan.Replace {
			// Only the pod that was copied: the precondition stops this
			// deleting one that has replaced it under the same name since.
			uid := pod.UID
			err := pods.Delete(ctx, pod.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("the copy %s was made, but the original could not be deleted: %w", plan.Pod, err)
			}
		}
		return nil

	case DebugNode:
		pod, err := nodeDebugPod(plan)
		if err != nil {
			return err
		}
		_, err = pods.Create(ctx, pod, metav1.CreateOptions{})
		return err
	}
	return fmt.Errorf("%q is not a way to debug", plan.Mode)
}

// hasContainer reports whether a pod declares a container by that name.
func hasContainer(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			return true
		}
	}
	return false
}

// Debug runs one debug session: it makes what the plan asks for, waits for the
// debug container to run, attaches a terminal to it, and blocks until the
// terminal closes. A pod made for the session is deleted on the way out
// unless the plan keeps it, however the way out was reached.
//
// `say` is given a line at each step, for the terminal to show while there is
// nothing else on it.
func (w *Watcher) Debug(
	ctx context.Context,
	kc Context,
	plan DebugPlan,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
	say func(string),
) error {
	if say == nil {
		say = func(string) {}
	}
	return w.withClient(kc, func(c *clusterClient) error {
		creating, cancel := context.WithTimeout(ctx, callTimeout)
		err := c.createDebugTarget(creating, plan)
		cancel()
		if err != nil {
			return fmt.Errorf("starting the debug container: %w", err)
		}

		if plan.Creates() {
			if plan.Keep {
				defer func() {
					say(fmt.Sprintf("Pod %s/%s was kept. Delete it when you are done with it.", plan.Namespace, plan.Pod))
				}()
			} else {
				// The pod exists from here on, so every path out removes it --
				// including the one where the terminal was closed, which is
				// the one where ctx can no longer be used to ask.
				defer func() {
					gone, cancel := context.WithTimeout(context.Background(), callTimeout)
					defer cancel()
					err := c.typed.CoreV1().Pods(plan.Namespace).Delete(gone, plan.Pod, metav1.DeleteOptions{})
					if err != nil && !apierrors.IsNotFound(err) {
						_, _ = fmt.Fprintf(stdout, "\r\n\x1b[33mThe debug pod %s/%s could not be removed: %v\x1b[0m\r\n",
							plan.Namespace, plan.Pod, err)
					}
				}()
			}
		}

		if !plan.Attaches() {
			// Started and left to run, as kubectl debug without -i leaves it.
			say(fmt.Sprintf("Started %s in pod %s/%s, without a terminal.", plan.Container, plan.Namespace, plan.Pod))
			return nil
		}

		say(fmt.Sprintf("Waiting for %s to start…", plan.Container))
		starting, cancel := context.WithTimeout(ctx, debugStartup)
		defer cancel()
		if err := c.waitDebugContainer(starting, plan); err != nil {
			return fmt.Errorf("waiting for the debug container: %w", err)
		}
		say("Attached. If you don't see a command prompt, try pressing enter.")

		// Through the relays, as a shell is: the user may have typed while the
		// image was pulling, and that has to reach the container now rather
		// than be lost to a reader nobody was holding.
		typed := newStdinRelay(ctx, stdin)
		defer typed.detach()
		resizes := newSizeRelay(ctx, sizes)
		defer resizes.detach()

		target := ExecTarget{Namespace: plan.Namespace, Pod: plan.Pod, Container: plan.Container}
		err = c.attach(ctx, target, typed.attach(), stdout, resizes.attach())
		if plan.Mode == DebugEphemeral && ctx.Err() == nil {
			say(fmt.Sprintf("The ephemeral container %s stays in pod %s, stopped: the API cannot remove one.", plan.Container, plan.Pod))
		}
		return err
	})
}

// DeleteDebugPod removes a pod this app made for debugging, and nothing else:
// a pod without the debug label is refused, so a stale button cannot delete a
// pod that has since been replaced by somebody's real one of the same name.
func (w *Watcher) DeleteDebugPod(kc Context, namespace, name string) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		pods := c.typed.CoreV1().Pods(namespace)
		pod, err := pods.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !isDebugPod(pod.Labels) {
			return fmt.Errorf("pod %s/%s was not made by this app for debugging, so it is left alone", namespace, name)
		}
		uid := pod.UID
		err = pods.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

// isDebugPod reports whether labels mark a pod as one of this app's debug pods.
func isDebugPod(labels map[string]string) bool {
	return labels[ManagedByLabel] == ManagedBy && labels[DebugLabel] != ""
}

// DebugInfo is what the launch form needs to know about a pod before it asks:
// which containers it has, and whether it runs as a user other than root.
type DebugInfo struct {
	Containers []string `json:"containers"`
	// NonRoot is set when the pod's own security context runs everything as
	// a non-root user. A profile's capabilities may then not take effect, as
	// the kubectl docs warn.
	NonRoot bool `json:"nonRoot"`
}

// debugInfoOf reads DebugInfo from a pod.
func debugInfoOf(pod *corev1.Pod) DebugInfo {
	info := DebugInfo{Containers: make([]string, 0, len(pod.Spec.Containers))}
	for _, c := range pod.Spec.Containers {
		info.Containers = append(info.Containers, c.Name)
	}
	if sc := pod.Spec.SecurityContext; sc != nil {
		info.NonRoot = (sc.RunAsNonRoot != nil && *sc.RunAsNonRoot) || (sc.RunAsUser != nil && *sc.RunAsUser != 0)
	}
	return info
}

// DebugInfo reads what the launch form needs to know about a pod.
func (w *Watcher) DebugInfo(kc Context, namespace, name string) (DebugInfo, error) {
	var out DebugInfo
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		pod, err := c.typed.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		out = debugInfoOf(pod)
		return nil
	})
	return out, err
}
