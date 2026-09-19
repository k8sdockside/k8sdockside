package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/addons"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// What a plugin adds to one object, rather than to the sidebar: buttons on the
// object's action bar, and panels of its own in the object's detail view. This
// is how a plugin gets product-specific -- a virtual machine's Start, Pause and
// Migrate, an Argo CD application's Sync -- without the app having to learn the
// product.
//
// An action is data, like the rest of the manifest: which kind it appears on,
// when it is offered, and the one request it makes. The frontend only ever names
// an action; the request itself is read from the manifest here, so a button
// does exactly what the file says and nothing the webview makes up.
//
// A section is code: a page from the plugin's ui folder in a sandboxed frame,
// given the object it is drawn for. See ui.go.

// The requests an action may make.
const (
	// RequestPatch merge-patches the object the button is on.
	RequestPatch = "patch"
	// RequestSubresource calls a verb the API serves beside the object rather
	// than as a write to it -- KubeVirt's pause, restart, migrate.
	RequestSubresource = "subresource"
	// RequestCreate creates a new object in the same namespace -- a
	// VirtualMachineInstanceMigration, an Argo CD sync operation's record.
	RequestCreate = "create"
)

// ToneDanger colours an action apart and pushes it to the end of the bar.
const ToneDanger = "danger"

// Action is one button a plugin puts on the action bar of every object of a
// kind.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitzero"`
	// Kind is what the button appears on.
	Kind string `json:"kind"`
	// Tone is ToneDanger or empty.
	Tone string `json:"tone,omitzero"`
	// Confirm is the question asked before the request is made, with {name}
	// and {namespace} filled in. Empty runs it on the click.
	Confirm string `json:"confirm,omitzero"`
	// Done is the notice shown once it has worked: "{name} is pausing".
	Done string `json:"done,omitzero"`
	// When lists what must hold of the object for the button to be offered.
	// All of them must; none means always.
	When []Condition `json:"when,omitzero"`
	// Request is what pressing it does.
	Request Request `json:"request"`
}

// Condition is one test of the object an action is offered on.
type Condition struct {
	// Field is a kube.FieldPath: `status.printableStatus`,
	// `status.conditions[Ready]`.
	Field kube.FieldPath `json:"field"`
	// In holds when the field's value is one of these.
	In []string `json:"in,omitzero"`
	// NotIn holds when it is none of these -- which includes being absent, so
	// `"notIn": ["False"]` reads "unless the cluster has said no".
	NotIn []string `json:"notIn,omitzero"`
}

// Request is the one call an action makes. Strings anywhere inside Patch, Body
// and Object may use {name} and {namespace}, which are the object's.
type Request struct {
	// Type is RequestPatch, RequestSubresource or RequestCreate.
	Type string `json:"type"`

	// Patch is the merge patch, for RequestPatch.
	Patch map[string]any `json:"patch,omitzero"`

	// For RequestSubresource: the path is
	// /apis/<apiGroup>/<version>/namespaces/<namespace>/<resource>/<name>/<subresource>.
	Subresource string `json:"subresource,omitzero"`
	// APIGroup defaults to the kind's own group. It may be another group only
	// under the kind's -- subresources.kubevirt.io under kubevirt.io -- so an
	// action cannot reach into APIs the plugin has nothing to do with.
	APIGroup string `json:"apiGroup,omitzero"`
	// Version is required: subresource groups are not in the REST mapper.
	Version string `json:"version,omitzero"`
	// Resource defaults to the kind's plural. A VM action pausing the VM's
	// instance names "virtualmachineinstances" here.
	Resource string `json:"resource,omitzero"`
	// Method is PUT (the default) or POST.
	Method string `json:"method,omitzero"`
	// Body is sent as JSON; empty sends none.
	Body map[string]any `json:"body,omitzero"`

	// For RequestCreate: the kind created, and the object. Its namespace is
	// always the namespace of the object the button is on.
	Kind   string         `json:"kind,omitzero"`
	Object map[string]any `json:"object,omitzero"`
}

// Section is one of a plugin's own panels in the detail view of every object
// of a kind: a page from its ui folder, told which object it is drawn for.
type Section struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind is the objects it is drawn for.
	Kind string `json:"kind"`
	// Entry is the file it opens, relative to the ui folder.
	Entry string `json:"entry,omitzero"`
	// Height is where the frame starts, in pixels, before the page says how
	// tall it is. Defaults to DefaultSectionHeight.
	Height int `json:"height,omitzero"`
}

// DefaultSectionHeight is a section's height before its page has measured
// itself.
const DefaultSectionHeight = 240

// forbiddenWrites are kinds no plugin action may write, whatever the file says.
// Each of them is a door to more than the object: a Secret is a credential, and
// the rest decide who may do what or rewrite requests as they arrive.
var forbiddenWrites = []string{
	kube.KindSecrets,
	kube.KindServiceAccounts,
	kube.KindRoles,
	kube.KindRoleBindings,
	kube.KindClusterRoles,
	kube.KindClusterRoleBindings,
	kube.KindMutatingWebhooks,
	kube.KindValidatingWebhooks,
	kube.KindCRDs,
}

// forbiddenGroups are the same doors, named as a custom resource would be.
var forbiddenGroups = []string{
	"rbac.authorization.k8s.io",
	"admissionregistration.k8s.io",
	"apiextensions.k8s.io",
	"certificates.k8s.io",
	"authentication.k8s.io",
	"authorization.k8s.io",
}

// writable reports whether an action may write a kind.
func writable(kind string) bool {
	if slices.Contains(forbiddenWrites, kind) {
		return false
	}
	if _, group, ok := kube.ParseCustomKind(kind); ok {
		if group == "" || slices.Contains(forbiddenGroups, group) {
			return false
		}
		// The core group spelled as a custom resource: crd:secrets.v1 and the
		// like. Nothing a plugin describes lives there.
		if !strings.Contains(group, ".") {
			return false
		}
	}
	return true
}

// segment is one element of an API path: what a resource, a version or a
// subresource is called.
var segment = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// validateActions checks every action, gathering what is wrong with each
// rather than stopping at the first -- see validate.
func validateActions(p *Plugin) error {
	var errs []error
	seen := map[string]bool{}
	for i, action := range p.Actions {
		action, err := validateAction(p.ID, action)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[action.ID] {
			errs = append(errs, fmt.Errorf("plugin %q has two actions with id %q", p.ID, action.ID))
			continue
		}
		seen[action.ID] = true
		p.Actions[i] = action
	}
	return errors.Join(errs...)
}

func validateAction(pluginID string, a Action) (Action, error) {
	a.ID = strings.TrimSpace(a.ID)
	a.Label = strings.TrimSpace(a.Label)
	a.Kind = strings.TrimSpace(a.Kind)

	if !addons.ValidID(a.ID) {
		return a, fmt.Errorf("plugin %q has an action with id %q, which must be lowercase letters, digits and dashes", pluginID, a.ID)
	}
	if a.Label == "" {
		a.Label = a.ID
	}
	if err := checkIcon(pluginID, fmt.Sprintf("action %q", a.ID), a.Icon); err != nil {
		return a, err
	}
	if a.Icon == "" {
		a.Icon = "puzzle"
	}
	if !kube.IsKnownKind(a.Kind) {
		return a, fmt.Errorf("plugin %q puts action %q on %q, which is not a kind this app can open", pluginID, a.ID, a.Kind)
	}
	if a.Tone != "" && a.Tone != ToneDanger {
		return a, fmt.Errorf("plugin %q, action %q has tone %q; the only tone is %q", pluginID, a.ID, a.Tone, ToneDanger)
	}
	for _, cond := range a.When {
		if !cond.Field.Valid() {
			return a, fmt.Errorf("plugin %q, action %q tests %q, which is not a field path", pluginID, a.ID, cond.Field)
		}
	}

	req, err := validateRequest(a.Kind, a.Request)
	if err != nil {
		return a, fmt.Errorf("plugin %q, action %q: %w", pluginID, a.ID, err)
	}
	a.Request = req
	return a, nil
}

func validateRequest(kind string, r Request) (Request, error) {
	switch r.Type {
	case RequestPatch:
		if len(r.Patch) == 0 {
			return r, fmt.Errorf("a patch request needs a patch")
		}
		if !writable(kind) {
			return r, fmt.Errorf("no plugin action may write %s", kind)
		}

	case RequestSubresource:
		plural, group, ok := kube.ParseCustomKind(kind)
		if !ok {
			return r, fmt.Errorf("a subresource request can only be made on a custom resource, and %s is not one", kind)
		}
		if !writable(kind) {
			return r, fmt.Errorf("no plugin action may call %s", kind)
		}
		if r.APIGroup == "" {
			r.APIGroup = group
		}
		if r.APIGroup != group && !strings.HasSuffix(r.APIGroup, "."+group) {
			return r, fmt.Errorf("a subresource on %s may only be called in %s or a group under it, not %s", kind, group, r.APIGroup)
		}
		if r.Resource == "" {
			r.Resource = plural
		}
		for what, value := range map[string]string{"apiGroup": r.APIGroup, "version": r.Version, "resource": r.Resource, "subresource": r.Subresource} {
			if !segment.MatchString(value) {
				return r, fmt.Errorf("a subresource request needs a %s of lowercase letters, digits, dots and dashes, not %q", what, value)
			}
		}
		r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
		if r.Method == "" {
			r.Method = "PUT"
		}
		if r.Method != "PUT" && r.Method != "POST" {
			return r, fmt.Errorf("a subresource request is a PUT or a POST, not %s", r.Method)
		}

	case RequestCreate:
		r.Kind = strings.TrimSpace(r.Kind)
		if !kube.IsKnownKind(r.Kind) {
			return r, fmt.Errorf("a create request makes %q, which is not a kind this app can open", r.Kind)
		}
		if !writable(r.Kind) {
			return r, fmt.Errorf("no plugin action may create %s", r.Kind)
		}
		if len(r.Object) == 0 {
			return r, fmt.Errorf("a create request needs the object to create")
		}
		if _, ok := r.Object["apiVersion"].(string); !ok {
			return r, fmt.Errorf("the object a create request makes needs an apiVersion")
		}
		if _, ok := r.Object["kind"].(string); !ok {
			return r, fmt.Errorf("the object a create request makes needs a kind")
		}

	case "":
		return r, fmt.Errorf("the request has no type; it is %q, %q or %q", RequestPatch, RequestSubresource, RequestCreate)
	default:
		return r, fmt.Errorf("the request type %q is not one of %q, %q or %q", r.Type, RequestPatch, RequestSubresource, RequestCreate)
	}
	return r, nil
}

// validateSections checks every section, gathering what is wrong with each
// rather than stopping at the first -- see validate.
func validateSections(p *Plugin) error {
	var errs []error
	seen := map[string]bool{}
	for i, s := range p.Sections {
		s.ID = strings.TrimSpace(s.ID)
		s.Label = strings.TrimSpace(s.Label)
		s.Kind = strings.TrimSpace(s.Kind)
		s.Entry = strings.TrimSpace(s.Entry)

		if !addons.ValidID(s.ID) {
			errs = append(errs, fmt.Errorf("plugin %q has a section with id %q, which must be lowercase letters, digits and dashes", p.ID, s.ID))
			continue
		}
		if seen[s.ID] {
			errs = append(errs, fmt.Errorf("plugin %q has two sections with id %q", p.ID, s.ID))
			continue
		}
		seen[s.ID] = true
		if s.Label == "" {
			s.Label = s.ID
		}
		if !kube.IsKnownKind(s.Kind) {
			errs = append(errs, fmt.Errorf("plugin %q draws section %q on %q, which is not a kind this app can open", p.ID, s.ID, s.Kind))
			continue
		}
		if s.Entry == "" {
			s.Entry = DefaultEntry
		}
		if !fs.ValidPath(s.Entry) || s.Entry == "." {
			errs = append(errs, fmt.Errorf("plugin %q has a section %q opening %q, which is not a file inside its ui folder", p.ID, s.ID, s.Entry))
			continue
		}
		if s.Height <= 0 {
			s.Height = DefaultSectionHeight
		}
		p.Sections[i] = s
	}
	return errors.Join(errs...)
}

// Action returns the plugin's action with the given id.
func (p Plugin) Action(id string) (Action, bool) {
	for _, a := range p.Actions {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// ActionKinds is every kind some enabled plugin puts an action on.
func (c Catalogue) ActionKinds() []string {
	var out []string
	for _, p := range c.Enabled() {
		for _, a := range p.Actions {
			if !slices.Contains(out, a.Kind) {
				out = append(out, a.Kind)
			}
		}
	}
	return out
}

// Offers reports whether an action is offered on an object in its current
// state.
func (a Action) Offers(obj *unstructured.Unstructured) bool {
	for _, cond := range a.When {
		value := cond.Field.Value(obj)
		if len(cond.In) > 0 && !slices.Contains(cond.In, value) {
			return false
		}
		if slices.Contains(cond.NotIn, value) {
			return false
		}
		// Neither list: the field has to be there at all.
		if len(cond.In) == 0 && len(cond.NotIn) == 0 && value == "" {
			return false
		}
	}
	return true
}

// Offered is one action as the action bar draws it: which plugin it is from,
// and its words with the object's name already in them.
type Offered struct {
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
	ID         string `json:"id"`
	Label      string `json:"label"`
	Icon       string `json:"icon"`
	Tone       string `json:"tone"`
	Confirm    string `json:"confirm"`
	Done       string `json:"done"`
}

// Offer is what one action says about one object.
func (a Action) Offer(p Plugin, namespace, name string) Offered {
	vars := Vars(namespace, name)
	return Offered{
		PluginID:   p.ID,
		PluginName: p.Name,
		ID:         a.ID,
		Label:      a.Label,
		Icon:       a.Icon,
		Tone:       a.Tone,
		Confirm:    expandString(a.Confirm, vars),
		Done:       expandString(a.Done, vars),
	}
}

// Vars are what an action's strings may refer to.
func Vars(namespace, name string) map[string]string {
	return map[string]string{"{name}": name, "{namespace}": namespace}
}

func expandString(s string, vars map[string]string) string {
	for key, value := range vars {
		s = strings.ReplaceAll(s, key, value)
	}
	return s
}

// Expand fills {name} and {namespace} into every string in a JSON value.
// Only strings are touched: a key, a number and a bool are left as written.
func Expand(v any, vars map[string]string) any {
	switch t := v.(type) {
	case string:
		return expandString(t, vars)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, inner := range t {
			out[k] = Expand(inner, vars)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, inner := range t {
			out[i] = Expand(inner, vars)
		}
		return out
	}
	return v
}

// SubresourcePath is the API path a subresource request calls for one object.
func (r Request) SubresourcePath(namespace, name string) string {
	parts := []string{"/apis", r.APIGroup, r.Version}
	if namespace != "" {
		parts = append(parts, "namespaces", namespace)
	}
	parts = append(parts, r.Resource, name, r.Subresource)
	return strings.Join(parts, "/")
}
