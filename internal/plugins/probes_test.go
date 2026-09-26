package plugins

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Most plugins are detected from the definitions the sidebar has already read:
// a custom resource is either served or it is not. Flannel installs none at
// all -- a DaemonSet, a ConfigMap and nothing else -- so it is recognised by
// the workload instead, which is a real query and is held to stricter rules.

// probeCluster records what was asked and answers with fixed counts.
type probeCluster struct {
	counts map[string]int
	fail   map[string]error
	asked  []string
}

func (p *probeCluster) KindsServed(kinds []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (p *probeCluster) CountBy(kind, namespace, selector string, _ kube.FieldPath) (kube.Tally, error) {
	p.asked = append(p.asked, strings.TrimSuffix(kind+" "+namespace+" "+selector, " "))
	if err, bad := p.fail[selector]; bad {
		return kube.Tally{}, err
	}
	return kube.Tally{Total: p.counts[selector]}, nil
}

func TestFlannelIsDetectedByItsDaemonSet(t *testing.T) {
	flannel, ok := FindKnown("flannel")
	if !ok {
		t.Fatal("flannel is not on the known list")
	}
	if len(flannel.Detect) > 0 {
		t.Errorf("flannel now detects kinds (%v) -- it has no custom resources to detect", flannel.Detect)
	}
	if len(flannel.DetectWorkloads) == 0 {
		t.Fatal("flannel has no workload probe, so it is suggested for every cluster")
	}

	cluster := &probeCluster{counts: map[string]int{"app=flannel": 1}}
	if here, told := flannel.RunsIn(cluster); !here || !told {
		t.Errorf("a cluster with a flannel DaemonSet was not recognised: here=%v told=%v", here, told)
	}
	// The first probe found it, so the second was never asked.
	if len(cluster.asked) != 1 {
		t.Errorf("asked %v, want to stop at the first match", cluster.asked)
	}

	empty := &probeCluster{counts: map[string]int{}}
	if here, told := flannel.RunsIn(empty); here || !told {
		t.Errorf("a cluster with no flannel: here=%v told=%v, want a plain no", here, told)
	}
	if len(empty.asked) != len(flannel.DetectWorkloads) {
		t.Errorf("asked %v, want every probe tried before giving up", empty.asked)
	}
}

func TestAProbeThatFailsIsNotAMatch(t *testing.T) {
	// "Is it worth suggesting this plugin here" has no third answer: a cluster
	// that would not say is not one to suggest anything for.
	flannel, _ := FindKnown("flannel")
	broken := &probeCluster{fail: map[string]error{
		"app=flannel":     errors.New("the cluster could not be reached"),
		"k8s-app=flannel": errors.New("the cluster could not be reached"),
	}}
	// Not a match, and -- unlike an empty cluster -- not an answer either: an
	// installed plugin is only called absent when the cluster has said so.
	if here, told := flannel.RunsIn(broken); here || told {
		t.Errorf("a cluster that could not be asked: here=%v told=%v, want no opinion", here, told)
	}
}

func TestAPluginWithNoProbesIsNeverProbed(t *testing.T) {
	cilium, _ := FindKnown("cilium")
	cluster := &probeCluster{counts: map[string]int{"app=flannel": 1}}
	if here, _ := cilium.RunsIn(cluster); here {
		t.Error("cilium matched a probe it does not have")
	}
	if len(cluster.asked) != 0 {
		t.Errorf("a plugin detected by its custom resources asked the cluster %v", cluster.asked)
	}
}

func TestAWorkloadProbeNeedsAKindAndASelector(t *testing.T) {
	if _, err := validateProbe("acme", Probe{Kind: "daemonsets"}); err == nil {
		t.Error("a probe with no selector was accepted; it would match every cluster with DaemonSets")
	}
	if _, err := validateProbe("acme", Probe{Kind: "widgets", Selector: "app=acme"}); err == nil {
		t.Error("a probe on a kind the app cannot open was accepted")
	}
	if _, err := validateProbe("acme", Probe{Kind: "daemonsets", Selector: "app in ("}); err == nil {
		t.Error("a probe with a malformed selector was accepted")
	}
	probe, err := validateProbe("acme", Probe{Kind: " daemonsets ", Namespace: " kube-system ", Selector: " app=acme "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(probe, Probe{Kind: "daemonsets", Namespace: "kube-system", Selector: "app=acme"}) {
		t.Errorf("a probe came back untrimmed: %+v", probe)
	}
}

// A plugin for a product that defines no custom resources requires only kinds
// every cluster serves -- DaemonSets, Nodes, ConfigMaps -- so the overview
// said it was installed everywhere. A requirement that names objects is the
// way out, and this is the behaviour that makes it worth having.
func TestARequirementThatNamesObjectsIsCheckedAgainstTheCluster(t *testing.T) {
	flannel := Plugin{
		ID: "flannel",
		Requires: []Requirement{
			{Kind: "daemonsets", Label: "Flannel daemonset", Selector: "app=flannel"},
			{Kind: "nodes", Label: "Nodes"},
		},
	}

	// Every cluster serves DaemonSets and Nodes; only one of them runs flannel.
	withIt := &fakeCluster{
		served:  map[string]bool{"daemonsets": true, "nodes": true},
		tallies: map[string]kube.Tally{"daemonsets": {Total: 1}},
	}
	if got := Summarise(flannel, withIt); !got.Installed {
		t.Errorf("a cluster running flannel reads as not installed: %+v", got.Requirements)
	}

	without := &fakeCluster{
		served:  map[string]bool{"daemonsets": true, "nodes": true},
		tallies: map[string]kube.Tally{"daemonsets": {Total: 0}},
	}
	got := Summarise(flannel, without)
	if got.Installed {
		t.Error("a cluster with no flannel daemonset still reads as running flannel")
	}
	if !got.Checked {
		t.Error("the cluster answered, so the page should say so plainly rather than hedging")
	}
	// The requirement says what it looked for, so "not installed here" can be
	// checked by the person reading it.
	if got.Requirements[0].Selector != "app=flannel" {
		t.Errorf("the requirement does not report its selector: %+v", got.Requirements[0])
	}
	if got.Requirements[1].Served != true {
		t.Error("a requirement with no selector stopped being answered by discovery")
	}
}

func TestAClusterThatCouldNotBeAskedIsNotCalledMissing(t *testing.T) {
	flannel := Plugin{
		ID:       "flannel",
		Requires: []Requirement{{Kind: "daemonsets", Selector: "app=flannel"}},
	}
	unreachable := &fakeCluster{
		served: map[string]bool{"daemonsets": true},
		fail:   map[string]error{"daemonsets": errors.New("connection refused")},
	}

	got := Summarise(flannel, unreachable)
	if got.Checked {
		t.Error("a cluster that could not be asked was reported as checked")
	}
	if got.Requirements[0].Error == "" {
		t.Error("the requirement does not say why it could not be answered")
	}

	// And the same question, as the sidebar asks it.
	if here, told := HasItsObjects(flannel, unreachable); told || here {
		t.Errorf("HasItsObjects = (%v, %v), want it to admit it could not tell", here, told)
	}
	if !NeedsObjects(flannel) {
		t.Error("a plugin with an object requirement was not recognised as needing one")
	}
	if NeedsObjects(Plugin{Requires: []Requirement{{Kind: "daemonsets"}}}) {
		t.Error("a plugin whose requirements are all kinds was sent to the cluster anyway")
	}
}

// The descheduler is the other shape of undetectable product: no custom
// resources, and it may be a CronJob or a Deployment depending on how the
// chart was installed -- or, between runs, neither, with only the Helm release
// left to say it is there at all.
func TestTheDeschedulerIsFoundByItsWorkloadItsPodsOrItsHelmRelease(t *testing.T) {
	d, ok := FindKnown("descheduler")
	if !ok {
		t.Fatal("descheduler is not on the known list")
	}
	if len(d.Detect) > 0 {
		t.Errorf("descheduler detects kinds (%v) -- it installs no custom resources", d.Detect)
	}

	// Each of the four on its own is enough: how it was installed is not
	// something the app gets to assume.
	for _, selector := range []string{
		"app.kubernetes.io/name=descheduler",
		"owner=helm,name=descheduler",
	} {
		cluster := &probeCluster{counts: map[string]int{selector: 1}}
		if here, told := d.RunsIn(cluster); !here || !told {
			t.Errorf("a cluster matching %q was not recognised: here=%v told=%v", selector, here, told)
		}
	}

	// A cluster with none of it is a plain no, and every probe was tried
	// before saying so -- this is the answer that hides the plugin's panels.
	empty := &probeCluster{counts: map[string]int{}}
	if here, told := d.RunsIn(empty); here || !told {
		t.Errorf("a cluster with no descheduler: here=%v told=%v, want a plain no", here, told)
	}
	if len(empty.asked) != len(d.DetectWorkloads) {
		t.Errorf("asked %v, want every probe tried before giving up", empty.asked)
	}

	// The workload comes before the release record: it is cheaper to list, and
	// a running descheduler is a better answer than a Helm release saying one
	// ought to be running.
	if first := d.DetectWorkloads[0].Kind; first == "secrets" {
		t.Errorf("the Helm release is probed first; the workloads are cheaper and more telling")
	}
}

// objectCluster answers probes from real objects, tallied by the probe's own
// field the way the watcher tallies them.
type objectCluster struct {
	objects map[string][]map[string]any
}

func (o *objectCluster) KindsServed(kinds []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (o *objectCluster) CountBy(kind, _, selector string, path kube.FieldPath) (kube.Tally, error) {
	tally := kube.Tally{Counts: map[string]int{}}
	if selector != "" {
		// None of these objects carries labels: a selector matches nothing.
		return tally, nil
	}
	for _, object := range o.objects[kind] {
		u := &unstructured.Unstructured{Object: object}
		tally.Total++
		tally.Counts[path.Value(u)]++
	}
	return tally, nil
}

func gpuNode(capacity map[string]any) map[string]any {
	return map[string]any{"metadata": map[string]any{"name": "n"}, "status": map[string]any{"capacity": capacity}}
}

// A GPU is recognised by what a node advertises or a DRA driver publishes,
// not only by labels a cluster may not have -- a machine with four H100s and
// no GPU feature discovery is still a GPU cluster.
func TestGPUIsDetectedByWhatTheClusterHas(t *testing.T) {
	gpu, ok := FindKnown("gpu")
	if !ok {
		t.Fatal("gpu is not on the known list")
	}
	cases := []struct {
		name    string
		objects map[string][]map[string]any
		want    bool
	}{
		{"a node advertising nvidia.com/gpu", map[string][]map[string]any{"nodes": {gpuNode(map[string]any{"cpu": "96", "nvidia.com/gpu": "4"})}}, true},
		{"a node advertising amd.com/gpu", map[string][]map[string]any{"nodes": {gpuNode(map[string]any{"amd.com/gpu": "8"})}}, true},
		{"a DRA driver publishing GPUs", map[string][]map[string]any{"resourceslices": {{"spec": map[string]any{"driver": "gpu.nvidia.com"}}}}, true},
		{"a node whose device plugin lost every GPU", map[string][]map[string]any{"nodes": {gpuNode(map[string]any{"nvidia.com/gpu": "0"})}}, false},
		{"a DRA driver for something else", map[string][]map[string]any{"resourceslices": {{"spec": map[string]any{"driver": "compute-domain.nvidia.com"}}}}, false},
		{"CPU nodes only", map[string][]map[string]any{"nodes": {gpuNode(map[string]any{"cpu": "8"})}}, false},
	}
	for _, c := range cases {
		here, told := gpu.RunsIn(&objectCluster{objects: c.objects})
		if here != c.want || !told {
			t.Errorf("%s: here=%v told=%v, want here=%v", c.name, here, told, c.want)
		}
	}
}

func TestAProbeNeedsASelectorOrAField(t *testing.T) {
	if _, err := validateProbe("x", Probe{Kind: "nodes"}); err == nil {
		t.Error("a probe with neither a selector nor a field was accepted")
	}
	if _, err := validateProbe("x", Probe{Kind: "nodes", Field: "status.capacity{nvidia.com/gpu}"}); err != nil {
		t.Errorf("a field probe was refused: %v", err)
	}
	if _, err := validateProbe("x", Probe{Kind: "nodes", Field: "status.capacity{a b}"}); err == nil {
		t.Error("a map key with a space in it was accepted")
	}
	if _, err := validateProbe("x", Probe{Kind: "nodes", Selector: "a=b", Values: []string{"x"}}); err == nil {
		t.Error("values without a field were accepted")
	}
}
