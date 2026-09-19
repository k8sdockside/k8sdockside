package plugins

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/k8sdockside/k8sdockside/internal/addons"
	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// Plugins that are not built in are found somewhere. Most people will not go
// looking for a repository address, so the app carries a short list of the
// plugins it knows are out there -- each with the repository it is installed
// from and the kinds that give its product away in a cluster -- and the
// settings view offers them with one button each. The sidebar can then say
// "cert-manager is running here, and there is a plugin for it" about a cluster
// that has it.
//
// The list is data compiled into the app rather than fetched: it changes when
// a plugin is written, not every day, and asking a server what exists would be
// the app phoning home on every launch. A plugin that is not on the list is
// installed exactly as before, from its address.

//go:embed known.json
var knownRaw []byte

// Known is a plugin kept in a repository of its own that this app knows of.
type Known struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tagline string `json:"tagline,omitzero"`
	// Category is what it is about, from Categories. Empty becomes "other",
	// exactly as it does on an installed plugin's manifest.
	Category    string `json:"category,omitzero"`
	Icon        string `json:"icon,omitzero"`
	Description string `json:"description,omitzero"`
	// Repo is what installing it clones. https, so it needs no key.
	Repo string `json:"repo"`
	// Author is who wrote it, and AuthorURL where to find them. Required: the
	// list credits everyone on it, the app's own author included.
	Author    string `json:"author"`
	AuthorURL string `json:"authorUrl,omitzero"`
	// Detect are kinds whose presence in a cluster means the product the
	// plugin is about is running there. Empty for one that works anywhere,
	// which is then never suggested for a cluster in particular.
	Detect []string `json:"detect,omitzero"`
	// DetectWorkloads are objects whose presence means the same thing, for a
	// product that installs no custom resources at all -- Flannel is the
	// example: it is a DaemonSet, a ConfigMap and nothing else, so there is
	// no definition to recognise it by and only the workload itself gives it
	// away. Any one of them matching is enough. See Probe.
	DetectWorkloads []Probe `json:"detectWorkloads,omitzero"`
	// Links point at what the plugin is about, as on an installed plugin.
	Links []Link `json:"links,omitzero"`
	// Official is one kept alongside the app, by its author.
	Official bool `json:"official,omitzero"`
}

// KnownPlugins is the list, in the order it is offered. A mistake in it is a
// mistake in our own data, which a test catches, so it is fatal.
var KnownPlugins = sync.OnceValue(func() []Known {
	var list []Known
	if err := decodeStrict(knownRaw, &list); err != nil {
		panic(fmt.Sprintf("plugins: known.json: %v", describe(knownRaw, err, "", true)))
	}
	seen := map[string]bool{}
	for i, k := range list {
		k, err := validateKnown(k)
		if err != nil {
			panic(fmt.Sprintf("plugins: known.json: %v", err))
		}
		if seen[k.ID] {
			panic(fmt.Sprintf("plugins: known.json lists %q twice", k.ID))
		}
		seen[k.ID] = true
		list[i] = k
	}
	return list
})

func validateKnown(k Known) (Known, error) {
	if !addons.ValidID(k.ID) {
		return k, fmt.Errorf("%q is not a plugin id", k.ID)
	}
	if strings.TrimSpace(k.Name) == "" {
		k.Name = k.ID
	}
	if err := checkIcon(k.ID, "itself", k.Icon); err != nil {
		return k, err
	}
	if err := checkCategory(k.ID, k.Category); err != nil {
		return k, err
	}
	k.Category = category(k.Category)
	if k.Icon == "" {
		k.Icon = "puzzle"
	}
	if !strings.HasPrefix(k.Repo, "https://") || !ValidGitURL(k.Repo) {
		return k, fmt.Errorf("%s: repo %q must be an https repository address", k.ID, k.Repo)
	}
	for _, kind := range k.Detect {
		if !kube.IsKnownKind(kind) {
			return k, fmt.Errorf("%s: detects %q, which is not a kind this app can open", k.ID, kind)
		}
	}
	for i, probe := range k.DetectWorkloads {
		probe, err := validateProbe(k.ID, probe)
		if err != nil {
			return k, err
		}
		k.DetectWorkloads[i] = probe
	}
	// Links and the author are held to the plugin rules, which need a plugin
	// to report against.
	p := Plugin{ID: k.ID, Links: k.Links, Author: k.Author, AuthorURL: k.AuthorURL}
	if err := errors.Join(validateLinks(&p), validateAuthor(&p)); err != nil {
		return k, err
	}
	if p.Author == "" {
		return k, fmt.Errorf("%s: names no author; every plugin on the list is credited", k.ID)
	}
	k.Links, k.Author, k.AuthorURL = p.Links, p.Author, p.AuthorURL
	return k, nil
}

// Probe is a set of objects whose presence in a cluster gives a product away.
//
// Detecting by kind costs nothing -- the sidebar has already read the
// cluster's definitions, and a custom resource either exists there or does
// not. A probe is the other case: a product with no custom resources of its
// own, which can only be recognised by finding the thing it runs. That is a
// real query against the cluster, so probes are kept narrow (a selector, and
// usually a kind that is cheap to list) and are only ever run for plugins that
// are not installed yet.
type Probe struct {
	// Kind is what to look for: a built-in name such as "daemonsets".
	Kind string `json:"kind"`
	// Namespace narrows the search; empty looks in every namespace, which is
	// the right default for something that may be installed anywhere.
	Namespace string `json:"namespace,omitzero"`
	// Selector is the label selector that recognises it. Required: listing a
	// whole kind and calling anything a match would suggest a plugin for every
	// cluster that has DaemonSets at all.
	Selector string `json:"selector"`
}

func validateProbe(id string, p Probe) (Probe, error) {
	p.Kind = strings.TrimSpace(p.Kind)
	p.Namespace = strings.TrimSpace(p.Namespace)
	p.Selector = strings.TrimSpace(p.Selector)
	if !kube.IsKnownKind(p.Kind) {
		return p, fmt.Errorf("%s: detects the workload %q, which is not a kind this app can open", id, p.Kind)
	}
	if p.Selector == "" {
		return p, fmt.Errorf("%s: a workload probe on %s needs a selector; without one every cluster with %s matches", id, p.Kind, p.Kind)
	}
	if err := checkFilter(p.Namespace, p.Selector); err != nil {
		return p, fmt.Errorf("%s: workload probe on %s: %w", id, p.Kind, err)
	}
	return p, nil
}

// RunsIn reports whether a known plugin's product is running in a cluster, as
// far as its workload probes can tell -- one list per probe, stopping at the
// first that finds something.
//
// told is false when a probe could not be answered, which is a different thing
// from finding nothing. Suggesting a plugin needs a match, so either answer
// means "do not suggest"; saying an installed plugin is *not* here needs the
// cluster to have actually said so.
func (k Known) RunsIn(cl Cluster) (here, told bool) {
	told = true
	for _, probe := range k.DetectWorkloads {
		tally, err := cl.CountBy(probe.Kind, probe.Namespace, probe.Selector, "")
		if err != nil {
			told = false
			continue
		}
		if tally.Total > 0 {
			return true, true
		}
	}
	return false, told
}

// KnownOffer is a known plugin as the settings view lists it: whether it is
// already installed here, and from where.
type KnownOffer struct {
	Known
	// Installed is true when a plugin with this id is built in or in the
	// plugins folder, whoever put it there and however. A copy read only from
	// a watched folder does not count: installing is how to get the published
	// copy back.
	Installed bool `json:"installed"`
}

// Offer lists the known plugins against what the catalogue already has.
func (c Catalogue) Offer() []KnownOffer {
	out := make([]KnownOffer, 0, len(KnownPlugins()))
	for _, k := range KnownPlugins() {
		_, installed := c.InstalledHere(k.ID)
		out = append(out, KnownOffer{Known: k, Installed: installed})
	}
	return out
}

// officialClone reports whether an installed plugin is an official one: it
// has an official entry's id *and* sits in a clone of that entry's
// repository. The repository is what counts -- anyone can write the id -- so
// git is asked only for a plugin whose id is an official one's.
func officialClone(p Plugin) bool {
	if p.Repo == "" {
		return false
	}
	k, ok := FindKnown(p.ID)
	if !ok || !k.Official {
		return false
	}
	origin, err := OriginOf(p.Repo)
	return err == nil && SameRepository(origin, k.Repo)
}

// knownCategory is a plugin's category, falling back to the one the known list
// gives a plugin of that id.
//
// A plugin written before categories existed names none, and would sit under
// "other" in a settings view that offers a chip for what it plainly is: the
// list already knows Cilium is a CNI, and it is the same plugin whether it is
// being offered or has just been installed. Matched on the id alone, unlike
// Official, which is checked against the repository -- a category is a label
// on a card, not a claim about who wrote it, so the worst an id borrowed from
// the list can do with it is file itself under the wrong chip.
func knownCategory(p Plugin) string {
	if p.Category != "" && p.Category != CategoryOther {
		return p.Category
	}
	if k, ok := FindKnown(p.ID); ok && k.Category != "" {
		return k.Category
	}
	return category(p.Category)
}

// FindKnown returns the known plugin with the given id.
func FindKnown(id string) (Known, bool) {
	for _, k := range KnownPlugins() {
		if k.ID == id {
			return k, true
		}
	}
	return Known{}, false
}
