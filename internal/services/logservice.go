package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// LogEvent is the event log lines arrive on. One event carries the lines that
// gathered since the last one; the payload names which stream they belong to.
const LogEvent = "pod:logs"

// Registering the event gives the binding generator the payload's type, the
// same way SnapshotEvent does in resourceservice.go.
func init() {
	application.RegisterEvent[kube.LogBatch](LogEvent)
}

// LogService follows the logs of a pod, or of every pod a workload has.
//
// Its own service rather than part of ResourceService because a log view has a
// lifetime: it is opened, it streams, and it is closed, and the registry that
// makes closing possible has nothing to do with serving tables. It shares the
// watcher, and so the client pool, with everything else.
type LogService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	// owners files each stream under whoever opened it, in the web version.
	owners *session.Owners

	mu      sync.Mutex
	streams map[string]context.CancelFunc
	nextID  atomic.Uint64
}

// NewLogService wires the service to the kubeconfig cache it resolves context
// IDs against, and to the watcher whose clients it borrows.
func NewLogService(configs *KubeconfigService, watcher *kube.Watcher) *LogService {
	return &LogService{
		configs: configs,
		watcher: watcher,
		streams: map[string]context.CancelFunc{},
	}
}

// ServiceShutdown stops every stream when the app quits, so no goroutine is
// left reading from a cluster on behalf of a window that has gone.
func (s *LogService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.streams {
		cancel()
		delete(s.streams, id)
	}
	return nil
}

// Containers names what a view could follow, for the picker above it.
func (s *LogService) Containers(contextID, kind, namespace, name string) ([]kube.ContainerRef, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return nil, err
	}
	return s.watcher.LogContainers(kc, kind, namespace, name)
}

// Open starts following an object's logs and returns the ID its lines will
// arrive under. An empty `containers` follows all of them.
//
// It returns as soon as the streams are opening. Lines arrive as events,
// because a log view is a thing that goes on happening rather than an answer.
func (s *LogService) Open(caller context.Context, contextID, kind, namespace, name string, containers []string, follow bool) (string, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("logs-%d", s.nextID.Add(1))
	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	s.streams[id] = cancel
	s.mu.Unlock()
	// Claimed before the stream starts, so its first lines have an owner to go
	// to. A fresh ID cannot be anyone else's.
	_ = s.owners.Claim(caller, id, cancel)

	go func() {
		defer s.finished(id)
		err := s.watcher.Logs(ctx, kc, kind, namespace, name, containers, follow, func(lines []kube.LogLine) {
			s.push(kube.LogBatch{StreamID: id, Lines: lines})
		})

		// Whether it ended because it was closed or because it broke, the view
		// is told: a log pane that simply stops is indistinguishable from a
		// container that has gone quiet.
		done := kube.LogBatch{StreamID: id, Lines: []kube.LogLine{}, Done: true}
		if err != nil && ctx.Err() == nil {
			done.Error = err.Error()
		}
		s.push(done)
	}()

	return id, nil
}

// Close stops one view's streams.
func (s *LogService) Close(caller context.Context, streamID string) {
	if !s.owners.Allowed(caller, streamID) {
		return
	}
	s.mu.Lock()
	cancel, found := s.streams[streamID]
	s.mu.Unlock()
	if found {
		cancel()
	}
}

// finished drops a stream's cancel once it has ended, however it ended.
func (s *LogService) finished(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, found := s.streams[id]; found {
		cancel()
		delete(s.streams, id)
	}
	s.owners.Release(id)
}

// push forwards one batch to the frontend. Called from the streams' own
// goroutines, so it tolerates the app being gone.
func (s *LogService) push(batch kube.LogBatch) {
	if app := application.Get(); app != nil {
		app.Event.Emit(LogEvent, batch)
	}
}

func (s *LogService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}
