package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// referencesOf only asks the cluster about an owner the app does not know, so
// a client with nothing in it serves every case below.
func refsOf(t *testing.T, kind string, fields map[string]any) map[string]Reference {
	t.Helper()
	c := &clusterClient{}
	out := map[string]Reference{}
	for _, r := range c.referencesOf(kind, obj(fields)) {
		out[r.Role+" "+r.Name] = r
	}
	return out
}

func TestConditionsAreTonedByWhatTheyMean(t *testing.T) {
	node := obj(map[string]any{
		"metadata": map[string]any{"name": "worker-1"},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "True", "reason": "KubeletReady"},
			map[string]any{"type": "MemoryPressure", "status": "True"},
			map[string]any{"type": "DiskPressure", "status": "False"},
			map[string]any{"type": "Progressing", "status": "False"},
			map[string]any{"type": "Mystery", "status": "Unknown"},
		}},
	})
	got := conditionsOf(node)
	tones := map[string]string{}
	for _, c := range got {
		tones[c.Type] = c.Tone
	}
	wantTones := map[string]string{
		"Ready": "ok", "MemoryPressure": "error", "DiskPressure": "ok", "Progressing": "warn", "Mystery": "info",
	}
	for kind, tone := range wantTones {
		if tones[kind] != tone {
			t.Errorf("%s toned %q, want %q", kind, tones[kind], tone)
		}
	}
	if got[0].Reason != "KubeletReady" {
		t.Errorf("reason = %q", got[0].Reason)
	}
}

func TestAConditionWithNoStatusIsTrue(t *testing.T) {
	csr := obj(map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Approved"}}}})
	if got := conditionsOf(csr); len(got) != 1 || got[0].Status != "True" || got[0].Tone != "ok" {
		t.Errorf("conditions = %+v", got)
	}
}

func TestAPodNamesWhatItRunsOnAndReads(t *testing.T) {
	refs := refsOf(t, KindPods, map[string]any{
		"metadata": map[string]any{"name": "web-1", "namespace": "prod"},
		"spec": map[string]any{
			"nodeName":           "worker-1",
			"serviceAccountName": "web",
			"volumes": []any{
				map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "web-data"}},
				map[string]any{"name": "cfg", "configMap": map[string]any{"name": "web-config"}},
				map[string]any{"name": "tok", "projected": map[string]any{"sources": []any{
					map[string]any{"secret": map[string]any{"name": "web-token"}},
				}}},
			},
			"containers": []any{map[string]any{
				"name":    "web",
				"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "web-config"}}},
				"env": []any{map[string]any{"name": "PW", "valueFrom": map[string]any{
					"secretKeyRef": map[string]any{"name": "web-db", "key": "password"},
				}}},
			}},
		},
	})

	for key, kind := range map[string]string{
		"Node worker-1":         KindNodes,
		"Service account web":   KindServiceAccounts,
		"Volume claim web-data": KindPVCs,
		"Config map web-config": KindConfigMaps,
		"Secret web-token":      KindSecrets,
		"Secret web-db":         KindSecrets,
	} {
		if refs[key].Kind != kind {
			t.Errorf("%s = %+v, want kind %s", key, refs[key], kind)
		}
	}
	// Named twice, listed once.
	if len(refs) != 6 {
		t.Errorf("got %d references, want 6: %v", len(refs), refs)
	}
	if refs["Node worker-1"].Namespace != "" || refs["Secret web-db"].Namespace != "prod" {
		t.Error("a cluster-scoped reference carries a namespace, or a namespaced one lacks it")
	}
}

func TestOwnersComeFirstAndSayWhetherTheyControl(t *testing.T) {
	c := &clusterClient{}
	u := obj(map[string]any{"metadata": map[string]any{
		"name": "web-7d9f", "namespace": "prod",
		"ownerReferences": []any{map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment", "name": "web", "uid": "1", "controller": true,
		}},
	}})
	got := c.referencesOf(KindReplicaSets, u)
	if len(got) == 0 || got[0].Role != "Controlled by" || got[0].Kind != KindDeployments || got[0].Name != "web" {
		t.Errorf("references = %+v", got)
	}
}

func TestABindingNamesItsRoleAndServiceAccounts(t *testing.T) {
	refs := refsOf(t, KindRoleBindings, map[string]any{
		"metadata": map[string]any{"name": "readers", "namespace": "prod"},
		"roleRef":  map[string]any{"kind": "Role", "name": "reader"},
		"subjects": []any{
			map[string]any{"kind": "ServiceAccount", "name": "deployer", "namespace": "ci"},
			map[string]any{"kind": "Group", "name": "devs"},
		},
	})
	if r := refs["Role reader"]; r.Kind != KindRoles || r.Namespace != "prod" {
		t.Errorf("role = %+v", r)
	}
	if r := refs["Subject deployer"]; r.Kind != KindServiceAccounts || r.Namespace != "ci" {
		t.Errorf("service account = %+v", r)
	}
	// A group is not an object, so there is nothing to open.
	if r := refs["Subject devs"]; r.Kind != "" || r.APIKind != "Group" {
		t.Errorf("group = %+v", r)
	}
}

func TestAnHTTPRouteNamesItsGatewaysAndBackends(t *testing.T) {
	refs := refsOf(t, KindHTTPRoutes, map[string]any{
		"metadata": map[string]any{"name": "web", "namespace": "prod"},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "public", "namespace": "infra"}},
			"rules": []any{map[string]any{"backendRefs": []any{
				map[string]any{"name": "web", "port": int64(80)},
				map[string]any{"name": "bucket", "kind": "S3Bucket", "group": "example.com"},
			}}},
		},
	})
	if r := refs["Parent public"]; r.Kind != KindGateways || r.Namespace != "infra" {
		t.Errorf("parent = %+v", r)
	}
	if r := refs["Backend web"]; r.Kind != KindServices || r.Namespace != "prod" {
		t.Errorf("backend = %+v", r)
	}
	if _, found := refs["Backend bucket"]; found {
		t.Error("a backend that is not a Service was listed as one")
	}
}

func TestAnIPAddressNamesItsService(t *testing.T) {
	refs := refsOf(t, KindIPAddresses, map[string]any{
		"metadata": map[string]any{"name": "10.96.0.10"},
		"spec": map[string]any{"parentRef": map[string]any{
			"group": "", "resource": "services", "namespace": "kube-system", "name": "kube-dns",
		}},
	})
	if r := refs["Parent kube-dns"]; r.Kind != KindServices || r.Namespace != "kube-system" {
		t.Errorf("parent = %+v", r)
	}
}

func TestPodSelectorsAreReadInEitherShape(t *testing.T) {
	service := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"selector": map[string]any{"tier": "web", "app": "shop"}},
	}}
	if got, ok := podSelectorOf(KindServices, service); !ok || got != "app=shop,tier=web" {
		t.Errorf("service selector = %q, %v", got, ok)
	}

	deployment := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "shop"}}},
	}}
	if got, ok := podSelectorOf(KindDeployments, deployment); !ok || got != "app=shop" {
		t.Errorf("deployment selector = %q, %v", got, ok)
	}

	// An empty pod selector is every pod in the namespace.
	policy := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"podSelector": map[string]any{}},
	}}
	if got, ok := podSelectorOf(KindNetworkPolicies, policy); !ok || got != "" {
		t.Errorf("empty policy selector = %q, %v", got, ok)
	}

	if _, ok := podSelectorOf(KindConfigMaps, deployment); ok {
		t.Error("a config map claims to select pods")
	}
	// A Service without a selector has hand-written endpoints, not pods.
	bare := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	if _, ok := podSelectorOf(KindServices, bare); ok {
		t.Error("a service without a selector claims to select pods")
	}
}
