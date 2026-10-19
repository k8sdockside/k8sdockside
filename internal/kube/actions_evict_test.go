package kube

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEvictAdvice(t *testing.T) {
	pod := func(mutate func(u *unstructured.Unstructured)) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"kind":     "Pod",
			"metadata": map[string]any{"namespace": "a", "name": "p"},
			"status":   map[string]any{"phase": "Running"},
		}}
		u.SetOwnerReferences([]metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs"}})
		mutate(u)
		return u
	}

	cases := []struct {
		name          string
		pod           *unstructured.Unstructured
		refuse, noted bool
	}{
		{"managed and running", pod(func(*unstructured.Unstructured) {}), false, false},
		{"terminating", pod(func(u *unstructured.Unstructured) { now := metav1.Now(); u.SetDeletionTimestamp(&now) }), true, false},
		{"static", pod(func(u *unstructured.Unstructured) {
			u.SetOwnerReferences(nil)
			u.SetAnnotations(map[string]string{mirrorPod: "abc"})
		}), true, false},
		{"finished", pod(func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "Succeeded", "status", "phase")
		}), true, false},
		{"daemonset", pod(func(u *unstructured.Unstructured) {
			u.SetOwnerReferences([]metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds"}})
		}), false, true},
		{"bare", pod(func(u *unstructured.Unstructured) { u.SetOwnerReferences(nil) }), false, true},
	}
	for _, c := range cases {
		state := stateOf(c.pod)
		if (state.NoEvict != "") != c.refuse || (state.EvictNote != "") != c.noted {
			t.Errorf("%s: NoEvict=%q EvictNote=%q, want refuse=%v noted=%v", c.name, state.NoEvict, state.EvictNote, c.refuse, c.noted)
		}
	}

	// Not a pod: nothing said about evicting it.
	deploy := &unstructured.Unstructured{Object: map[string]any{"kind": "Deployment"}}
	if s := stateOf(deploy); s.NoEvict != "" || s.EvictNote != "" {
		t.Errorf("deployment: %+v", s)
	}
}
