package services

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/registry"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/k8sdockside/k8sdockside/internal/updates"
	"github.com/wailsapp/wails/v3/pkg/application"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PluginService is how the frontend gets the solution plugins: the ones that
// ship with the app, the ones the user has installed, and -- per cluster --
// whether the thing each one describes is actually in front of you.
//
// It mirrors ThemeService deliberately. The two extension points are installed
// the same way, fail the same way, and are managed from the same shape of
// settings section, because they are the same promise made twice: drop a JSON
// file in a folder and the app knows about your thing.
//
// Unlike themes, the catalogue is cached. A theme is read when the settings
// view asks; a plugin is read every time a tab is opened, because a tab's kind
// has to be resolved back to the view it names -- see Resolve.
//
// In the web version the installed plugins are everyone's, so only an
// administrator may install, remove, switch or add folders of them.
type PluginService struct {
	store   *appconfig.Store
	watcher *kube.Watcher
	configs *KubeconfigService
	// server is set in the web version, which has no file manager to open.
	server bool

	// registries answers the views that ask what tags an image has.
	registries *registry.Client

	mu     sync.RWMutex
	cached *plugins.Catalogue
	// running is, per context, which images its pods run -- the only images a
	// view may ask a registry about.
	running map[string]runningImages
	// found is where the services plugins find by selector were last found,
	// per context and plugin. See serviceTarget.
	found map[string]foundService
}

// NewPluginService wires the service to the settings store, and to the cluster
// connections the overview's counts come from.
func NewPluginService(store *appconfig.Store, configs *KubeconfigService, watcher *kube.Watcher) *PluginService {
	return &PluginService{
		store:      store,
		configs:    configs,
		watcher:    watcher,
		registries: registry.New("k8sdockside/" + DisplayVersion() + " (+https://github.com/" + updates.Repo + ")"),
		running:    map[string]runningImages{},
		found:      map[string]foundService{},
	}
}

// List returns every plugin available right now, with anything that failed to
// load and why.
func (s *PluginService) List() plugins.Catalogue {
	return s.catalogue()
}

// catalogue reads the plugin folders, or hands back what was read last time.
//
// The cache exists because Resolve is on the path of every tab open and every
// snapshot subscription, and re-reading a directory to answer "what does
// plugin:argocd/applications mean" would put a filesystem walk behind an
// interaction that should be instant. Everything that could change the answer
// -- reloading, adding a folder, dropping one -- clears it explicitly, so it is
// never stale in a way the user did not ask for.
func (s *PluginService) catalogue() plugins.Catalogue {
	s.mu.RLock()
	cached := s.cached
	s.mu.RUnlock()
	if cached != nil {
		return *cached
	}

	loaded := plugins.LoadAt(DisplayVersion(), s.store.PluginsDir(), s.store.PluginFolders(), s.store.DisabledPlugins())
	s.mu.Lock()
	s.cached = &loaded
	s.mu.Unlock()
	return loaded
}

// forget drops the cache, so the next read goes back to disk.
func (s *PluginService) forget() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// Reload rereads the plugin folders, picking up a file edited since launch.
func (s *PluginService) Reload() plugins.Catalogue {
	s.forget()
	return s.catalogue()
}

// Resolve turns a "plugin:" tab kind into the underlying kind to list and the
// filters the view pins, so the rest of the app can go on treating a tab as a
// kind and a namespace.
//
// It is exported for the frontend as well as used by ResourceService: the tab
// needs to know whether its namespace is fixed before it draws the filter.
func (s *PluginService) Resolve(kind string) (plugins.Resolved, error) {
	return s.catalogue().ResolveKind(kind)
}

// Summary is the plugin's overview for one context: whether this cluster serves
// what the plugin needs, and a live count of what it manages.
func (s *PluginService) Summary(contextID, pluginID string) (plugins.Summary, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return plugins.Summary{PluginID: pluginID}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return plugins.Summary{PluginID: pluginID}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return plugins.Summary{PluginID: pluginID}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return plugins.Summarise(plugin, &clusterFor{watcher: s.watcher, ctx: ctx}), nil
}

// ----- the bridge a plugin's own views reach the cluster through ------------
//
// The frame hosting a custom view calls these on the view's behalf, naming the
// plugin whose view it is. The frame decides which plugin that is, not the
// view, and the kind check is made here as well as there, so the rule lives
// in Go whatever the frontend gets wrong.

// Objects lists one kind for a plugin's own view.
func (s *PluginService) Objects(contextID, pluginID, kind, namespace, selector string) ([]map[string]any, error) {
	_, ctx, err := s.forView(contextID, pluginID, kind, false)
	if err != nil {
		return []map[string]any{}, err
	}
	return s.watcher.Objects(ctx, kind, namespace, selector)
}

// Object reads one object for a plugin's own view.
func (s *PluginService) Object(contextID, pluginID, kind, namespace, name string) (map[string]any, error) {
	_, ctx, err := s.forView(contextID, pluginID, kind, false)
	if err != nil {
		return nil, err
	}
	return s.watcher.Object(ctx, kind, namespace, name)
}

// Patch applies a merge patch a plugin's own view asked for. By the time it
// gets here the user has seen the patch and said yes; that is the frame's job,
// and this only checks the plugin was allowed to ask.
func (s *PluginService) Patch(contextID, pluginID, kind, namespace, name, patch string) error {
	_, ctx, err := s.forView(contextID, pluginID, kind, true)
	if err != nil {
		return err
	}
	report, err := s.watcher.PatchMany(ctx, kind, []kube.ObjectRef{{Namespace: namespace, Name: name}}, patch)
	if err != nil {
		return err
	}
	if len(report.Failures) > 0 {
		return errors.New(report.Failures[0].Error)
	}
	return nil
}

// Create makes one object a plugin's own view asked for and returns the name
// it was given. As with Patch, the user has already seen the object and said
// yes; this checks the plugin may ask, and that the kind is one no plugin may
// write.
func (s *PluginService) Create(contextID, pluginID, kind, namespace, object string) (string, error) {
	plugin, ctx, err := s.forView(contextID, pluginID, kind, true)
	if err != nil {
		return "", err
	}
	obj, err := plugin.NewObject(kind, namespace, object)
	if err != nil {
		return "", err
	}
	return s.watcher.Create(ctx, kind, namespace, obj)
}

// forView checks a plugin's view may touch a kind, and finds the context.
func (s *PluginService) forView(contextID, pluginID, kind string, write bool) (plugins.Plugin, kube.Context, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return plugin, kube.Context{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return plugin, kube.Context{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	if !plugin.CanRead(kind) {
		return plugin, kube.Context{}, fmt.Errorf("the %s plugin does not declare %q, so its views cannot read it", plugin.Name, kind)
	}
	if write && !plugin.CanWrite(kind) {
		return plugin, kube.Context{}, fmt.Errorf("the %s plugin does not declare \"ui\": {\"write\": true}, so its views cannot change anything", plugin.Name)
	}
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return plugin, kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return plugin, ctx, nil
}

// RegistryLookup asks an image's registry which tags it has, and what the
// image's tag points at now, for a plugin's own view.
//
// The view names the image, so the image must be one a pod in the cluster
// runs: otherwise a plugin could have the app -- in the web version, the pod
// -- make requests to any host it liked. A registry that fails is reported in
// the answer, not as an error.
func (s *PluginService) RegistryLookup(ctx context.Context, contextID, pluginID, image string, refresh bool) (registry.Report, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return registry.Report{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return registry.Report{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	if !plugin.CanAskRegistries() {
		return registry.Report{}, fmt.Errorf("the %s plugin does not declare \"ui\": {\"registries\": true}, so its views cannot ask registries", plugin.Name)
	}
	kc, ok := s.configs.lookup(contextID)
	if !ok {
		return registry.Report{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	ref, err := registry.Parse(image)
	if err != nil {
		return registry.Report{}, fmt.Errorf("%q is not an image reference: %w", image, err)
	}
	runs, err := s.runs(kc, ref)
	if err != nil {
		return registry.Report{}, err
	}
	if !runs {
		return registry.Report{}, fmt.Errorf("no pod in %s runs %s, so its registry is not asked", kc.Name, image)
	}
	return s.registries.Lookup(ctx, image, refresh)
}

// runningImages is what a context's pods ran when last listed: each image as
// registry/repository:tag, and as registry/repository for a reference with
// only a digest.
type runningImages struct {
	images map[string]bool
	at     time.Time
}

const (
	// runningTTL is how long a list of what runs is used. A page asks about
	// every image at once, and one list of the pods answers all of it.
	runningTTL = time.Minute
	// runningRecheck is how old a list must be before an image missing from
	// it is looked for again -- in a pod started since.
	runningRecheck = 5 * time.Second
)

// runs reports whether a pod in the context runs the image.
func (s *PluginService) runs(kc kube.Context, ref registry.Ref) (bool, error) {
	key := ref.Key()
	if ref.Tag != "" {
		key = ref.Tagged()
	}
	s.mu.RLock()
	kept, ok := s.running[kc.ID]
	s.mu.RUnlock()
	age := time.Since(kept.at)
	if ok && age < runningTTL && (kept.images[key] || age < runningRecheck) {
		return kept.images[key], nil
	}

	pods, err := s.watcher.Objects(kc, "pods", kube.AllNamespaces, "")
	if err != nil {
		return false, fmt.Errorf("could not list the pods to check the image runs: %w", err)
	}
	kept = runningImages{images: imagesIn(pods), at: time.Now()}
	s.mu.Lock()
	s.running[kc.ID] = kept
	s.mu.Unlock()
	return kept.images[key], nil
}

// imagesIn is every image the pods' containers name -- init and ephemeral
// ones included -- keyed as runs looks them up.
func imagesIn(pods []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, pod := range pods {
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			containers, _, _ := unstructured.NestedSlice(pod, "spec", field)
			for _, c := range containers {
				container, _ := c.(map[string]any)
				image, _ := container["image"].(string)
				ref, err := registry.Parse(image)
				if err != nil {
					continue
				}
				out[ref.Key()] = true
				if ref.Tag != "" {
					out[ref.Tagged()] = true
				}
			}
		}
	}
	return out
}

// ServiceGet makes a GET request, for a plugin's own view, to one of the
// in-cluster Services the plugin declares -- through the API server's service
// proxy -- and returns what the Service answered.
//
// The view names only the declared service and a path; where the request goes
// is the manifest's, and the path must sit under one of the prefixes it
// declares. A Service found by a selector is looked for again after a short
// while, so a reinstall in another namespace is followed.
func (s *PluginService) ServiceGet(ctx context.Context, contextID, pluginID, serviceID, path string, query map[string][]string) (kube.ServiceAnswer, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return kube.ServiceAnswer{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return kube.ServiceAnswer{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	svc, ok := plugin.Service(serviceID)
	if !ok {
		return kube.ServiceAnswer{}, fmt.Errorf("the %s plugin does not declare a service %q in \"ui\": {\"services\": [...]}, so its views cannot call it", plugin.Name, serviceID)
	}
	if !svc.Allows(path) {
		return kube.ServiceAnswer{}, fmt.Errorf("the %s plugin may not request %q from %s -- it declares %s", plugin.Name, path, svc.Label, strings.Join(svc.Paths, ", "))
	}
	params, err := serviceQuery(query)
	if err != nil {
		return kube.ServiceAnswer{}, err
	}
	kc, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.ServiceAnswer{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	target, err := s.serviceTarget(kc, pluginID, svc)
	if err != nil {
		return kube.ServiceAnswer{}, err
	}
	return s.watcher.ServiceGet(ctx, kc, target, path, params)
}

// foundService is where a service found by a selector was, and when.
type foundService struct {
	target kube.ServiceTarget
	at     time.Time
}

// foundTTL is how long a Service found by a selector is used before it is
// looked for again. A page polls; one lookup answers a burst of its calls.
const foundTTL = 30 * time.Second

// serviceTarget works out which Service port a declared service is.
func (s *PluginService) serviceTarget(kc kube.Context, pluginID string, svc plugins.UIService) (kube.ServiceTarget, error) {
	if svc.Name != "" {
		return kube.ServiceTarget{Namespace: svc.Namespace, Name: svc.Name, Port: string(svc.Port), Scheme: svc.Scheme}, nil
	}

	key := kc.ID + "\x00" + pluginID + "\x00" + svc.ID
	s.mu.RLock()
	kept, ok := s.found[key]
	s.mu.RUnlock()
	if ok && time.Since(kept.at) < foundTTL {
		return kept.target, nil
	}

	candidates, err := s.watcher.FindServices(kc, svc.Namespace, svc.Selector)
	if err != nil {
		return kube.ServiceTarget{}, fmt.Errorf("could not look for %s: %w", svc.Label, err)
	}
	target, ok := pickService(candidates, svc)
	if !ok {
		where := "any namespace"
		if svc.Namespace != "" {
			where = svc.Namespace
		}
		return kube.ServiceTarget{}, fmt.Errorf("no service labelled %s with a port %s in %s -- is %s installed?", svc.Selector, svc.Port, where, svc.Label)
	}
	s.mu.Lock()
	s.found[key] = foundService{target: target, at: time.Now()}
	s.mu.Unlock()
	return target, nil
}

// pickService is the first candidate that has the declared port, by name or
// by number.
func pickService(candidates []kube.ServiceFound, svc plugins.UIService) (kube.ServiceTarget, bool) {
	number, byNumber := svc.Port.Number()
	for _, c := range candidates {
		for _, port := range c.Ports {
			if port.Name == string(svc.Port) || (byNumber && int(port.Number) == number) {
				return kube.ServiceTarget{Namespace: c.Namespace, Name: c.Name, Port: string(svc.Port), Scheme: svc.Scheme}, true
			}
		}
	}
	return kube.ServiceTarget{}, false
}

// Limits on the query a view sends with a service call.
const (
	maxQueryKeys  = 64
	maxQueryBytes = 16 << 10
)

// serviceQuery checks the query parameters a view asked for and encodes them.
func serviceQuery(query map[string][]string) (url.Values, error) {
	if len(query) > maxQueryKeys {
		return nil, fmt.Errorf("a service call may carry at most %d query parameters", maxQueryKeys)
	}
	out := url.Values{}
	size := 0
	for key, values := range query {
		if key == "" {
			return nil, errors.New("a query parameter has no name")
		}
		size += len(key)
		for _, value := range values {
			size += len(value)
		}
		out[key] = slices.Clone(values)
	}
	if size > maxQueryBytes {
		return nil, fmt.Errorf("the query is %d bytes; a service call may carry at most %d KiB", size, maxQueryBytes>>10)
	}
	return out, nil
}

// assetMiddleware serves plugins' own views to the webview, and keeps them
// from calling the app's services directly. See plugins.Middleware.
//
// Unexported so it is not bound: it is for main.go, not for the frontend.
func (s *PluginService) assetMiddleware() application.Middleware {
	return plugins.Middleware(s.catalogue)
}

// ----- the buttons plugins put on an object ---------------------------------

// ObjectActions is what every enabled plugin offers on one object right now:
// its buttons, less those whose conditions the object does not meet.
//
// The object is read only when some plugin has actions for its kind, so the
// action bar of everything else costs nothing.
func (s *PluginService) ObjectActions(contextID, kind, namespace, name string) ([]plugins.Offered, error) {
	out := []plugins.Offered{}
	cat := s.catalogue()
	if !slices.Contains(cat.ActionKinds(), kind) {
		return out, nil
	}
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return out, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	raw, err := s.watcher.Object(ctx, kind, namespace, name)
	if err != nil {
		return out, err
	}
	obj := &unstructured.Unstructured{Object: raw}
	for _, plugin := range cat.Enabled() {
		for _, action := range plugin.Actions {
			if action.Kind == kind && action.Offers(obj) {
				out = append(out, action.Offer(plugin, namespace, name))
			}
		}
	}
	return out, nil
}

// RunAction makes the request one of a plugin's actions declares, against one
// object. The frontend names the action and the object; what is sent is read
// from the manifest here.
//
// The object is read again first, so a button pressed on a state that has
// since moved on -- Pause, on a machine someone stopped a second ago -- is
// refused with the reason rather than sent.
func (s *PluginService) RunAction(contextID, pluginID, actionID, namespace, name string) (string, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return "", fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return "", fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	action, ok := plugin.Action(actionID)
	if !ok {
		return "", fmt.Errorf("the %s plugin has no action called %q", plugin.Name, actionID)
	}
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return "", fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}

	raw, err := s.watcher.Object(ctx, action.Kind, namespace, name)
	if err != nil {
		return "", err
	}
	if !action.Offers(&unstructured.Unstructured{Object: raw}) {
		return "", fmt.Errorf("%s is not offered on %s as it is now -- it may have changed since the button was drawn", action.Label, name)
	}

	vars := plugins.Vars(namespace, name)
	req := action.Request
	switch req.Type {
	case plugins.RequestPatch:
		patch, err := json.Marshal(plugins.Expand(req.Patch, vars))
		if err != nil {
			return "", err
		}
		report, err := s.watcher.PatchMany(ctx, action.Kind, []kube.ObjectRef{{Namespace: namespace, Name: name}}, string(patch))
		if err != nil {
			return "", err
		}
		if len(report.Failures) > 0 {
			return "", errors.New(report.Failures[0].Error)
		}
		return "", nil

	case plugins.RequestSubresource:
		var body []byte
		if len(req.Body) > 0 {
			if body, err = json.Marshal(plugins.Expand(req.Body, vars)); err != nil {
				return "", err
			}
		}
		return "", s.watcher.CallSubresource(ctx, req.Method, req.SubresourcePath(namespace, name), body)

	case plugins.RequestCreate:
		object, _ := plugins.Expand(req.Object, vars).(map[string]any)
		// Always the namespace of the object the button is on, whatever the
		// template says: the button is about this object, and an action is not
		// a way to put things in other namespaces.
		meta, _ := object["metadata"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
		}
		meta["namespace"] = namespace
		object["metadata"] = meta
		return s.watcher.Create(ctx, req.Kind, namespace, object)
	}
	return "", fmt.Errorf("the %s plugin's action %q has a request this app cannot make", plugin.Name, actionID)
}

// clusterFor adapts the watcher to the narrow interface the summary builder
// wants, so that the wording-and-ordering half of the overview can be tested
// without a cluster.
type clusterFor struct {
	watcher *kube.Watcher
	ctx     kube.Context
}

func (c *clusterFor) KindsServed(kinds []string) (map[string]bool, error) {
	return c.watcher.KindsServed(c.ctx, kinds)
}

func (c *clusterFor) CountBy(kind, namespace, selector string, path kube.FieldPath) (kube.Tally, error) {
	return c.watcher.CountBy(c.ctx, kind, namespace, selector, path)
}

// SetEnabled switches a plugin on or off. A switched-off plugin keeps its place
// in the settings list -- that is where it is switched back on -- but stops
// being offered anywhere else: no sidebar rows, no charts, no overview.
//
// The wanted state is passed rather than toggled so that the switch in the
// settings view cannot drift out of step with what is on disk.
func (s *PluginService) SetEnabled(ctx context.Context, id string, enabled bool) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	if _, ok := s.catalogue().Find(id); !ok {
		return s.catalogue(), fmt.Errorf("no plugin called %q is installed", id)
	}
	if _, err := s.store.SetPluginEnabled(id, enabled); err != nil {
		return s.catalogue(), err
	}
	s.forget()
	return s.catalogue(), nil
}

// ClusterProbe is what only a query can answer about one cluster: which
// plugins belong to it, for the ones a sweep of the definitions cannot tell
// apart.
//
// Both halves exist for the same reason. A product that defines custom
// resources gives itself away in the definitions the sidebar has already read,
// and costs nothing to recognise. A product that defines none -- Flannel is a
// DaemonSet, a ConfigMap and nothing else -- can only be recognised by finding
// what it runs, and that is a real request, which is why it is asked for here
// rather than folded into what the frontend already holds.
type ClusterProbe struct {
	// Known are the ids of known plugins whose product is running here, found
	// by their workload probes. Only plugins that are not installed are asked
	// about: one already in the catalogue is under Installed, where its own
	// overview answers this.
	Known []string `json:"known"`
	// Absent are the ids of installed plugins whose object requirements are
	// not met here -- the ones that would otherwise read as installed in every
	// cluster, because the kinds they need are kinds every cluster serves.
	//
	// Only plugins with such a requirement appear either way: one whose
	// requirements are all kinds is answered from the definitions, as before.
	// A plugin the cluster could not be asked about is in neither list, which
	// leaves it as it was rather than calling it absent.
	Absent []string `json:"absent"`
}

// Probe asks a cluster what the definitions cannot say: which known plugins
// are running here, and which installed plugins are not. See ClusterProbe.
//
// It is one list per probe or object requirement and nothing at all for a
// plugin that has none, so a cluster is usually one request or none.
func (s *PluginService) Probe(contextID string) (ClusterProbe, error) {
	out := ClusterProbe{Known: []string{}, Absent: []string{}}
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return out, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	catalogue := s.catalogue()
	cl := &clusterFor{watcher: s.watcher, ctx: ctx}

	for _, known := range plugins.KnownPlugins() {
		if len(known.DetectWorkloads) == 0 {
			continue
		}
		if _, installed := catalogue.InstalledHere(known.ID); installed {
			continue
		}
		if known.RunsIn(cl) {
			out.Known = append(out.Known, known.ID)
		}
	}

	for _, plugin := range catalogue.Plugins {
		if plugin.Disabled || !plugins.NeedsObjects(plugin) {
			continue
		}
		if here, told := plugins.HasItsObjects(plugin, cl); told && !here {
			out.Absent = append(out.Absent, plugin.ID)
		}
	}
	return out, nil
}

// HideSuggestion stops the sidebar suggesting a known plugin, or lets it
// suggest it again, and returns the settings as saved.
func (s *PluginService) HideSuggestion(id string, hidden bool) (appconfig.Settings, error) {
	if _, ok := plugins.FindKnown(id); !ok {
		return s.store.Get(), fmt.Errorf("%q is not a plugin this app knows of", id)
	}
	return s.store.HidePluginSuggestion(id, hidden)
}

// SetState keeps one value for a plugin's own pages on one context -- what the
// bridge's storage.set asks for -- or forgets it when value is empty, and
// returns the settings as saved. The frame answers reads from the settings it
// already holds, so there is no getter.
func (s *PluginService) SetState(pluginID, contextID, key, value string) (appconfig.Settings, error) {
	return s.store.SetPluginState(pluginID, contextID, key, value)
}

// Dir is the folder user plugins are read from by default.
func (s *PluginService) Dir() string {
	return s.store.PluginsDir()
}

// RevealDir opens the plugins folder in the platform's file manager, creating
// it first if it has never been used. The path comes from the store rather than
// the frontend, so nothing the webview says can decide what gets opened.
func (s *PluginService) RevealDir() error {
	if s.server {
		return errDesktopOnly
	}
	dir := s.store.PluginsDir()
	if err := plugins.EnsureDir(dir); err != nil {
		return err
	}
	return application.Get().Env.OpenFileManager(dir, false)
}

// CreateExample writes a starter plugin into the plugins folder and returns the
// path it wrote.
func (s *PluginService) CreateExample(ctx context.Context) (string, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return "", err
	}
	path, err := plugins.WriteExample(s.store.PluginsDir())
	if err != nil {
		return "", err
	}
	s.forget()
	return path, nil
}

// InstallFromGit clones a plugin repository into the plugins folder and reads
// it, or updates the clone already there -- see plugins.Install. The
// repository's plugin.json has to be at its root.
//
// A clone that worked but holds nothing that loads is still an error, with
// what was wrong: "installed" followed by nothing appearing is the one outcome
// that leaves the user with no idea where to look. The clone is kept either
// way, so a plugin waiting on a newer app loads once the app is updated, and
// one with a mistake in it can be fixed and updated in place.
func (s *PluginService) InstallFromGit(ctx context.Context, url string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	dest, err := plugins.Install(s.store.PluginsDir(), url)
	if err != nil {
		return s.catalogue(), err
	}
	s.forget()
	cat := s.catalogue()
	return cat, installed(cat, dest)
}

// InstallKnown installs one of the plugins the app knows of, from the
// repository the app has for it. The frontend names the plugin rather than the
// address, so the known list is the only place that address comes from.
func (s *PluginService) InstallKnown(ctx context.Context, id string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	known, ok := plugins.FindKnown(id)
	if !ok {
		return s.catalogue(), fmt.Errorf("%q is not a plugin this app knows of", id)
	}
	// A copy read from a folder the user watches does not count: installing
	// puts one in the plugins folder, which takes the id from it.
	if _, ok := s.catalogue().InstalledHere(id); ok {
		return s.catalogue(), fmt.Errorf("%s is already installed", known.Name)
	}
	return s.InstallFromGit(ctx, known.Repo)
}

// Known is the list of plugins the app knows of, each marked with whether it
// is already installed here.
func (s *PluginService) Known() []plugins.KnownOffer {
	return s.catalogue().Offer()
}

// installed says what became of a freshly cloned folder: nil when a plugin
// from it loaded, and otherwise why nothing did.
func installed(cat plugins.Catalogue, dest string) error {
	inside := func(path string) bool {
		rel, err := filepath.Rel(dest, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	for _, p := range cat.Plugins {
		if !p.Builtin() && inside(p.Origin) {
			return nil
		}
	}
	var reasons []string
	for _, problem := range cat.Problems {
		if inside(problem.Path) {
			reasons = append(reasons, problem.Message)
		}
	}
	if len(reasons) == 0 {
		return fmt.Errorf("cloned into %s, but there is no plugin.json at its root", dest)
	}
	return fmt.Errorf("cloned into %s, but it would not load:\n%s", dest, strings.Join(reasons, "\n"))
}

// UpdateFromGit pulls the repository a plugin was cloned from and reads it
// again.
func (s *PluginService) UpdateFromGit(ctx context.Context, id string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	plugin, ok := s.catalogue().Find(id)
	if !ok {
		return s.catalogue(), fmt.Errorf("no plugin called %q is installed", id)
	}
	// Only a clone the app made: a watched folder is the user's own checkout,
	// and pulling into it is theirs to do.
	if !plugin.Builtin() && !plugins.InPluginsDir(s.store.PluginsDir(), plugin) {
		return s.catalogue(), fmt.Errorf("%s is read from a folder you watch -- it is your own checkout, so pull it yourself", plugin.Name)
	}
	repo, ok := plugins.RepoOf(plugin)
	if !ok {
		return s.catalogue(), fmt.Errorf("%s was not installed from a repository, so there is nothing to pull", plugin.Name)
	}
	if err := plugins.Pull(repo); err != nil {
		return s.catalogue(), err
	}
	s.forget()
	return s.catalogue(), nil
}

// UninstallPreview says what uninstalling a plugin would delete -- its clone,
// its folder or its file in the plugins folder -- and which other plugins go
// with it, without deleting anything. The settings view asks the user about
// exactly that before calling Uninstall.
func (s *PluginService) UninstallPreview(id string) (plugins.Removal, error) {
	cat := s.catalogue()
	plugin, ok := cat.Find(id)
	if !ok {
		return plugins.Removal{Plugins: []string{}}, fmt.Errorf("no plugin called %q is installed", id)
	}
	return plugins.Removable(cat, s.store.PluginsDir(), plugin)
}

// Uninstall deletes an installed plugin from the plugins folder and reads the
// folders again -- see plugins.Uninstall. A built-in, or a plugin read from a
// folder the user added, is refused: the first is only switched off, and the
// second is theirs.
func (s *PluginService) Uninstall(ctx context.Context, id string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	cat := s.catalogue()
	plugin, ok := cat.Find(id)
	if !ok {
		return cat, fmt.Errorf("no plugin called %q is installed", id)
	}
	if _, err := plugins.Uninstall(cat, s.store.PluginsDir(), plugin); err != nil {
		return cat, err
	}
	s.forget()
	return s.catalogue(), nil
}

// AddFolder starts reading plugins from another directory.
func (s *PluginService) AddFolder(ctx context.Context, path string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return s.catalogue(), err
	}
	if !info.IsDir() {
		return s.catalogue(), errors.New(path + " is a file, not a folder")
	}
	if _, err := s.store.AddPluginFolder(path); err != nil {
		return s.catalogue(), err
	}
	s.forget()
	return s.catalogue(), nil
}

// RemoveFolder stops reading plugins from a directory. Nothing is deleted; the
// plugins in it stop being offered, and a tab open on one of their views says
// so rather than emptying.
func (s *PluginService) RemoveFolder(ctx context.Context, path string) (plugins.Catalogue, error) {
	if err := session.RequireAdmin(ctx); err != nil {
		return s.catalogue(), err
	}
	if _, err := s.store.RemovePluginFolder(path); err != nil {
		return s.catalogue(), err
	}
	s.forget()
	return s.catalogue(), nil
}

// BrowseForFolder opens the native picker in directory mode and adds the folder
// chosen. Cancelling leaves everything as it was and is not an error.
func (s *PluginService) BrowseForFolder(ctx context.Context) (plugins.Catalogue, error) {
	if s.server {
		return s.catalogue(), errDesktopOnly
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Add a folder of plugins").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		ShowHiddenFiles(true)

	if home, err := os.UserHomeDir(); err == nil {
		dialog.SetDirectory(home)
	}

	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return s.catalogue(), err
	}
	if path == "" {
		return s.catalogue(), nil // cancelled
	}
	return s.AddFolder(ctx, path)
}
