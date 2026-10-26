package toolcli

// Extraction: reading a few named fields out of a command's output, so that
// the output itself never has to leave the app. See plugins.UIToolExtract.
//
// What comes back is only ever scalars -- a string, a number, true or false --
// at the paths the manifest named. A path that leads to a mapping or a list is
// refused rather than handed back, since that would be handing back a piece
// of the output. Parser errors are worded by the app, never quoted, as a
// parser's message can carry a piece of the text it choked on.

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Bounds on what is parsed and what is handed back.
const (
	maxExtractInput = 4 << 20
	maxResources    = 256
	maxSpecLength   = 1 << 20
	maxSpecDocs     = 64
	maxValueLength  = 4096
	maxAliasDepth   = 16
)

// FormatYAMLDocuments is plugins.ExtractYAMLDocuments.
const FormatYAMLDocuments = "yaml-documents"

// Extracted is what was found for one resource the command printed: the
// machine it came from, and the fields that were there. A field that is not
// there is not in Fields.
type Extracted struct {
	Node   string         `json:"node"`
	Fields map[string]any `json:"fields"`
	// Error says why a field could not be read for this resource -- one that
	// is not a single value, say. Empty when every field was read or absent.
	Error string `json:"error,omitzero"`
}

// Extract reads fields out of a command's output in the given format.
func Extract(format, output string, fields []string) ([]Extracted, error) {
	if format != FormatYAMLDocuments {
		return nil, fmt.Errorf("the format %q is not one the app reads", format)
	}
	if len(output) > maxExtractInput {
		return nil, fmt.Errorf("the output is longer than the app reads for fields (%d MiB)", maxExtractInput>>20)
	}
	docs, err := yamlDocuments(output, maxResources)
	if err != nil {
		return nil, fmt.Errorf("the output is not YAML documents: %w", err)
	}
	out := []Extracted{}
	for _, doc := range docs {
		if doc.Kind != yaml.MappingNode {
			continue
		}
		spec := child(doc, "spec")
		if spec == nil {
			continue
		}
		item := Extracted{Fields: map[string]any{}}
		if n := child(doc, "node"); n != nil && n.Kind == yaml.ScalarNode {
			item.Node = n.Value
		}
		specs, err := specDocuments(spec)
		if err != nil {
			item.Error = err.Error()
			out = append(out, item)
			continue
		}
		var problems []string
		for _, field := range fields {
			value, found, err := lookup(specs, field)
			switch {
			case err != nil:
				problems = append(problems, err.Error())
			case found:
				item.Fields[field] = value
			}
		}
		item.Error = strings.Join(problems, "; ")
		out = append(out, item)
	}
	return out, nil
}

// yamlDocuments parses a stream of YAML documents, at most max of them.
func yamlDocuments(text string, max int) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			// Not err: a parser's message can quote the text.
			return nil, errors.New("it could not be parsed")
		}
		if len(docs) == max {
			return nil, fmt.Errorf("there are more than %d", max)
		}
		if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
			docs = append(docs, doc.Content[0])
		}
	}
}

// specDocuments are the documents a resource's spec holds: the spec itself
// when it is a mapping, or the YAML stream written in it when it is text.
func specDocuments(spec *yaml.Node) ([]*yaml.Node, error) {
	spec = resolve(spec)
	switch {
	case spec == nil:
		return nil, errors.New("the spec could not be read")
	case spec.Kind == yaml.MappingNode:
		return []*yaml.Node{spec}, nil
	case spec.Kind == yaml.ScalarNode && spec.Tag == "!!str":
		if len(spec.Value) > maxSpecLength {
			return nil, fmt.Errorf("the spec is longer than the app reads for fields (%d MiB)", maxSpecLength>>20)
		}
		docs, err := yamlDocuments(spec.Value, maxSpecDocs)
		if err != nil {
			return nil, fmt.Errorf("the spec is not YAML documents: %w", err)
		}
		return docs, nil
	case spec.Kind == yaml.ScalarNode && spec.Tag == "!!null":
		return nil, nil
	}
	return nil, errors.New("the spec is neither a document nor text")
}

// lookup finds a dotted path in the first document that has it. A null is as
// good as absent; anything but a scalar is refused.
func lookup(docs []*yaml.Node, path string) (any, bool, error) {
	steps := strings.Split(path, ".")
	for _, doc := range docs {
		node := resolve(doc)
		for _, step := range steps {
			if node == nil || node.Kind != yaml.MappingNode {
				node = nil
				break
			}
			node = resolve(child(node, step))
		}
		if node == nil || (node.Kind == yaml.ScalarNode && node.Tag == "!!null") {
			continue
		}
		if node.Kind != yaml.ScalarNode {
			return nil, false, fmt.Errorf("%s is not a single value, so it is not handed back", path)
		}
		if len(node.Value) > maxValueLength {
			return nil, false, fmt.Errorf("%s is longer than %d characters, so it is not handed back", path, maxValueLength)
		}
		switch node.Tag {
		case "!!bool", "!!int", "!!float":
			var v any
			if err := node.Decode(&v); err == nil {
				return v, true, nil
			}
		}
		return node.Value, true, nil
	}
	return nil, false, nil
}

// child is the value of a key in a mapping; nil when it is not there.
func child(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// resolve follows aliases, a bounded number of times.
func resolve(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode; i++ {
		if i == maxAliasDepth {
			return nil
		}
		n = n.Alias
	}
	return n
}
