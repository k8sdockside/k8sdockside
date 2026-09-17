package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/rogerwesterbo/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// DrainEvent is the event a drain's progress arrives on. One event is one
// report about one drain; the payload names which.
const DrainEvent = "node:drain"

// Registering the event gives the binding generator the payload's type, the
// same way SnapshotEvent does in resourceservice.go.
func init() {
	application.RegisterEvent[kube.DrainProgress](DrainEvent)
}

// ActionService performs the operations that change a cluster from a button:
// deleting an object, scaling or restarting a workload, cordoning or draining a
// node.
//
// It is a separate service from ResourceService because the two answer
// different questions -- one serves what a cluster contains, this one changes
// it -- but they share a watcher, and so a client pool. A context already open
// in a tab costs no second connection and no second credential exec when you
// act on something in it.
type ActionService struct {
	configs *KubeconfigService
	watcher *kube.Watcher

	// Drains in flight, by ID, so one can be called off. A drain is the only
	// action that outlives its call: everything else is one request.
	mu     sync.Mutex
	drains map[string]context.CancelFunc
	nextID atomic.Uint64
	// owners files each drain under whoever started it, in the web version,
	// so its progress goes to the panel that asked.
	owners *session.Owners
}

// NewActionService wires the service to the kubeconfig cache it resolves
// context IDs against, and to the watcher whose clients it borrows.
func NewActionService(configs *KubeconfigService, watcher *kube.Watcher) *ActionService {
	return &ActionService{
		configs: configs,
		watcher: watcher,
		drains:  map[string]context.CancelFunc{},
	}
}

// ServiceShutdown calls off every drain still running when the app quits. A
// drain left going would go on evicting against a window that has gone.
func (s *ActionService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.drains {
		cancel()
		delete(s.drains, id)
	}
	return nil
}

// ObjectState reads the few facts the action bar needs about one object: what
// its buttons should say, and what they should start from.
func (s *ActionService) ObjectState(contextID, kind, namespace, name string) (kube.ObjectState, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.ObjectState{}, err
	}
	return s.watcher.ObjectState(kc, kind, namespace, name)
}

// Delete removes one object from its cluster.
func (s *ActionService) Delete(contextID, kind, namespace, name string) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Delete(kc, kind, namespace, name)
}

// DeleteMany removes several objects of one kind from a cluster and reports
// which of them it could not. One call rather than one per row, so a selection
// is one round trip whatever its size and the refusals come back together.
func (s *ActionService) DeleteMany(contextID, kind string, refs []kube.ObjectRef) (kube.BulkReport, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.BulkReport{Failures: []kube.Failure{}}, err
	}
	return s.watcher.DeleteMany(kc, kind, refs)
}

// PatchMany applies one merge patch to several objects of one kind and reports
// which of them refused it. The patch arrives as text, JSON from the form's
// fields or YAML from its editor, and the backend reads it either way.
func (s *ActionService) PatchMany(contextID, kind string, refs []kube.ObjectRef, patch string) (kube.BulkReport, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.BulkReport{Failures: []kube.Failure{}}, err
	}
	return s.watcher.PatchMany(kc, kind, refs, patch)
}

// PreviewPatch reads a merge patch typed as YAML and answers with the JSON it
// comes to, or with what is wrong with it. Called as the form is typed in, the
// way CheckYAML is by the editor, so it touches no cluster.
func (s *ActionService) PreviewPatch(text string) kube.PatchPreview {
	return kube.MergePatchFromYAML(text)
}

// Scale sets a workload's replica count.
func (s *ActionService) Scale(contextID, kind, namespace, name string, replicas int32) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Scale(kc, kind, namespace, name, replicas)
}

// Restart rolls a workload, the way `kubectl rollout restart` does.
func (s *ActionService) Restart(contextID, kind, namespace, name string) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Restart(kc, kind, namespace, name)
}

// Suspend stops a Job or CronJob, or lets it carry on.
func (s *ActionService) Suspend(contextID, kind, namespace, name string, on bool) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Suspend(kc, kind, namespace, name, on)
}

// PauseRollout holds a Deployment's rollout, or lets it go on: `kubectl
// rollout pause` and `resume`.
func (s *ActionService) PauseRollout(contextID, namespace, name string, on bool) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.PauseRollout(kc, namespace, name, on)
}

// TriggerCronJob runs a CronJob now and returns the name of the Job it made.
func (s *ActionService) TriggerCronJob(contextID, namespace, name string) (string, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	return s.watcher.TriggerCronJob(kc, namespace, name)
}

// Evict moves one pod through the eviction API, which a disruption budget can
// refuse.
func (s *ActionService) Evict(contextID, namespace, name string) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Evict(kc, namespace, name)
}

// AnswerCSR approves or denies a certificate signing request.
func (s *ActionService) AnswerCSR(contextID, name string, approve bool) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.AnswerCSR(kc, name, approve)
}

// RolloutHistory reads a Deployment's, StatefulSet's or DaemonSet's
// revisions, newest first.
func (s *ActionService) RolloutHistory(contextID, kind, namespace, name string) ([]kube.RolloutRevision, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return []kube.RolloutRevision{}, err
	}
	return s.watcher.RolloutHistory(kc, kind, namespace, name)
}

// RollbackWorkload puts a workload back to an earlier revision: `kubectl
// rollout undo --to-revision`.
func (s *ActionService) RollbackWorkload(contextID, kind, namespace, name string, revision int64) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Rollback(kc, kind, namespace, name, revision)
}

// VMOperation runs one virtual machine lifecycle operation: start, stop,
// restart, pause, unpause, softreboot or migrate. The virtctl set, minus the
// two that are a terminal rather than a command -- see kube/kubevirtops.go for
// which mechanism each one uses and why.
func (s *ActionService) VMOperation(contextID, op, namespace, name string) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.VMOperation(kc, op, namespace, name)
}

// VMState reads which of those operations make sense right now: a stopped
// machine offers Start, a running one Stop and Pause, a paused one Unpause.
func (s *ActionService) VMState(contextID, kind, namespace, name string) (kube.VMState, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.VMState{}, err
	}
	return s.watcher.VMState(kc, kind, namespace, name)
}

// Cordon closes a node to new work, or reopens it.
func (s *ActionService) Cordon(contextID, name string, on bool) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.Cordon(kc, name, on)
}

// Drain starts moving everything off a node and returns the ID its progress
// will arrive under.
//
// It returns as soon as the drain is under way. A drain takes minutes -- it
// waits on disruption budgets, which is the point of using the eviction API --
// so it reports through events rather than making the window wait. Options
// that cannot work are refused here, before the node is touched.
func (s *ActionService) Drain(caller context.Context, contextID, node string, opts kube.DrainOptions) (string, error) {
	if err := opts.Validate(); err != nil {
		return "", err
	}
	kc, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("drain-%d", s.nextID.Add(1))
	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	s.drains[id] = cancel
	s.mu.Unlock()
	// Claimed before the drain starts reporting. A fresh ID cannot be anyone
	// else's.
	_ = s.owners.Claim(caller, id, cancel)

	go func() {
		defer s.finished(id)
		// The error is reported through the progress events, which carry it to
		// the panel that asked; there is nobody here to return it to.
		_ = s.watcher.Drain(ctx, kc, id, node, opts, s.push)
	}()

	return id, nil
}

// CancelDrain calls off a drain in flight. The node stays cordoned: it is half
// emptied, and quietly letting work back onto it is not what stopping meant.
func (s *ActionService) CancelDrain(caller context.Context, drainID string) {
	if !s.owners.Allowed(caller, drainID) {
		return
	}
	s.mu.Lock()
	cancel, found := s.drains[drainID]
	s.mu.Unlock()
	if found {
		cancel()
	}
}

// finished drops a drain's cancel once it has ended, however it ended.
func (s *ActionService) finished(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, found := s.drains[id]; found {
		cancel()
		delete(s.drains, id)
	}
	s.owners.Release(id)
}

// push forwards one drain report to the frontend. Called from the drain's own
// goroutine, so it tolerates the app being gone.
func (s *ActionService) push(progress kube.DrainProgress) {
	if app := application.Get(); app != nil {
		app.Event.Emit(DrainEvent, progress)
	}
}

func (s *ActionService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}
