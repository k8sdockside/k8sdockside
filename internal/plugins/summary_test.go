package plugins

import (
	"errors"
	"slices"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// fakeCluster answers the two questions Summarise asks, so the wording and
// ordering can be tested without a Kubernetes API.
type fakeCluster struct {
	served  map[string]bool
	tallies map[string]kube.Tally
	fail    map[string]error
	// discoveryErr is a cluster we could not ask at all. Discovery is one sweep
	// of the whole API surface, so it either answers or it does not; there is
	// no per-kind failure left to model.
	discoveryErr error
	// sweeps counts the discovery calls, to prove a plugin naming a kind in
	// several places still costs one round trip.
	sweeps int
	// asked is every kind the last sweep was given.
	asked []string
}

func (f *fakeCluster) KindsServed(kinds []string) (map[string]bool, error) {
	f.sweeps++
	f.asked = append([]string{}, kinds...)
	if f.discoveryErr != nil {
		return nil, f.discoveryErr
	}
	out := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		out[kind] = f.served[kind]
	}
	return out, nil
}

func (f *fakeCluster) CountBy(kind, _, _ string, _ kube.FieldPath) (kube.Tally, error) {
	if err, bad := f.fail[kind]; bad {
		return kube.Tally{}, err
	}
	return f.tallies[kind], nil
}

var argo = Plugin{
	ID:   "argocd",
	Name: "Argo CD",
	Requires: []Requirement{
		{Kind: "crd:applications.argoproj.io", Label: "Applications"},
		{Kind: "crd:applicationsets.argoproj.io", Label: "Application Sets", Optional: true},
	},
	Cards: []Card{{
		Label:   "Applications by health",
		Kind:    "crd:applications.argoproj.io",
		GroupBy: "status.health.status",
		Tones: map[string]string{
			"Healthy": "ok", "Progressing": "warn", "Degraded": "error", "Unknown": "info",
		},
	}},
}

func TestSummariseAHealthyInstall(t *testing.T) {
	cl := &fakeCluster{
		served: map[string]bool{"crd:applications.argoproj.io": true, "crd:applicationsets.argoproj.io": true},
		tallies: map[string]kube.Tally{
			"crd:applications.argoproj.io": {Total: 42, Counts: map[string]int{"Healthy": 38, "Progressing": 3, "Degraded": 1}},
		},
	}

	got := Summarise(argo, cl)

	if !got.Installed || !got.Checked {
		t.Fatalf("summary = %+v, want installed and checked", got)
	}
	if len(got.Cards) != 1 || got.Cards[0].Total != 42 {
		t.Fatalf("cards = %+v", got.Cards)
	}
	// The kind is named by a requirement and a card; asking twice would be two
	// discovery round trips for one answer.
	if cl.sweeps != 1 {
		t.Errorf("swept discovery %d times, want 1", cl.sweeps)
	}
}

// A missing optional requirement is reported but must not make the plugin read
// as absent: Argo CD without ApplicationSets is still Argo CD.
func TestOptionalRequirementsDoNotDecideInstalled(t *testing.T) {
	cl := &fakeCluster{served: map[string]bool{"crd:applications.argoproj.io": true}}

	got := Summarise(argo, cl)

	if !got.Installed {
		t.Error("a missing optional requirement made the plugin read as not installed")
	}
	var sets Presence
	for _, req := range got.Requirements {
		if req.Kind == "crd:applicationsets.argoproj.io" {
			sets = req
		}
	}
	if sets.Served || !sets.Optional {
		t.Errorf("the optional requirement is reported as %+v", sets)
	}
}

// The state the user asked about: the plugin is installed on this machine and
// the CRDs are not in this cluster.
func TestSummariseWhenTheSolutionIsNotInstalled(t *testing.T) {
	got := Summarise(argo, &fakeCluster{served: map[string]bool{}})

	if got.Installed {
		t.Error("a cluster serving none of the CRDs read as installed")
	}
	if !got.Checked {
		t.Error("we did ask and got an answer, so the answer should count")
	}
	if len(got.Cards) != 1 || got.Cards[0].Error == "" {
		t.Fatalf("the card should say why it has no number: %+v", got.Cards)
	}
	if got.Cards[0].Total != 0 {
		t.Errorf("total = %d, want nothing counted", got.Cards[0].Total)
	}
}

// An unreachable cluster must not read as "Argo CD is not installed here" --
// they call for opposite reactions.
func TestAnUnreachableClusterIsNotTheSameAsNotInstalled(t *testing.T) {
	cl := &fakeCluster{discoveryErr: errors.New("connection refused")}

	got := Summarise(argo, cl)

	if got.Checked {
		t.Error("a cluster we could not ask was reported as checked")
	}
	if got.Installed {
		t.Error("a cluster we could not ask was reported as installed")
	}
	if got.Error == "" {
		t.Error("nothing says why the check did not happen")
	}
	if got.Requirements[0].Error == "" {
		t.Error("the requirement does not carry its reason")
	}
}

// One Degraded application among forty Healthy ones is the reason the tile is
// on screen; ordering by count would bury it under the good news.
func TestBucketsLeadWithWhatIsWrong(t *testing.T) {
	cl := &fakeCluster{
		served: map[string]bool{"crd:applications.argoproj.io": true, "crd:applicationsets.argoproj.io": true},
		tallies: map[string]kube.Tally{
			"crd:applications.argoproj.io": {Total: 45, Counts: map[string]int{
				"Healthy": 38, "Progressing": 3, "Degraded": 1, "Unknown": 2, "": 1,
			}},
		},
	}

	buckets := Summarise(argo, cl).Cards[0].Buckets

	var order []string
	for _, b := range buckets {
		order = append(order, b.Value)
	}
	want := []string{"Degraded", "Progressing", "Healthy", "Unknown", ""}
	if len(order) != len(want) {
		t.Fatalf("buckets = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("buckets = %v, want %v (worst first, the absent-field bucket last)", order, want)
		}
	}
	if buckets[0].Tone != "error" {
		t.Errorf("the Degraded bucket has tone %q", buckets[0].Tone)
	}
}

// A card with no groupBy is a plain count, and should not pretend to a
// breakdown it was never given.
func TestAnUngroupedCardIsJustACount(t *testing.T) {
	plugin := Plugin{ID: "x", Cards: []Card{{Label: "Pods", Kind: "pods"}}}
	cl := &fakeCluster{
		served:  map[string]bool{"pods": true},
		tallies: map[string]kube.Tally{"pods": {Total: 7, Counts: map[string]int{"": 7}}},
	}

	card := Summarise(plugin, cl).Cards[0]

	if card.Total != 7 {
		t.Errorf("total = %d, want 7", card.Total)
	}
	if card.Grouped || len(card.Buckets) != 0 {
		t.Errorf("card = %+v, want no breakdown", card)
	}
}

// A plugin that declares no requirements is for something with no CRDs to look
// for, and is taken at its word rather than reported as missing.
func TestAPluginWithNoRequirementsCountsAsInstalled(t *testing.T) {
	plugin := Plugin{ID: "x", Views: []View{{ID: "pods", Kind: "pods"}}}

	if got := Summarise(plugin, &fakeCluster{}); !got.Installed {
		t.Errorf("summary = %+v, want installed", got)
	}
}

// The whole reason the check is batched. The real Argo CD plugin names three
// custom resources across its requirements and its cards, and every one of them
// used to cost a full discovery fan-out of its own -- worse on a cluster that
// does not have Argo CD, where each miss also threw away the cache the last
// lookup had just built. One sweep answers for all of them, which is what
// `kubectl api-resources` does.
func TestSummariseSweepsDiscoveryOnceForEveryKindItNeeds(t *testing.T) {
	plugin := Plugin{
		ID:   "argocd",
		Name: "Argo CD",
		Requires: []Requirement{
			{Kind: "crd:applications.argoproj.io"},
			{Kind: "crd:appprojects.argoproj.io"},
			{Kind: "crd:applicationsets.argoproj.io", Optional: true},
		},
		Cards: []Card{
			{Label: "By health", Kind: "crd:applications.argoproj.io", GroupBy: "status.health.status"},
			{Label: "By sync", Kind: "crd:applications.argoproj.io", GroupBy: "status.sync.status"},
			{Label: "Sets", Kind: "crd:applicationsets.argoproj.io"},
			{Label: "Projects", Kind: "crd:appprojects.argoproj.io"},
			{Label: "Workloads", Kind: "deployments"},
		},
	}
	cl := &fakeCluster{}

	Summarise(plugin, cl)

	if cl.sweeps != 1 {
		t.Errorf("swept discovery %d times, want exactly 1 for the whole overview", cl.sweeps)
	}
	// Every kind has to go in the one sweep. Leaving the cards out would send
	// each of them back to the cluster on its own, which is the bug.
	want := []string{
		"crd:applications.argoproj.io",
		"crd:appprojects.argoproj.io",
		"crd:applicationsets.argoproj.io",
		"deployments",
	}
	for _, kind := range want {
		if !slices.Contains(cl.asked, kind) {
			t.Errorf("the sweep did not ask about %q; asked %v", kind, cl.asked)
		}
	}
	// Named three times across requirements and cards, asked for once.
	times := 0
	for _, kind := range cl.asked {
		if kind == "crd:applications.argoproj.io" {
			times++
		}
	}
	if times != 1 {
		t.Errorf("a kind named several times went into the sweep %d times: %v", times, cl.asked)
	}
}
