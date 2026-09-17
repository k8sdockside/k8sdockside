package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/rogerwesterbo/k8sdockside/internal/plugins"
	"github.com/rogerwesterbo/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SnapshotEvent is the event a tab's rows arrive on. One event carries the
// whole current contents of one subscription; the payload names which.
const SnapshotEvent = "resource:snapshot"

// Registering the event gives the binding generator the payload's type, so the
// frontend receives a typed Snapshot rather than an any. The generator only
// discovers direct calls with constant arguments from an init function.
func init() {
	application.RegisterEvent[kube.Snapshot](SnapshotEvent)
}

// ResourceService serves the cluster data behind each tab, live.
//
// Tables are not fetched, they are subscribed to: Subscribe opens a watch and
// returns immediately, and every change to the cluster arrives at the frontend
// as a SnapshotEvent. The one-shot methods below (the dashboard, the describe
// panel, the namespace filter) share the same connections.
type ResourceService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	// plugins resolves a "plugin:" tab kind back to the kind and filters the
	// view names. Set after construction because the plugin service needs this
	// service's watcher, and one of the two has to be built first.
	plugins *PluginService
	// graphs resolves where a context's Prometheus is, for the budget views'
	// usage fallback. Set after construction for the same reason: it is built
	// from this service's watcher.
	graphs *MetricsService
	// owners files each subscription under whoever opened it, in the web
	// version, so a tab's rows go to the user looking at that tab.
	owners *session.Owners
}

// NewResourceService wires the service to the kubeconfig cache it resolves
// context IDs against.
func NewResourceService(configs *KubeconfigService) *ResourceService {
	s := &ResourceService{configs: configs}
	s.watcher = kube.NewWatcher(s.push)
	return s
}

// usePlugins gives the service the plugin catalogue it resolves plugin views
// against. Unexported so it stays out of the generated bindings: it is wiring
// between two services in the same package, not something the frontend calls.
func (s *ResourceService) usePlugins(p *PluginService) { s.plugins = p }

// useMetrics gives the service the Prometheus endpoint resolver its budget
// views fall back to. Unexported for the same reason as usePlugins: wiring
// between two services, not something the frontend calls.
func (s *ResourceService) useMetrics(m *MetricsService) { s.graphs = m }

// push forwards one snapshot to the frontend. It is called from the watcher's
// background goroutines, which is why it tolerates being called before the app
// exists and after it has gone.
func (s *ResourceService) push(snap kube.Snapshot) {
	if app := application.Get(); app != nil {
		app.Event.Emit(SnapshotEvent, snap)
	}
}

// ServiceShutdown closes every watch when the app quits, so credentials and
// connections are not left open behind a closed window.
func (s *ResourceService) ServiceShutdown() error {
	s.watcher.Close()
	return nil
}

// Subscribe opens a live view of one resource kind and returns the subscription
// ID that its snapshots will carry. Pass no namespaces for all of them.
//
// It returns as soon as the watch is started. The first rows arrive as an event
// once the cluster has answered, so a slow or unreachable cluster leaves the
// tab in its loading state rather than blocking the UI.
func (s *ResourceService) Subscribe(caller context.Context, contextID, kind string, namespaces []string) (string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}

	// A plugin view is a kind like any other as far as the tab machinery is
	// concerned; it is turned back into a real kind and the filters it pins
	// here, at the last moment, so nothing between the sidebar and this line
	// has to know that plugins exist.
	kind, namespaces, selector, err := s.view(kind, namespaces)
	if err != nil {
		return "", err
	}
	return s.watcher.SubscribeFor(ctx, kind, namespaces, selector, claimSubscription(caller, s.owners, s.watcher))
}

// view resolves a tab's kind, which may name a plugin's view, into the kind to
// watch and the filters to apply. A kind that is not a plugin view passes
// through untouched, which is every kind but one.
//
// A view that pins a namespace overrides whatever the tab asked for, rather
// than intersecting with it: the pin is the whole point of the view, and a
// tab's namespace filter that silently did nothing would be worse than one that
// is not offered.
func (s *ResourceService) view(kind string, namespaces []string) (string, []string, string, error) {
	if !strings.HasPrefix(kind, plugins.Prefix) {
		return kind, namespaces, kube.NoSelector, nil
	}
	if s.plugins == nil {
		return "", nil, "", fmt.Errorf("plugins are not available")
	}

	resolved, err := s.plugins.Resolve(kind)
	if err != nil {
		return "", nil, "", err
	}
	if resolved.Overview {
		return "", nil, "", fmt.Errorf("%s is a plugin overview, which is not a resource listing", kind)
	}
	if resolved.Custom {
		return "", nil, "", fmt.Errorf("%s is one of the plugin's own views, which is not a resource listing", kind)
	}
	if resolved.Namespace != "" {
		namespaces = []string{resolved.Namespace}
	}
	return resolved.Kind, namespaces, resolved.Selector, nil
}

// Unsubscribe closes a tab's view. The underlying watch stays open if another
// tab is still using it.
func (s *ResourceService) Unsubscribe(caller context.Context, subscriptionID string) {
	if !s.owners.Allowed(caller, subscriptionID) {
		return
	}
	dropSubscription(s.owners, s.watcher, subscriptionID)
}

// SetNamespaces re-points an open subscription at other namespaces, none
// meaning all. The watch is cluster-wide, so this is a filter change: the new
// rows arrive as an event without anything being re-fetched.
func (s *ResourceService) SetNamespaces(caller context.Context, subscriptionID string, namespaces []string) {
	if !s.owners.Allowed(caller, subscriptionID) {
		return
	}
	s.watcher.SetNamespaces(subscriptionID, namespaces)
}

// Overview is the dashboard payload for one context.
func (s *ResourceService) Overview(contextID string) (kube.Overview, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return kube.Overview{Error: err.Error()}, err
	}
	return s.watcher.Overview(ctx)
}

// Access is the payload behind the access overview: every role and binding the
// caller may read, and who the cluster says the caller is. A cluster that lets
// the caller read only some of it answers with what it could, and says in
// Unreadable what was missing.
func (s *ResourceService) Access(contextID string) (kube.AccessModel, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return kube.AccessModel{Error: err.Error()}, err
	}
	return s.watcher.Access(ctx)
}

// MyAccess asks the cluster what the caller may do in one namespace. It works
// for anybody who can reach the cluster, including somebody who may read no
// RBAC objects at all.
func (s *ResourceService) MyAccess(contextID, namespace string) (kube.MyRules, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return kube.MyRules{Namespace: namespace, Error: err.Error()}, err
	}
	return s.watcher.MyAccess(ctx, namespace)
}

// Ping reports whether a context's cluster can be reached, for the sidebar's
// connection indicator. It returns nil when the cluster answered and an error
// carrying the reason when it did not; there is no payload because the only
// question being asked is whether this works.
func (s *ResourceService) Ping(contextID string) error {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Ping(ctx)
}

// Disconnect lets go of a context without removing it: its watches stop and
// its client is forgotten, so nothing talks to that cluster until something
// asks for it again -- which connects afresh. The window closes its own tabs
// first; this is for whatever is left.
//
// In the web version the connection is shared by everyone signed in, and the
// tabs on it may be someone else's, so there is nothing more to let go of
// than what the caller's own tabs already have.
func (s *ResourceService) Disconnect(contextID string) error {
	if s.owners != nil {
		return nil
	}
	if _, err := s.resolve(contextID); err != nil {
		return err
	}
	s.watcher.Disconnect(contextID)
	return nil
}

// CustomResourceKinds lists what a cluster defines, grouped by API group, for
// the definitions section of the sidebar.
func (s *ResourceService) CustomResourceKinds(contextID string) ([]kube.CustomResourceGroup, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return []kube.CustomResourceGroup{}, err
	}
	return s.watcher.CustomResourceKinds(ctx)
}

// Describe renders the detail report shown in the slide-in panel.
//
// reveal decodes a Secret's values into plain text, and does nothing to any
// other kind. It is asked for per read rather than applied to a report already
// on screen, so the values cross only when somebody has pressed the button.
func (s *ResourceService) Describe(contextID, kind, namespace, name string, reveal bool) (string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	return s.watcher.Describe(ctx, kind, namespace, name, reveal)
}

// ObjectLinks reads what the detail panel lays out above the report: the
// object's conditions, the objects it names, and the pods it selects.
//
// A call of its own rather than part of Describe, so the report is on screen
// as soon as it is read, and a cluster that refuses the pod listing costs the
// panel one section rather than the report.
func (s *ResourceService) ObjectLinks(contextID, kind, namespace, name string) (kube.ObjectLinks, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.ObjectLinks{Conditions: []kube.Condition{}, References: []kube.Reference{}}, err
	}
	return s.watcher.ObjectLinks(kc, kind, namespace, name)
}

// ResourceYAML returns one object as the YAML the editor opens with. It is a
// live read rather than the informer's copy: the cache drops managed fields and
// redacts secret values, and an editor must open on the object rather than on
// the table's view of it.
// KubeVirtDetail reads one KubeVirt object laid out for its own panel: the
// facts and tables a person opens a virtual machine to find, rather than the
// YAML every other kind describes as.
//
// A call of its own rather than another field on Describe, for the reason the
// budget is one: it reads several objects -- the VirtualMachine, its instance,
// the virt-launcher pod, the migrations naming it -- and a cluster that has no
// KubeVirt should not pay for any of that when describing an ordinary pod.
func (s *ResourceService) KubeVirtDetail(contextID, kind, namespace, name string) (kube.KubeVirtDetail, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.KubeVirtDetail{Error: err.Error()}, err
	}
	return s.watcher.KubeVirtDetail(kc, kind, namespace, name)
}

func (s *ResourceService) ResourceYAML(contextID, kind, namespace, name string) (string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	return s.watcher.ResourceYAML(ctx, kind, namespace, name)
}

// ApplyYAML writes an edited object back to the cluster and returns it as the
// server left it -- with the resourceVersion the next save will be checked
// against, and whatever defaulting and admission control did to it on the way
// in. The editor replaces its contents with the result, which is what makes a
// second save work rather than fail as a conflict.
//
// opened is the document the editor started from. It is what lets a save the
// cluster refuses be retried against the object's current state when nothing
// the edit touched has moved -- the ordinary case for anything with a
// controller writing its status. See kube.ApplyYAML.
func (s *ResourceService) ApplyYAML(contextID, kind, namespace, name, yaml, opened string) (string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	return s.watcher.ApplyYAML(ctx, kind, namespace, name, yaml, opened)
}

// CheckYAML reports whether what is in the editor is still YAML. It touches no
// cluster: it is called as the user types, and the only question it answers is
// whether the document parses.
func (s *ResourceService) CheckYAML(yaml string) kube.YAMLCheck {
	return kube.ValidateYAML(yaml)
}

// Namespaces lists the namespaces available for the namespace filter.
func (s *ResourceService) Namespaces(contextID string) ([]string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return []string{}, err
	}
	return s.watcher.Namespaces(ctx)
}

func (s *ResourceService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}
