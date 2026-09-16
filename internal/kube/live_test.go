package kube

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// The live tests run against a real cluster, which most runs will not have, so
// they are opt-in:
//
//	K8SDOCKSIDE_TEST_KUBECONFIG=~/kubeconfig/hrw.config \
//	K8SDOCKSIDE_TEST_CONTEXT=admin@hrw go test ./internal/kube/ -run Live -v
//
// They are the only check that the informer, projection and mapping layers
// agree with an actual API server; everything else in this package is exercised
// against literals.
func liveContext(t *testing.T) Context {
	t.Helper()

	path := os.Getenv("K8SDOCKSIDE_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("set K8SDOCKSIDE_TEST_KUBECONFIG to run the live tests")
	}
	resolved := resolve(path)

	file := ParseFile(resolved, SourceManual)
	if file.Error != "" {
		t.Fatalf("reading %s: %s", resolved, file.Error)
	}

	want := os.Getenv("K8SDOCKSIDE_TEST_CONTEXT")
	for _, ctx := range file.Contexts {
		if want == "" || ctx.Name == want {
			return ctx
		}
	}
	t.Fatalf("context %q not found in %s", want, resolved)
	return Context{}
}

func TestLiveSubscribeDeliversASnapshot(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 8)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	id, err := w.Subscribe(ctx, KindPods, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	select {
	case snap := <-snapshots:
		if snap.SubscriptionID != id {
			t.Errorf("subscription = %q, want %q", snap.SubscriptionID, id)
		}
		if snap.Table.Error != "" {
			t.Fatalf("table error: %s", snap.Table.Error)
		}
		if !snap.Table.Namespaced {
			t.Error("pods reported as cluster-scoped")
		}
		if len(snap.Table.Columns) == 0 {
			t.Fatal("no columns")
		}
		t.Logf("%d pods, columns: %v", len(snap.Table.Rows), snap.Table.Columns)
		for i, row := range snap.Table.Rows {
			if i >= 3 {
				break
			}
			t.Logf("  %s/%s %v", row.Namespace, row.Name, cellTexts(row))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("no snapshot within 60s")
	}
}

// Disconnect closes every subscription to the context and its watches, and
// the context can be opened again straight after.
func TestLiveDisconnectClosesTheContext(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 16)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	for _, kind := range []string{KindPods, KindPods, KindNamespaces} {
		if _, err := w.Subscribe(ctx, kind, nil, NoSelector); err != nil {
			t.Fatalf("Subscribe(%s): %v", kind, err)
		}
	}
	select {
	case <-snapshots:
	case <-time.After(60 * time.Second):
		t.Fatal("no snapshot within 60s")
	}

	if closed := w.Disconnect(ctx.ID); closed != 3 {
		t.Errorf("Disconnect closed %d subscriptions, want 3", closed)
	}
	w.mu.Lock()
	subs, held := len(w.subs), len(w.clusters)
	w.mu.Unlock()
	if subs != 0 || held != 0 {
		t.Fatalf("after Disconnect: %d subscriptions and %d clients left, want none", subs, held)
	}

	id, err := w.Subscribe(ctx, KindPods, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe after Disconnect: %v", err)
	}
	deadline := time.After(60 * time.Second)
	for {
		select {
		case snap := <-snapshots:
			if snap.SubscriptionID == id {
				return
			}
		case <-deadline:
			t.Fatal("no snapshot for the new subscription within 60s")
		}
	}
}

func TestLiveGatewayAPIAndCustomResources(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 32)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	// The CRD list is the entry point for the drill-in, so it has to work even
	// on a cluster with no Gateway API installed.
	id, err := w.Subscribe(ctx, KindCRDs, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe(%s): %v", KindCRDs, err)
	}

	var definitions Table
	deadline := time.After(60 * time.Second)
	for definitions.Columns == nil {
		select {
		case snap := <-snapshots:
			if snap.SubscriptionID == id {
				definitions = snap.Table
			}
		case <-deadline:
			t.Fatal("no CRD snapshot within 60s")
		}
	}
	if definitions.Error != "" {
		t.Fatalf("CRD table error: %s", definitions.Error)
	}
	t.Logf("%d CRDs installed", len(definitions.Rows))

	// Drilling into the first CRD must produce a working tab: this is the whole
	// point of the "crd:" kind, and it exercises reading printer columns off a
	// definition we have never seen.
	if len(definitions.Rows) == 0 {
		t.Skip("cluster has no CRDs to drill into")
	}
	first := definitions.Rows[0].Name

	customID, err := w.Subscribe(ctx, CustomPrefix+first, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe(crd:%s): %v", first, err)
	}
	deadline = time.After(60 * time.Second)
	for {
		select {
		case snap := <-snapshots:
			if snap.SubscriptionID != customID {
				continue
			}
			if snap.Table.Error != "" {
				t.Fatalf("crd:%s error: %s", first, snap.Table.Error)
			}
			t.Logf("crd:%s -> %d rows, columns %v", first, len(snap.Table.Rows), snap.Table.Columns)
			return
		case <-deadline:
			t.Fatalf("no snapshot for crd:%s within 60s", first)
		}
	}
}

func TestLiveOverviewAndNamespaces(t *testing.T) {
	ctx := liveContext(t)

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	namespaces, err := w.Namespaces(ctx)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if len(namespaces) == 0 {
		t.Error("no namespaces")
	}

	overview, err := w.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.Version == "" {
		t.Error("no server version")
	}
	t.Logf("version %s, distribution %q, %d namespaces", overview.Version, overview.Distribution, len(overview.Namespaces))
	// Each counter names the kind it counts, so the dashboard can open that
	// kind's list when the tile is clicked -- and it has to be a kind the
	// sidebar lists, or the click opens a tab on nothing.
	wantKinds := map[string]string{"Nodes": KindNodes, "Pods": KindPods, "Deployments": KindDeployments, "Namespaces": KindNamespaces}
	for _, s := range overview.Stats {
		t.Logf("  %s (%s): %d/%d", s.Label, s.Kind, s.Ready, s.Total)
		if s.Kind != wantKinds[s.Label] {
			t.Errorf("stat %q names kind %q, want %q", s.Label, s.Kind, wantKinds[s.Label])
		}
	}

	// The dashboard's resource accounting comes from Budget now. Against a real
	// cluster this is the only place the metrics-server path is exercised at
	// all -- and a cluster without one must still answer, with the used column
	// absent and a reason given.
	budget, err := w.Budget(ctx, Scope{Kind: ScopeCluster}, nil)
	if err != nil {
		t.Fatalf("Budget: %v", err)
	}
	t.Logf("usage source %q %s", budget.Usage.Source, budget.Usage.Error)
	for _, a := range budget.Amounts {
		t.Logf("  %s: %.2f requested, %.2f limits, %.2f used of %.2f allocatable (%.2f capacity) %s",
			a.Label, a.Requested, a.Limits, a.Used, a.Allocatable, a.Capacity, a.Unit)
	}
	if len(budget.Amounts) == 0 {
		t.Error("no amounts in the cluster budget")
	}
}

func cellTexts(row Row) []string {
	out := make([]string, len(row.Cells))
	for i, c := range row.Cells {
		out[i] = c.Text
	}
	return out
}

// TestLiveGatewayKindsResolve checks the Gateway API entries against a cluster
// that actually serves them: the version is picked by the mapper, so this is
// the only place the group/kind table is proven right.
func TestLiveGatewayKindsResolve(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 32)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	served := 0
	for _, kind := range []string{
		KindGatewayClasses, KindGateways, KindListenerSets,
		KindHTTPRoutes, KindGRPCRoutes, KindTLSRoutes, KindTCPRoutes, KindUDPRoutes,
		KindBackendTLSPolicies, KindReferenceGrants,
	} {
		id, err := w.Subscribe(ctx, kind, nil, NoSelector)
		if err != nil {
			// The Gateway API is optional, and a cluster without it must say so
			// rather than fail: that message is what the tab shows.
			if ErrNotServed(err) {
				t.Logf("%-18s not served: %v", kind, err)
				continue
			}
			t.Errorf("Subscribe(%s): %v", kind, err)
			continue
		}
		if awaitSnapshot(t, snapshots, id, kind) {
			served++
		}
	}
	if served == 0 {
		t.Skip("this cluster does not have the Gateway API installed")
	}
}

// TestLiveCustomColumnsComeFromTheDefinition proves the drill-in renders the
// CRD's own additionalPrinterColumns rather than falling back to name and age.
func TestLiveCustomColumnsComeFromTheDefinition(t *testing.T) {
	ctx := liveContext(t)

	const kind = CustomPrefix + "certificates.cert-manager.io"

	snapshots := make(chan Snapshot, 8)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	id, err := w.Subscribe(ctx, kind, nil, NoSelector)
	if err != nil {
		t.Skipf("cert-manager not installed: %v", err)
	}

	select {
	case snap := <-snapshots:
		if snap.SubscriptionID != id {
			t.Fatalf("unexpected subscription %q", snap.SubscriptionID)
		}
		if snap.Table.Error != "" {
			t.Fatalf("table error: %s", snap.Table.Error)
		}
		t.Logf("columns: %v", snap.Table.Columns)

		// cert-manager's Certificate CRD declares Ready, Secret and Issuer.
		// Seeing only the fallback set means the definition was never read.
		if len(snap.Table.Columns) <= 3 {
			t.Errorf("columns = %v, want the CRD's own printer columns", snap.Table.Columns)
		}
		for i, row := range snap.Table.Rows {
			if i >= 3 {
				break
			}
			t.Logf("  %s/%s %v", row.Namespace, row.Name, cellTexts(row))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("no snapshot within 60s")
	}
}

// awaitSnapshot waits for one subscription's first push, reporting what arrived.
func awaitSnapshot(t *testing.T, snapshots chan Snapshot, id, kind string) bool {
	t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case snap := <-snapshots:
			if snap.SubscriptionID != id {
				continue
			}
			if snap.Table.Error != "" {
				t.Errorf("%s: %s", kind, snap.Table.Error)
				return false
			}
			t.Logf("%-18s %2d rows  %v", kind, len(snap.Table.Rows), snap.Table.Columns)
			return true
		case <-deadline:
			t.Errorf("%s: no snapshot within 60s", kind)
			return false
		}
	}
}

// TestLiveNamespaceFilterAppliesWithoutReopening checks the filter path: the
// watch is cluster-wide, so narrowing to one namespace must change the rows
// without the subscription being torn down and rebuilt.
func TestLiveNamespaceFilterAppliesWithoutReopening(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 16)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	id, err := w.Subscribe(ctx, KindPods, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	all := awaitTable(t, snapshots, id)
	if len(all.Rows) == 0 {
		t.Skip("no pods to filter")
	}
	target := all.Rows[0].Namespace

	w.SetNamespaces(id, []string{target})

	filtered := awaitTable(t, snapshots, id)
	for len(filtered.Rows) == len(all.Rows) && namespaceCount(all, target) != len(all.Rows) {
		// The first push after SetNamespace may still be an in-flight snapshot
		// of the unfiltered set; keep reading until the filter has landed.
		filtered = awaitTable(t, snapshots, id)
	}

	for _, row := range filtered.Rows {
		if row.Namespace != target {
			t.Fatalf("row %s is in %q, want only %q", row.Name, row.Namespace, target)
		}
	}
	t.Logf("all namespaces: %d pods; %s: %d pods", len(all.Rows), target, len(filtered.Rows))

	if len(filtered.Rows) != namespaceCount(all, target) {
		t.Errorf("filtered to %d rows, want %d", len(filtered.Rows), namespaceCount(all, target))
	}
}

func namespaceCount(t Table, namespace string) int {
	n := 0
	for _, row := range t.Rows {
		if row.Namespace == namespace {
			n++
		}
	}
	return n
}

func awaitTable(t *testing.T, snapshots chan Snapshot, id string) Table {
	t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case snap := <-snapshots:
			if snap.SubscriptionID != id {
				continue
			}
			if snap.Table.Error != "" {
				t.Fatalf("table error: %s", snap.Table.Error)
			}
			return snap.Table
		case <-deadline:
			t.Fatal("no snapshot within 60s")
		}
	}
}

// TestLiveEventsAreMostRecentFirst checks the ordering against a real cluster's
// events, which is where the timestamp fields actually vary: older controllers
// set lastTimestamp, newer ones only eventTime.
func TestLiveEventsAreMostRecentFirst(t *testing.T) {
	ctx := liveContext(t)

	snapshots := make(chan Snapshot, 8)
	w := NewWatcher(func(s Snapshot) { snapshots <- s })
	defer w.Close()

	id, err := w.Subscribe(ctx, KindEvents, nil, NoSelector)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	table := awaitTable(t, snapshots, id)
	if len(table.Rows) < 2 {
		t.Skip("not enough events to check an order")
	}

	at := -1
	for i, c := range table.Columns {
		if c == "Last Seen" {
			at = i
		}
	}
	if at == -1 {
		t.Fatal("no Last Seen column")
	}

	previous := int64(-1)
	for i, row := range table.Rows {
		cell := row.Cells[at]
		if cell.Sort == "" {
			t.Fatalf("row %d (%s) has no sort key -- its timestamp field was not read", i, row.Name)
		}
		age, err := strconv.ParseInt(cell.Sort, 10, 64)
		if err != nil {
			t.Fatalf("row %d sort key %q is not a number", i, cell.Sort)
		}
		if age < previous {
			t.Errorf("row %d (%s, %s) is more recent than the row above it", i, row.Name, cell.Text)
		}
		previous = age
		if i < 5 {
			t.Logf("  %-8s %-22s %s", cell.Text, truncate(row.Name, 22), truncate(cellTexts(row)[1], 60))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// TestLiveDashboardEventsAreOrdered checks the dashboard panel, which builds
// its table on a different path from the events tab and so could drift from it.
func TestLiveDashboardEventsAreOrdered(t *testing.T) {
	ctx := liveContext(t)

	w := NewWatcher(func(Snapshot) {})
	defer w.Close()

	overview, err := w.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.Events.Error != "" {
		t.Fatalf("events panel: %s", overview.Events.Error)
	}
	if len(overview.Events.Columns) == 0 {
		t.Fatal("the events panel has no columns to sort by")
	}
	t.Logf("columns: %v", overview.Events.Columns)

	if len(overview.Events.Rows) > dashboardEvents {
		t.Errorf("%d rows, want at most %d", len(overview.Events.Rows), dashboardEvents)
	}
	if len(overview.Events.Rows) < 2 {
		t.Skip("not enough events to check an order")
	}

	at := -1
	for i, c := range overview.Events.Columns {
		if c == "Last Seen" {
			at = i
		}
	}
	if at == -1 {
		t.Fatal("no Last Seen column")
	}

	previous := int64(-1)
	for i, row := range overview.Events.Rows {
		age, err := strconv.ParseInt(row.Cells[at].Sort, 10, 64)
		if err != nil {
			t.Fatalf("row %d has no usable sort key (%q)", i, row.Cells[at].Sort)
		}
		if age < previous {
			t.Errorf("row %d (%s) is more recent than the row above it", i, row.Cells[at].Text)
		}
		previous = age
		if i < 4 {
			t.Logf("  %-8s %s", row.Cells[at].Text, truncate(row.Name, 40))
		}
	}
}
