package kube

// A cluster's events on a time axis, for the dashboard.
//
// The events table sorts by when, but it reads as a list: forty BackOff rows
// and one NodeNotReady in the middle of them look alike. Drawn against time
// the shape shows -- the node went, then everything on it complained -- and
// that shape is usually the answer to "what happened here".

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// timelineLimit bounds how many events the timeline carries. The newest are
// kept: a timeline is read from its right-hand edge.
const timelineLimit = 1500

// TimelineEvent is one event, placed in time.
type TimelineEvent struct {
	// At is when it last happened, RFC3339, and First when it first did --
	// the same unless it repeated.
	At    string `json:"at"`
	First string `json:"first"`
	Type  string `json:"type"`
	// Reason is the event's reason, which the timeline groups its rows by.
	Reason string `json:"reason"`
	// Object is what the event is about, as Kind/name, in ObjectNamespace.
	Object          string `json:"object"`
	ObjectNamespace string `json:"objectNamespace"`
	Message         string `json:"message"`
	Count           int    `json:"count"`
	// Namespace and Name are the event object's own, for opening its report.
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Timeline is the events of one cluster, or one namespace of it, over a
// window ending now.
type Timeline struct {
	From   string          `json:"from"`
	To     string          `json:"to"`
	Events []TimelineEvent `json:"events"`
	// Truncated says there were more events in the window than were kept.
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

// EventTimeline reads the events of the last `minutes` minutes. An empty
// namespace means the whole cluster.
func (w *Watcher) EventTimeline(kc Context, namespace string, minutes int) (Timeline, error) {
	now := time.Now()
	if minutes <= 0 {
		minutes = 60
	}
	from := now.Add(-time.Duration(minutes) * time.Minute)
	out := Timeline{From: from.UTC().Format(time.RFC3339), To: now.UTC().Format(time.RFC3339), Events: []TimelineEvent{}}

	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		mapping, err := c.mappingForKind(KindEvents)
		if err != nil {
			return err
		}
		list, err := resourceFor(c.dynamic, mapping, namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		out.Events, out.Truncated = timelineOf(list.Items, from)
		return nil
	})
	if err != nil {
		out.Error = err.Error()
	}
	return out, err
}

// timelineOf keeps the events that happened since from, newest first, capped
// at timelineLimit.
func timelineOf(items []unstructured.Unstructured, from time.Time) ([]TimelineEvent, bool) {
	type placed struct {
		event TimelineEvent
		at    time.Time
	}
	var kept []placed
	for i := range items {
		e := &items[i]
		at := eventTime(e)
		if at.IsZero() || at.Before(from) {
			continue
		}
		first := parseEventTime(nestedString(e, "firstTimestamp"))
		if first.IsZero() {
			first = parseEventTime(nestedString(e, "eventTime"))
		}
		if first.IsZero() || first.After(at) {
			first = at
		}
		count := int(nestedInt(e, "count"))
		if series := nestedInt(e, "series", "count"); series > 0 {
			count = int(series)
		}
		if count < 1 {
			count = 1
		}
		message := nestedString(e, "message")
		if message == "" {
			message = nestedString(e, "note")
		}
		kept = append(kept, placed{
			at: at,
			event: TimelineEvent{
				At:              at.UTC().Format(time.RFC3339),
				First:           first.UTC().Format(time.RFC3339),
				Type:            nestedString(e, "type"),
				Reason:          nestedString(e, "reason"),
				Object:          nestedString(e, "involvedObject", "kind") + "/" + nestedString(e, "involvedObject", "name"),
				ObjectNamespace: nestedString(e, "involvedObject", "namespace"),
				Message:         message,
				Count:           count,
				Namespace:       e.GetNamespace(),
				Name:            e.GetName(),
			},
		})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })

	truncated := len(kept) > timelineLimit
	if truncated {
		kept = kept[:timelineLimit]
	}
	out := make([]TimelineEvent, len(kept))
	for i := range kept {
		out[i] = kept[i].event
	}
	return out, truncated
}
