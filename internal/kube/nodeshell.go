package kube

// A terminal on a node.
//
// There is no such thing as an exec against a node: the API server can reach
// what the kubelet runs, and the machine underneath it is not one of those
// things. So a shell on a node is a shell in a pod that has been given enough
// of the host to be indistinguishable from one -- the node's own PID, network
// and IPC namespaces, its filesystem mounted at /host, and a chroot into it.
// That is what `kubectl debug node/x` does, and this does the same, because
// anything else would be a different answer to "what am I looking at".
//
// It creates something in the cluster, which nothing else in this app does
// without being asked twice. Two things follow from that:
//
//   - The pod is deleted when the terminal closes, whatever closed it.
//   - It carries a deadline as well, so that a pod orphaned by the app being
//     killed does not sit privileged on a node until somebody notices.

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultNodeShellImage is what the debug pod runs. busybox because it is
	// tiny, is mirrored everywhere, and the shell it brings is only the way in
	// -- everything the user actually runs comes from the host, on the far side
	// of the chroot.
	//
	// Pinned to a release and its digest, because this pod is privileged with
	// the node's filesystem mounted: whatever a moving tag pointed at on the
	// day would run as root on the machine. The digest is the multi-arch
	// index, so every node architecture resolves it. Keep it equal to
	// appconfig.DefaultNodeImage, and move both on together.
	DefaultNodeShellImage = "busybox:1.38.0@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4"
	// DefaultNodeShellNamespace follows kubectl debug, which creates its pod in
	// `default` unless told otherwise. It is a setting because a cluster with
	// Pod Security Admission enforcing `restricted` there will refuse a
	// privileged pod, and the way out is to name a namespace that allows one.
	DefaultNodeShellNamespace = "default"
)

// nodeShellLifetime bounds a debug pod that nothing came back to clean up. Long
// enough that a working day's terminal is never cut off by it, short enough
// that a forgotten pod is not a permanent one.
const nodeShellLifetime = 12 * time.Hour

// nodeShellStartup bounds the wait for the pod to run. Pulling an image on a
// slow link is the reason it is minutes rather than seconds; a pull that has
// actually failed is reported as soon as the kubelet says so, and does not wait
// this out.
const nodeShellStartup = 3 * time.Minute

// NodeShellSpec is what the debug pod will be made of. Both fields are
// settings, and both are the sort of thing a cluster can force: an air-gapped
// cluster mirrors its own images, and a namespace with a restricted pod
// security policy will not take a privileged pod at all.
type NodeShellSpec struct {
	Namespace string `json:"namespace"`
	Image     string `json:"image"`
}

// withDefaults fills in what the user has not chosen.
func (s NodeShellSpec) withDefaults() NodeShellSpec {
	if s.Namespace == "" {
		s.Namespace = DefaultNodeShellNamespace
	}
	if s.Image == "" {
		s.Image = DefaultNodeShellImage
	}
	return s
}

// nodeShellPod is the pod that will be created, described in full so that what
// the app makes in someone's cluster can be read in one place.
func nodeShellPod(node string, spec NodeShellSpec) *corev1.Pod {
	privileged := true
	root := int64(0)
	deadline := int64(nodeShellLifetime.Seconds())
	grace := int64(0)
	noToken := false

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "k8sdockside-node-shell-",
			Namespace:    spec.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "k8sdockside",
				"k8sdockside.io/node-shell":    node,
			},
			Annotations: map[string]string{
				"k8sdockside.io/description": "Temporary privileged shell on " + node + ", created by K8s Dockside and deleted when the terminal closes.",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:    node,
			HostPID:     true,
			HostNetwork: true,
			HostIPC:     true,
			// Nothing here talks to the API server, so it is given no
			// credentials to do it with. The privilege it does have is over the
			// machine, and mounting a token as well would hand a shell on one
			// node an identity in the cluster it has no use for.
			AutomountServiceAccountToken: &noToken,
			RestartPolicy:                corev1.RestartPolicyNever,
			// A node worth opening a shell on is often one that has been
			// tainted -- a control-plane node, or one that is misbehaving --
			// and a debug pod that would not schedule there is no use.
			Tolerations:                   []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &grace,
			Volumes: []corev1.Volume{{
				Name:         "host",
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}},
			}},
			Containers: []corev1.Container{{
				Name:  "shell",
				Image: spec.Image,
				// The container's own job is only to exist: what the user runs
				// arrives later as an exec, chrooted into the host.
				Command:         []string{"sleep", fmt.Sprintf("%d", deadline)},
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					Privileged: &privileged,
					RunAsUser:  &root,
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "host", MountPath: "/host"}},
			}},
		},
	}
}

// waitRunning blocks until the debug pod is running, or says why it will not
// be.
//
// A pull that cannot succeed is the failure worth catching: without this the
// user would watch a spinner for three minutes to be told the deadline passed,
// when the kubelet knew within seconds that the image does not exist.
func (c *clusterClient) waitRunning(ctx context.Context, namespace, name string) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		pod, err := c.typed.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("the shell pod ended before it could be used: %s", podReason(pod))
		}

		for _, status := range pod.Status.ContainerStatuses {
			waiting := status.State.Waiting
			if waiting == nil {
				continue
			}
			switch waiting.Reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError":
				return fmt.Errorf("%s: %s", waiting.Reason, waiting.Message)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// podReason is the best sentence a pod has about why it is in the state it is.
func podReason(pod *corev1.Pod) string {
	if pod.Status.Message != "" {
		return pod.Status.Message
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	return string(pod.Status.Phase)
}

// NodeShell creates a privileged pod on one node, opens a shell chrooted into
// the node's filesystem, and blocks until the terminal closes. The pod is
// deleted on the way out, however the way out was reached.
//
// `ready` is called once the pod is running, with the target the session ended
// up on -- the terminal shows it, because a shell that quietly created
// something in a cluster should say what it created.
func (w *Watcher) NodeShell(
	ctx context.Context,
	kc Context,
	node string,
	spec NodeShellSpec,
	shells []string,
	stdin io.Reader,
	stdout io.Writer,
	sizes <-chan TerminalSize,
	ready func(ExecTarget),
) error {
	spec = spec.withDefaults()

	return w.withClient(kc, func(c *clusterClient) error {
		created, err := c.typed.CoreV1().Pods(spec.Namespace).
			Create(ctx, nodeShellPod(node, spec), metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating a shell pod on %s: %w", node, err)
		}

		// From here on the pod exists, so every path out of this function has
		// to remove it -- including the one where the user closed the terminal,
		// which is to say the one where ctx is already cancelled and cannot be
		// used to make the request.
		defer func() {
			gone, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()
			err := c.typed.CoreV1().Pods(spec.Namespace).
				Delete(gone, created.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				_, _ = fmt.Fprintf(stdout, "\r\n\x1b[33mThe shell pod %s/%s could not be removed: %v\x1b[0m\r\n",
					spec.Namespace, created.Name, err)
			}
		}()

		starting, cancel := context.WithTimeout(ctx, nodeShellStartup)
		defer cancel()
		if err := c.waitRunning(starting, spec.Namespace, created.Name); err != nil {
			return fmt.Errorf("waiting for the shell pod on %s: %w", node, err)
		}

		target := ExecTarget{Namespace: spec.Namespace, Pod: created.Name, Container: "shell"}
		if ready != nil {
			ready(target)
		}

		// chroot rather than a bare shell: without it the terminal is inside
		// busybox, which is not the machine anybody opened a node shell to
		// look at.
		return c.shell(ctx, target, []string{"chroot", "/host"}, shells, stdin, stdout, sizes)
	})
}
