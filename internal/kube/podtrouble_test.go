package kube

import (
	"testing"
	"time"

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
	if got.Worst[0].Trouble != TroubleCrashLoop || got.Worst[1].Trouble != TroubleFailed || got.Worst[2].Trouble != TroubleEvicted {
		t.Errorf("troubles = %s, %s, %s", got.Worst[0].Trouble, got.Worst[1].Trouble, got.Worst[2].Trouble)
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

// A restart count is a total since the pod was made. What tells a pod falling
// over now from one that fell over once last month is when, and why.
func TestPodTroubleTellsRecentRestartsFromOldOnes(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	restarted := func(name string, ago time.Duration, reason string, exit int64) unstructured.Unstructured {
		return troublePod("apps", name, map[string]any{
			"phase": "Running",
			"containerStatuses": []any{map[string]any{
				"name":         "app",
				"restartCount": int64(2),
				"lastState": map[string]any{"terminated": map[string]any{
					"reason":     reason,
					"exitCode":   exit,
					"finishedAt": now.Add(-ago).Format(time.RFC3339),
				}},
			}},
		})
	}
	pods := []unstructured.Unstructured{
		restarted("old", 30*24*time.Hour, "Error", 1),
		restarted("fresh", 10*time.Minute, "OOMKilled", 137),
	}

	got := podTroubleAt(pods, now)

	if got.Restarting != 2 || got.RestartedRecently != 1 {
		t.Fatalf("restarting/recently = %d/%d, want 2/1", got.Restarting, got.RestartedRecently)
	}
	if got.Worst[0].Name != "fresh" || got.Worst[0].Reason != "Restarting" {
		t.Errorf("first = %s (%s), want fresh (Restarting)", got.Worst[0].Name, got.Worst[0].Reason)
	}
	if got.Worst[0].LastTermination != "OOMKilled (exit 137)" || got.Worst[0].LastRestart != "10m" {
		t.Errorf("fresh last = %q %q", got.Worst[0].LastTermination, got.Worst[0].LastRestart)
	}
	if got.Worst[1].Reason != "Restarted" || got.Worst[1].LastTermination != "Error (exit 1)" {
		t.Errorf("old = %s %q", got.Worst[1].Reason, got.Worst[1].LastTermination)
	}
}

func TestPodTroubleKeepsTheEvictionMessage(t *testing.T) {
	got := podTrouble([]unstructured.Unstructured{
		troublePod("apps", "gone", map[string]any{
			"phase": "Failed", "reason": "Evicted", "message": "The node was low on resource: memory.",
		}),
	})
	if got.Worst[0].Message != "The node was low on resource: memory." {
		t.Errorf("message = %q", got.Worst[0].Message)
	}
}

// A time cell reads as an age, but carries the moment too, so the window can
// write it the way the user likes dates written.
func TestTimeCellCarriesTheMoment(t *testing.T) {
	at := time.Date(2026, 9, 24, 10, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	if got := timeCell(at).At; got != "2026-09-24T08:30:00Z" {
		t.Errorf("at = %q, want the moment in UTC", got)
	}
	if got := timeCell(time.Time{}).At; got != "" {
		t.Errorf("no time carries %q", got)
	}
}

// restartedPod is a running pod whose one container has restarted count
// times, the last of them at finished.
func restartedPod(name string, count int64, finished time.Time) unstructured.Unstructured {
	return troublePod("kube-system", name, map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name":         "main",
			"restartCount": count,
			"lastState": map[string]any{"terminated": map[string]any{
				"reason":     "Completed",
				"finishedAt": finished.UTC().Format(time.RFC3339),
			}},
		}},
	})
}

func TestPodRestartsColumnCountsAllButTonesOnlyCurrent(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		pod  unstructured.Unstructured
		text string
		tone string
	}{
		{"never", troublePod("a", "p", map[string]any{"containerStatuses": []any{map[string]any{"restartCount": int64(0)}}}), "0", ""},
		{"after a reboot two weeks ago", restartedPod("p", 2, now.Add(-13*24*time.Hour)), "2", ""},
		{"just now", restartedPod("p", 2, now.Add(-5*time.Minute)), "2", "warn"},
		{"many, just now", restartedPod("p", 9, now.Add(-5*time.Minute)), "9", "error"},
		{"crash looping, no record of when", troublePod("a", "p", map[string]any{"containerStatuses": []any{map[string]any{
			"restartCount": int64(4),
			"state":        map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		}}}), "4", "warn"},
		{"no record of when", troublePod("a", "p", map[string]any{"containerStatuses": []any{map[string]any{"restartCount": int64(3)}}}), "3", ""},
	}
	for _, c := range cases {
		got := podRestartsAt(&c.pod, now)
		if got.Text != c.text || got.Tone != c.tone {
			t.Errorf("%s: got %q/%q, want %q/%q", c.name, got.Text, got.Tone, c.text, c.tone)
		}
	}

	// Sorted by the count, not by the text: 10 after 9.
	nine := podRestartsAt(ptr(restartedPod("p", 9, now)), now)
	ten := podRestartsAt(ptr(restartedPod("p", 10, now)), now)
	if nine.Sort >= ten.Sort {
		t.Errorf("sort keys %q, %q: 9 should sort before 10", nine.Sort, ten.Sort)
	}
}

func TestPodTroubleLastRestartAt(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	older := now.Add(-13 * 24 * time.Hour)
	newer := now.Add(-2 * time.Hour)

	got := podTroubleAt([]unstructured.Unstructured{
		restartedPod("scheduler", 2, older),
		restartedPod("apiserver", 1, newer),
		troublePod("a", "fine", map[string]any{"phase": "Running"}),
	}, now)

	if want := newer.Format(time.RFC3339); got.LastRestartAt != want {
		t.Errorf("LastRestartAt = %q, want %q", got.LastRestartAt, want)
	}
	for _, issue := range got.Worst {
		if issue.LastRestartAt == "" {
			t.Errorf("%s has no LastRestartAt", issue.Name)
		}
	}

	none := podTroubleAt([]unstructured.Unstructured{troublePod("a", "fine", map[string]any{"phase": "Running"})}, now)
	if none.LastRestartAt != "" {
		t.Errorf("LastRestartAt with no restarts = %q, want empty", none.LastRestartAt)
	}
}

func TestPodTroubleListsEveryRestartedPodNewestFirst(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	got := podTroubleAt([]unstructured.Unstructured{
		restartedPod("old", 2, now.Add(-13*24*time.Hour)),
		restartedPod("new", 1, now.Add(-2*time.Hour)),
		troublePod("a", "fine", map[string]any{"phase": "Running"}),
	}, now)

	if len(got.Restarted) != 2 || got.Restarted[0].Name != "new" || got.Restarted[1].Name != "old" {
		t.Fatalf("Restarted = %+v, want new then old", got.Restarted)
	}
	if got.Restarted[1].Restarts != 2 || got.Restarted[1].LastRestartAt == "" {
		t.Errorf("old = %+v", got.Restarted[1])
	}
}
