package plugins

import (
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// A gRPC service declaration is where a call can go and what it may be, so
// it is held to exactly that, and its methods are found in the descriptors
// when the file is read rather than when a page first calls one.

// descriptorSet is a small API: Echo is unary, Watch streams from the server
// and Upload from the client.
func descriptorSet(t *testing.T) string {
	t.Helper()
	yes := true
	msg := func(name string) *descriptorpb.DescriptorProto {
		return &descriptorpb.DescriptorProto{Name: proto.String(name)}
	}
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:        proto.String("demo.proto"),
		Package:     proto.String("demo.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{msg("Req"), msg("Res")},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Demo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("Echo"), InputType: proto.String(".demo.v1.Req"), OutputType: proto.String(".demo.v1.Res")},
				{Name: proto.String("Watch"), InputType: proto.String(".demo.v1.Req"), OutputType: proto.String(".demo.v1.Res"), ServerStreaming: &yes},
				{Name: proto.String("Upload"), InputType: proto.String(".demo.v1.Req"), OutputType: proto.String(".demo.v1.Res"), ClientStreaming: &yes},
			},
		}},
	}}}
	raw, err := proto.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func loadGRPC(t *testing.T, ui string, descriptors map[string]string) Catalogue {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "demo"), "plugin.json", `{ "id": "demo", "name": "Demo", "ui": { `+ui+` }, "views": [{ "id": "v", "label": "V", "type": "custom" }] }`)
	write(t, filepath.Join(dir, "demo", "ui"), "index.html", "<p>demo</p>")
	for name, content := range descriptors {
		write(t, filepath.Join(dir, "demo", "ui"), name, content)
	}
	return Load(dir, nil, nil)
}

func TestAGRPCDeclarationIsCheckedWhenTheFileIsRead(t *testing.T) {
	set := descriptorSet(t)
	for name, tc := range map[string]struct {
		grpc    string
		files   map[string]string
		refused string
	}{
		"unary and server streaming": {`{ "id": "demo", "selector": "app=demo", "port": "grpc", "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo", "/demo.v1.Demo/Watch"] }`, map[string]string{"demo.binpb": set}, ""},
		"by name":                    {`{ "id": "demo", "namespace": "x", "name": "demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, map[string]string{"demo.binpb": set}, ""},
		"client streaming":           {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Upload"] }`, map[string]string{"demo.binpb": set}, "streams from the client"},
		"a method not there":         {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Delete"] }`, map[string]string{"demo.binpb": set}, "has no method Delete"},
		"a service not there":        {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Other/Echo"] }`, map[string]string{"demo.binpb": set}, "no service demo.v1.Other"},
		"no descriptors file":        {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "missing.binpb", "methods": ["demo.v1.Demo/Echo"] }`, nil, "not in the ui folder"},
		"descriptors not a set":      {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, map[string]string{"demo.binpb": "syntax = \"proto3\";"}, "not a binary FileDescriptorSet"},
		"descriptors outside":        {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "../demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, nil, "not a file in its ui folder"},
		"no descriptors":             {`{ "id": "demo", "selector": "app=demo", "port": 80, "methods": ["demo.v1.Demo/Echo"] }`, nil, "name the descriptor set"},
		"no methods":                 {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": [] }`, nil, "list the methods"},
		"a bad method":               {`{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["Echo"] }`, nil, "not a gRPC method name"},
		"nowhere":                    {`{ "id": "demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, nil, "give a selector"},
		"a bad port":                 {`{ "id": "demo", "selector": "app=demo", "port": 0, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, nil, "out of range"},
		"a bad id":                   {`{ "id": "Demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["demo.v1.Demo/Echo"] }`, nil, "gRPC service with id"},
	} {
		t.Run(name, func(t *testing.T) {
			cat := loadGRPC(t, `"grpc": [`+tc.grpc+`]`, tc.files)
			if tc.refused == "" {
				if len(cat.Problems) > 0 {
					t.Fatalf("refused: %v", cat.Problems)
				}
				return
			}
			if len(cat.Problems) != 1 || !strings.Contains(cat.Problems[0].Message, tc.refused) {
				t.Errorf("problems %v, want one saying %q", cat.Problems, tc.refused)
			}
		})
	}
}

func TestAGRPCServiceAllowsOnlyItsMethods(t *testing.T) {
	cat := loadGRPC(t, `"grpc": [{ "id": "demo", "selector": "app=demo", "port": 80, "descriptors": "demo.binpb", "methods": ["/demo.v1.Demo/Echo"] }]`, map[string]string{"demo.binpb": descriptorSet(t)})
	if len(cat.Problems) > 0 {
		t.Fatal(cat.Problems)
	}
	p, _ := cat.Find("demo")
	g, ok := p.GRPC("demo")
	if !ok {
		t.Fatal("no gRPC service demo")
	}
	if g.Label != "demo" || !g.Allows("demo.v1.Demo/Echo") || g.Allows("demo.v1.Demo/Watch") {
		t.Fatalf("declared %+v", g)
	}
	files, done, _ := p.UIFiles()
	defer done()
	schema, err := LoadGRPCSchema(files, g)
	if err != nil {
		t.Fatal(err)
	}
	if md := schema.Methods["demo.v1.Demo/Echo"]; md == nil || md.Output().FullName() != "demo.v1.Res" {
		t.Fatalf("methods %v", schema.Methods)
	}
	if g.AsService().ID == "demo" {
		t.Error("a gRPC service is found apart from an HTTP service with the same id")
	}
}

func TestAForwardDeclarationIsCheckedWhenTheFileIsRead(t *testing.T) {
	for name, tc := range map[string]struct {
		forward string
		refused string
	}{
		"by selector": {`{ "id": "ui", "label": "Hubble UI", "selector": "k8s-app=hubble-ui", "port": 80 }`, ""},
		"by name":     {`{ "id": "ui", "namespace": "kube-system", "name": "hubble-ui", "port": "http" }`, ""},
		"nowhere":     {`{ "id": "ui", "port": 80 }`, "give a selector"},
		"no port":     {`{ "id": "ui", "selector": "a=b" }`, "name the port"},
		"a bad id":    {`{ "id": "UI", "selector": "a=b", "port": 80 }`, "forward with id"},
	} {
		t.Run(name, func(t *testing.T) {
			cat := loadGRPC(t, `"forwards": [`+tc.forward+`]`, nil)
			if tc.refused == "" {
				if len(cat.Problems) > 0 {
					t.Fatalf("refused: %v", cat.Problems)
				}
				p, _ := cat.Find("demo")
				if f, ok := p.Forward("ui"); !ok || f.Label == "" {
					t.Fatalf("forward %+v", f)
				}
				return
			}
			if len(cat.Problems) != 1 || !strings.Contains(cat.Problems[0].Message, tc.refused) {
				t.Errorf("problems %v, want one saying %q", cat.Problems, tc.refused)
			}
		})
	}
}
