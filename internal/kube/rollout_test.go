package kube

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var deploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

func ownedBy(uid string) []any {
	return []any{map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "name": "web", "uid": uid, "controller": true,
	}}
}

func replicaSet(name, revision, image, owner string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]any{
			"name":            name,
			"namespace":       "prod",
			"annotations":     map[string]any{revisionAnnotation: revision, changeCauseAnnotation: "image " + image},
			"ownerReferences": ownedBy(owner),
		},
		"spec": map[string]any{"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]any{"app": "web", "pod-template-hash": name}},
			"spec": map[string]any{"containers": []any{
				map[string]any{"name": "web", "image": image},
			}},
		}},
	}}
}

func TestDeploymentHistoryIsNewestFirstAndMarksTheCurrent(t *testing.T) {
	history := deploymentHistory([]*unstructured.Unstructured{
		replicaSet("web-1", "1", "web:1", "d"),
		replicaSet("web-3", "3", "web:3", "d"),
		replicaSet("web-2", "2", "web:2", "d"),
	})
	if len(history) != 3 {
		t.Fatalf("history = %+v", history)
	}
	if history[0].Revision != 3 || !history[0].Current || history[1].Current {
		t.Errorf("history = %+v, want 3 first and current", history)
	}
	if history[2].Images[0] != "web:1" || history[2].ChangeCause != "image web:1" {
		t.Errorf("oldest = %+v", history[2])
	}
}

func TestControllerRevisionHistoryReadsTheStoredTemplate(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "db-7f"},
		"revision": int64(4),
		"data": map[string]any{"spec": map[string]any{"template": map[string]any{
			"$patch": "replace",
			"spec":   map[string]any{"containers": []any{map[string]any{"name": "db", "image": "postgres:17"}}},
		}}},
	}}
	history := controllerRevisionHistory([]*unstructured.Unstructured{cr})
	if len(history) != 1 || history[0].Revision != 4 || history[0].Images[0] != "postgres:17" {
		t.Errorf("history = %+v", history)
	}
}

func TestDeploymentRollbackPatchDropsTheHashLabel(t *testing.T) {
	patch, err := deploymentRollbackPatch(replicaSet("web-1", "1", "web:1", "d"))
	if err != nil {
		t.Fatal(err)
	}
	var ops []map[string]any
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatalf("not a JSON patch: %v", err)
	}
	if len(ops) != 1 || ops[0]["op"] != "replace" || ops[0]["path"] != "/spec/template" {
		t.Fatalf("patch = %s", patch)
	}
	// The Deployment controller adds the hash to each ReplicaSet's copy;
	// putting it into the Deployment would pin every future ReplicaSet to it.
	if strings.Contains(string(patch), "pod-template-hash") {
		t.Errorf("patch carries the hash label: %s", patch)
	}
}

func TestRollbackToPutsTheChosenTemplateBack(t *testing.T) {
	deployment := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "web", "namespace": "prod", "uid": "d"},
		"spec": map[string]any{"template": map[string]any{
			"spec": map[string]any{"containers": []any{map[string]any{"name": "web", "image": "web:2"}}},
		}},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{deploymentsGVR: "DeploymentList"}, deployment,
	)
	c := &clusterClient{dynamic: client}
	mapping := &meta.RESTMapping{
		Resource:         deploymentsGVR,
		GroupVersionKind: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		Scope:            meta.RESTScopeNamespace,
	}
	owned := []*unstructured.Unstructured{
		replicaSet("web-1", "1", "web:1", "d"),
		replicaSet("web-2", "2", "web:2", "d"),
	}

	if err := c.rollbackTo(context.Background(), KindDeployments, mapping, deployment, owned, 2); err == nil {
		t.Error("rolling back to the revision it is on was not refused")
	}
	if err := c.rollbackTo(context.Background(), KindDeployments, mapping, deployment, owned, 9); err == nil {
		t.Error("rolling back to a revision that does not exist was not refused")
	}
	if err := c.rollbackTo(context.Background(), KindDeployments, mapping, deployment, owned, 1); err != nil {
		t.Fatalf("rollbackTo: %v", err)
	}

	got, err := client.Resource(deploymentsGVR).Namespace("prod").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	containers, _, _ := unstructured.NestedSlice(got.Object, "spec", "template", "spec", "containers")
	if image := mapString(asMap(containers[0]), "image"); image != "web:1" {
		t.Errorf("image after rollback = %q, want web:1", image)
	}
}

func TestControlledByNeedsTheControllerFlag(t *testing.T) {
	owner := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"uid": "d"}}}
	rs := replicaSet("web-1", "1", "web:1", "d")
	if !controlledBy(rs, owner) {
		t.Error("a replica set controlled by the deployment is not recognised")
	}
	rs.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "name": "web", "uid": "d",
	}}
	if controlledBy(rs, owner) {
		t.Error("an owner that is not the controller counted as one")
	}
}
