package services

import (
	"context"
	"fmt"
	"time"

	"github.com/rogerwesterbo/k8sdockside/internal/appconfig"
	"github.com/rogerwesterbo/k8sdockside/internal/helmcli"
	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/rogerwesterbo/k8sdockside/internal/session"
)

// HelmService serves Helm releases: the tab that lists them, and the drawer
// that opens one.
//
// Helm being its own service rather than a corner of ResourceService is the
// point rather than an accident. A release is not a Kubernetes kind: it is a
// Secret with a gzipped JSON payload, so nothing here can be watched, nothing
// here resolves through the REST mapper, and the interesting half of that
// payload is exactly the half the resource cache refuses to hold. Those are
// three exceptions to how every other view in the app works, and they are
// better stated once, here, than threaded as special cases through the
// machinery that serves real kinds.
//
// It shares a watcher with ResourceService, and so a client pool: a context
// already open in a tab costs no second connection and no second credential
// exec when its releases are read.
//
// The two halves of it are not symmetric, and that asymmetry is the design.
// Reading needs nothing installed: the record is decoded here. Changing a
// release is Helm's own operation -- fetching a chart, rendering it, three-way
// merging it, running its hooks -- and is done by running helm, which the user
// may not have. So the reads always work and the writes say plainly when they
// cannot. See internal/helmcli.
type HelmService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	// store holds where helm is and how to run it, which is a setting because
	// on macOS it has to be: an app started from Finder cannot see the PATH the
	// user's helm is on.
	store *appconfig.Store
	// owners files each releases view under whoever opened it, in the web
	// version -- the same way ResourceService does its tabs.
	owners *session.Owners
}

// NewHelmService wires the service to the kubeconfig cache it resolves context
// IDs against, the watcher whose clients it borrows, and the settings that say
// where helm is.
func NewHelmService(configs *KubeconfigService, watcher *kube.Watcher, store *appconfig.Store) *HelmService {
	return &HelmService{configs: configs, watcher: watcher, store: store}
}

// Tool reports whether helm is available and where, for the settings view and
// for the buttons that need it.
//
// It is a plain answer rather than an error because "no helm" is an ordinary
// state rather than a fault: everything else about a release still works, and
// the four buttons that do not are better greyed out with a reason than left
// to fail when they are pressed.
func (s *HelmService) Tool() helmcli.Tool {
	return helmcli.Locate(s.store.Get().Preferences.Helm.Path)
}

// helm resolves the settings into a runnable helm and the options to run it
// with, or says why there is none.
func (s *HelmService) helm() (*helmcli.Helm, helmcli.Options, error) {
	prefs := s.store.Get().Preferences.Helm
	tool, err := helmcli.New(helmcli.Locate(prefs.Path))
	if err != nil {
		return nil, helmcli.Options{}, err
	}
	return tool, helmcli.Options{
		Wait:    prefs.Wait,
		Atomic:  prefs.Atomic,
		Timeout: time.Duration(prefs.TimeoutSeconds) * time.Second,
	}, nil
}

// target names the cluster a command runs against.
//
// The kubeconfig file and the context are both passed on, never left to helm's
// defaults. A dozen kubeconfigs open at once is this app's whole premise, and a
// helm reading $KUBECONFIG would upgrade whichever cluster the user last
// selected in a shell.
func (s *HelmService) target(contextID, namespace string) (helmcli.Target, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return helmcli.Target{}, err
	}
	return helmcli.Target{
		Kubeconfig: ctx.File,
		Context:    ctx.Name,
		Namespace:  namespace,
	}, nil
}

// Upgrade re-releases one release: a new chart version, new values, or both.
//
// The values are the complete set of user-supplied values the release should
// have afterwards, not additions to what it has. That is what the editor shows
// and so what a save has to mean -- deleting a line has to delete the value.
// See helmcli.UpgradeRequest.
func (s *HelmService) Upgrade(contextID, namespace, release, chart, version, values string) (string, error) {
	tool, options, err := s.helm()
	if err != nil {
		return "", err
	}
	target, err := s.target(contextID, namespace)
	if err != nil {
		return "", err
	}
	return tool.Upgrade(context.Background(), target, helmcli.UpgradeRequest{
		Release: release,
		Chart:   chart,
		Version: version,
		Values:  values,
	}, options)
}

// Rollback returns a release to an earlier revision.
//
// It needs no chart reference, unlike an upgrade: the revision being returned
// to stored its own rendered manifest and its own values, and that is what helm
// applies. A release whose chart nobody can find any more can still be rolled
// back.
func (s *HelmService) Rollback(contextID, namespace, release string, revision int) (string, error) {
	tool, options, err := s.helm()
	if err != nil {
		return "", err
	}
	target, err := s.target(contextID, namespace)
	if err != nil {
		return "", err
	}
	return tool.Rollback(context.Background(), target, release, revision, options)
}

// Uninstall removes a release and everything it installed.
//
// keepHistory leaves the release's records behind, so it stays listed as
// "uninstalled" and can still be rolled back. Off is helm's own default and is
// what the word reads as, so the caller has to ask for it.
func (s *HelmService) Uninstall(contextID, namespace, release string, keepHistory bool) (string, error) {
	tool, options, err := s.helm()
	if err != nil {
		return "", err
	}
	target, err := s.target(contextID, namespace)
	if err != nil {
		return "", err
	}
	return tool.Uninstall(context.Background(), target, release, keepHistory, options)
}

// ChartVersions lists the versions of a chart the configured repositories
// offer, for the upgrade form's picker.
//
// A chart installed from an OCI registry or a local path will find nothing.
// That is an answer rather than a failure -- Helm's release record does not say
// where a chart came from, so there is nothing to search with -- and the form
// still takes a version typed by hand.
func (s *HelmService) ChartVersions(chart string) ([]helmcli.ChartVersion, error) {
	tool, _, err := s.helm()
	if err != nil {
		return []helmcli.ChartVersion{}, err
	}
	return tool.Versions(context.Background(), chart)
}

// Releases lists the Helm releases installed in a cluster.
//
// A plain call rather than a subscription, because releases are not a watchable
// resource: they are Secrets whose payload has to be decoded, and that payload
// is what must not sit in an informer cache. The table is built and the payload
// dropped in the same breath.
func (s *HelmService) Releases(contextID string, namespaces []string) (kube.Table, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return kube.Table{}, err
	}
	return s.watcher.HelmReleases(ctx, namespaces)
}

// Subscribe opens a live view of a cluster's releases and returns the
// subscription ID its snapshots will carry. Pass no namespaces for all of them.
//
// The rows arrive on the same event as every other table's, through the same
// watcher, so the frontend routes them by ID without knowing that releases are
// special. What is different is underneath: the watch is on the Secrets holding
// the releases, and each change re-reads them rather than serving a cache --
// see kube.SubscribeHelm for why a release payload is never kept.
func (s *HelmService) Subscribe(caller context.Context, contextID string, namespaces []string) (string, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	return s.watcher.SubscribeHelmFor(ctx, namespaces, claimSubscription(caller, s.owners, s.watcher))
}

// Unsubscribe closes a releases view.
//
// It goes through this service rather than ResourceService only so that a
// caller holding a Helm subscription has one place to give it back; the watcher
// underneath is the same one, and the IDs come from the same sequence.
func (s *HelmService) Unsubscribe(caller context.Context, subscriptionID string) {
	if !s.owners.Allowed(caller, subscriptionID) {
		return
	}
	dropSubscription(s.owners, s.watcher, subscriptionID)
}

// SetNamespaces re-points an open releases view at other namespaces, none
// meaning all.
//
// Unlike a watched kind, this does re-read: the filter is applied to a fresh
// listing rather than to a cache that already holds every namespace. It is
// still one call, and still no watch is reopened.
func (s *HelmService) SetNamespaces(caller context.Context, subscriptionID string, namespaces []string) {
	if !s.owners.Allowed(caller, subscriptionID) {
		return
	}
	s.watcher.SetNamespaces(subscriptionID, namespaces)
}

// Detail reads one release in full: the values it was installed with, the notes
// it printed, the objects it rendered, and the revisions behind it.
//
// This is the call the detail drawer makes instead of Describe. A release has
// no kind for the REST mapper to resolve, so the generic describe path can only
// answer "unknown resource kind" -- correctly, since there is no such kind. The
// release's own record is the report.
func (s *HelmService) Detail(contextID, namespace, name string) (kube.HelmReleaseDetail, error) {
	ctx, err := s.resolve(contextID)
	if err != nil {
		return kube.HelmReleaseDetail{}, err
	}
	return s.watcher.HelmReleaseDetail(ctx, namespace, name)
}

func (s *HelmService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}
