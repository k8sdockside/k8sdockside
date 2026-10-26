package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The custom resources a cluster serves, arranged for the sidebar.
//
// The definitions themselves are already listable as a table; this is the same
// data seen the other way round -- as a tree of API groups, each holding the
// kinds it defines -- because that is how someone looks for a custom resource
// they know the name of.

// CustomResourceGroup is one API group and the kinds defined under it.
type CustomResourceGroup struct {
	Group string               `json:"group"`
	Kinds []CustomResourceKind `json:"kinds"`
}

// groupCustomKinds arranges definitions by API group, both levels sorted so a
// long list can be scanned.
//
// A definition with no group is skipped: it would be filed under a blank
// heading, and a CRD without a group is not something the API server serves.
func groupCustomKinds(crds []*unstructured.Unstructured) []CustomResourceGroup {
	kinds := make([]CustomResourceKind, 0, len(crds))
	for _, crd := range crds {
		kinds = append(kinds, CustomResourceKindFor(crd))
	}
	return groupKinds(kinds)
}

// groupKinds is groupCustomKinds for kinds already read, from the definitions
// or from discovery.
func groupKinds(all []CustomResourceKind) []CustomResourceGroup {
	byGroup := map[string][]CustomResourceKind{}
	for _, kind := range all {
		if kind.Group == "" || kind.Plural == "" {
			continue
		}
		byGroup[kind.Group] = append(byGroup[kind.Group], kind)
	}

	groups := make([]CustomResourceGroup, 0, len(byGroup))
	for group, kinds := range byGroup {
		sort.Slice(kinds, func(i, j int) bool { return kinds[i].Label < kinds[j].Label })
		groups = append(groups, CustomResourceGroup{Group: group, Kinds: kinds})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Group < groups[j].Group })
	return groups
}

// definitionsFrom turns the outcome of listing definitions into what the
// sidebar should show.
//
// A cluster that does not serve the definitions API is not a failure: it has no
// custom resources, which is an answer. Reporting it as an error would leave
// the reader unable to tell "none here" from "I could not look" -- and those
// call for opposite reactions. Every other failure stays one.
func definitionsFrom(items []unstructured.Unstructured, err error) ([]CustomResourceGroup, error) {
	if ErrNotServed(err) {
		return []CustomResourceGroup{}, nil
	}
	if err != nil {
		return []CustomResourceGroup{}, err
	}

	pointers := make([]*unstructured.Unstructured, 0, len(items))
	for i := range items {
		pointers = append(pointers, &items[i])
	}
	return groupCustomKinds(pointers), nil
}

// CustomResourceKinds lists what a cluster defines, grouped by API group.
//
// One-shot rather than watched: the sidebar asks for it when the definitions
// section is opened, and a cluster's set of CRDs changes when someone installs
// an operator, not continuously.
//
// It is asked for as soon as a cluster answers -- which plugins a cluster has
// is read from it -- and again every minute for every connected cluster, so it
// is on the path of opening a context and must be cheap. Listing the
// definitions themselves is not: each carries its whole OpenAPI schema, and a
// cluster with a few operators installed answers with tens of megabytes to
// download and decode, for four fields of each. So the names come from the API
// server's own table of them -- a name and an age per row -- and the rest from
// the discovery cache every tab already fills. A definition discovery does not
// know about yet, one installed since the cache was filled, is the case for
// reading them in full, as is a server that will not print a table.
func (w *Watcher) CustomResourceKinds(kc Context) ([]CustomResourceGroup, error) {
	groups := []CustomResourceGroup{}
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		if kinds, ok := c.customKindsFromDiscovery(ctx); ok {
			groups = groupKinds(kinds)
			return nil
		}

		items, _, err := c.list(ctx, KindCRDs, metav1.ListOptions{})
		found, err := definitionsFrom(items, err)
		if err != nil {
			return err
		}
		groups = found
		return nil
	})
	return groups, err
}

// tableAccept asks the API server to print a list the way kubectl get shows it
// rather than send the objects.
const tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io"

// customKindsFromDiscovery is the cheap way to the sidebar's definitions: their
// names from a printed table, their kinds and scopes from discovery. False
// whenever that cannot give the whole answer, for the caller to read the
// definitions instead.
func (c *clusterClient) customKindsFromDiscovery(ctx context.Context) ([]CustomResourceKind, bool) {
	names, err := c.definitionNames(ctx)
	if err != nil {
		return nil, false
	}
	if len(names) == 0 {
		return []CustomResourceKind{}, true
	}
	lists, err := serverResources(c.disco)
	if err != nil {
		return nil, false
	}
	if kinds, ok := customKindsIn(names, lists); ok {
		return kinds, true
	}
	// A definition the cached listing has not heard of is most likely one
	// installed since it was filled. Looking again -- no more than once a
	// minute, the same allowance a mapper miss has -- answers it now and keeps
	// the next minute's recheck on the cheap path.
	if !c.refreshDiscovery() {
		return nil, false
	}
	lists, err = serverResources(c.disco)
	if err != nil {
		return nil, false
	}
	return customKindsIn(names, lists)
}

// definitionNames lists the names of a cluster's definitions -- plural.group,
// which is what a definition is required to be called -- without their bodies.
func (c *clusterClient) definitionNames(ctx context.Context) ([]string, error) {
	gk := builtinKinds[KindCRDs]
	raw, err := c.disco.RESTClient().Get().
		AbsPath("/apis", gk.Group, "v1", KindCRDs).
		SetHeader("Accept", tableAccept).
		Param("includeObject", string(metav1.IncludeNone)).
		Do(ctx).Raw()
	if err != nil {
		return nil, err
	}
	var table metav1.Table
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, err
	}
	if table.Kind != "Table" {
		return nil, fmt.Errorf("asked for a table of definitions, got %q", table.Kind)
	}
	return tableNames(table)
}

// tableNames reads the name column out of a printed table.
func tableNames(table metav1.Table) ([]string, error) {
	col := -1
	for i, def := range table.ColumnDefinitions {
		if def.Format == "name" || strings.EqualFold(def.Name, "name") {
			col = i
			break
		}
	}
	if col == -1 {
		return nil, errors.New("the table of definitions has no name column")
	}
	names := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		if col >= len(row.Cells) {
			return nil, errors.New("a row of the table of definitions has no name")
		}
		name, ok := row.Cells[col].(string)
		if !ok || name == "" {
			return nil, errors.New("a row of the table of definitions has no name")
		}
		names = append(names, name)
	}
	return names, nil
}

// customKindsIn answers for each definition, by name, from a discovery
// listing. False when any of them is missing from it: a definition created
// since the listing was cached, or one not served at any version, which only
// the definition itself can describe.
func customKindsIn(names []string, lists []*metav1.APIResourceList) ([]CustomResourceKind, bool) {
	served := map[schema.GroupResource]metav1.APIResource{}
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") {
				continue
			}
			gr := schema.GroupResource{Group: gv.Group, Resource: r.Name}
			if _, seen := served[gr]; !seen {
				served[gr] = r
			}
		}
	}

	out := make([]CustomResourceKind, 0, len(names))
	for _, name := range names {
		plural, group, found := strings.Cut(name, ".")
		if !found {
			return nil, false
		}
		r, ok := served[schema.GroupResource{Group: group, Resource: plural}]
		if !ok {
			return nil, false
		}
		label := r.Kind
		if label == "" {
			label = plural
		}
		out = append(out, CustomResourceKind{
			Kind:   CustomKind(plural, group),
			Label:  label,
			Group:  group,
			Plural: plural,
			Scoped: r.Namespaced,
		})
	}
	return out, true
}
