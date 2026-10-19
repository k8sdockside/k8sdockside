package kube

import (
	"slices"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// obj builds an unstructured object from a literal, so the tests read like the
// YAML they stand in for.
func obj(fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: fields}
	created := u.GetCreationTimestamp()
	if created.IsZero() {
		u.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-90 * time.Minute)))
	}
	return u
}

func cellsByHeader(t *testing.T, table Table, row int) map[string]string {
	t.Helper()
	if row >= len(table.Rows) {
		t.Fatalf("row %d out of range, table has %d rows", row, len(table.Rows))
	}
	out := map[string]string{}
	for i, header := range table.Columns {
		if i < len(table.Rows[row].Cells) {
			out[header] = table.Rows[row].Cells[i].Text
		}
	}
	return out
}

// cellsWithTone is cellsByHeader for the tests that are about the colour a cell
// is given rather than the text in it.
func cellsWithTone(t *testing.T, table Table, row int) map[string]Cell {
	t.Helper()
	if row >= len(table.Rows) {
		t.Fatalf("row %d out of range, table has %d rows", row, len(table.Rows))
	}
	out := map[string]Cell{}
	for i, header := range table.Columns {
		if i < len(table.Rows[row].Cells) {
			out[header] = table.Rows[row].Cells[i]
		}
	}
	return out
}

// podOf builds a one-container pod in a phase, with that container ready or not.
func podOf(name, phase string, ready bool, state map[string]any) *unstructured.Unstructured {
	return obj(map[string]any{
		"metadata": map[string]any{"name": name, "namespace": "prod"},
		"spec":     map[string]any{"nodeName": "worker-1", "containers": []any{map[string]any{"name": "app"}}},
		"status": map[string]any{
			"phase": phase,
			"containerStatuses": []any{map[string]any{
				"ready":        ready,
				"restartCount": int64(0),
				"state":        state,
			}},
		},
	})
}

func podRow(t *testing.T, pod *unstructured.Unstructured) map[string]Cell {
	t.Helper()
	table := buildLiveTable(KindPods, builtinColumns[KindPods], false, []*unstructured.Unstructured{pod})
	return cellsWithTone(t, table, 0)
}

// A pod that has run to completion has no ready containers and never will, so
// the ratio rules would mark every finished Job pod as a broken one -- and in a
// namespace running a CronJob every few minutes, that is most of the list.
func TestAFinishedPodsReadyCountIsStatedRatherThanJudged(t *testing.T) {
	completed := map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)}}
	cells := podRow(t, podOf("backup-29285", "Succeeded", false, completed))

	if got := cells["Ready"]; got.Text != "0/1" || got.Tone != "info" {
		t.Errorf("Ready = %+v, want 0/1 muted rather than coloured as a failure", got)
	}
	// What happened to it is the Status column's to say, and it says it well.
	if got := cells["Status"]; got.Text != "Succeeded" || got.Tone != "ok" {
		t.Errorf("Status = %+v, want Succeeded in the good tone", got)
	}
}

// ...including one that finished badly. The pod is still red, in the column
// that is actually about how it went.
func TestAFailedPodIsRedInItsStatusRatherThanItsReadyCount(t *testing.T) {
	failed := map[string]any{"terminated": map[string]any{"reason": "Error", "exitCode": int64(1)}}
	cells := podRow(t, podOf("migrate-x9", "Failed", false, failed))

	if got := cells["Ready"]; got.Text != "0/1" || got.Tone != "info" {
		t.Errorf("Ready = %+v, want 0/1 muted", got)
	}
	if got := cells["Status"]; got.Text != "Error" || got.Tone != "error" {
		t.Errorf("Status = %+v, want Error in the error tone", got)
	}
}

// The point of the change is not to stop colouring the column: a pod that is
// meant to be serving and is not ready is exactly what the red is for.
func TestARunningPodWithNothingReadyIsStillRed(t *testing.T) {
	cells := podRow(t, podOf("api-0", "Running", false, map[string]any{"running": map[string]any{}}))

	if got := cells["Ready"]; got.Text != "0/1" || got.Tone != "error" {
		t.Errorf("Ready = %+v, want 0/1 in the error tone", got)
	}
}

func TestARunningPodThatIsReadyIsStillGreen(t *testing.T) {
	cells := podRow(t, podOf("api-0", "Running", true, map[string]any{"running": map[string]any{}}))

	if got := cells["Ready"]; got.Text != "1/1" || got.Tone != "ok" {
		t.Errorf("Ready = %+v, want 1/1 in the good tone", got)
	}
}

// The phase is the signal, not what created the pod: a bare pod with a
// restartPolicy of Never that runs to completion is in the same position as a
// Job's, and nothing here has to know which it was.
func TestABarePodThatRanToCompletionIsTreatedTheSame(t *testing.T) {
	pod := podOf("one-shot", "Succeeded", false, map[string]any{
		"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)},
	})
	_ = unstructured.SetNestedField(pod.Object, "Never", "spec", "restartPolicy")

	if got := podRow(t, pod)["Ready"]; got.Tone != "info" {
		t.Errorf("Ready = %+v, want it muted whatever created the pod", got)
	}
}

func TestPodStatusPrefersTheWaitingReasonOverThePhase(t *testing.T) {
	pod := obj(map[string]any{
		"metadata": map[string]any{"name": "api-0", "namespace": "prod"},
		"spec":     map[string]any{"nodeName": "worker-1", "containers": []any{map[string]any{"name": "api"}}},
		"status": map[string]any{
			"phase":    "Pending",
			"qosClass": "Burstable",
			"containerStatuses": []any{map[string]any{
				"ready":        false,
				"restartCount": int64(7),
				"state":        map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}},
			}},
		},
	})

	table := buildLiveTable(KindPods, withNamespace(builtinColumns[KindPods], true), true, []*unstructured.Unstructured{pod})
	cells := cellsByHeader(t, table, 0)

	// The phase alone would say "Pending", which hides why.
	if cells["Status"] != "ImagePullBackOff" {
		t.Errorf("Status = %q, want ImagePullBackOff", cells["Status"])
	}
	if cells["Ready"] != "0/1" {
		t.Errorf("Ready = %q, want 0/1", cells["Ready"])
	}
	if cells["Restarts"] != "7" {
		t.Errorf("Restarts = %q, want 7", cells["Restarts"])
	}
	if cells["Node"] != "worker-1" {
		t.Errorf("Node = %q, want worker-1", cells["Node"])
	}
	if cells["Namespace"] != "prod" {
		t.Errorf("Namespace = %q, want prod", cells["Namespace"])
	}
}

func TestPodRestartsAreSummedAcrossContainers(t *testing.T) {
	pod := obj(map[string]any{
		"metadata": map[string]any{"name": "api-0"},
		"status": map[string]any{
			"phase": "Running",
			"containerStatuses": []any{
				map[string]any{"ready": true, "restartCount": int64(2), "state": map[string]any{"running": map[string]any{}}},
				map[string]any{"ready": true, "restartCount": int64(3), "state": map[string]any{"running": map[string]any{}},
					// Recent, so the restarts are current and the list shows them.
					"lastState": map[string]any{"terminated": map[string]any{
						"finishedAt": time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
					}}},
			},
		},
	})

	table := buildLiveTable(KindPods, builtinColumns[KindPods], false, []*unstructured.Unstructured{pod})
	cells := cellsByHeader(t, table, 0)

	if cells["Restarts"] != "5" {
		t.Errorf("Restarts = %q, want 5", cells["Restarts"])
	}
	if cells["Ready"] != "2/2" {
		t.Errorf("Ready = %q, want 2/2", cells["Ready"])
	}
	if cells["Status"] != "Running" {
		t.Errorf("Status = %q, want Running", cells["Status"])
	}
}

func TestGatewayColumnsReadAddressAndProgrammedCondition(t *testing.T) {
	gw := obj(map[string]any{
		"metadata": map[string]any{"name": "external", "namespace": "ingress"},
		"spec":     map[string]any{"gatewayClassName": "cilium"},
		"status": map[string]any{
			"addresses":  []any{map[string]any{"type": "IPAddress", "value": "192.168.3.200"}},
			"conditions": []any{map[string]any{"type": "Programmed", "status": "True"}},
		},
	})

	table := buildLiveTable(KindGateways, withNamespace(builtinColumns[KindGateways], true), true, []*unstructured.Unstructured{gw})
	cells := cellsByHeader(t, table, 0)

	if cells["Class"] != "cilium" {
		t.Errorf("Class = %q, want cilium", cells["Class"])
	}
	if cells["Address"] != "192.168.3.200" {
		t.Errorf("Address = %q, want 192.168.3.200", cells["Address"])
	}
	if cells["Programmed"] != "True" {
		t.Errorf("Programmed = %q, want True", cells["Programmed"])
	}
}

func TestHTTPRouteListsItsParentGateways(t *testing.T) {
	route := obj(map[string]any{
		"metadata": map[string]any{"name": "api", "namespace": "prod"},
		"spec": map[string]any{
			"hostnames":  []any{"api.example.com", "www.example.com"},
			"parentRefs": []any{map[string]any{"name": "external", "namespace": "ingress", "sectionName": "https"}},
		},
	})

	table := buildLiveTable(KindHTTPRoutes, withNamespace(builtinColumns[KindHTTPRoutes], true), true, []*unstructured.Unstructured{route})
	cells := cellsByHeader(t, table, 0)

	if want := "api.example.com, www.example.com"; cells["Hostnames"] != want {
		t.Errorf("Hostnames = %q, want %q", cells["Hostnames"], want)
	}
	if want := "ingress/external#https"; cells["Parents"] != want {
		t.Errorf("Parents = %q, want %q", cells["Parents"], want)
	}
}

// linksIn returns the URL of every entry in one column of the first row, with
// "" for an entry that opens nothing.
func linksIn(t *testing.T, table Table, header string) []string {
	t.Helper()
	for i, c := range table.Columns {
		if c != header {
			continue
		}
		var urls []string
		for _, l := range table.Rows[0].Cells[i].Links {
			urls = append(urls, l.URL)
		}
		return urls
	}
	t.Fatalf("no %s column", header)
	return nil
}

func TestHTTPRouteHostnamesOpenInTheBrowser(t *testing.T) {
	route := func(parents ...any) *unstructured.Unstructured {
		return obj(map[string]any{
			"metadata": map[string]any{"name": "api", "namespace": "prod"},
			"spec": map[string]any{
				"hostnames":  []any{"api.example.com", "*.example.com"},
				"parentRefs": parents,
			},
		})
	}
	cases := []struct {
		name   string
		route  *unstructured.Unstructured
		scheme string
	}{
		{"an unnamed listener is taken as https", route(map[string]any{"name": "external"}), "https"},
		{"the http listener by name", route(map[string]any{"name": "external", "sectionName": "http"}), "http"},
		{"the http listener by port", route(map[string]any{"name": "external", "port": int64(80)}), "http"},
		{"one https parent is enough", route(map[string]any{"name": "a", "sectionName": "http"}, map[string]any{"name": "b"}), "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := buildLiveTable(KindHTTPRoutes, withNamespace(builtinColumns[KindHTTPRoutes], true), true, []*unstructured.Unstructured{tc.route})
			got := linksIn(t, table, "Hostnames")
			// A wildcard is shown but opens nothing.
			want := []string{tc.scheme + "://api.example.com", ""}
			if !slices.Equal(got, want) {
				t.Errorf("links = %q, want %q", got, want)
			}
		})
	}
}

func TestIngressHostsOpenOverHTTPSOnlyWhereTLSCoversThem(t *testing.T) {
	ing := obj(map[string]any{
		"metadata": map[string]any{"name": "web", "namespace": "prod"},
		"spec": map[string]any{
			"tls": []any{map[string]any{"hosts": []any{"*.example.com"}}},
			"rules": []any{
				map[string]any{"host": "shop.example.com"},
				map[string]any{"host": "legacy.internal"},
				map[string]any{"host": "a.b.example.com"},
				map[string]any{"host": "four.example.com"},
			},
		},
	})

	table := buildLiveTable(KindIngresses, withNamespace(builtinColumns[KindIngresses], true), true, []*unstructured.Unstructured{ing})
	cells := cellsByHeader(t, table, 0)

	if want := "shop.example.com, legacy.internal, a.b.example.com, +1 more"; cells["Hosts"] != want {
		t.Errorf("Hosts = %q, want %q", cells["Hosts"], want)
	}
	// A wildcard covers one label, so a.b.example.com is not under its cert.
	want := []string{"https://shop.example.com", "http://legacy.internal", "http://a.b.example.com", ""}
	if got := linksIn(t, table, "Hosts"); !slices.Equal(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
}

func TestListenerSetOpensItsWebListeners(t *testing.T) {
	ls := obj(map[string]any{
		"metadata": map[string]any{"name": "shop", "namespace": "shop"},
		"spec": map[string]any{
			"parentRef": map[string]any{"name": "shared"},
			"listeners": []any{
				map[string]any{"name": "https", "hostname": "shop.example.com", "port": int64(443), "protocol": "HTTPS"},
				map[string]any{"name": "alt", "hostname": "shop.example.com", "port": int64(8080), "protocol": "HTTP"},
				map[string]any{"name": "tls", "hostname": "db.example.com", "port": int64(5432), "protocol": "TLS"},
			},
		},
	})

	table := buildLiveTable(KindListenerSets, withNamespace(builtinColumns[KindListenerSets], true), true, []*unstructured.Unstructured{ls})
	want := []string{"https://shop.example.com", "http://shop.example.com:8080", ""}
	if got := linksIn(t, table, "Listeners"); !slices.Equal(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
}

func TestTCPRouteListsItsBackendsAcrossRules(t *testing.T) {
	route := obj(map[string]any{
		"metadata": map[string]any{"name": "postgres", "namespace": "prod"},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "internal", "sectionName": "pg"}},
			"rules": []any{
				map[string]any{"backendRefs": []any{map[string]any{"name": "db", "port": int64(5432)}}},
				map[string]any{"backendRefs": []any{map[string]any{"name": "db-replica", "namespace": "data", "port": int64(5432)}}},
			},
		},
	})

	table := buildLiveTable(KindTCPRoutes, withNamespace(builtinColumns[KindTCPRoutes], true), true, []*unstructured.Unstructured{route})
	cells := cellsByHeader(t, table, 0)

	if want := "internal#pg"; cells["Parents"] != want {
		t.Errorf("Parents = %q, want %q", cells["Parents"], want)
	}
	if want := "db:5432, data/db-replica:5432"; cells["Backends"] != want {
		t.Errorf("Backends = %q, want %q", cells["Backends"], want)
	}
	if _, ok := cells["Hostnames"]; ok {
		t.Error("a TCPRoute has no hostnames, but the table has a Hostnames column")
	}
}

func TestListenerSetShowsItsGatewayAndListeners(t *testing.T) {
	ls := obj(map[string]any{
		"metadata": map[string]any{"name": "shop", "namespace": "shop"},
		"spec": map[string]any{
			"parentRef": map[string]any{"name": "shared", "namespace": "ingress"},
			"listeners": []any{
				map[string]any{"name": "https", "hostname": "shop.example.com", "port": int64(443), "protocol": "HTTPS"},
				map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP"},
			},
		},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Accepted", "status": "True"},
			map[string]any{"type": "Programmed", "status": "False"},
		}},
	})

	table := buildLiveTable(KindListenerSets, withNamespace(builtinColumns[KindListenerSets], true), true, []*unstructured.Unstructured{ls})
	cells := cellsByHeader(t, table, 0)

	if want := "ingress/shared"; cells["Parent"] != want {
		t.Errorf("Parent = %q, want %q", cells["Parent"], want)
	}
	if want := "shop.example.com:443/HTTPS, 80/HTTP"; cells["Listeners"] != want {
		t.Errorf("Listeners = %q, want %q", cells["Listeners"], want)
	}
	if cells["Accepted"] != "True" || cells["Programmed"] != "False" {
		t.Errorf("Accepted, Programmed = %q, %q; want True, False", cells["Accepted"], cells["Programmed"])
	}
}

func TestBackendTLSPolicyIsAcceptedOnlyWhenEveryAncestorSaysSo(t *testing.T) {
	ancestor := func(status string) any {
		return map[string]any{
			"ancestorRef": map[string]any{"name": "external"},
			"conditions":  []any{map[string]any{"type": "Accepted", "status": status}},
		}
	}
	policy := func(ancestors ...any) *unstructured.Unstructured {
		return obj(map[string]any{
			"metadata": map[string]any{"name": "api-tls", "namespace": "prod"},
			"spec": map[string]any{
				"targetRefs": []any{map[string]any{"group": "", "kind": "Service", "name": "api", "sectionName": "https"}},
				"validation": map[string]any{"hostname": "api.internal"},
			},
			"status": map[string]any{"ancestors": ancestors},
		})
	}

	cases := []struct {
		name   string
		policy *unstructured.Unstructured
		want   string
	}{
		{"every ancestor accepts", policy(ancestor("True"), ancestor("True")), "True"},
		{"one ancestor refuses", policy(ancestor("True"), ancestor("False")), "False"},
		{"no controller has reported", policy(), "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := buildLiveTable(KindBackendTLSPolicies, withNamespace(builtinColumns[KindBackendTLSPolicies], true), true, []*unstructured.Unstructured{tc.policy})
			cells := cellsByHeader(t, table, 0)

			if cells["Accepted"] != tc.want {
				t.Errorf("Accepted = %q, want %q", cells["Accepted"], tc.want)
			}
			if want := "Service/api#https"; cells["Targets"] != want {
				t.Errorf("Targets = %q, want %q", cells["Targets"], want)
			}
			if cells["Hostname"] != "api.internal" {
				t.Errorf("Hostname = %q, want api.internal", cells["Hostname"])
			}
		})
	}
}

func TestJSONPathColumnsSurviveAMissingField(t *testing.T) {
	// An optional field that is simply absent must render blank, not break the
	// row -- most CRD printer columns point at optional status fields.
	svc := obj(map[string]any{"metadata": map[string]any{"name": "headless", "namespace": "data"}})

	table := buildLiveTable(KindServices, withNamespace(builtinColumns[KindServices], true), true, []*unstructured.Unstructured{svc})
	cells := cellsByHeader(t, table, 0)

	if cells["Type"] != "" {
		t.Errorf("Type = %q, want empty", cells["Type"])
	}
	if cells["Ports"] != "<none>" {
		t.Errorf("Ports = %q, want <none>", cells["Ports"])
	}
	if cells["External IP"] != "<pending>" {
		t.Errorf("External IP = %q, want <pending>", cells["External IP"])
	}
}

func TestCRDVersionsMarkTheStorageVersion(t *testing.T) {
	crd := obj(map[string]any{
		"metadata": map[string]any{"name": "gateways.gateway.networking.k8s.io"},
		"spec": map[string]any{
			"group": "gateway.networking.k8s.io",
			"scope": "Namespaced",
			"names": map[string]any{"kind": "Gateway"},
			"versions": []any{
				map[string]any{"name": "v1beta1", "served": true, "storage": false},
				map[string]any{"name": "v1", "served": true, "storage": true},
			},
		},
	})

	table := buildLiveTable(KindCRDs, builtinColumns[KindCRDs], false, []*unstructured.Unstructured{crd})
	cells := cellsByHeader(t, table, 0)

	if want := "v1beta1, v1*"; cells["Version"] != want {
		t.Errorf("Version = %q, want %q", cells["Version"], want)
	}
	if cells["Kind"] != "Gateway" {
		t.Errorf("Kind = %q, want Gateway", cells["Kind"])
	}
}

func TestPickCRDVersionPrefersStorageOverServed(t *testing.T) {
	crd := obj(map[string]any{
		"metadata": map[string]any{"name": "certificates.cert-manager.io"},
		"spec": map[string]any{"versions": []any{
			map[string]any{"name": "v1alpha2", "served": true, "storage": false,
				"additionalPrinterColumns": []any{map[string]any{"name": "Old", "jsonPath": ".status.old"}}},
			map[string]any{"name": "v1", "served": true, "storage": true,
				"additionalPrinterColumns": []any{map[string]any{"name": "Ready", "jsonPath": `.status.conditions[?(@.type=="Ready")].status`}}},
		}},
	})

	cols := pickCRDVersion(crd)
	if len(cols) != 1 {
		t.Fatalf("got %d columns, want 1: %v", len(cols), cols)
	}
	if name := mapString(asMap(cols[0]), "name"); name != "Ready" {
		t.Errorf("column = %q, want the storage version's Ready", name)
	}
}

// TestCRDColumnsKeepTheStandardTableAndDropTheWideOnes pins the priority rule:
// a tab is a plain `kubectl get`, so priority-0 columns are the table and the
// higher ones -- the `-o wide` extras -- are left out.
func TestCRDColumnsKeepTheStandardTableAndDropTheWideOnes(t *testing.T) {
	crd := obj(map[string]any{
		"metadata": map[string]any{"name": "certificates.cert-manager.io"},
		"spec": map[string]any{"versions": []any{
			map[string]any{"name": "v1", "served": true, "storage": true,
				"additionalPrinterColumns": []any{
					map[string]any{"name": "Ready", "jsonPath": ".status.ready"},
					// Wide-only, spelled both ways a decoded manifest can hold
					// a number.
					map[string]any{"name": "Issuer", "jsonPath": ".spec.issuerRef.name", "priority": int64(1)},
					map[string]any{"name": "Secret", "jsonPath": ".spec.secretName", "priority": float64(1)},
					// Absent priority means 0, which is the standard table.
					map[string]any{"name": "Expires", "jsonPath": ".status.notAfter"},
					// A CRD's own Age would double the one we append.
					map[string]any{"name": "Age", "jsonPath": ".metadata.creationTimestamp"},
					// Neither half of a column is optional.
					map[string]any{"name": "Nameless", "jsonPath": ""},
					map[string]any{"name": "", "jsonPath": ".spec.pathless"},
				}},
		}},
	})

	var got []string
	for _, c := range crdColumns(crd) {
		got = append(got, c.Name)
	}

	want := []string{"Name", "Ready", "Expires", "Age"}
	if !slices.Equal(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

func TestStripBulkRedactsSecretValuesBeforeTheyAreCached(t *testing.T) {
	secret := obj(map[string]any{
		"kind":     "Secret",
		"metadata": map[string]any{"name": "api-tls", "namespace": "prod", "managedFields": []any{map[string]any{"manager": "kubectl"}}},
		"type":     "kubernetes.io/tls",
		"data":     map[string]any{"tls.crt": "REALCERT", "tls.key": "REALKEY"},
	})

	out, err := stripBulk(secret)
	if err != nil {
		t.Fatal(err)
	}
	u := out.(*unstructured.Unstructured)

	data, _, _ := unstructured.NestedMap(u.Object, "data")
	if len(data) != 2 {
		t.Errorf("got %d keys, want the key names kept for the count", len(data))
	}
	for k, v := range data {
		if v != "" {
			t.Errorf("data[%q] = %v, want the value dropped before caching", k, v)
		}
	}
	if u.GetManagedFields() != nil {
		t.Error("managed fields were cached")
	}
}

func TestParseCustomKindRoundTrips(t *testing.T) {
	kind := CustomKind("certificates", "cert-manager.io")
	if kind != "crd:certificates.cert-manager.io" {
		t.Fatalf("kind = %q", kind)
	}

	plural, group, ok := ParseCustomKind(kind)
	if !ok || plural != "certificates" || group != "cert-manager.io" {
		t.Errorf("ParseCustomKind = (%q, %q, %t)", plural, group, ok)
	}

	for _, bad := range []string{"pods", "crd:", "crd:nogroup", ""} {
		if _, _, ok := ParseCustomKind(bad); ok {
			t.Errorf("ParseCustomKind(%q) accepted a non-custom kind", bad)
		}
	}
}

// event builds an Event object whose last-seen moment is `ago` in the past.
func event(name, reason string, ago time.Duration) *unstructured.Unstructured {
	return obj(map[string]any{
		"metadata":       map[string]any{"name": name, "namespace": "default"},
		"type":           "Warning",
		"reason":         reason,
		"message":        reason + " happened",
		"involvedObject": map[string]any{"kind": "Pod", "name": name},
		"lastTimestamp":  time.Now().Add(-ago).Format(time.RFC3339),
	})
}

func TestEventsComeBackMostRecentFirst(t *testing.T) {
	// Deliberately built in an order that both alphabetical-by-type and
	// alphabetical-by-name would leave alone, so only a real time sort passes.
	items := []*unstructured.Unstructured{
		event("alpha", "Older", 6*time.Hour),
		event("bravo", "Newest", 2*time.Minute),
		event("charlie", "Oldest", 40*time.Hour),
		event("delta", "Middle", 90*time.Minute),
	}

	table := buildLiveTable(KindEvents, builtinColumns[KindEvents], false, items)

	var order []string
	for _, row := range table.Rows {
		order = append(order, row.Name)
	}
	if want := []string{"bravo", "delta", "alpha", "charlie"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v (most recent first)", order, want)
	}
}

func TestEventsStayOrderedAsTheyArrive(t *testing.T) {
	// A watch delivers changes in no particular order; the table has to be
	// sorted on every projection, not merely seeded that way.
	first := buildLiveTable(KindEvents, builtinColumns[KindEvents], false, []*unstructured.Unstructured{
		event("old", "Old", 5*time.Hour),
	})
	if first.Rows[0].Name != "old" {
		t.Fatalf("first row = %q", first.Rows[0].Name)
	}

	second := buildLiveTable(KindEvents, builtinColumns[KindEvents], false, []*unstructured.Unstructured{
		event("old", "Old", 5*time.Hour),
		event("new", "New", 1*time.Minute),
	})
	if second.Rows[0].Name != "new" {
		t.Errorf("a newly arrived event landed at row %d, want the top", indexOf(second, "new"))
	}
}

func TestAgeCellsSortByTimeNotByText(t *testing.T) {
	// "5m", "2h" and "3d" in text order read 2h, 3d, 5m -- which is why the
	// cell carries seconds alongside what it displays.
	items := []*unstructured.Unstructured{
		event("a", "A", 5*time.Minute),
		event("b", "B", 2*time.Hour),
		event("c", "C", 3*24*time.Hour),
	}

	table := buildLiveTable(KindEvents, builtinColumns[KindEvents], false, items)
	at := slices.Index(table.Columns, "Last Seen")
	if at == -1 {
		t.Fatal("no Last Seen column")
	}

	var texts []string
	var keys []int64
	for _, row := range table.Rows {
		texts = append(texts, row.Cells[at].Text)
		keys = append(keys, seconds(t, row.Cells[at]))
	}
	if want := []string{"5m", "2h", "3d"}; !slices.Equal(texts, want) {
		t.Errorf("displayed = %v, want %v", texts, want)
	}
	// The keys must rise as the rows go from most to least recent; sorting the
	// text alone would have produced 2h, 3d, 5m.
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Errorf("sort keys %v are not increasing at %d", keys, i)
		}
	}
}

// seconds reads a cell's sort key as the number it is meant to be.
func seconds(t *testing.T, cell Cell) int64 {
	t.Helper()
	if cell.Sort == "" {
		t.Fatalf("cell %q carries no sort key", cell.Text)
	}
	n, err := strconv.ParseInt(cell.Sort, 10, 64)
	if err != nil {
		t.Fatalf("sort key %q of cell %q is not a number: %v", cell.Sort, cell.Text, err)
	}
	return n
}

func TestOtherKindsStaySortedByNamespaceAndName(t *testing.T) {
	pods := []*unstructured.Unstructured{
		obj(map[string]any{"metadata": map[string]any{"name": "zulu", "namespace": "alpha"}}),
		obj(map[string]any{"metadata": map[string]any{"name": "alpha", "namespace": "zulu"}}),
		obj(map[string]any{"metadata": map[string]any{"name": "mike", "namespace": "alpha"}}),
	}

	table := buildLiveTable(KindPods, withNamespace(builtinColumns[KindPods], true), true, pods)

	var order []string
	for _, row := range table.Rows {
		order = append(order, row.Namespace+"/"+row.Name)
	}
	if want := []string{"alpha/mike", "alpha/zulu", "zulu/alpha"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestVolumeSizesSortByValueNotByText(t *testing.T) {
	claim := obj(map[string]any{
		"metadata": map[string]any{"name": "data", "namespace": "prod"},
		"status":   map[string]any{"capacity": map[string]any{"storage": "500Mi"}, "phase": "Bound"},
	})
	bigger := obj(map[string]any{
		"metadata": map[string]any{"name": "bulk", "namespace": "prod"},
		"status":   map[string]any{"capacity": map[string]any{"storage": "5Gi"}, "phase": "Bound"},
	})

	table := buildLiveTable(KindPVCs, withNamespace(builtinColumns[KindPVCs], true), true,
		[]*unstructured.Unstructured{claim, bigger})
	at := slices.Index(table.Columns, "Size")

	byName := map[string]Cell{}
	for _, row := range table.Rows {
		byName[row.Name] = row.Cells[at]
	}
	if byName["data"].Text != "500Mi" || byName["bulk"].Text != "5Gi" {
		t.Fatalf("sizes rendered as %q and %q", byName["data"].Text, byName["bulk"].Text)
	}
	// 500Mi is 524288000 bytes, 5Gi is 5368709120. As text, "500Mi" sorts
	// after "5Gi", which is exactly the order the sort key exists to correct.
	small, big := seconds(t, byName["data"]), seconds(t, byName["bulk"])
	if small >= big {
		t.Errorf("500Mi sorts as %d and 5Gi as %d -- want the smaller first", small, big)
	}
}

func indexOf(t Table, name string) int {
	for i, row := range t.Rows {
		if row.Name == name {
			return i
		}
	}
	return -1
}

// ---- node conditions -------------------------------------------------------

// nodeWith builds a node carrying the given conditions, as type/status pairs.
func nodeWith(cordoned bool, conditions ...string) *unstructured.Unstructured {
	conds := []any{}
	for i := 0; i+1 < len(conditions); i += 2 {
		conds = append(conds, map[string]any{"type": conditions[i], "status": conditions[i+1]})
	}
	return obj(map[string]any{
		"metadata": map[string]any{"name": "wrkr01"},
		"spec":     map[string]any{"unschedulable": cordoned},
		"status":   map[string]any{"conditions": conds},
	})
}

// The ordinary case stays an ordinary cell: one word, one colour, no tags.
func TestANodeThatIsReadyReadsReadyAlone(t *testing.T) {
	got := nodeCondition(nodeWith(false, "Ready", "True", "MemoryPressure", "False", "DiskPressure", "False"))

	if got.Text != "Ready" || got.Tone != "ok" {
		t.Errorf("nodeCondition = %q (%s), want Ready (ok)", got.Text, got.Tone)
	}
	if got.Tags != nil {
		t.Errorf("Tags = %v, want none for a cell that says one thing", got.Tags)
	}
}

// Cordoning a node does not make it any less Ready, and showing only
// SchedulingDisabled hid that. kubectl prints both; so do we, each in its own
// colour.
func TestACordonedNodeIsStillReady(t *testing.T) {
	got := nodeCondition(nodeWith(true, "Ready", "True"))

	if got.Text != "Ready,SchedulingDisabled" {
		t.Errorf("Text = %q, want Ready,SchedulingDisabled", got.Text)
	}
	want := []Tag{{Text: "Ready", Tone: "ok"}, {Text: "SchedulingDisabled", Tone: "warn"}}
	if !slices.Equal(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
	// The cell's own tone is the worst of them, for anything drawing only one.
	if got.Tone != "warn" {
		t.Errorf("Tone = %q, want warn", got.Tone)
	}
}

// For a node every condition but Ready is bad news when True, and a node under
// pressure is evicting pods while it still reads Ready.
func TestANodeUnderPressureSaysWhichPressure(t *testing.T) {
	got := nodeCondition(nodeWith(false,
		"Ready", "True",
		"MemoryPressure", "True",
		"DiskPressure", "True",
		"PIDPressure", "False",
		"NetworkUnavailable", "False",
	))

	if got.Text != "Ready,MemoryPressure,DiskPressure" {
		t.Errorf("Text = %q, want Ready,MemoryPressure,DiskPressure", got.Text)
	}
}

// Whatever a node-problem-detector adds is a condition like any other.
func TestANodeWithEverythingWrongSaysAllOfIt(t *testing.T) {
	got := nodeCondition(nodeWith(true, "Ready", "False", "NetworkUnavailable", "True", "KernelDeadlock", "True"))

	if want := "NotReady,SchedulingDisabled,NetworkUnavailable,KernelDeadlock"; got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
	if got.Tone != "error" {
		t.Errorf("Tone = %q, want error", got.Tone)
	}
}

func TestANodeThatHasNotReportedIsUnknown(t *testing.T) {
	got := nodeCondition(nodeWith(false))

	if got.Text != "Unknown" || got.Tone != "error" {
		t.Errorf("nodeCondition = %q (%s), want Unknown (error)", got.Text, got.Tone)
	}
}

// The column is what the table draws, so the tags have to arrive through it.
func TestTheNodesTableCarriesEveryCondition(t *testing.T) {
	n := nodeWith(true, "Ready", "True")

	table := buildLiveTable(KindNodes, builtinColumns[KindNodes], false, []*unstructured.Unstructured{n})

	if got := cellsByHeader(t, table, 0)["Conditions"]; got != "Ready,SchedulingDisabled" {
		t.Errorf("Conditions = %q, want Ready,SchedulingDisabled", got)
	}
}
