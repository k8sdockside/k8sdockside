package kube

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func ops(lines []DiffLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Op + l.Text + "\n")
	}
	return b.String()
}

func TestDiffLinesMarksWhatOnlyOneSideHas(t *testing.T) {
	left := "a: 1\nb: 2\nc: 3\nd: 4\n"
	right := "a: 1\nb: 20\nc: 3\nd: 4\ne: 5\n"

	got := DiffLines(left, right)

	want := " a: 1\n-b: 2\n+b: 20\n c: 3\n d: 4\n+e: 5\n"
	if ops(got) != want {
		t.Errorf("diff =\n%s\nwant\n%s", ops(got), want)
	}
	// Line numbers are each side's own.
	if got[2].Right != 2 || got[2].Left != 0 || got[5].Right != 5 {
		t.Errorf("numbers = %+v", got)
	}
	if got[4].Left != 4 || got[4].Right != 4 {
		t.Errorf("common tail numbers = %+v", got[4])
	}
}

func TestDiffLinesOfIdenticalDocumentsHasNoChanges(t *testing.T) {
	for _, l := range DiffLines("x\ny\n", "x\ny\n") {
		if l.Op != " " {
			t.Fatalf("change in identical documents: %+v", l)
		}
	}
}

func TestCompareReportsASideThatCouldNotBeRead(t *testing.T) {
	read := func(side CompareSide) (string, error) {
		if side.ContextID == "gone" {
			return "", errors.New("not found")
		}
		return "kind: ConfigMap\n", nil
	}
	got := Compare(read, CompareSide{ContextID: "here"}, CompareSide{ContextID: "gone"})
	if got.RightError != "not found" || got.Same || got.Changes != 1 {
		t.Errorf("comparison = %+v", got)
	}

	same := Compare(read, CompareSide{ContextID: "a"}, CompareSide{ContextID: "b"})
	if !same.Same {
		t.Errorf("identical sides not same: %+v", same)
	}
}

func TestForComparingDropsWhatAlwaysDiffers(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":              "web",
			"uid":               "1234",
			"resourceVersion":   "99",
			"generation":        int64(4),
			"creationTimestamp": "2026-01-01T00:00:00Z",
			"annotations": map[string]any{
				"deployment.kubernetes.io/revision": "7",
				"team":                              "payments",
			},
			"ownerReferences": []any{map[string]any{"apiVersion": "v1", "kind": "Thing", "name": "owner", "uid": "abcd"}},
		},
		"spec":   map[string]any{"replicas": int64(3)},
		"status": map[string]any{"readyReplicas": int64(3)},
	}}

	text, err := toYAML(forComparing(u))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "revision", "status", "readyReplicas"} {
		if strings.Contains(text, gone) {
			t.Errorf("%s still in\n%s", gone, text)
		}
	}
	for _, kept := range []string{"team: payments", "replicas: 3", "name: owner"} {
		if !strings.Contains(text, kept) {
			t.Errorf("%s missing from\n%s", kept, text)
		}
	}
}

func TestForComparingShowsASecretsDigestNotItsValue(t *testing.T) {
	secret := func(value string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "db"},
			"data":       map[string]any{"password": value},
		}}
	}
	a, _ := toYAML(forComparing(secret("aHVudGVyMg==")))
	b, _ := toYAML(forComparing(secret("aHVudGVyMg==")))
	c, _ := toYAML(forComparing(secret("c3dvcmRmaXNo")))

	if strings.Contains(a, "aHVudGVyMg") || strings.Contains(a, "hunter2") {
		t.Errorf("secret value on screen:\n%s", a)
	}
	if !strings.Contains(a, "<sha256:") || a != b || a == c {
		t.Errorf("digests: a=%q b=%q c=%q", a, b, c)
	}
}
