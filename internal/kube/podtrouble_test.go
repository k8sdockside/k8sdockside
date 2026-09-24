package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func troublePod(namespace, name string, status map[string]any) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"namespace": namespace, "name": name},
		"status":   status,
	}}
}

func TestPodTroubleCountsAndRanks(t *testing.T) {
	pods := []unstructured.Unstructured{
		troublePod("default", "fine", map[string]any{"phase": "Running"}),
		troublePod("apps", "evicted-a", map[string]any{"phase": "Failed", "reason": "Evicted"}),
		troublePod("apps", "evicted-b", map[string]any{"phase": "Failed", "reason": "Evicted"}),
		troublePod("apps", "oom", map[string]any{"phase": "Failed"}),
		troublePod("apps", "looping", map[string]any{
			"phase": "Running",
			"containerStatuses": []any{map[string]any{
				"restartCount": int64(12),
				"state":        map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
			}},
		}),
		troublePod("monitoring", "flaky", map[string]any{
			"phase":                 "Running",
			"initContainerStatuses": []any{map[string]any{"restartCount": int64(1)}},
			"containerStatuses":     []any{map[string]any{"restartCount": int64(3)}},
		}),
		troublePod("default", "starting", map[string]any{
			"phase": "Pending",
			"containerStatuses": []any{map[string]any{
				"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
			}},
		}),
	}

	got := podTrouble(pods)

	if got.Evicted != 2 || got.Failed != 1 || got.CrashLooping != 1 {
		t.Fatalf("evicted/failed/crashlooping = %d/%d/%d, want 2/1/1", got.Evicted, got.Failed, got.CrashLooping)
	}
	if got.Restarting != 2 || got.Restarts != 16 {
		t.Fatalf("restarting/restarts = %d/%d, want 2/16", got.Restarting, got.Restarts)
	}

	want := []string{"looping", "oom", "evicted-a", "evicted-b", "flaky"}
	if len(got.Worst) != len(want) {
		t.Fatalf("worst has %d pods, want %d: %+v", len(got.Worst), len(want), got.Worst)
	}
	for i, name := range want {
		if got.Worst[i].Name != name {
			t.Errorf("worst[%d] = %s, want %s", i, got.Worst[i].Name, name)
		}
	}
	if got.Worst[0].Reason != "CrashLoopBackOff" || got.Worst[1].Reason != "Failed" || got.Worst[2].Reason != "Evicted" {
		t.Errorf("reasons = %s, %s, %s", got.Worst[0].Reason, got.Worst[1].Reason, got.Worst[2].Reason)
	}
}

func TestPodTroubleCapsTheList(t *testing.T) {
	var pods []unstructured.Unstructured
	for i := 0; i < podTroubleWorst+5; i++ {
		pods = append(pods, troublePod("ns", string(rune('a'+i)), map[string]any{"phase": "Failed", "reason": "Evicted"}))
	}
	got := podTrouble(pods)
	if got.Evicted != podTroubleWorst+5 {
		t.Errorf("evicted = %d, want %d", got.Evicted, podTroubleWorst+5)
	}
	if len(got.Worst) != podTroubleWorst {
		t.Errorf("worst has %d pods, want %d", len(got.Worst), podTroubleWorst)
	}
}

func TestPodTroubleEmpty(t *testing.T) {
	got := podTrouble(nil)
	if got.Worst == nil || len(got.Worst) != 0 {
		t.Errorf("worst = %#v, want an empty list", got.Worst)
	}
}
