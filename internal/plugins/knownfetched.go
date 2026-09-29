package plugins

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/k8sdockside/k8sdockside/internal/updates"
)

// The known list, read again from the repository.
//
// known.json on the repository's main branch is the same file that is compiled
// into the app, so adding a plugin there reaches every app that fetches it,
// without a release. The fetch itself lives with the services (see
// services/knownlist.go); what is here is what the app does with the answer,
// and it is careful in three ways the compiled-in copy never needs to be:
//
//   - It is read leniently. The file on main is written for the newest app,
//     and an older one must not throw the whole list away over a field it has
//     never heard of: unknown fields are ignored, and an entry that does not
//     pass this app's checks -- a kind it cannot open, a category it does not
//     know -- is left out on its own.
//   - An entry for a newer app than this one is left out too, by its
//     minAppVersion, rather than offered as an install that would not load.
//   - It can add plugins, and update what an entry says about itself, but it
//     cannot move a plugin the compiled-in list already has to another
//     repository, nor change which of those are official. The repository is
//     what installing clones and what the Official badge is checked against;
//     for the plugins this release shipped with, that stays what shipped.
//
// Entries the compiled-in list has and the fetched one does not are kept: a
// plugin leaves the list with a release, not with an edit on main.

// fetchedKnown is the merged list once a fetched copy has been accepted; nil
// until then, when the compiled-in list is the list.
var fetchedKnown atomic.Pointer[[]Known]

// KnownPlugins is the list the app offers right now, in the order it is
// offered: the fetched copy merged over the compiled-in one when there is one,
// and the compiled-in one alone otherwise.
func KnownPlugins() []Known {
	if list := fetchedKnown.Load(); list != nil {
		return *list
	}
	return embeddedKnown()
}

// ParseFetchedKnown reads a copy of known.json from outside the app, keeping
// the entries this app can offer. skipped says, one line each, what was left
// out and why. It fails only when the file is not a list at all, or has
// nothing usable in it -- an empty answer is a broken answer, and must not
// empty the list.
func ParseFetchedKnown(data []byte, appVersion string) (list []Known, skipped []string, err error) {
	var raw []jsontext.Value
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("the plugin list is not a JSON list: %w", err)
	}
	seen := map[string]bool{}
	for i, entry := range raw {
		var k Known
		// Lenient on purpose: unknown fields are ignored (the default for
		// encoding/json/v2), so a field added for a newer app is no reason to
		// drop the entry, let alone the list.
		if err := json.Unmarshal(entry, &k); err != nil {
			skipped = append(skipped, fmt.Sprintf("entry %d: %v", i+1, err))
			continue
		}
		k, err := validateKnown(k)
		if err != nil {
			skipped = append(skipped, err.Error())
			continue
		}
		if k.MinAppVersion != "" && updates.IsVersion(appVersion) && updates.Compare(appVersion, k.MinAppVersion) < 0 {
			skipped = append(skipped, fmt.Sprintf("%s: needs K8s Dockside %s or newer", k.ID, k.MinAppVersion))
			continue
		}
		if seen[k.ID] {
			skipped = append(skipped, fmt.Sprintf("%s: listed twice; the first is kept", k.ID))
			continue
		}
		seen[k.ID] = true
		list = append(list, k)
	}
	if len(list) == 0 {
		return nil, skipped, fmt.Errorf("the plugin list has no entry this app can offer")
	}
	return list, skipped, nil
}

// UseFetchedKnown makes a parsed copy the list, merged over the compiled-in
// one as described above. Nil goes back to the compiled-in list alone.
func UseFetchedKnown(list []Known) {
	if list == nil {
		fetchedKnown.Store(nil)
		return
	}
	merged := mergeKnown(embeddedKnown(), list)
	fetchedKnown.Store(&merged)
}

// mergeKnown lays fetched over base: base's order first, each entry replaced by
// the fetched one of the same id (keeping base's repository and official
// flag), then the entries only the fetched list has, in its order.
func mergeKnown(base, fetched []Known) []Known {
	byID := make(map[string]Known, len(fetched))
	for _, k := range fetched {
		byID[k.ID] = k
	}
	out := make([]Known, 0, len(base)+len(fetched))
	have := make(map[string]bool, len(base))
	for _, b := range base {
		have[b.ID] = true
		f, ok := byID[b.ID]
		if !ok {
			out = append(out, b)
			continue
		}
		f.Repo, f.Official = b.Repo, b.Official
		out = append(out, f)
	}
	for _, f := range fetched {
		if !have[f.ID] {
			out = append(out, f)
		}
	}
	return slices.Clip(out)
}
