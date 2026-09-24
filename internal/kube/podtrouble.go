package kube

// What is wrong with a cluster's pods, for the dashboard.
//
// The Pods tile counts running against total, which says that something is
// off but not what: a cluster with fifty evicted pods left lying around and a
// cluster with one pod stuck pulling an image read the same. This says which.

import (
	"fmt"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// podTroubleWorst is how many pods the dashboard lists by name. The counts
// cover the rest; the list is the few worth opening first.
const podTroubleWorst = 8

// recentRestart is how close a restart has to be to count as happening now
// rather than as history.
const recentRestart = time.Hour

// The kinds of trouble a pod on the attention list is in.
const (
	TroubleCrashLoop  = "crashloop"
	TroubleFailed     = "failed"
	TroubleEvicted    = "evicted"
	TroubleRestarting = "restarting"
	TroubleRestarted  = "restarted"
)

// troubleNames maps a severity to the name the frontend is given for it.
var troubleNames = map[int]string{
	troubleCrashLoop:      TroubleCrashLoop,
	troubleFailed:         TroubleFailed,
	troubleEvicted:        TroubleEvicted,
	troubleRecentRestarts: TroubleRestarting,
	troubleOldRestarts:    TroubleRestarted,
}

// Severity of a pod's trouble, for ordering the list. Higher is worse.
const (
	troubleOldRestarts = iota + 1
	troubleRecentRestarts
	troubleEvicted
	troubleFailed
	troubleCrashLoop
)

func podTrouble(pods []unstructured.Unstructured) PodTrouble {
	return podTroubleAt(pods, time.Now())
}

// podTroubleAt is podTrouble with the clock passed in, so a test can say
// what "recent" is measured from.
func podTroubleAt(pods []unstructured.Unstructured, now time.Time) PodTrouble {
	out := PodTrouble{Worst: []PodIssue{}}

	type ranked struct {
		issue    PodIssue
		severity int
		last     time.Time
	}
	var all []ranked

	for i := range pods {
		p := &pods[i]
		restarts := int(podRestartCount(p))
		last := lastTermination(p)
		severity := 0
		reason := ""
		message := ""

		switch {
		case nestedString(p, "status", "phase") == "Failed" && nestedString(p, "status", "reason") == "Evicted":
			out.Evicted++
			severity, reason = troubleEvicted, "Evicted"
			message = nestedString(p, "status", "message")
		case nestedString(p, "status", "phase") == "Failed":
			out.Failed++
			reason = nestedString(p, "status", "reason")
			if reason == "" {
				reason = "Failed"
			}
			severity = troubleFailed
			message = nestedString(p, "status", "message")
		default:
			if wait := badWait(p); wait != "" {
				out.CrashLooping++
				severity, reason = troubleCrashLoop, wait
			}
		}

		recent := !last.finished.IsZero() && now.Sub(last.finished) < recentRestart
		if restarts > 0 {
			out.Restarting++
			out.Restarts += restarts
			if recent {
				out.RestartedRecently++
			}
			if severity == 0 {
				severity, reason = troubleOldRestarts, "Restarted"
				if recent {
					severity, reason = troubleRecentRestarts, "Restarting"
				}
			}
		}

		if severity > 0 {
			issue := PodIssue{
				Namespace:       p.GetNamespace(),
				Name:            p.GetName(),
				Trouble:         troubleNames[severity],
				Reason:          reason,
				Restarts:        restarts,
				Message:         message,
				LastTermination: last.describe(),
			}
			if !last.finished.IsZero() {
				issue.LastRestart = age(int(now.Sub(last.finished).Minutes()))
			}
			all = append(all, ranked{issue: issue, severity: severity, last: last.finished})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].severity != all[j].severity {
			return all[i].severity > all[j].severity
		}
		// Among pods in the same trouble, the one that fell over last is the
		// one to look at: it is the one still doing it.
		if !all[i].last.Equal(all[j].last) {
			return all[i].last.After(all[j].last)
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

// termination is how one container last stopped.
type termination struct {
	container string
	reason    string
	exitCode  int64
	signal    int64
	finished  time.Time
}

// describe renders a termination as a reader wants it: the reason the
// kubelet gave, and the exit code when the reason alone does not say it.
func (t termination) describe() string {
	if t.reason == "" && t.exitCode == 0 {
		return ""
	}
	reason := t.reason
	if reason == "" {
		reason = "Terminated"
	}
	switch {
	case t.exitCode != 0:
		return fmt.Sprintf("%s (exit %d)", reason, t.exitCode)
	case t.signal != 0:
		return fmt.Sprintf("%s (signal %d)", reason, t.signal)
	}
	return reason
}

// lastTermination is the most recent time any of a pod's containers stopped,
// read from each container's lastState -- which the kubelet keeps for exactly
// one restart back, and which is the only record the API has of why.
func lastTermination(u *unstructured.Unstructured) termination {
	var latest termination
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, raw := range nestedSlice(u, "status", field) {
			status := asMap(raw)
			if mapInt(status, "restartCount") == 0 {
				continue
			}
			term := asMap(asMap(status["lastState"])["terminated"])
			if term == nil {
				continue
			}
			at := parseTime(mapString(term, "finishedAt"))
			if !latest.finished.IsZero() && !at.After(latest.finished) {
				continue
			}
			latest = termination{
				container: mapString(status, "name"),
				reason:    mapString(term, "reason"),
				exitCode:  mapInt(term, "exitCode"),
				signal:    mapInt(term, "signal"),
				finished:  at,
			}
		}
	}
	return latest
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

// describeRestarts writes a pod's restarts as the describe panel's first
// section, container by container: how many, why the last one stopped, and
// when. All of it is in the status YAML further down, but spread over three
// nested fields per container, and "why does this keep restarting" is the
// question a pod's report is most often opened to answer.
//
// Nothing is written for a pod that has never restarted.
func describeRestarts(d *describer, u *unstructured.Unstructured) {
	type row struct {
		name, count, last, when string
	}
	var rows []row
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, raw := range nestedSlice(u, "status", field) {
			status := asMap(raw)
			n := mapInt(status, "restartCount")
			if n == 0 {
				continue
			}
			term := asMap(asMap(status["lastState"])["terminated"])
			t := termination{
				reason:   mapString(term, "reason"),
				exitCode: mapInt(term, "exitCode"),
				signal:   mapInt(term, "signal"),
			}
			last := t.describe()
			if last == "" {
				last = "<unknown>"
			}
			rows = append(rows, row{
				name:  mapString(status, "name"),
				count: fmt.Sprintf("%d", n),
				last:  last,
				when:  since(mapString(term, "finishedAt")),
			})
		}
	}
	if len(rows) == 0 {
		return
	}
	d.section("Restarts")
	for _, r := range rows {
		d.line(2, "%-24s %-5s last %-26s %s ago", r.name, r.count, r.last, r.when)
	}
	d.blank()
}
