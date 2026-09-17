package kube

// Acting on one object: deleting it, scaling it, restarting it, cordoning a
// node.
//
// These are the operations that change a cluster from a button rather than
// from the editor. Everything here goes through the same dynamic client and
// REST mapper the rest of the app uses, so a custom resource is deleted by
// exactly the path a Pod is, and none of it needs a kind compiled in.
//
// The wrappers are deliberately thin. What decides anything -- which patch to
// send, what an object's state means for the buttons -- is a plain function
// below, which is what the tests are pointed at.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// restartedAt is the annotation `kubectl rollout restart` stamps on a pod
// template. Writing the same one means a restart from this app and one from the
// command line are indistinguishable to everything watching -- and that the two
// do not fight over separate annotations that mean the same thing.
const restartedAt = "kubectl.kubernetes.io/restarted-at"

// ObjectState is what the action bar needs to know about an object beyond its
// name: which of its buttons apply, and what they should start from.
type ObjectState struct {
	// Scalable reports whether the object carries a replica count. The
	// workloads that have one are exactly the ones worth offering Scale for.
	Scalable bool  `json:"scalable"`
	Replicas int32 `json:"replicas"`
	// Cordoned is a node's spec.unschedulable, which decides whether its button
	// offers to cordon or to uncordon.
	Cordoned bool `json:"cordoned"`
	// Containers is a pod's containers as the same squares the table draws.
	// The panel shows the row again and uses it as the log view's picker, so
	// the call it already makes carries them rather than making a second.
	Containers []Pill `json:"containers"`
	// Suspended is a Job's or CronJob's spec.suspend, which decides whether
	// its button offers to suspend or to resume.
	Suspended bool `json:"suspended"`
	// Paused is a Deployment's spec.paused: whether its rollout is on hold.
	Paused bool `json:"paused"`
	// Pending reports a certificate signing request nobody has approved or
	// denied yet, which is the only time the panel offers either.
	Pending bool `json:"pending"`
}

// stateOf reads an object for the few facts the action bar needs.
//
// It reads spec rather than asking discovery what subresources exist: a replica
// count in the spec is the same question as "can this be scaled", one Get
// answers it, and the answer also pre-fills the field the user is about to type
// into.
func stateOf(u *unstructured.Unstructured) ObjectState {
	var out ObjectState
	if replicas, found, err := unstructured.NestedInt64(u.Object, "spec", "replicas"); err == nil && found {
		out.Scalable = true
		out.Replicas = replicaCount(replicas)
	}
	if cordoned, _, err := unstructured.NestedBool(u.Object, "spec", "unschedulable"); err == nil {
		out.Cordoned = cordoned
	}
	// Empty for everything that is not a pod: a Deployment's containers live
	// under spec.template, which this deliberately does not reach into.
	out.Containers = podContainers(u).Pills
	out.Suspended, _, _ = unstructured.NestedBool(u.Object, "spec", "suspend")
	out.Paused, _, _ = unstructured.NestedBool(u.Object, "spec", "paused")
	if u.GetKind() == "CertificateSigningRequest" {
		out.Pending = csrPending(u)
	}
	return out
}

// replicaCount narrows a replica count to the int32 the API uses.
//
// Unstructured objects carry numbers as int64, and a custom resource is free to
// put anything in the field. Narrowing it by conversion alone would wrap: a
// count too large to fit comes back negative, which reads as a real answer
// rather than as the nonsense it is.
func replicaCount(n int64) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(n)
	}
}

// restartPatch is the merge patch that rolls a workload: a new value under the
// annotation kubectl uses, which changes the pod template and so starts a
// rollout without anything else about the object changing.
func restartPatch(at time.Time) []byte {
	// Marshalled rather than written as a string so the timestamp cannot be
	// pasted into the document unescaped.
	patch, _ := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{restartedAt: at.UTC().Format(time.RFC3339)},
				},
			},
		},
	})
	return patch
}

// cordonPatch is the merge patch that closes a node to new work, or reopens it.
func cordonPatch(on bool) []byte {
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"unschedulable": on}})
	return patch
}

// ObjectState reads one object for what its action bar needs to know.
func (w *Watcher) ObjectState(kc Context, kind, namespace, name string) (ObjectState, error) {
	var out ObjectState
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		got, _, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		out = stateOf(got)
		return nil
	})
	return out, err
}

// Delete removes one object.
//
// No propagation policy is set, which leaves the API server's own default in
// place -- the same thing `kubectl delete` does, so a Deployment deleted here
// takes its ReplicaSets and Pods with it exactly as it would from the command
// line.
func (w *Watcher) Delete(kc Context, kind, namespace, name string) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		return resourceFor(c.dynamic, mapping, namespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
}

// ObjectRef names one object of a kind: the two fields a table row carries.
type ObjectRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Failure is one object a bulk action could not act on, with the API server's
// own words for why.
type Failure struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// BulkReport is how a bulk action went: how many objects took it, and which
// ones did not.
type BulkReport struct {
	Done     int       `json:"done"`
	Failures []Failure `json:"failures"`
}

// bulkParallel is how many deletes are in flight at once. Enough that a
// selection of fifty pods does not go one at a time; few enough that the
// client's own rate limit (see newClusterClient) is what paces them rather
// than the API server's fairness queue.
const bulkParallel = 8

// DeleteMany removes several objects of one kind.
//
// Each object is its own request, and one refusal does not stop the rest: a
// selection of twelve pods where one is forbidden should still lose the other
// eleven, with that one named. An object that has already gone counts as
// deleted -- the table is live, and a pod can finish between being ticked and
// the button being pressed -- since what the user asked for is what holds.
func (w *Watcher) DeleteMany(kc Context, kind string, refs []ObjectRef) (BulkReport, error) {
	report := BulkReport{Failures: []Failure{}}
	err := w.withClient(kc, func(c *clusterClient) error {
		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		report = deleteEach(c.dynamic, mapping, refs)
		return nil
	})
	return report, err
}

// PatchMany applies one merge patch to several objects of one kind: the same
// label on every pod ticked, the same field on every deployment. The patch
// arrives as text, YAML or JSON, and is read by MergePatchFromYAML before a
// single object is touched: half a selection patched and the other half told
// the patch was malformed is the outcome to avoid.
//
// A merge patch (RFC 7386) rather than a strategic one, because it is the one
// form every kind accepts -- a custom resource has no strategic merge rules --
// and because its two moves are exactly the two asked for: a value sets a
// field and null removes it. What it cannot do is edit one entry of a list; a
// list in the patch replaces the list in the object, and the form that builds
// these patches says so.
func (w *Watcher) PatchMany(kc Context, kind string, refs []ObjectRef, patch string) (BulkReport, error) {
	report := BulkReport{Failures: []Failure{}}
	body, err := mergePatchBytes(patch)
	if err != nil {
		return report, err
	}
	err = w.withClient(kc, func(c *clusterClient) error {
		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		report = patchEach(c.dynamic, mapping, refs, body)
		return nil
	})
	return report, err
}

func deleteEach(client dynamic.Interface, mapping *meta.RESTMapping, refs []ObjectRef) BulkReport {
	return eachOf(refs, func(ctx context.Context, ref ObjectRef) error {
		err := resourceFor(client, mapping, ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
		// Finished between being ticked and the button: what was asked for
		// already holds.
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

func patchEach(client dynamic.Interface, mapping *meta.RESTMapping, refs []ObjectRef, patch []byte) BulkReport {
	return eachOf(refs, func(ctx context.Context, ref ObjectRef) error {
		_, err := resourceFor(client, mapping, ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	})
}

// eachOf runs one action over every ref, a few at a time, and gathers what it
// could not do. It is the loop under both bulk actions and knows nothing of
// the client, which is what lets the tests run the actions against a fake one.
func eachOf(refs []ObjectRef, act func(context.Context, ObjectRef) error) BulkReport {
	report := BulkReport{Failures: []Failure{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, bulkParallel)

	for _, ref := range refs {
		wg.Add(1)
		go func(ref ObjectRef) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			// A timeout per request rather than one over the whole batch: a
			// large selection on a slow cluster is not a reason to abandon
			// the tail of it.
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()

			err := act(ctx, ref)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				report.Failures = append(report.Failures, Failure{Namespace: ref.Namespace, Name: ref.Name, Error: err.Error()})
				return
			}
			report.Done++
		}(ref)
	}
	wg.Wait()

	// In the order the table shows them, whatever order the requests finished in.
	sort.Slice(report.Failures, func(i, j int) bool {
		a, b := report.Failures[i], report.Failures[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return report
}

// Scale sets a workload's replica count through the scale subresource.
//
// The subresource rather than a patch of spec.replicas, because it is the one
// interface every scalable kind shares: a Deployment, a StatefulSet and a
// custom resource with a scale subresource are all set the same way, and a kind
// that has no scale is refused by the API server rather than silently patched.
func (w *Watcher) Scale(kc Context, kind, namespace, name string, replicas int32) error {
	if replicas < 0 {
		return fmt.Errorf("a replica count may not be negative")
	}
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		client := resourceFor(c.dynamic, mapping, namespace)

		scale, err := client.Get(ctx, name, metav1.GetOptions{}, "scale")
		if err != nil {
			return err
		}
		if err := unstructured.SetNestedField(scale.Object, int64(replicas), "spec", "replicas"); err != nil {
			return err
		}
		_, err = client.Update(ctx, scale, metav1.UpdateOptions{}, "scale")
		return err
	})
}

// Restart rolls a workload by stamping its pod template, the way
// `kubectl rollout restart` does.
func (w *Watcher) Restart(kc Context, kind, namespace, name string) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		_, err = resourceFor(c.dynamic, mapping, namespace).Patch(
			ctx, name, types.MergePatchType, restartPatch(time.Now()), metav1.PatchOptions{},
		)
		return err
	})
}

// Cordon closes a node to new work, or reopens it.
func (w *Watcher) Cordon(kc Context, name string, on bool) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return c.cordon(ctx, name, on)
	})
}

// cordon is the patch itself, split out because a drain starts with one.
func (c *clusterClient) cordon(ctx context.Context, name string, on bool) error {
	mapping, err := c.mappingForKind(KindNodes)
	if err != nil {
		return err
	}
	_, err = resourceFor(c.dynamic, mapping, "").Patch(
		ctx, name, types.MergePatchType, cordonPatch(on), metav1.PatchOptions{},
	)
	return err
}
