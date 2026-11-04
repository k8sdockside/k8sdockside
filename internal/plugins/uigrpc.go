package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/addons"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// gRPC services a plugin's own views may call, and Services they may open a
// port-forward to.
//
// Some products answer only over gRPC -- Hubble Relay, a database's admin
// API, an operator's control endpoint -- and a page has no network of its
// own, let alone HTTP/2. So a plugin may name those Services, the methods its
// views may call on them, and the descriptors that say what the messages are.
// The app reaches the Service through a port-forward to one of its pods,
// speaks gRPC for the page, and hands it each message as JSON.
//
// What is declared is where a call may go and what it may be: the page picks
// a declared service and one of its declared methods, and nothing else. Only
// calls that answer -- unary, or a stream from the server that ends -- are
// made: a call the page streams into could carry anything, and the app could
// not show what it was.

// UIGRPC is one in-cluster gRPC service a plugin's views may call.
type UIGRPC struct {
	// ID is what a view names the service by.
	ID string `json:"id"`
	// Label is how the app names it on the plugin's card and in errors.
	// Defaults to the service name, or the id.
	Label string `json:"label,omitzero"`
	// Namespace, Name and Selector find the Service, exactly as a UIService's
	// do.
	Namespace string `json:"namespace,omitzero"`
	Name      string `json:"name,omitzero"`
	Selector  string `json:"selector,omitzero"`
	// Port is the Service port: its name, or its number.
	Port ServicePort `json:"port"`
	// Descriptors is a file in the ui folder holding a FileDescriptorSet --
	// what `protoc --include_imports --descriptor_set_out` or
	// `buf build -o` writes -- with every message the methods use.
	Descriptors string `json:"descriptors"`
	// Methods are the methods the views may call, each as
	// "package.Service/Method", the way gRPC names them on the wire.
	Methods []string `json:"methods"`
}

// UIForward is a Service a plugin's views may ask the app to port-forward to
// and open in the browser -- a product's own web UI, say. The user is asked
// first, and the forward is listed with the others the app keeps.
type UIForward struct {
	ID        string      `json:"id"`
	Label     string      `json:"label,omitzero"`
	Namespace string      `json:"namespace,omitzero"`
	Name      string      `json:"name,omitzero"`
	Selector  string      `json:"selector,omitzero"`
	Port      ServicePort `json:"port"`
}

// Limits on the declarations: each one is a line on the plugin's card.
const (
	maxGRPC     = 8
	maxForwards = 4
	// maxMethods bounds one service's methods.
	maxMethods = 32
	// maxDescriptorBytes bounds a descriptor file. A whole product's API
	// with its imports is tens of kilobytes; this is generous.
	maxDescriptorBytes = 4 << 20
)

// grpcMethodName is a method as gRPC names it on the wire, without the
// leading slash: "observer.Observer/GetFlows".
var grpcMethodName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*/[A-Za-z_][A-Za-z0-9_]*$`)

// AsService is the Service a gRPC declaration names, for finding it the way
// a UIService is found. The id is kept apart from the plugin's HTTP services,
// which may use the same one.
func (g UIGRPC) AsService() UIService {
	return UIService{ID: "grpc:" + g.ID, Label: g.Label, Namespace: g.Namespace, Name: g.Name, Selector: g.Selector, Port: g.Port, Scheme: SchemeHTTP, Paths: []string{"/"}}
}

// AsService is the Service a forward declaration names.
func (f UIForward) AsService() UIService {
	return UIService{ID: "forward:" + f.ID, Label: f.Label, Namespace: f.Namespace, Name: f.Name, Selector: f.Selector, Port: f.Port, Scheme: SchemeHTTP, Paths: []string{"/"}}
}

// Allows reports whether a view may call a method.
func (g UIGRPC) Allows(method string) bool {
	for _, m := range g.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// checkLocation holds a Service's whereabouts to what a UIService accepts:
// a name with its namespace, or a selector.
func checkLocation(namespace, name, selector string) error {
	s := UIService{ID: "x", Namespace: namespace, Name: name, Selector: selector, Port: "1", Paths: []string{"/"}}
	_, err := validateService("", s)
	if err == nil {
		return nil
	}
	// validateService words its errors for a service; keep what follows the
	// prefix, which is the part about where it is.
	msg := err.Error()
	if i := strings.Index(msg, `service "x": `); i >= 0 {
		msg = msg[i+len(`service "x": `):]
	}
	return errors.New(msg)
}

// validateGRPC checks the gRPC services a plugin's views may call. Whether
// the descriptors hold the methods is checked when the ui folder is read;
// see checkGRPCDescriptors.
func validateGRPC(pluginID string, list []UIGRPC) ([]UIGRPC, error) {
	if len(list) > maxGRPC {
		return list, fmt.Errorf("plugin %q lets its views call %d gRPC services; at most %d may be declared", pluginID, len(list), maxGRPC)
	}
	var errs []error
	seen := map[string]bool{}
	for i, g := range list {
		g.ID = strings.TrimSpace(g.ID)
		g.Label = strings.TrimSpace(g.Label)
		g.Namespace = strings.TrimSpace(g.Namespace)
		g.Name = strings.TrimSpace(g.Name)
		g.Selector = strings.TrimSpace(g.Selector)
		g.Descriptors = strings.TrimSpace(g.Descriptors)
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("plugin %q, gRPC service %q: %s", pluginID, g.ID, fmt.Sprintf(format, args...)))
		}
		if !addons.ValidID(g.ID) {
			errs = append(errs, fmt.Errorf("plugin %q has a gRPC service with id %q; use lowercase letters, digits and dashes", pluginID, g.ID))
			continue
		}
		if seen[g.ID] {
			errs = append(errs, fmt.Errorf("plugin %q declares two gRPC services with id %q", pluginID, g.ID))
			continue
		}
		seen[g.ID] = true
		if err := checkLocation(g.Namespace, g.Name, g.Selector); err != nil {
			fail("%v", err)
			continue
		}
		if err := checkPort(g.Port); err != nil {
			fail("%v", err)
			continue
		}
		if g.Descriptors == "" {
			fail("name the descriptor set in its ui folder that describes its methods, such as \"api.binpb\"")
			continue
		}
		if !fs.ValidPath(g.Descriptors) || path.Clean(g.Descriptors) != g.Descriptors {
			fail("descriptors %q is not a file in its ui folder", g.Descriptors)
			continue
		}
		if len(g.Methods) == 0 {
			fail("list the methods its views may call, such as \"package.Service/Method\"")
			continue
		}
		if len(g.Methods) > maxMethods {
			fail("it lists %d methods; at most %d may be declared", len(g.Methods), maxMethods)
			continue
		}
		bad := false
		for j, m := range g.Methods {
			m = strings.TrimPrefix(strings.TrimSpace(m), "/")
			if !grpcMethodName.MatchString(m) {
				fail("method %q is not a gRPC method name, which is written \"package.Service/Method\"", m)
				bad = true
				break
			}
			g.Methods[j] = m
		}
		if bad {
			continue
		}
		if g.Label == "" {
			g.Label = g.Name
		}
		if g.Label == "" {
			g.Label = g.ID
		}
		if len(g.Label) > 60 {
			fail("the label is %d characters; keep it to 60", len(g.Label))
			continue
		}
		list[i] = g
	}
	return list, errors.Join(errs...)
}

// validateForwards checks the Services a plugin's views may ask to open a
// port-forward to.
func validateForwards(pluginID string, list []UIForward) ([]UIForward, error) {
	if len(list) > maxForwards {
		return list, fmt.Errorf("plugin %q lets its views open %d port-forwards; at most %d may be declared", pluginID, len(list), maxForwards)
	}
	var errs []error
	seen := map[string]bool{}
	for i, f := range list {
		f.ID = strings.TrimSpace(f.ID)
		f.Label = strings.TrimSpace(f.Label)
		f.Namespace = strings.TrimSpace(f.Namespace)
		f.Name = strings.TrimSpace(f.Name)
		f.Selector = strings.TrimSpace(f.Selector)
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("plugin %q, forward %q: %s", pluginID, f.ID, fmt.Sprintf(format, args...)))
		}
		if !addons.ValidID(f.ID) {
			errs = append(errs, fmt.Errorf("plugin %q has a forward with id %q; use lowercase letters, digits and dashes", pluginID, f.ID))
			continue
		}
		if seen[f.ID] {
			errs = append(errs, fmt.Errorf("plugin %q declares two forwards with id %q", pluginID, f.ID))
			continue
		}
		seen[f.ID] = true
		if err := checkLocation(f.Namespace, f.Name, f.Selector); err != nil {
			fail("%v", err)
			continue
		}
		if err := checkPort(f.Port); err != nil {
			fail("%v", err)
			continue
		}
		if f.Label == "" {
			f.Label = f.Name
		}
		if f.Label == "" {
			f.Label = f.ID
		}
		if len(f.Label) > 60 {
			fail("the label is %d characters; keep it to 60", len(f.Label))
			continue
		}
		list[i] = f
	}
	return list, errors.Join(errs...)
}

// GRPC returns the gRPC service a plugin's views may call by that id.
func (p Plugin) GRPC(id string) (UIGRPC, bool) {
	if p.UI == nil {
		return UIGRPC{}, false
	}
	for _, g := range p.UI.GRPC {
		if g.ID == id {
			return g, true
		}
	}
	return UIGRPC{}, false
}

// Forward returns the forward a plugin's views may open by that id.
func (p Plugin) Forward(id string) (UIForward, bool) {
	if p.UI == nil {
		return UIForward{}, false
	}
	for _, f := range p.UI.Forwards {
		if f.ID == id {
			return f, true
		}
	}
	return UIForward{}, false
}

// GRPCSchema is what a gRPC service's descriptors say: the methods the
// manifest allows, and the types their messages are made of.
type GRPCSchema struct {
	Methods map[string]protoreflect.MethodDescriptor
	// Types resolves the messages an Any names, for writing them as JSON.
	Types *dynamicpb.Types
}

// LoadGRPCSchema reads a gRPC service's descriptor set from the plugin's ui
// folder and finds each declared method in it. A method that is not there,
// or that the client streams into, is refused.
func LoadGRPCSchema(files fs.FS, g UIGRPC) (*GRPCSchema, error) {
	info, err := fs.Stat(files, g.Descriptors)
	if err != nil {
		return nil, fmt.Errorf("its descriptors %s are not in the ui folder", g.Descriptors)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("its descriptors %s are a folder, not a descriptor set", g.Descriptors)
	}
	if info.Size() > maxDescriptorBytes {
		return nil, fmt.Errorf("its descriptors %s are %d bytes; at most %d MiB", g.Descriptors, info.Size(), maxDescriptorBytes>>20)
	}
	raw, err := fs.ReadFile(files, g.Descriptors)
	if err != nil {
		return nil, fmt.Errorf("its descriptors %s cannot be read: %w", g.Descriptors, err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("its descriptors %s are not a binary FileDescriptorSet: %w", g.Descriptors, err)
	}
	registry, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("its descriptors %s do not hold together -- build them with every import included: %w", g.Descriptors, err)
	}

	schema := &GRPCSchema{Methods: map[string]protoreflect.MethodDescriptor{}, Types: dynamicpb.NewTypes(registry)}
	var errs []error
	for _, m := range g.Methods {
		service, method, _ := strings.Cut(m, "/")
		desc, err := registry.FindDescriptorByName(protoreflect.FullName(service))
		if err != nil {
			errs = append(errs, fmt.Errorf("method %s: %s has no service %s", m, g.Descriptors, service))
			continue
		}
		sd, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			errs = append(errs, fmt.Errorf("method %s: %s is not a service in %s", m, service, g.Descriptors))
			continue
		}
		md := sd.Methods().ByName(protoreflect.Name(method))
		if md == nil {
			errs = append(errs, fmt.Errorf("method %s: service %s has no method %s", m, service, method))
			continue
		}
		if md.IsStreamingClient() {
			errs = append(errs, fmt.Errorf("method %s streams from the client; only unary calls and streams from the server can be made", m))
			continue
		}
		schema.Methods[m] = md
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return schema, nil
}

// checkGRPCDescriptors refuses a plugin whose gRPC services name descriptors
// its ui folder does not have, or methods they do not hold.
func checkGRPCDescriptors(p Plugin, files fs.FS) error {
	if p.UI == nil {
		return nil
	}
	var errs []error
	for _, g := range p.UI.GRPC {
		if _, err := LoadGRPCSchema(files, g); err != nil {
			errs = append(errs, fmt.Errorf("plugin %q, gRPC service %q: %w", p.ID, g.ID, err))
		}
	}
	return errors.Join(errs...)
}
