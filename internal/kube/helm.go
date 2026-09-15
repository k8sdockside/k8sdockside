package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Helm releases, which are not a Kubernetes kind at all.
//
// Helm 3 keeps each revision of a release as a Secret whose payload is the
// release JSON, gzipped and base64-encoded -- inside the base64 the API server
// already applies to every Secret value. So this reads Secrets and decodes them
// rather than watching a resource.
//
// The rows are read rather than taken from a cache, and deliberately: that
// payload carries the rendered manifest and the chart values, which routinely
// hold credentials, and the informer cache is exactly where those must not sit.
// The decode keeps six summary fields and drops everything else on the floor
// before returning.
//
// That is not the same as not being live. SubscribeHelm watches the release
// Secrets for the one thing the cache may safely hold -- that they changed --
// and re-reads on each change, so a release upgraded from another machine
// repaints here the way a pod does. See stripReleasePayload for what the watch
// is allowed to remember, which is nothing that was in the release.

// HelmReleaseSecretType marks a Secret as one of Helm 3's release records.
const HelmReleaseSecretType = "helm.sh/release.v1" // #nosec G101 -- a Secret type label, not a credential

// helmReleaseField narrows a listing -- or a watch -- to those records.
//
// A *field* selector on the Secret's type, not to be confused with
// helmReleaseSelector in helmdetail.go, which is a label selector picking out
// the revisions of one named release.
//
// Asking the API server to filter is what keeps the payloads of every unrelated
// Secret in the cluster from crossing the wire at all, which matters more for
// the watch than for the read: a watch left unfiltered would stream every
// Secret change in the cluster for as long as the tab is open.
const helmReleaseField = "type=" + HelmReleaseSecretType

// maxReleasePayload caps how much a single release may decompress to. A gzip
// stream can claim to be far larger than it is, and nothing here should be able
// to exhaust memory because a cluster held an unusual object.
const maxReleasePayload = 8 << 20

// HelmRelease is the summary of one release: everything shown in the table, and
// nothing else from the payload.
//
// Description is Helm's own one-line log entry for the revision -- "Upgrade
// complete", "Rollback to 3" -- which the table has no column for and the
// history in the detail drawer is largely made of. It is read here rather than
// in a second decoder because it comes from info, alongside the status and the
// timestamp, and not from the parts of the payload this decode exists to drop.
type HelmRelease struct {
	Name        string
	Namespace   string
	Revision    int64
	Status      string
	Chart       string
	AppVersion  string
	Updated     string
	Description string
}

// releaseJSON is the subset of Helm's release record worth reading. Every other
// field -- config, manifest, hooks -- is deliberately absent, so decoding
// cannot retain them even by accident.
type releaseJSON struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int64  `json:"version"`
	Info      struct {
		Status       string `json:"status"`
		LastDeployed string `json:"last_deployed"`
		Description  string `json:"description"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
}

// releasePayload unwraps one release Secret into the JSON Helm stored in it.
//
// Split out from the decode below because the detail drawer reads the same
// wrapper for the opposite purpose: the summary here parses six fields out of
// this JSON and drops it, while helmdetail.go parses the values, the notes and
// the manifest out of it. One unwrapper means the two cannot disagree about
// what a release Secret is.
func releasePayload(u *unstructured.Unstructured) ([]byte, error) {
	if nestedString(u, "type") != HelmReleaseSecretType {
		return nil, errors.New("not a Helm release secret")
	}

	encoded := nestedString(u, "data", "release")
	if encoded == "" {
		return nil, errors.New("release secret has no payload")
	}

	// Two layers of base64: the API server's, then Helm's own.
	outer, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	payload, err := base64.StdEncoding.DecodeString(string(outer))
	if err != nil {
		// Older Helm writes the gzip stream without the inner encoding.
		payload = outer
	}

	return gunzip(payload)
}

// decodeHelmRelease turns one release Secret into its summary.
func decodeHelmRelease(u *unstructured.Unstructured) (HelmRelease, error) {
	raw, err := releasePayload(u)
	if err != nil {
		return HelmRelease{}, err
	}

	var record releaseJSON
	if err := json.Unmarshal(raw, &record); err != nil {
		return HelmRelease{}, fmt.Errorf("parsing release: %w", err)
	}

	chart := record.Chart.Metadata.Name
	if v := record.Chart.Metadata.Version; v != "" {
		chart += "-" + v
	}
	namespace := record.Namespace
	if namespace == "" {
		namespace = u.GetNamespace()
	}

	return HelmRelease{
		Name:        record.Name,
		Namespace:   namespace,
		Revision:    record.Version,
		Status:      record.Info.Status,
		Chart:       chart,
		AppVersion:  record.Chart.Metadata.AppVersion,
		Updated:     record.Info.LastDeployed,
		Description: record.Info.Description,
	}, nil
}

// gunzip decompresses a release payload, refusing one that claims to be absurd.
func gunzip(payload []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("release is not gzipped: %w", err)
	}
	defer func() { _ = zr.Close() }()

	raw, err := io.ReadAll(io.LimitReader(zr, maxReleasePayload))
	if err != nil {
		return nil, fmt.Errorf("reading release: %w", err)
	}
	return raw, nil
}

// helmTable projects release secrets into the table the UI renders, keeping the
// current revision of each release.
//
// A release that cannot be decoded is skipped rather than failing the listing:
// one unreadable record -- an older Helm, a hand-edited secret -- should not
// cost the user the sight of every other release they have.
func helmTable(secrets []*unstructured.Unstructured) Table {
	current := map[string]HelmRelease{}
	for _, s := range secrets {
		release, err := decodeHelmRelease(s)
		if err != nil || release.Name == "" {
			continue
		}
		key := release.Namespace + "/" + release.Name
		if existing, seen := current[key]; !seen || release.Revision > existing.Revision {
			current[key] = release
		}
	}

	releases := make([]HelmRelease, 0, len(current))
	for _, r := range current {
		releases = append(releases, r)
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Namespace != releases[j].Namespace {
			return releases[i].Namespace < releases[j].Namespace
		}
		return releases[i].Name < releases[j].Name
	})

	rows := make([]Row, 0, len(releases))
	for _, r := range releases {
		rows = append(rows, Row{
			ID:        rowID(KindHelmReleases, r.Namespace, r.Name),
			Name:      r.Name,
			Namespace: r.Namespace,
			Cells: []Cell{
				plain(r.Name),
				muted(r.Namespace),
				number(int(r.Revision)),
				status(r.Status),
				muted(r.Chart),
				muted(r.AppVersion),
				timeCell(parseTime(r.Updated)),
			},
		})
	}

	return Table{
		Kind:       KindHelmReleases,
		Columns:    []string{"Name", "Namespace", "Revision", "Status", "Chart", "App Version", "Updated"},
		Rows:       rows,
		Namespaced: true,
	}
}

// HelmReleases lists the releases installed in a cluster, in the given
// namespaces or in all of them when none is named.
func (w *Watcher) HelmReleases(kc Context, namespaces []string) (Table, error) {
	return w.helmReleases(kc, namespaceFilter(namespaces))
}

// helmReleases is the read itself, taking the namespace filter already resolved
// so that a subscription -- whose filter can change without the tab reopening --
// can share it with the one-shot call above.
func (w *Watcher) helmReleases(kc Context, keep map[string]bool) (Table, error) {
	var table Table
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		items, _, err := c.list(ctx, KindSecrets, metav1.ListOptions{
			FieldSelector: helmReleaseField,
		})
		if err != nil {
			return err
		}

		pointers := make([]*unstructured.Unstructured, 0, len(items))
		for i := range items {
			if keep != nil && !keep[items[i].GetNamespace()] {
				continue
			}
			pointers = append(pointers, &items[i])
		}
		table = helmTable(pointers)
		return nil
	})
	return table, err
}

// SubscribeHelm opens a live view of a cluster's Helm releases and returns its
// subscription ID, pushing through the same emit as every other subscription.
//
// It is the one subscription that does not serve its rows from the cache it
// watches. A release is a Secret whose payload must not be cached, so the watch
// is narrowed to release records, stripped of that payload on the way in, and
// used purely as a signal: when it fires, the releases are read again and
// decoded down to their summaries. The user sees a live table; the app holds no
// chart values.
//
// The cost of that is one LIST per change, against a field-selected collection,
// after the pump's coalescing window. A release changes when someone deploys,
// which is not the rate a pod list changes at.
func (w *Watcher) SubscribeHelm(kc Context, namespaces []string) (string, error) {
	return w.SubscribeHelmFor(kc, namespaces, nil)
}

// SubscribeHelmFor is SubscribeHelm with a claim, called with the new
// subscription's ID before anything is pushed under it -- see SubscribeFor.
func (w *Watcher) SubscribeHelmFor(kc Context, namespaces []string, claim func(id string)) (string, error) {
	cl, err := w.clusterFor(kc)
	if err != nil {
		return "", err
	}

	// Secrets rather than helmreleases: there is no such kind, which is the
	// whole reason this path exists.
	mapping, err := cl.client.mappingForKind(KindSecrets)
	if err != nil {
		w.releaseCluster(kc.ID)
		return "", err
	}

	live := w.informerFor(cl, mapping, helmReleaseField, stripReleasePayload)

	sub := &subscription{
		id:         fmt.Sprintf("sub-%d", w.nextID.Add(1)),
		contextID:  kc.ID,
		kc:         kc,
		kind:       KindHelmReleases,
		reread:     true,
		namespaces: namespaceFilter(namespaces),
		live:       live,
		dirty:      make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	if claim != nil {
		claim(sub.id)
	}

	w.mu.Lock()
	w.subs[sub.id] = sub
	w.mu.Unlock()

	go w.pump(sub)
	go w.firstSnapshot(sub)

	return sub.id, nil
}

// stripReleasePayload drops a release Secret's payload before it is cached.
//
// stripBulk redacts Secret values too, but only for an object that arrives
// carrying its kind, and this is not a place to depend on that: what would be
// retained on a miss is precisely the rendered manifest and the chart values.
// Here the payload is removed unconditionally, because this watch has no use
// for it under any circumstances -- it exists to notice that a release changed,
// and the release itself is then read live. See SubscribeHelm.
func stripReleasePayload(obj any) (any, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil
	}
	u.SetManagedFields(nil)
	unstructured.RemoveNestedField(u.Object, "data")
	return u, nil
}
