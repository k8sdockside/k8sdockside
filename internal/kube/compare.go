package kube

// The same object in two clusters, side by side.
//
// Environments drift: a ConfigMap edited by hand in one, a Deployment a
// version behind in another. Reading two YAML documents for the difference is
// what nobody does well, so this prints both the same way, drops what is
// always different -- the uid, the resourceVersion, the status -- and marks
// what is left line by line.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CompareSide names one of the two objects being compared.
type CompareSide struct {
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// DiffLine is one line of a comparison. Op is " " for a line both sides
// have, "-" for one only the left has and "+" for one only the right has.
// Left and Right are its line numbers on each side, zero where it is absent.
type DiffLine struct {
	Op    string `json:"op"`
	Text  string `json:"text"`
	Left  int    `json:"left"`
	Right int    `json:"right"`
}

// Comparison is the two documents and the difference between them. A side
// that could not be read has its error set and an empty document, and the
// diff then shows the other side as wholly added or removed.
type Comparison struct {
	Left       string     `json:"left"`
	Right      string     `json:"right"`
	LeftError  string     `json:"leftError"`
	RightError string     `json:"rightError"`
	Lines      []DiffLine `json:"lines"`
	// Same is true when the two documents are identical once the noise is
	// gone -- which is the answer somebody comparing was hoping for.
	Same bool `json:"same"`
	// Changes counts the lines only one side has.
	Changes int `json:"changes"`
}

// Compare reads both objects, at the same time since they are usually in two
// different clusters, and diffs them.
func Compare(read func(CompareSide) (string, error), left, right CompareSide) Comparison {
	var out Comparison
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		text, err := read(left)
		out.Left = text
		if err != nil {
			out.LeftError = err.Error()
		}
	}()
	go func() {
		defer wg.Done()
		text, err := read(right)
		out.Right = text
		if err != nil {
			out.RightError = err.Error()
		}
	}()
	wg.Wait()

	out.Lines = DiffLines(out.Left, out.Right)
	for _, l := range out.Lines {
		if l.Op != " " {
			out.Changes++
		}
	}
	out.Same = out.Changes == 0 && out.LeftError == "" && out.RightError == ""
	return out
}

// ComparableYAML reads one live object and prints it for comparing: see
// forComparing for what is left out.
func (w *Watcher) ComparableYAML(kc Context, kind, namespace, name string) (string, error) {
	var out string
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		got, _, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		out, err = toYAML(forComparing(got))
		return err
	})
	return out, err
}

// noisyAnnotations differ between any two copies of an object for reasons
// that have nothing to do with what it says.
var noisyAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
}

// forComparing strips what differs between any two copies of an object: its
// identity in the cluster that holds it, its bookkeeping, and its status --
// which is the cluster's report on it, not what anybody asked for.
//
// A Secret's values are replaced by a digest of each. Two clusters holding the
// same password then read as the same without either password being put on
// screen, and a rotated one reads as changed.
func forComparing(u *unstructured.Unstructured) *unstructured.Unstructured {
	out := u.DeepCopy()
	out.SetManagedFields(nil)
	out.SetUID("")
	out.SetResourceVersion("")
	out.SetGeneration(0)
	out.SetSelfLink("")
	unstructured.RemoveNestedField(out.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(out.Object, "status")

	// Each owner's uid is the owner's identity in this cluster, as the
	// object's own is. Removed from the map rather than blanked through the
	// typed setter, which would print it as an empty string.
	if owners, found, _ := unstructured.NestedSlice(out.Object, "metadata", "ownerReferences"); found {
		for _, raw := range owners {
			delete(asMap(raw), "uid")
		}
		_ = unstructured.SetNestedSlice(out.Object, owners, "metadata", "ownerReferences")
	}

	if annotations := out.GetAnnotations(); len(annotations) > 0 {
		for _, key := range noisyAnnotations {
			delete(annotations, key)
		}
		if len(annotations) == 0 {
			annotations = nil
		}
		out.SetAnnotations(annotations)
	}

	if out.GetKind() == "Secret" && out.GetAPIVersion() == "v1" {
		digestSecret(out)
	}
	return out
}

// digestSecret replaces each of a Secret's values with a short digest of it.
func digestSecret(u *unstructured.Unstructured) {
	data, found, _ := unstructured.NestedMap(u.Object, "data")
	if !found {
		return
	}
	for key, value := range data {
		encoded, _ := value.(string)
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			raw = []byte(encoded)
		}
		sum := sha256.Sum256(raw)
		data[key] = fmt.Sprintf("<sha256:%s, %d bytes>", hex.EncodeToString(sum[:6]), len(raw))
	}
	_ = unstructured.SetNestedMap(u.Object, data, "data")
}

// diffCells bounds the table the line diff fills in. Beyond it -- two
// documents of several thousand lines that differ all through -- the middle is
// reported as removed and added whole, which is still a true diff, only not
// the shortest one.
const diffCells = 16_000_000

// DiffLines is a line diff of two documents: the longest common subsequence
// of their lines, with what is outside it marked as removed or added.
func DiffLines(left, right string) []DiffLine {
	a := splitLines(left)
	b := splitLines(right)

	// The common head and tail are matched without the table: two copies of
	// one object usually differ in a few lines in the middle.
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}

	out := make([]DiffLine, 0, len(a)+len(b))
	for i := 0; i < head; i++ {
		out = append(out, DiffLine{Op: " ", Text: a[i], Left: i + 1, Right: i + 1})
	}

	midA := a[head : len(a)-tail]
	midB := b[head : len(b)-tail]
	out = append(out, diffMiddle(midA, midB, head, head)...)

	for i := 0; i < tail; i++ {
		ia := len(a) - tail + i
		ib := len(b) - tail + i
		out = append(out, DiffLine{Op: " ", Text: a[ia], Left: ia + 1, Right: ib + 1})
	}
	return out
}

func diffMiddle(a, b []string, offA, offB int) []DiffLine {
	var out []DiffLine
	removed := func(i int) { out = append(out, DiffLine{Op: "-", Text: a[i], Left: offA + i + 1}) }
	added := func(j int) { out = append(out, DiffLine{Op: "+", Text: b[j], Right: offB + j + 1}) }

	if len(a)*len(b) > diffCells {
		for i := range a {
			removed(i)
		}
		for j := range b {
			added(j)
		}
		return out
	}

	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:], filled from the end so the walk below can go forwards.
	width := len(b) + 1
	lcs := make([]int32, (len(a)+1)*width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*width+j] = lcs[(i+1)*width+j+1] + 1
			} else {
				lcs[i*width+j] = max(lcs[(i+1)*width+j], lcs[i*width+j+1])
			}
		}
	}

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Op: " ", Text: a[i], Left: offA + i + 1, Right: offB + j + 1})
			i++
			j++
		case lcs[(i+1)*width+j] >= lcs[i*width+j+1]:
			removed(i)
			i++
		default:
			added(j)
			j++
		}
	}
	for ; i < len(a); i++ {
		removed(i)
	}
	for ; j < len(b); j++ {
		added(j)
	}
	return out
}

// splitLines splits a document into lines, without the empty one a trailing
// newline would otherwise leave at the end.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
