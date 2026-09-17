package kube

// A workload's rollout history, and going back to an earlier entry in it:
// `kubectl rollout history` and `kubectl rollout undo`.
//
// The history is kept in two different places. A Deployment keeps each
// revision as a ReplicaSet, numbered by an annotation; a StatefulSet and a
// DaemonSet keep theirs as ControllerRevisions, each holding the patch that
// puts the pod template back. Both are found the same way -- the objects in
// the namespace this one controls -- and read into one shape.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// The annotations a Deployment's ReplicaSets carry: which revision each one
// is, and the reason it was made when somebody wrote one down.
const (
	revisionAnnotation    = "deployment.kubernetes.io/revision"
	changeCauseAnnotation = "kubernetes.io/change-cause"
)

// RolloutRevision is one entry in a workload's history.
type RolloutRevision struct {
	Revision int64 `json:"revision"`
	// Images is what the revision runs, which is what tells one revision from
	// the next far more often than anything else in the template does.
	Images      []string `json:"images"`
	ChangeCause string   `json:"changeCause"`
	Age         string   `json:"age"`
	// Current marks the revision the workload is on, which is the one there is
	// no point offering to go back to.
	Current bool `json:"current"`
}

// historyKinds are the kinds a rollout history can be read for.
var historyKinds = map[string]string{
	KindDeployments: KindReplicaSets,
	KindStatefulSet: KindControllerRevisions,
	KindDaemonSets:  KindControllerRevisions,
}

// controlledBy reports whether owner is the controller of u.
func controlledBy(u, owner *unstructured.Unstructured) bool {
	for _, ref := range u.GetOwnerReferences() {
		if ref.UID == owner.GetUID() && ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

// imagesOf lists the images of a pod template's containers.
func imagesOf(template map[string]any) []string {
	containers, _, _ := unstructured.NestedSlice(template, "spec", "containers")
	return fieldOfEach(containers, "image")
}

// replicaSetRevision reads a ReplicaSet's place in its Deployment's history.
func replicaSetRevision(rs *unstructured.Unstructured) (int64, bool) {
	n, err := strconv.ParseInt(rs.GetAnnotations()[revisionAnnotation], 10, 64)
	return n, err == nil
}

// deploymentHistory reads a Deployment's history out of its ReplicaSets.
func deploymentHistory(owned []*unstructured.Unstructured) []RolloutRevision {
	var out []RolloutRevision
	for _, rs := range owned {
		n, ok := replicaSetRevision(rs)
		if !ok {
			continue
		}
		template, _, _ := unstructured.NestedMap(rs.Object, "spec", "template")
		out = append(out, RolloutRevision{
			Revision:    n,
			Images:      imagesOf(template),
			ChangeCause: rs.GetAnnotations()[changeCauseAnnotation],
			Age:         ageOf(rs),
		})
	}
	return markCurrent(out)
}

// controllerRevisionHistory reads a StatefulSet's or DaemonSet's history out
// of its ControllerRevisions.
func controllerRevisionHistory(owned []*unstructured.Unstructured) []RolloutRevision {
	var out []RolloutRevision
	for _, cr := range owned {
		n, found, _ := unstructured.NestedInt64(cr.Object, "revision")
		if !found {
			continue
		}
		template, _, _ := unstructured.NestedMap(cr.Object, "data", "spec", "template")
		out = append(out, RolloutRevision{
			Revision:    n,
			Images:      imagesOf(template),
			ChangeCause: cr.GetAnnotations()[changeCauseAnnotation],
			Age:         ageOf(cr),
		})
	}
	return markCurrent(out)
}

// markCurrent orders a history newest first and marks the newest as the one
// the workload is on, which is how both kinds of controller number them: a
// rollback does not reuse an old number, it takes the next one.
func markCurrent(history []RolloutRevision) []RolloutRevision {
	sort.Slice(history, func(i, j int) bool { return history[i].Revision > history[j].Revision })
	if len(history) > 0 {
		history[0].Current = true
	}
	return history
}

// ownedRevisions lists the objects in a workload's namespace that hold its
// history.
func (c *clusterClient) ownedRevisions(ctx context.Context, kind string, owner *unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	holder, ok := historyKinds[kind]
	if !ok {
		return nil, fmt.Errorf("%s has no rollout history", kind)
	}
	mapping, err := c.mappingForKind(holder)
	if err != nil {
		return nil, err
	}
	list, err := resourceFor(c.dynamic, mapping, owner.GetNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []*unstructured.Unstructured
	for i := range list.Items {
		if controlledBy(&list.Items[i], owner) {
			out = append(out, &list.Items[i])
		}
	}
	return out, nil
}

// RolloutHistory reads a workload's revisions, newest first.
func (w *Watcher) RolloutHistory(kc Context, kind, namespace, name string) ([]RolloutRevision, error) {
	history := []RolloutRevision{}
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		owner, _, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		owned, err := c.ownedRevisions(ctx, kind, owner)
		if err != nil {
			return err
		}
		if kind == KindDeployments {
			history = deploymentHistory(owned)
		} else {
			history = controllerRevisionHistory(owned)
		}
		return nil
	})
	return history, err
}

// deploymentRollbackPatch is the JSON patch `kubectl rollout undo` sends a
// Deployment: the pod template of the chosen ReplicaSet, without the hash
// label the Deployment controller adds to each ReplicaSet's copy.
func deploymentRollbackPatch(rs *unstructured.Unstructured) ([]byte, error) {
	template, found, err := unstructured.NestedMap(rs.Object, "spec", "template")
	if err != nil || !found {
		return nil, fmt.Errorf("replica set %s has no pod template", rs.GetName())
	}
	unstructured.RemoveNestedField(template, "metadata", "labels", "pod-template-hash")
	return json.Marshal([]map[string]any{{"op": "replace", "path": "/spec/template", "value": template}})
}

// Rollback puts a workload's pod template back to what it was at an earlier
// revision. The controller then rolls to it as it would to any other change,
// and records it as a new revision.
func (w *Watcher) Rollback(kc Context, kind, namespace, name string, revision int64) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		owner, mapping, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		if paused, _, _ := unstructured.NestedBool(owner.Object, "spec", "paused"); paused {
			return fmt.Errorf("%s %s is paused -- resume its rollout before rolling it back", owner.GetKind(), name)
		}
		owned, err := c.ownedRevisions(ctx, kind, owner)
		if err != nil {
			return err
		}
		return c.rollbackTo(ctx, kind, mapping, owner, owned, revision)
	})
}

func (c *clusterClient) rollbackTo(
	ctx context.Context, kind string, mapping *meta.RESTMapping,
	owner *unstructured.Unstructured, owned []*unstructured.Unstructured, revision int64,
) error {
	history := controllerRevisionHistory(owned)
	if kind == KindDeployments {
		history = deploymentHistory(owned)
	}
	if len(history) > 0 && history[0].Revision == revision {
		return fmt.Errorf("%s %s is already at revision %d", owner.GetKind(), owner.GetName(), revision)
	}

	client := resourceFor(c.dynamic, mapping, owner.GetNamespace())
	for _, held := range owned {
		if kind == KindDeployments {
			if n, ok := replicaSetRevision(held); !ok || n != revision {
				continue
			}
			patch, err := deploymentRollbackPatch(held)
			if err != nil {
				return err
			}
			_, err = client.Patch(ctx, owner.GetName(), types.JSONPatchType, patch, metav1.PatchOptions{})
			return err
		}

		if n, _, _ := unstructured.NestedInt64(held.Object, "revision"); n != revision {
			continue
		}
		// A ControllerRevision's data is already the strategic merge patch
		// that restores the template -- it is what the controller wrote it
		// as -- so it is sent as it is, which is what kubectl does too.
		data, found, _ := unstructured.NestedMap(held.Object, "data")
		if !found {
			return fmt.Errorf("revision %d of %s holds nothing to roll back to", revision, owner.GetName())
		}
		patch, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = client.Patch(ctx, owner.GetName(), types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		return err
	}
	return fmt.Errorf("%s %s has no revision %d", owner.GetKind(), owner.GetName(), revision)
}
