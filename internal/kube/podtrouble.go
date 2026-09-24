package kube

// What is wrong with a cluster's pods, for the dashboard.
//
// The Pods tile counts running against total, which says that something is
// off but not what: a cluster with fifty evicted pods left lying around and a
// cluster with one pod stuck pulling an image read the same. This says which.

import (
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// podTroubleWorst is how many pods the dashboard lists by name. The counts
// cover the rest; the list is the few worth opening first.
const podTroubleWorst = 8

// Severity of a pod's trouble, for ordering the list. Higher is worse.
const (
	troubleRestarts = iota + 1
	troubleEvicted
	troubleFailed
	troubleCrashLoop
)

func podTrouble(pods []unstructured.Unstructured) PodTrouble {
	out := PodTrouble{Worst: []PodIssue{}}

	type ranked struct {
		issue    PodIssue
		severity int
	}
	var all []ranked

	for i := range pods {
		p := &pods[i]
		restarts := int(podRestartCount(p))
		severity := 0
		reason := ""

		switch {
		case nestedString(p, "status", "phase") == "Failed" && nestedString(p, "status", "reason") == "Evicted":
			out.Evicted++
			severity, reason = troubleEvicted, "Evicted"
		case nestedString(p, "status", "phase") == "Failed":
			out.Failed++
			reason = nestedString(p, "status", "reason")
			if reason == "" {
				reason = "Failed"
			}
			severity = troubleFailed
		default:
			if wait := badWait(p); wait != "" {
				out.CrashLooping++
				severity, reason = troubleCrashLoop, wait
			}
		}

		if restarts > 0 {
			out.Restarting++
			out.Restarts += restarts
			if severity == 0 {
				severity, reason = troubleRestarts, "Restarting"
			}
		}

		if severity > 0 {
			all = append(all, ranked{
				issue: PodIssue{
					Namespace: p.GetNamespace(),
					Name:      p.GetName(),
					Reason:    reason,
					Restarts:  restarts,
				},
				severity: severity,
			})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].severity != all[j].severity {
			return all[i].severity > all[j].severity
		}
		if all[i].issue.Restarts != all[j].issue.Restarts {
			return all[i].issue.Restarts > all[j].issue.Restarts
		}
		if all[i].issue.Namespace != all[j].issue.Namespace {
			return all[i].issue.Namespace < all[j].issue.Namespace
		}
		return all[i].issue.Name < all[j].issue.Name
	})
	for i := range all {
		if i == podTroubleWorst {
			break
		}
		out.Worst = append(out.Worst, all[i].issue)
	}
	return out
}

// podRestartCount adds up the restarts of a pod's containers, init
// containers included: an init container that keeps failing holds the pod
// back just as surely.
func podRestartCount(u *unstructured.Unstructured) int64 {
	var total int64
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, raw := range nestedSlice(u, "status", field) {
			total += mapInt(asMap(raw), "restartCount")
		}
	}
	return total
}

// badWait is the reason one of the pod's containers is waiting and not coming
// up on its own, or empty when none is.
func badWait(u *unstructured.Unstructured) string {
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, raw := range nestedSlice(u, "status", field) {
			state := asMap(asMap(raw)["state"])
			if reason := mapString(asMap(state["waiting"]), "reason"); badWaits[reason] {
				return reason
			}
		}
	}
	return ""
}
