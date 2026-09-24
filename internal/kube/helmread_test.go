package kube

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func releaseHead(namespace, secret, release, revision string) metav1.PartialObjectMetadata {
	m := metav1.PartialObjectMetadata{}
	m.Namespace = namespace
	m.Name = secret
	if release != "" {
		m.Labels = map[string]string{"owner": "helm", "name": release, "version": revision}
	}
	return m
}

// A release keeps ten revisions by default and each carries its whole chart:
// only the current one of each is read.
func TestCurrentRevisionsPicksTheNewestOfEachRelease(t *testing.T) {
	heads := []metav1.PartialObjectMetadata{
		releaseHead("prod", "sh.helm.release.v1.web.v1", "web", "1"),
		releaseHead("prod", "sh.helm.release.v1.web.v10", "web", "10"),
		releaseHead("prod", "sh.helm.release.v1.web.v9", "web", "9"),
		releaseHead("staging", "sh.helm.release.v1.web.v2", "web", "2"),
		releaseHead("prod", "sh.helm.release.v1.db.v3", "db", "3"),
		// No labels to go by: kept, to be judged on what it says.
		releaseHead("prod", "hand-made", "", ""),
	}

	got := currentRevisions(heads, nil)

	want := []ObjectRef{
		{Namespace: "prod", Name: "hand-made"},
		{Namespace: "prod", Name: "sh.helm.release.v1.db.v3"},
		{Namespace: "prod", Name: "sh.helm.release.v1.web.v10"},
		{Namespace: "staging", Name: "sh.helm.release.v1.web.v2"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	if only := currentRevisions(heads, map[string]bool{"staging": true}); len(only) != 1 || only[0].Namespace != "staging" {
		t.Errorf("filtered to staging: %+v", only)
	}
}

var secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

func TestReadSecretsReadsEachAndLeavesOutTheGone(t *testing.T) {
	s := helmSecret(t, "sh.helm.release.v1.ingress-nginx.v7", nginxRelease)
	s.SetAPIVersion("v1")
	s.SetKind("Secret")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{secretsGVR: "SecretList"}, s,
	)
	mapping := &meta.RESTMapping{
		Resource:         secretsGVR,
		GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "Secret"},
		Scope:            meta.RESTScopeNamespace,
	}
	c := &clusterClient{dynamic: client}

	got, err := readSecrets(context.Background(), c, mapping, []ObjectRef{
		{Namespace: "prod", Name: "sh.helm.release.v1.ingress-nginx.v7"},
		// Deleted between the list and the read.
		{Namespace: "prod", Name: "sh.helm.release.v1.gone.v1"},
	})

	if len(got) != 1 || got[0].GetName() != "sh.helm.release.v1.ingress-nginx.v7" {
		t.Fatalf("read %d, want the one that exists", len(got))
	}
	if err == nil {
		t.Error("the failed read was not reported")
	}
	table := helmTable(got)
	if len(table.Rows) != 1 || table.Rows[0].Cells[4].Text != "ingress-nginx-4.11.3" {
		t.Errorf("table = %+v", table.Rows)
	}
}
