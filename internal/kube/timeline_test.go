package kube

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTimelineKeepsTheWindowNewestFirst(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	event := func(name, reason string, first, last time.Time, count int64) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]any{
			"metadata":       map[string]any{"namespace": "apps", "name": name},
			"type":           "Warning",
			"reason":         reason,
			"message":        reason + " happened",
			"count":          count,
			"firstTimestamp": first.Format(time.RFC3339),
			"lastTimestamp":  last.Format(time.RFC3339),
			"involvedObject": map[string]any{"kind": "Pod", "name": "web-1", "namespace": "apps"},
		}}
	}
	items := []unstructured.Unstructured{
		event("old", "BackOff", now.Add(-3*time.Hour), now.Add(-2*time.Hour), 4),
		event("early", "Unhealthy", now.Add(-50*time.Minute), now.Add(-40*time.Minute), 2),
		event("late", "BackOff", now.Add(-30*time.Minute), now.Add(-time.Minute), 9),
	}

	got, truncated := timelineOf(items, now.Add(-time.Hour))

	if truncated {
		t.Error("truncated with three events")
	}
	if len(got) != 2 || got[0].Name != "late" || got[1].Name != "early" {
		t.Fatalf("events = %+v", got)
	}
	e := got[0]
	if e.Count != 9 || e.Object != "Pod/web-1" || e.ObjectNamespace != "apps" || e.Reason != "BackOff" {
		t.Errorf("late = %+v", e)
	}
	if e.First != now.Add(-30*time.Minute).Format(time.RFC3339) || e.At != now.Add(-time.Minute).Format(time.RFC3339) {
		t.Errorf("late times = %s..%s", e.First, e.At)
	}
}
