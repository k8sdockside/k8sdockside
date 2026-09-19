package plugins

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// What a plugin is about, in one word.
//
// The list of plugins is now long enough that reading all of it is the wrong
// way to find one: someone looking at a cluster's storage wants Longhorn and
// nothing else on the way to it. A category is the coarsest useful cut -- one
// per plugin, from a fixed list -- and the settings view filters and groups by
// it.
//
// The list is fixed rather than free text on purpose. A free-text category
// would give "network", "networking" and "Networking" as three groups within a
// week, and a filter built on it would be useless. A plugin about something
// genuinely new says nothing and is filed under Other, which is a better
// answer than a category of one.

// Categories are the categories a plugin may put itself in, in the order the
// settings view offers them. CategoryOther is last and is what an unset
// category becomes.
var Categories = []string{
	"storage",
	// The datapath is its own category rather than a corner of networking:
	// a cluster has exactly one CNI, and the plugins for them -- Cilium,
	// Calico, Flannel, Kube-OVN -- are alternatives to each other, while
	// what is under "networking" sits on top of whichever one you chose.
	"cni",
	"networking",
	"security",
	"images",
	"observability",
	"delivery",
	"virtualization",
	"cost",
	"platform",
	CategoryOther,
}

// CategoryOther is the category of a plugin that names none, and of one whose
// subject nothing else on the list describes.
const CategoryOther = "other"

// checkCategory says what is wrong with a category, or nothing. Empty is fine:
// it becomes CategoryOther.
func checkCategory(pluginID, category string) error {
	if category == "" || slices.Contains(Categories, category) {
		return nil
	}
	msg := fmt.Sprintf("plugin %q puts itself in the category %q, which is not one the app has", pluginID, category)
	if near := nearest(category, Categories); near != "" {
		msg += fmt.Sprintf(" -- did you mean %q?", near)
	} else {
		msg += fmt.Sprintf(" (one of %s)", strings.Join(Categories, ", "))
	}
	return errors.New(msg)
}

// category normalises what a manifest wrote: trimmed, lowercased, and
// CategoryOther when it said nothing.
func category(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return CategoryOther
	}
	return value
}
