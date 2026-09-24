package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The one-shot reads: everything the UI asks for once and does not watch. They
// borrow a client from the same pool the informers use, so a context already
// open in a tab costs no second connection and no second credential exec.

// withClient runs fn against a context's live client, taking and releasing a
// reference around it.
func (w *Watcher) withClient(kc Context, fn func(*clusterClient) error) error {
	cl, err := w.clusterFor(kc)
	if err != nil {
		return err
	}
	defer w.releaseCluster(kc.ID, cl)
	return fn(cl.client)
}

// list fetches a collection in one call, for the views that do not watch.
func (c *clusterClient) list(ctx context.Context, kind string, opts metav1.ListOptions) ([]unstructured.Unstructured, bool, error) {
	mapping, err := c.mappingForKind(kind)
	if err != nil {
		return nil, false, err
	}
	l, err := c.dynamic.Resource(mapping.Resource).List(ctx, opts)
	if err != nil {
		return nil, false, err
	}
	return l.Items, mapping.Scope.Name() == "namespace", nil
}

// Namespaces lists the namespaces of a cluster, for the tab's namespace filter.
func (w *Watcher) Namespaces(kc Context) ([]string, error) {
	var out []string
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		items, _, err := c.list(ctx, KindNamespaces, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range items {
			out = append(out, items[i].GetName())
		}
		sort.Strings(out)
		return nil
	})
	return out, err
}

// CustomResourceKind describes one CRD well enough for the frontend to open a
// tab on it without asking the backend what it is called.
type CustomResourceKind struct {
	Kind   string `json:"kind"`   // the "crd:" string a tab is opened with
	Label  string `json:"label"`  // the CRD's own plural display name
	Group  string `json:"group"`  //
	Plural string `json:"plural"` //
	Scoped bool   `json:"scoped"` // namespaced rather than cluster-wide
}

// CustomResourceKindFor reports what a row in the CRD table opens: it turns the
// definition's name into the kind string and label its instance tab will use.
func CustomResourceKindFor(crd *unstructured.Unstructured) CustomResourceKind {
	plural := nestedString(crd, "spec", "names", "plural")
	group := nestedString(crd, "spec", "group")

	label := nestedString(crd, "spec", "names", "kind")
	if label == "" {
		label = plural
	}
	return CustomResourceKind{
		Kind:   CustomKind(plural, group),
		Label:  label,
		Group:  group,
		Plural: plural,
		Scoped: nestedString(crd, "spec", "scope") == "Namespaced",
	}
}

// Overview is the dashboard payload for one context, read live.
func (w *Watcher) Overview(kc Context) (Overview, error) {
	out := Overview{
		ContextID: kc.ID,
		Context:   kc.Name,
		Cluster:   kc.Cluster,
		Server:    kc.Server,
		Stats:     []Stat{},
		Pods:      PodTrouble{Worst: []PodIssue{}},
		Events:    Table{Kind: KindEvents, Columns: []string{}, Rows: []Row{}},
	}

	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		if v, err := c.disco.ServerVersion(); err == nil {
			out.Version = v.GitVersion
		}
		out.Server = c.host

		namespaces, _, err := c.list(ctx, KindNamespaces, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range namespaces {
			out.Namespaces = append(out.Namespaces, namespaces[i].GetName())
		}
		sort.Strings(out.Namespaces)

		nodes, _, err := c.list(ctx, KindNodes, metav1.ListOptions{})
		if err != nil {
			return err
		}
		nodesReady := 0
		for i := range nodes {
			n := &nodes[i]
			if conditionStatus(n, "Ready", "status", "conditions") == "True" {
				nodesReady++
			}
		}
		out.Distribution = distributionOf(nodes)

		pods, _, err := c.list(ctx, KindPods, metav1.ListOptions{})
		if err != nil {
			return err
		}
		podsRunning := 0
		for i := range pods {
			if nestedString(&pods[i], "status", "phase") == "Running" {
				podsRunning++
			}
		}
		out.Pods = podTrouble(pods)

		deployTotal, deployReady := 0, 0
		if deployments, _, err := c.list(ctx, KindDeployments, metav1.ListOptions{}); err == nil {
			for i := range deployments {
				d := &deployments[i]
				deployTotal++
				if nestedInt(d, "status", "readyReplicas") >= nestedInt(d, "spec", "replicas") {
					deployReady++
				}
			}
		}

		out.Stats = []Stat{
			{Label: "Nodes", Kind: KindNodes, Ready: nodesReady, Total: len(nodes)},
			{Label: "Pods", Kind: KindPods, Ready: podsRunning, Total: len(pods)},
			{Label: "Deployments", Kind: KindDeployments, Ready: deployReady, Total: deployTotal},
			{Label: "Namespaces", Kind: KindNamespaces, Ready: len(out.Namespaces), Total: len(out.Namespaces)},
		}
		// Capacity and requests are deliberately not computed here: they are
		// the Budget's, which counts them for a node and a namespace as well
		// and by the scheduler's own rules. Two implementations of the same
		// sum would eventually disagree.

		out.Events = c.eventTable(ctx)
		return nil
	})

	if err != nil {
		out.Error = err.Error()
	}
	return out, err
}

// dashboardEvents is how many rows the dashboard panel shows. Enough to be
// worth sorting, few enough not to become the whole page.
const dashboardEvents = 12

// eventTable is the dashboard's events panel: the same table the events tab
// renders, through the same columns and the same ordering, then capped.
//
// A failure here costs the panel and nothing else -- a cluster that denies
// access to events should still show its nodes and pods.
func (c *clusterClient) eventTable(ctx context.Context) Table {
	empty := Table{Kind: KindEvents, Columns: []string{}, Rows: []Row{}}

	items, namespaced, err := c.list(ctx, KindEvents, metav1.ListOptions{Limit: 500})
	if err != nil {
		empty.Error = err.Error()
		return empty
	}
	cols, err := c.columnsFor(KindEvents, namespaced)
	if err != nil {
		empty.Error = err.Error()
		return empty
	}

	refs := make([]*unstructured.Unstructured, 0, len(items))
	for i := range items {
		refs = append(refs, &items[i])
	}

	table := buildLiveTable(KindEvents, cols, namespaced, refs)
	if len(table.Rows) > dashboardEvents {
		table.Rows = table.Rows[:dashboardEvents]
	}
	return table
}

// recentEvents renders events as report lines for the describe panel, which has
// no table to put them in.
func recentEvents(items []unstructured.Unstructured) []Event {
	out := make([]Event, 0, len(items))
	for i := range items {
		e := &items[i]
		out = append(out, Event{
			Type:    nestedString(e, "type"),
			Reason:  nestedString(e, "reason"),
			Object:  nestedString(e, "involvedObject", "kind") + "/" + nestedString(e, "involvedObject", "name"),
			Message: nestedString(e, "message"),
			Age:     since(lastSeen(e)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Type == "Warning" && out[j].Type != "Warning"
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func lastSeen(e *unstructured.Unstructured) string {
	if ts := nestedString(e, "lastTimestamp"); ts != "" {
		return ts
	}
	return nestedString(e, "eventTime")
}

// Describe renders the report behind the slide-in panel for one live object.
//
// reveal decodes a Secret's values into plain text. It is a parameter rather
// than something the frontend does to the report it is given, so that the
// decoded form is produced only when somebody has asked to see it: the panel
// holds base64 until the button is pressed, and pressing it reads again.
func (w *Watcher) Describe(kc Context, kind, namespace, name string, reveal bool) (string, error) {
	var out string
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		got, mapping, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}

		if reveal {
			// The same decoding the editor opens a Secret with, so that the two
			// views agree about what a revealed value looks like rather than
			// each having its own idea. Does nothing to anything else.
			got = got.DeepCopy()
			readableSecret(got)
		}
		out = describeLive(ctx, c, got, mapping.Resource)
		return nil
	})
	return out, err
}

// describeLive renders one object the way the panel expects: the identity a
// reader scans for first, then the object itself, then what has happened to it.
//
// The body is YAML rather than a hand-written per-kind report. `kubectl
// describe` has a bespoke printer for every built-in kind and nothing at all
// for a CRD; YAML is complete for every kind alike, which is the only thing
// that can be true of a view that has to render resources nobody compiled in.
func describeLive(ctx context.Context, c *clusterClient, u *unstructured.Unstructured, gvr schema.GroupVersionResource) string {
	d := &describer{}
	d.field("Name", u.GetName())
	if ns := u.GetNamespace(); ns != "" {
		d.field("Namespace", ns)
	}
	d.field("Kind", u.GetKind())
	d.field("API Version", u.GetAPIVersion())
	d.field("Created", ageOf(u)+" ago")
	d.field("Labels", indented(joinMap(u.GetLabels())))
	d.field("Annotations", indented(joinMap(stripLastApplied(u.GetAnnotations()))))
	d.blank()
	if u.GetKind() == "Pod" && u.GetAPIVersion() == "v1" {
		describeRestarts(d, u)
	}

	body := map[string]any{}
	// stringData is here for a revealed Secret: readableSecret moves the
	// decodable entries there, and a body that listed only `data` would answer
	// the reveal with an empty report.
	for _, key := range []string{"spec", "status", "data", "stringData", "type", "subjects", "roleRef", "rules"} {
		if v, found := u.Object[key]; found {
			body[key] = v
		}
	}
	if len(body) > 0 {
		if raw, err := yaml.Marshal(body); err == nil {
			d.b.WriteString(string(raw))
			d.blank()
		}
	}

	d.section("Events")
	events := objectEvents(ctx, c, u)
	if len(events) == 0 {
		d.line(2, "<none>")
		return d.b.String()
	}
	for _, e := range events {
		d.line(2, "%-9s %-24s %-8s %s", e.Type, e.Reason, e.Age, e.Message)
	}
	return d.b.String()
}

// objectEvents fetches the events naming this object. A cluster that denies
// access to events should cost the panel nothing but its Events section.
func objectEvents(ctx context.Context, c *clusterClient, u *unstructured.Unstructured) []Event {
	mapping, err := c.mappingForKind(KindEvents)
	if err != nil {
		return nil
	}
	selector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=%s", u.GetName(), u.GetKind())
	opts := metav1.ListOptions{FieldSelector: selector, Limit: 20}

	ri := c.dynamic.Resource(mapping.Resource)
	var list *unstructured.UnstructuredList
	if ns := u.GetNamespace(); ns != "" {
		list, err = ri.Namespace(ns).List(ctx, opts)
	} else {
		list, err = ri.List(ctx, opts)
	}
	if err != nil {
		return nil
	}
	return recentEvents(list.Items)
}

// stripLastApplied drops kubectl's copy of the whole object, which is an
// annotation only by accident of implementation and would bury the real ones.
func stripLastApplied(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		out[k] = v
	}
	return out
}

// indented wraps a comma-separated list onto the describer's field column.
func indented(s string) string {
	return strings.ReplaceAll(s, ", ", "\n"+strings.Repeat(" ", 22))
}

// distributionOf names the Kubernetes distribution from what the nodes report.
// It is a guess from well-known markers, so an unrecognised cluster says
// nothing rather than something wrong.
func distributionOf(nodes []unstructured.Unstructured) string {
	if len(nodes) == 0 {
		return ""
	}
	n := &nodes[0]
	version := nestedString(n, "status", "nodeInfo", "kubeletVersion")
	osImage := nestedString(n, "status", "nodeInfo", "osImage")

	switch {
	case strings.Contains(osImage, "Talos"):
		return "Talos"
	case strings.Contains(version, "+k3s"):
		return "k3s"
	case strings.Contains(version, "+rke2"):
		return "RKE2"
	case strings.Contains(version, "-eks"):
		return "EKS"
	case strings.Contains(version, "-gke"):
		return "GKE"
	case strings.HasSuffix(version, "+aks"):
		return "AKS"
	}
	if _, found := n.GetLabels()["node.kubernetes.io/instance-type"]; found {
		return "managed"
	}
	return ""
}

// probeTimeout bounds a reachability check. It is far shorter than callTimeout
// because a probe backs an indicator, not a view: a cluster that has not
// answered in a few seconds is one the sidebar should already be calling
// unreachable, and the user is not waiting on the result to read anything.
const probeTimeout = 6 * time.Second

// Ping reports whether a context's API server can be reached and will talk to
// us. It is the check behind the sidebar's connection indicator.
//
// GET /version is the cheapest call that proves the whole path rather than just
// the socket: DNS, the TCP connection, the TLS handshake and the credentials
// all have to work for it to come back, and each of them fails with a message
// the UI can tell apart. Note that it deliberately does not go through the
// cached discovery client, whose whole purpose is to answer without a request.
//
// The client is borrowed from the same pool the informers use, so probing a
// context you then open costs no second connection and no second credential
// exec -- the ping warms exactly what the tab is about to need.
func (w *Watcher) Ping(kc Context) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()

		return c.disco.RESTClient().Get().AbsPath("/version").Do(ctx).Error()
	})
}
