package kube

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParseSearchSplitsFiltersFromNames(t *testing.T) {
	q := ParseSearch("  Nginx kind:Pod,svc ns:default n:web label:app=web l:tier=front system:controller:x ")

	if want := []string{"nginx", "system:controller:x"}; !slices.Equal(q.Terms, want) {
		t.Errorf("terms = %q, want %q", q.Terms, want)
	}
	if want := []string{"pod", "svc"}; !slices.Equal(q.Kinds, want) {
		t.Errorf("kinds = %q, want %q", q.Kinds, want)
	}
	if want := []string{"default", "web"}; !slices.Equal(q.Namespaces, want) {
		t.Errorf("namespaces = %q, want %q", q.Namespaces, want)
	}
	if want := "app=web,tier=front"; q.Selector != want {
		t.Errorf("selector = %q, want %q", q.Selector, want)
	}
}

func TestAnEmptyFilterIsAName(t *testing.T) {
	q := ParseSearch("kind: ns:")
	if want := []string{"kind:", "ns:"}; !slices.Equal(q.Terms, want) {
		t.Errorf("terms = %q, want %q", q.Terms, want)
	}
}

func TestEmptyQuery(t *testing.T) {
	if !ParseSearch("   ").Empty() {
		t.Error("whitespace should be an empty query")
	}
	if ParseSearch("kind:applications").Empty() {
		t.Error("a kind alone lists that kind, and is not empty")
	}
}

func TestValidateRefusesABadSelector(t *testing.T) {
	if err := ParseSearch("label:=web").Validate(); err == nil {
		t.Error("expected a malformed selector to be refused")
	}
	if err := ParseSearch("label:app=web").Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMatchTerm(t *testing.T) {
	cases := []struct {
		s, term string
		want    bool
	}{
		{"coredns-5d78c9869d-abcde", "dns", true},
		{"coredns-5d78c9869d-abcde", "core*", true},
		{"coredns-5d78c9869d-abcde", "*abcde", true},
		{"coredns-5d78c9869d-abcde", "core*-abcde", true},
		{"coredns-5d78c9869d-abcde", "dns*", false},
		{"a", "a*a", false},
		{"aba", "a*a", true},
		{"api-7-worker", "api-*-worker", true},
		{"api-worker", "api-*-worker", false},
	}
	for _, c := range cases {
		if got := matchTerm(c.s, c.term); got != c.want {
			t.Errorf("matchTerm(%q, %q) = %v, want %v", c.s, c.term, got, c.want)
		}
	}
}

func TestMatchesNamesAndNamespaces(t *testing.T) {
	q := ParseSearch("kube-system/core")
	if !q.matches("kube-system", "coredns") {
		t.Error("a slash should match against namespace/name")
	}
	if q.matches("default", "coredns") {
		t.Error("the namespace half should have to match too")
	}

	q = ParseSearch("web ns:prod")
	if !q.matches("prod", "WebServer") {
		t.Error("names should match without regard to case")
	}
	if q.matches("staging", "webserver") {
		t.Error("a namespace filter should keep other namespaces out")
	}
}

func discoveryFixture() []*metav1.APIResourceList {
	list := []string{"get", "list", "watch"}
	return []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", SingularName: "pod", Kind: "Pod", Namespaced: true, ShortNames: []string{"po"}, Verbs: list},
			{Name: "pods/log", Kind: "Pod", Namespaced: true, Verbs: []string{"get"}},
			{Name: "services", SingularName: "service", Kind: "Service", Namespaced: true, ShortNames: []string{"svc"}, Verbs: list},
			{Name: "events", SingularName: "event", Kind: "Event", Namespaced: true, ShortNames: []string{"ev"}, Verbs: list},
			{Name: "componentstatuses", SingularName: "componentstatus", Kind: "ComponentStatus", ShortNames: []string{"cs"}, Verbs: []string{"get", "list"}},
			{Name: "bindings", Kind: "Binding", Namespaced: true, Verbs: []string{"create"}},
		}},
		{GroupVersion: "argoproj.io/v1alpha1", APIResources: []metav1.APIResource{
			{Name: "applications", SingularName: "application", Kind: "Application", Namespaced: true, ShortNames: []string{"app", "apps"}, Verbs: list},
		}},
		{GroupVersion: "metrics.k8s.io/v1beta1", APIResources: []metav1.APIResource{
			{Name: "pods", Kind: "PodMetrics", Namespaced: true, Verbs: list},
		}},
	}
}

func kindsOf(s []searchable) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		out = append(out, x.kind)
	}
	return out
}

func TestSearchablesAreNamedAsTheAppOpensThem(t *testing.T) {
	got := kindsOf(searchablesIn(discoveryFixture()))
	// Subresources, what cannot be listed, the metrics API and core kinds
	// the app has no name for are all left out.
	want := []string{"crd:applications.argoproj.io", "events", "pods", "services"}
	if !slices.Equal(got, want) {
		t.Errorf("searchable kinds = %q, want %q", got, want)
	}
}

func TestChooseSearchable(t *testing.T) {
	all := searchablesIn(discoveryFixture())

	if got, want := kindsOf(chooseSearchable(all, nil)), []string{"crd:applications.argoproj.io", "pods", "services"}; !slices.Equal(got, want) {
		t.Errorf("no kinds asked for: got %q, want %q (events left out)", got, want)
	}
	if got, want := kindsOf(chooseSearchable(all, []string{"po", "SVC"})), []string{"pods", "services"}; !slices.Equal(got, want) {
		t.Errorf("short names: got %q, want %q", got, want)
	}
	if got, want := kindsOf(chooseSearchable(all, []string{"application"})), []string{"crd:applications.argoproj.io"}; !slices.Equal(got, want) {
		t.Errorf("singular: got %q, want %q", got, want)
	}
	if got, want := kindsOf(chooseSearchable(all, []string{"applications.argoproj.io"})), []string{"crd:applications.argoproj.io"}; !slices.Equal(got, want) {
		t.Errorf("plural.group: got %q, want %q", got, want)
	}
	if got, want := kindsOf(chooseSearchable(all, []string{"events"})), []string{"events"}; !slices.Equal(got, want) {
		t.Errorf("named quiet kind: got %q, want %q", got, want)
	}
}
