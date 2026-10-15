package kube

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/jsonpath"
)

// This file holds the column values that cannot be read straight out of a
// field: counts across a pod's containers, a workload's condition, a service's
// port list. Everything simpler is a JSONPath in columns.go.

// evalPath renders a kubectl-style JSONPath against an object. An expression
// that matches nothing yields "", which is what the tables want -- a missing
// optional field is not an error worth showing in a cell.
func evalPath(u *unstructured.Unstructured, path string) string {
	if path == "" {
		return ""
	}
	jp := jsonpath.New("column").AllowMissingKeys(true)
	if err := jp.Parse("{" + path + "}"); err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := jp.Execute(&buf, u.Object); err != nil {
		return ""
	}
	return buf.String()
}

// ratio renders "ready/desired" and tones it by how far short it falls.
func ratio(ready, desired int64) Cell {
	text := fmt.Sprintf("%d/%d", ready, desired)
	switch {
	case desired == 0 && ready == 0:
		return muted(text)
	case ready >= desired:
		return toned(text, "ok")
	case ready == 0:
		return toned(text, "error")
	default:
		return toned(text, "warn")
	}
}

// parseTime reads an RFC3339 timestamp, returning the zero time for anything
// missing or unparseable so the cell renders as "<none>" rather than as noise.
func parseTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}

// since renders an RFC3339 timestamp as an age, for the report text that has
// nowhere to carry a sort key.
func since(ts string) string {
	t := parseTime(ts)
	if t.IsZero() {
		return "<none>"
	}
	return age(int(time.Since(t).Minutes()))
}

// joinMap renders a label or selector map deterministically.
func joinMap(m map[string]string) string {
	if len(m) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// joinAny renders the first few entries of a string list, saying how many more
// there are rather than overflowing the cell.
func joinAny(items []any, limit int) string {
	if len(items) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			parts = append(parts, s)
		}
	}
	return joinStrings(parts, limit)
}

// joinStrings is joinAny for a list already built as strings.
func joinStrings(parts []string, limit int) string {
	if len(parts) == 0 {
		return "<none>"
	}
	if len(parts) > limit {
		return strings.Join(parts[:limit], ", ") + fmt.Sprintf(" +%d more", len(parts)-limit)
	}
	return strings.Join(parts, ", ")
}

// ---- pods ------------------------------------------------------------------

// podReady is the ready count kubectl shows: how many of the pod's containers
// are passing their readiness check, out of how many it has.
//
// The colouring is the part worth explaining. For a running pod the usual ratio
// rules apply -- all of them ready is good, none of them is bad -- but a pod
// that has finished has no ready containers and never will, because its
// containers have exited. Toned as a ratio, every completed Job pod in the
// namespace reads as a broken one, and in a namespace that runs a CronJob every
// few minutes that is most of the list.
//
// So a finished pod's count is stated rather than judged. What actually
// happened to it is in the Status column, which reads Succeeded or Failed and
// is coloured for it -- a failed pod is still red there.
func podReady(u *unstructured.Unstructured) Cell {
	statuses := nestedSlice(u, "status", "containerStatuses")
	ready := 0
	for _, raw := range statuses {
		if isReady, _ := asMap(raw)["ready"].(bool); isReady {
			ready++
		}
	}
	total := len(statuses)
	if total == 0 {
		total = len(nestedSlice(u, "spec", "containers"))
	}
	// terminal is the budget's own test for a finished pod, and the same one
	// belongs here: the phase, not "was it made by a Job". A bare pod with a
	// restartPolicy of Never that runs to completion is in exactly the same
	// position as a Job's, and nothing here has to know which it was.
	if terminal(u) {
		return muted(fmt.Sprintf("%d/%d", ready, total))
	}
	return ratio(int64(ready), int64(total))
}

// podStatus reproduces the status kubectl shows, which is not simply the phase:
// a pod stuck pulling an image is Pending with a container waiting on
// ImagePullBackOff, and the waiting reason is the useful half.
func podStatus(u *unstructured.Unstructured) Cell {
	if u.GetDeletionTimestamp() != nil {
		return status("Terminating")
	}
	for _, raw := range nestedSlice(u, "status", "containerStatuses") {
		state := asMap(asMap(raw)["state"])
		if waiting := asMap(state["waiting"]); waiting != nil {
			if reason := mapString(waiting, "reason"); reason != "" {
				return status(reason)
			}
		}
		if term := asMap(state["terminated"]); term != nil {
			if reason := mapString(term, "reason"); reason != "" && reason != "Completed" {
				return status(reason)
			}
		}
	}
	if reason := nestedString(u, "status", "reason"); reason != "" {
		return status(reason)
	}
	return status(nestedString(u, "status", "phase"))
}

// podRestarts is the pod's restart count as kubectl and k9s show it: every
// restart of the app's containers, not its init containers, since the pod was
// made.
//
// A total cannot say by itself whether anything is wrong now: control-plane
// pods that came back after a node reboot two weeks ago read "2" for as long as
// they live, on a cluster that is fine. So the number is the same as k9s's, but
// it is only toned as trouble while the restarts are current -- the last one
// within recentRestart, or a container stuck in CrashLoopBackOff or the like.
func podRestarts(u *unstructured.Unstructured) Cell {
	return podRestartsAt(u, time.Now())
}

// podRestartsAt is podRestarts with the clock passed in, for tests.
func podRestartsAt(u *unstructured.Unstructured, now time.Time) Cell {
	var total int64
	for _, raw := range nestedSlice(u, "status", "containerStatuses") {
		total += mapInt(asMap(raw), "restartCount")
	}
	cell := Cell{Text: fmt.Sprintf("%d", total), Sort: fmt.Sprintf("%08d", total)}
	if total == 0 {
		return cell
	}
	last := lastTermination(u).finished
	recent := !last.IsZero() && now.Sub(last) < recentRestart
	switch {
	case !recent && badWait(u) == "":
		// History: stated, not judged.
	case total > 5:
		cell.Tone = "error"
	default:
		cell.Tone = "warn"
	}
	return cell
}

// ---- workloads -------------------------------------------------------------

func firstImage(u *unstructured.Unstructured) Cell {
	containers := nestedSlice(u, "spec", "template", "spec", "containers")
	if len(containers) == 0 {
		return muted("")
	}
	image := mapString(asMap(containers[0]), "image")
	if len(containers) > 1 {
		image += fmt.Sprintf(" +%d more", len(containers)-1)
	}
	return muted(image)
}

func workloadCondition(u *unstructured.Unstructured) Cell {
	ready := nestedInt(u, "status", "readyReplicas")
	desired := nestedInt(u, "spec", "replicas")
	switch {
	case desired == 0:
		return muted("Scaled to zero")
	case ready >= desired:
		return toned("Available", "ok")
	case ready == 0:
		return toned("Unavailable", "error")
	default:
		return toned("Progressing", "warn")
	}
}

func jobDuration(u *unstructured.Unstructured) Cell {
	start := nestedString(u, "status", "startTime")
	if start == "" {
		return muted("")
	}
	from, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return muted("")
	}
	until := time.Now()
	if done := parseTime(nestedString(u, "status", "completionTime")); !done.IsZero() {
		until = done
	}
	return durationCell(until.Sub(from))
}

func jobCondition(u *unstructured.Unstructured) Cell {
	switch {
	case conditionStatus(u, "Complete", "status", "conditions") == "True":
		return status("Complete")
	case conditionStatus(u, "Failed", "status", "conditions") == "True":
		return status("Failed")
	case nestedInt(u, "status", "active") > 0:
		return status("Running")
	default:
		return muted("Pending")
	}
}

// ---- services and ingresses ------------------------------------------------

func servicePorts(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "spec", "ports") {
		p := asMap(raw)
		port, _ := p["port"].(int64)
		proto := mapString(p, "protocol")
		if proto == "" {
			proto = "TCP"
		}
		if node, ok := p["nodePort"].(int64); ok && node > 0 {
			parts = append(parts, fmt.Sprintf("%d:%d/%s", port, node, proto))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d/%s", port, proto))
	}
	if len(parts) == 0 {
		return muted("<none>")
	}
	return muted(strings.Join(parts, ", "))
}

func serviceExternalIP(u *unstructured.Unstructured) Cell {
	if external := nestedSlice(u, "spec", "externalIPs"); len(external) > 0 {
		return muted(joinAny(external, 2))
	}
	return loadBalancerAddress(u)
}

// loadBalancerAddress reads the address a load balancer or ingress controller
// has assigned, which may be an IP or a hostname depending on the provider.
func loadBalancerAddress(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "status", "loadBalancer", "ingress") {
		e := asMap(raw)
		if ip := mapString(e, "ip"); ip != "" {
			parts = append(parts, ip)
			continue
		}
		if host := mapString(e, "hostname"); host != "" {
			parts = append(parts, host)
		}
	}
	if len(parts) == 0 {
		return muted("<pending>")
	}
	return muted(strings.Join(parts, ", "))
}

// ingressHosts lists the hosts an Ingress answers on, each opening in the
// browser. A host its TLS section covers is opened over https, and any other
// over http, since that is all the Ingress itself promises.
func ingressHosts(u *unstructured.Unstructured) Cell {
	var secure []string
	for _, raw := range nestedSlice(u, "spec", "tls") {
		for _, h := range nestedSlice(&unstructured.Unstructured{Object: asMap(raw)}, "hosts") {
			if s, ok := h.(string); ok {
				secure = append(secure, s)
			}
		}
	}
	var links []Link
	for _, raw := range nestedSlice(u, "spec", "rules") {
		host := mapString(asMap(raw), "host")
		if host == "" {
			continue
		}
		scheme := "http"
		if hostCovered(host, secure) {
			scheme = "https"
		}
		links = append(links, Link{Text: host, URL: webURL(scheme, host, 0)})
	}
	if len(links) == 0 {
		return muted("*")
	}
	return linked(links, 3)
}

// hostCovered reports whether a host is one of a list of names, where a name
// may be a wildcard covering one label: "*.example.com" covers
// "shop.example.com" but not "example.com".
func hostCovered(host string, names []string) bool {
	for _, name := range names {
		if name == host {
			return true
		}
		if suffix, ok := strings.CutPrefix(name, "*"); ok {
			if label, found := strings.CutSuffix(host, suffix); found && label != "" && !strings.Contains(label, ".") {
				return true
			}
		}
	}
	return false
}

// webURL is where a browser should go for a host: the scheme, the host, and
// the port only when it is not the scheme's own. A wildcard has no address.
func webURL(scheme, host string, port int64) string {
	if host == "" || strings.Contains(host, "*") {
		return ""
	}
	defaultPort := (scheme == "https" && port == 443) || (scheme == "http" && port == 80)
	if port > 0 && !defaultPort {
		host = fmt.Sprintf("%s:%d", host, port)
	}
	return scheme + "://" + host
}

// linked is a cell of entries that open in the browser. Like joinAny it stops
// at a few, and the count of the rest is an entry of its own that opens
// nothing, so the text and what is drawn stay the same list.
func linked(links []Link, limit int) Cell {
	if len(links) > limit {
		links = append(links[:limit:limit], Link{Text: fmt.Sprintf("+%d more", len(links)-limit)})
	}
	texts := make([]string, len(links))
	for i, l := range links {
		texts[i] = l.Text
	}
	return Cell{Text: strings.Join(texts, ", "), Links: links}
}

// ---- nodes -----------------------------------------------------------------

// nodeRoles reads the roles off the well-known label prefix, the same place
// kubectl looks -- a node's role is a label convention, not a field.
func nodeRoles(u *unstructured.Unstructured) Cell {
	var roles []string
	for k, v := range u.GetLabels() {
		if role, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok && role != "" {
			roles = append(roles, role)
			continue
		}
		if k == "kubernetes.io/role" && v != "" {
			roles = append(roles, v)
		}
	}
	if len(roles) == 0 {
		return muted("<none>")
	}
	sort.Strings(roles)
	return plain(strings.Join(roles, ", "))
}

// nodeAddress reads every address of one type off a node -- InternalIP or
// ExternalIP, the two `kubectl get nodes -o wide` shows. kubectl prints the
// first of each; every one is shown here, since a dual-stack node has an IPv4
// and an IPv6 internal address and neither is the spare.
func nodeAddress(kind string) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		var out []string
		for _, raw := range nestedSlice(u, "status", "addresses") {
			a := asMap(raw)
			if mapString(a, "type") != kind {
				continue
			}
			if v := mapString(a, "address"); v != "" {
				out = append(out, v)
			}
		}
		if len(out) == 0 {
			return muted("<none>")
		}
		return plain(strings.Join(out, ", "))
	}
}

// nodeCondition says everything about a node's state worth a look, not just the
// first thing: whether it is Ready, whether it is cordoned, and every other
// condition that is True. kubectl prints the first two joined,
// "Ready,SchedulingDisabled"; the rest are added here because for a node every
// condition but Ready is bad news when True -- MemoryPressure, DiskPressure,
// PIDPressure, NetworkUnavailable, and whatever node-problem-detector reports
// -- and a node under pressure is evicting pods while still reading Ready.
func nodeCondition(u *unstructured.Unstructured) Cell {
	var tags []Tag
	switch conditionStatus(u, "Ready", "status", "conditions") {
	case "True":
		tags = append(tags, Tag{Text: "Ready", Tone: "ok"})
	case "False":
		tags = append(tags, Tag{Text: "NotReady", Tone: "error"})
	default:
		tags = append(tags, Tag{Text: "Unknown", Tone: "error"})
	}

	if unschedulable, _, _ := unstructured.NestedBool(u.Object, "spec", "unschedulable"); unschedulable {
		tags = append(tags, Tag{Text: "SchedulingDisabled", Tone: "warn"})
	}

	for _, raw := range nestedSlice(u, "status", "conditions") {
		c := asMap(raw)
		kind := mapString(c, "type")
		if kind == "" || kind == "Ready" || mapString(c, "status") != "True" {
			continue
		}
		// A node with no network runs nothing that works; pressure is a node
		// still working, and shedding pods to stay that way.
		tone := "warn"
		if kind == "NetworkUnavailable" {
			tone = "error"
		}
		tags = append(tags, Tag{Text: kind, Tone: tone})
	}

	return tagged(tags)
}

// ---- Gateway API -----------------------------------------------------------

func gatewayAddress(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "status", "addresses") {
		if v := mapString(asMap(raw), "value"); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return muted("<pending>")
	}
	return muted(strings.Join(parts, ", "))
}

// parentRefName renders a reference to a Gateway the way it was written: the
// namespace only when it is not the referrer's own, and the listener it picks
// out, if any.
func parentRefName(p map[string]any) string {
	name := mapString(p, "name")
	if name == "" {
		return ""
	}
	if ns := mapString(p, "namespace"); ns != "" {
		name = ns + "/" + name
	}
	if section := mapString(p, "sectionName"); section != "" {
		name += "#" + section
	}
	return name
}

// routeParents lists the Gateways a route has attached itself to, which is the
// question anyone opening a route list is actually asking.
func routeParents(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "spec", "parentRefs") {
		if name := parentRefName(asMap(raw)); name != "" {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return muted("<none>")
	}
	return muted(strings.Join(parts, ", "))
}

// routeBackends lists where a route sends its traffic, across all of its rules:
// "db:5432", or "data/db:5432" for a backend in another namespace.
func routeBackends(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, rule := range nestedSlice(u, "spec", "rules") {
		for _, raw := range nestedSlice(&unstructured.Unstructured{Object: asMap(rule)}, "backendRefs") {
			b := asMap(raw)
			name := mapString(b, "name")
			if name == "" {
				continue
			}
			if ns := mapString(b, "namespace"); ns != "" {
				name = ns + "/" + name
			}
			if port := mapInt(b, "port"); port > 0 {
				name += fmt.Sprintf(":%d", port)
			}
			parts = append(parts, name)
		}
	}
	return muted(joinStrings(parts, 3))
}

// listenerSetParent names the Gateway a ListenerSet adds its listeners to.
// Unlike a route it has exactly one.
func listenerSetParent(u *unstructured.Unstructured) Cell {
	if name := parentRefName(asMap(nestedMap(u, "spec", "parentRef"))); name != "" {
		return muted(name)
	}
	return muted("<none>")
}

// listenerSetListeners renders each listener as "shop.example.com:443/HTTPS".
// The hostname leads because handing a team its own hostnames on a shared
// Gateway is what a ListenerSet is for, and an HTTP or HTTPS listener with one
// opens in the browser.
func listenerSetListeners(u *unstructured.Unstructured) Cell {
	var links []Link
	for _, raw := range nestedSlice(u, "spec", "listeners") {
		l := asMap(raw)
		port, protocol, host := mapInt(l, "port"), mapString(l, "protocol"), mapString(l, "hostname")
		link := Link{Text: fmt.Sprintf("%d/%s", port, protocol)}
		if host != "" {
			link.Text = host + ":" + link.Text
		}
		if protocol == "HTTP" || protocol == "HTTPS" {
			link.URL = webURL(strings.ToLower(protocol), host, port)
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return muted("<none>")
	}
	cell := linked(links, 3)
	cell.Tone = "info"
	return cell
}

// httpRouteHostnames lists the hostnames an HTTPRoute answers on, each opening
// in the browser.
func httpRouteHostnames(u *unstructured.Unstructured) Cell {
	var links []Link
	scheme := routeScheme(u)
	for _, raw := range nestedSlice(u, "spec", "hostnames") {
		if host, ok := raw.(string); ok && host != "" {
			links = append(links, Link{Text: host, URL: webURL(scheme, host, 0)})
		}
	}
	if len(links) == 0 {
		return plain("<none>")
	}
	return linked(links, 3)
}

// routeScheme guesses how an HTTPRoute is reached. Whether it is served over
// TLS is a property of the Gateway listener it attaches to, not of the route,
// and looking that up is a second object per row -- so the answer is https
// unless every parent is plainly an HTTP listener, by port or by name.
func routeScheme(u *unstructured.Unstructured) string {
	parents := nestedSlice(u, "spec", "parentRefs")
	if len(parents) == 0 {
		return "https"
	}
	for _, raw := range parents {
		p := asMap(raw)
		if mapInt(p, "port") != 80 && !strings.EqualFold(mapString(p, "sectionName"), "http") {
			return "https"
		}
	}
	return "http"
}

// policyTargets names what a policy is attached to. The kind is kept because a
// target need not be a Service.
func policyTargets(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "spec", "targetRefs") {
		t := asMap(raw)
		name := mapString(t, "name")
		if name == "" {
			continue
		}
		if kind := mapString(t, "kind"); kind != "" {
			name = kind + "/" + name
		}
		if section := mapString(t, "sectionName"); section != "" {
			name += "#" + section
		}
		parts = append(parts, name)
	}
	return muted(joinStrings(parts, 3))
}

// policyAccepted folds a policy's status into one answer. A policy is reported
// on once per ancestor -- each Gateway whose routes lead to its target -- so it
// is accepted only when every one of them says so, and a single refusal is the
// thing worth showing.
func policyAccepted(u *unstructured.Unstructured) Cell {
	ancestors := nestedSlice(u, "status", "ancestors")
	accepted := 0
	for _, raw := range ancestors {
		switch conditionStatus(&unstructured.Unstructured{Object: asMap(raw)}, "Accepted", "conditions") {
		case "False":
			return toned("False", "error")
		case "True":
			accepted++
		}
	}
	if len(ancestors) > 0 && accepted == len(ancestors) {
		return toned("True", "ok")
	}
	return muted("Unknown")
}

func referenceGrantFrom(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "spec", "from") {
		f := asMap(raw)
		parts = append(parts, mapString(f, "kind")+" in "+mapString(f, "namespace"))
	}
	if len(parts) == 0 {
		return muted("<none>")
	}
	return muted(strings.Join(parts, ", "))
}

// crdVersions lists the versions a CRD serves, marking the storage version,
// since that is the one whose schema actually persists.
func crdVersions(u *unstructured.Unstructured) Cell {
	var parts []string
	for _, raw := range nestedSlice(u, "spec", "versions") {
		v := asMap(raw)
		name := mapString(v, "name")
		if name == "" {
			continue
		}
		if stored, _ := v["storage"].(bool); stored {
			name += "*"
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return muted("")
	}
	return plain(strings.Join(parts, ", "))
}

// ---- resource quantities ---------------------------------------------------

// parseCPU reads a CPU quantity ("500m", "2") as a number of cores.
func parseCPU(s string) float64 {
	if s == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.AsApproximateFloat64()
}

// parseMemory reads a memory quantity ("512Mi", "8Gi") as GiB, the unit the
// dashboard gauges are labelled in.
func parseMemory(s string) float64 {
	if s == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.AsApproximateFloat64() / (1 << 30)
}
