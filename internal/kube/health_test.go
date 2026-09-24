package kube

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func warningEvent(reason string, at time.Time, count int64) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"type":          "Warning",
		"reason":        reason,
		"count":         count,
		"lastTimestamp": at.Format(time.RFC3339),
	}}
}

func TestRecentWarningsCountsTheLastHourOnly(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	events := []unstructured.Unstructured{
		warningEvent("BackOff", now.Add(-5*time.Minute), 12),
		warningEvent("BackOff", now.Add(-20*time.Minute), 3),
		warningEvent("FailedScheduling", now.Add(-50*time.Minute), 1),
		// Yesterday's is not today's problem.
		warningEvent("Unhealthy", now.Add(-26*time.Hour), 40),
		// A Normal event that slipped past the field selector is not counted.
		{Object: map[string]any{"type": "Normal", "reason": "Pulled", "lastTimestamp": now.Format(time.RFC3339)}},
	}

	total, reasons := recentWarnings(events, now)

	if total != 16 {
		t.Errorf("total = %d, want 16", total)
	}
	if len(reasons) != 2 || reasons[0] != (ReasonCount{"BackOff", 15}) || reasons[1] != (ReasonCount{"FailedScheduling", 1}) {
		t.Errorf("reasons = %+v", reasons)
	}
}

func TestRecentWarningsReadsTheSeriesOfANewStyleEvent(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	event := unstructured.Unstructured{Object: map[string]any{
		"type":   "Warning",
		"reason": "BackOff",
		"series": map[string]any{
			"count":            int64(7),
			"lastObservedTime": now.Add(-time.Minute).Format("2006-01-02T15:04:05.000000Z07:00"),
		},
	}}
	total, _ := recentWarnings([]unstructured.Unstructured{event}, now)
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
}

func TestEvictedAmongKeepsOnlyTheEvicted(t *testing.T) {
	pods := []unstructured.Unstructured{
		troublePod("b", "gone-2", map[string]any{"phase": "Failed", "reason": "Evicted"}),
		troublePod("a", "gone-1", map[string]any{"phase": "Failed", "reason": "Evicted"}),
		troublePod("a", "crashed", map[string]any{"phase": "Failed", "reason": "Error"}),
		troublePod("a", "running", map[string]any{"phase": "Running"}),
	}
	got := evictedAmong(pods)
	want := []ObjectRef{{Namespace: "a", Name: "gone-1"}, {Namespace: "b", Name: "gone-2"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("evicted = %+v, want %+v", got, want)
	}
}
