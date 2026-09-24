package kube

// A cluster's health in a few numbers, for the fleet view, the sidebar and
// the desktop notifications.
//
// The dashboard already knows all of this about one cluster; what it cannot do
// is say it about fifteen at once. This is the part of the dashboard worth
// asking every cluster for on a timer: whether its nodes are up, what is wrong
// with its pods, and how much it has been complaining -- and none of the parts
// that are only worth reading once somebody has opened it.

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// warningWindow is how far back a warning event counts towards a cluster's
// health. An hour: long enough that a problem does not vanish from the count
// between two looks, short enough that last night's rollout is not still
// being held against the cluster this afternoon.
const warningWindow = time.Hour

// warningReasons is how many of the commonest reasons are named.
const warningReasons = 5

// ClusterHealth is one cluster's health, as the fleet view lists it.
type ClusterHealth struct {
	ContextID  string `json:"contextId"`
	NodesReady int    `json:"nodesReady"`
	NodesTotal int    `json:"nodesTotal"`
	// NotReadyNodes names the nodes that are not Ready, so a notification
	// can say which rather than how many.
	NotReadyNodes []string `json:"notReadyNodes"`
	PodsRunning   int      `json:"podsRunning"`
	PodsTotal     int      `json:"podsTotal"`
	// Pods is what is wrong with the pods, the same as the dashboard's.
	Pods PodTrouble `json:"pods"`
	// Warnings is how many warning events the cluster has recorded in the
	// last hour, counting an event's repeats, and WarningReasons the
	// commonest reasons among them.
	Warnings       int           `json:"warnings"`
	WarningReasons []ReasonCount `json:"warningReasons"`
	// Error is why the cluster could not be read at all. The counts are
	// zero when it is set.
	Error string `json:"error"`
}

// ReasonCount is one event reason and how often it came up.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Health reads one cluster's health: its nodes, its pods and its recent
// warnings. Events are allowed to fail -- a cluster that denies them still has
// nodes and pods worth reporting -- but nodes and pods are not.
func (w *Watcher) Health(kc Context) (ClusterHealth, error) {
	out := ClusterHealth{
		ContextID:      kc.ID,
		NotReadyNodes:  []string{},
		Pods:           PodTrouble{Worst: []PodIssue{}},
		WarningReasons: []ReasonCount{},
	}

	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		// Three reads at once, for the reason Overview makes its own so.
		var (
			nodes, pods, events         []unstructured.Unstructured
			nodesErr, podsErr, eventErr error
		)
		parallel(
			func() { nodes, _, nodesErr = c.list(ctx, KindNodes, metav1.ListOptions{}) },
			func() { pods, _, podsErr = c.list(ctx, KindPods, metav1.ListOptions{}) },
			// Only the warnings: the field selector is served by the API
			// server, so a cluster with a hundred thousand Normal events does
			// not send them all to be thrown away here.
			func() {
				events, _, eventErr = c.list(ctx, KindEvents, metav1.ListOptions{FieldSelector: "type=Warning", Limit: 2000})
			},
		)
		if nodesErr != nil {
			return nodesErr
		}
		if podsErr != nil {
			return podsErr
		}

		out.NodesTotal = len(nodes)
		for i := range nodes {
			if conditionStatus(&nodes[i], "Ready", "status", "conditions") == "True" {
				out.NodesReady++
			} else {
				out.NotReadyNodes = append(out.NotReadyNodes, nodes[i].GetName())
			}
		}
		sort.Strings(out.NotReadyNodes)

		out.PodsTotal = len(pods)
		for i := range pods {
			if nestedString(&pods[i], "status", "phase") == "Running" {
				out.PodsRunning++
			}
		}
		out.Pods = podTrouble(pods)

		// Events are allowed to fail -- see above.
		if eventErr == nil {
			out.Warnings, out.WarningReasons = recentWarnings(events, time.Now())
		}
		return nil
	})

	if err != nil {
		out.Error = err.Error()
	}
	return out, err
}

// recentWarnings counts the warning events seen within warningWindow of now,
// and names the commonest reasons among them.
func recentWarnings(events []unstructured.Unstructured, now time.Time) (int, []ReasonCount) {
	byReason := map[string]int{}
	total := 0
	for i := range events {
		e := &events[i]
		if nestedString(e, "type") != "Warning" {
			continue
		}
		at := eventTime(e)
		if at.IsZero() || now.Sub(at) > warningWindow {
			continue
		}
		n := int(nestedInt(e, "count"))
		if series := nestedInt(e, "series", "count"); series > 0 {
			n = int(series)
		}
		if n < 1 {
			n = 1
		}
		total += n
		byReason[nestedString(e, "reason")] += n
	}

	reasons := make([]ReasonCount, 0, len(byReason))
	for reason, count := range byReason {
		reasons = append(reasons, ReasonCount{Reason: reason, Count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Count != reasons[j].Count {
			return reasons[i].Count > reasons[j].Count
		}
		return reasons[i].Reason < reasons[j].Reason
	})
	if len(reasons) > warningReasons {
		reasons = reasons[:warningReasons]
	}
	return total, reasons
}

// eventTime is when an event last happened, from whichever of the several
// places the two event APIs keep it is filled in.
func eventTime(e *unstructured.Unstructured) time.Time {
	for _, field := range [][]string{
		{"series", "lastObservedTime"},
		{"lastTimestamp"},
		{"eventTime"},
		{"firstTimestamp"},
	} {
		if t := parseEventTime(nestedString(e, field...)); !t.IsZero() {
			return t
		}
	}
	return e.GetCreationTimestamp().Time
}

// parseEventTime reads an event's timestamps, which come in two precisions:
// seconds for the old fields and microseconds for eventTime and the series.
func parseEventTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t
	}
	return time.Time{}
}

// EvictedPods lists every evicted pod in a cluster, for the dashboard's
// clean-up button to delete. The phase is filtered by the API server and the
// reason here, because a field selector can say Failed but not why.
func (w *Watcher) EvictedPods(kc Context) ([]ObjectRef, error) {
	out := []ObjectRef{}
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		pods, _, err := c.list(ctx, KindPods, metav1.ListOptions{FieldSelector: "status.phase=Failed"})
		if err != nil {
			return err
		}
		out = evictedAmong(pods)
		return nil
	})
	return out, err
}

func evictedAmong(pods []unstructured.Unstructured) []ObjectRef {
	out := []ObjectRef{}
	for i := range pods {
		p := &pods[i]
		if nestedString(p, "status", "phase") == "Failed" && nestedString(p, "status", "reason") == "Evicted" {
			out = append(out, ObjectRef{Namespace: p.GetNamespace(), Name: p.GetName()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}
