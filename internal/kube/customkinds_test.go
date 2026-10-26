package kube

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// crd builds a CustomResourceDefinition literal.
func crd(group, kind, plural, scope string) *unstructured.Unstructured {
	return obj(map[string]any{
		"metadata": map[string]any{"name": plural + "." + group},
		"spec": map[string]any{
			"group": group,
			"scope": scope,
			"names": map[string]any{"kind": kind, "plural": plural},
		},
	})
}

func TestCustomKindsAreGroupedByTheirAPIGroup(t *testing.T) {
	groups := groupCustomKinds([]*unstructured.Unstructured{
		crd("cert-manager.io", "Certificate", "certificates", "Namespaced"),
		crd("vitistack.io", "Machine", "machines", "Namespaced"),
		crd("cert-manager.io", "Issuer", "issuers", "Namespaced"),
	})

	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// Alphabetical, so a long list is scannable.
	if groups[0].Group != "cert-manager.io" || groups[1].Group != "vitistack.io" {
		t.Errorf("groups = %q, %q, want cert-manager.io then vitistack.io", groups[0].Group, groups[1].Group)
	}
	if len(groups[0].Kinds) != 2 {
		t.Fatalf("cert-manager kinds = %d, want 2", len(groups[0].Kinds))
	}
	if groups[0].Kinds[0].Label != "Certificate" || groups[0].Kinds[1].Label != "Issuer" {
		t.Errorf("kinds = %q, %q, want Certificate then Issuer", groups[0].Kinds[0].Label, groups[0].Kinds[1].Label)
	}
}

func TestEachCustomKindCarriesWhatOpensItsTab(t *testing.T) {
	groups := groupCustomKinds([]*unstructured.Unstructured{
		crd("vitistack.io", "Machine", "machines", "Namespaced"),
	})

	kind := groups[0].Kinds[0]
	if kind.Kind != CustomKind("machines", "vitistack.io") {
		t.Errorf("kind = %q, want the crd: form", kind.Kind)
	}
	if !kind.Scoped {
		t.Error("a Namespaced definition should be reported as scoped")
	}
}

// A definition with no group belongs to the core API and would otherwise be
// filed under an empty heading.
func TestADefinitionWithoutAGroupIsSkipped(t *testing.T) {
	groups := groupCustomKinds([]*unstructured.Unstructured{
		crd("", "Odd", "odds", "Namespaced"),
		crd("vitistack.io", "Machine", "machines", "Namespaced"),
	})

	if len(groups) != 1 || groups[0].Group != "vitistack.io" {
		t.Errorf("groups = %+v, want only vitistack.io", groups)
	}
}

func TestNoDefinitionsGivesAnEmptyListRatherThanNil(t *testing.T) {
	groups := groupCustomKinds(nil)
	if groups == nil {
		t.Error("groups is nil, want an empty list the frontend can render")
	}
}

// A cluster that does not serve the definitions API has no custom resources.
// That is an answer, not a failure, and reporting it as an error leaves the
// user unable to tell "none here" from "I could not look".
func TestAClusterWithoutTheDefinitionsAPIReportsNoneRatherThanFailing(t *testing.T) {
	groups, err := definitionsFrom(nil, notServed(KindCRDs, "apiextensions.k8s.io", &meta.NoKindMatchError{}))

	if err != nil {
		t.Fatalf("err = %v, want nil -- a cluster without the API simply has none", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %v, want empty", groups)
	}
	if groups == nil {
		t.Error("groups is nil, want an empty list")
	}
}

// Anything else is a real failure and has to stay one: being unable to read the
// definitions is not the same as there being none.
func TestARefusedListingIsStillAFailure(t *testing.T) {
	refused := errors.New(`customresourcedefinitions.apiextensions.k8s.io is forbidden: User "x" cannot list resource`)

	if _, err := definitionsFrom(nil, refused); err == nil {
		t.Error("a forbidden listing came back as success")
	}
}

func TestASuccessfulListingIsGrouped(t *testing.T) {
	items := []unstructured.Unstructured{*crd("vitistack.io", "Machine", "machines", "Namespaced")}

	groups, err := definitionsFrom(items, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Group != "vitistack.io" {
		t.Errorf("groups = %+v, want vitistack.io", groups)
	}
}

// The sidebar's definitions are read from a printed table of their names and
// the discovery cache, never from the definitions themselves: those carry
// their whole schemas, and on a cluster with a few operators that is tens of
// megabytes, asked for every time a context is opened.
func TestCustomKindsAreReadWithoutTheDefinitions(t *testing.T) {
	srv := newFakeServer(t)
	w := NewWatcher(func(Snapshot) {})
	t.Cleanup(w.Close)

	groups, err := w.CustomResourceKinds(kubeconfigFor(t, srv.URL))
	if err != nil {
		t.Fatalf("CustomResourceKinds: %v", err)
	}
	if len(groups) != 1 || groups[0].Group != "example.com" || len(groups[0].Kinds) != 1 {
		t.Fatalf("groups = %+v, want example.com with one kind", groups)
	}
	want := CustomResourceKind{
		Kind:   CustomKind("widgets", "example.com"),
		Label:  "Widget",
		Group:  "example.com",
		Plural: "widgets",
		Scoped: true,
	}
	if got := groups[0].Kinds[0]; got != want {
		t.Errorf("kind = %+v, want %+v", got, want)
	}
	if n := srv.count("/apis/apiextensions.k8s.io/v1/customresourcedefinitions"); n != 0 {
		t.Errorf("the definitions were listed in full %d times, want never", n)
	}
	if n := srv.count("/apis/apiextensions.k8s.io/v1/customresourcedefinitions?table"); n != 1 {
		t.Errorf("the table of definitions was asked for %d times, want once", n)
	}
}

// A definition discovery has not heard of -- installed since the cache was
// filled, or served at no version -- is described by the definition itself,
// so the answer is the same one reading them all would have given.
func TestCustomKindsUnknownToDiscoveryAreReadInFull(t *testing.T) {
	srv := newFakeServer(t)
	srv.defined = append(srv.defined, "gadgets.example.org")
	w := NewWatcher(func(Snapshot) {})
	t.Cleanup(w.Close)

	groups, err := w.CustomResourceKinds(kubeconfigFor(t, srv.URL))
	if err != nil {
		t.Fatalf("CustomResourceKinds: %v", err)
	}
	if len(groups) != 2 || groups[0].Group != "example.com" || groups[1].Group != "example.org" {
		t.Fatalf("groups = %+v, want example.com and example.org", groups)
	}
	if got := groups[1].Kinds[0]; got.Label != "Gadget" || got.Plural != "gadgets" {
		t.Errorf("kind = %+v, want Gadget read from its definition", got)
	}
	if n := srv.count("/apis/apiextensions.k8s.io/v1/customresourcedefinitions"); n != 1 {
		t.Errorf("the definitions were listed in full %d times, want once", n)
	}
}

func TestCustomKindsInNeedsEveryDefinition(t *testing.T) {
	lists := []*metav1.APIResourceList{{
		GroupVersion: "example.com/v1",
		APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Namespaced: true},
			{Name: "widgets/status", Kind: "Widget", Namespaced: true},
			{Name: "clusterwidgets", Kind: "ClusterWidget"},
		},
	}}

	kinds, ok := customKindsIn([]string{"widgets.example.com", "clusterwidgets.example.com"}, lists)
	if !ok || len(kinds) != 2 {
		t.Fatalf("kinds = %+v, %v; want both answered", kinds, ok)
	}
	if !kinds[0].Scoped || kinds[1].Scoped {
		t.Errorf("scopes = %v, %v; want namespaced then cluster-wide", kinds[0].Scoped, kinds[1].Scoped)
	}

	if _, ok := customKindsIn([]string{"widgets.example.com", "gadgets.example.com"}, lists); ok {
		t.Error("answered with a definition missing from discovery, want the caller sent to the definitions")
	}
	if _, ok := customKindsIn([]string{"nodot"}, lists); ok {
		t.Error("answered for a name that is not plural.group")
	}
}

func TestTableNamesNeedsANameColumn(t *testing.T) {
	table := metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Created At"}, {Name: "Name", Format: "name"}},
		Rows:              []metav1.TableRow{{Cells: []any{"x", "widgets.example.com"}}},
	}
	names, err := tableNames(table)
	if err != nil || len(names) != 1 || names[0] != "widgets.example.com" {
		t.Errorf("names = %v, %v; want the name column read wherever it is", names, err)
	}
	if _, err := tableNames(metav1.Table{ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Age"}}}); err == nil {
		t.Error("read names from a table without a name column")
	}
}
