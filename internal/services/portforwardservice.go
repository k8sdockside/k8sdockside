package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ForwardEvent is the event a forward's state arrives on. One event is one
// report about one forward; the payload is the whole record, so the frontend
// replaces a row rather than patching it.
const ForwardEvent = "portforward:changed"

func init() {
	application.RegisterEvent[Forward](ForwardEvent)
}

// The states a forward can be in. A forward that has never come up and one that
// was deliberately stopped are the same state -- disconnected, with a button --
// because they call for the same thing from the user.
const (
	ForwardConnecting = "connecting"
	ForwardActive     = "active"
	ForwardStopped    = "stopped"
	ForwardFailed     = "error"
)

// forwardStartTimeout bounds how long Start waits for the tunnel to come up
// before reporting that it has not. Long enough for a slow API server and an
// exec credential plugin, short enough that a button does not appear stuck.
const forwardStartTimeout = 30 * time.Second

// Forward is one tunnel as the window sees it: what was asked for, what it is
// doing, and what it actually reached.
type Forward struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// RemotePort is what the user chose -- a service port on a Service, a
	// container port on a pod.
	RemotePort int `json:"remotePort"`
	// LocalPort is the port on this machine. Zero until it has come up once.
	LocalPort int `json:"localPort"`
	// Random records that the user asked for any free port. It is why falling
	// back to a different one is acceptable when the remembered port is taken.
	Random bool `json:"random"`
	// Browser records that this forward opens a browser when it comes up.
	Browser bool   `json:"browser"`
	State   string `json:"state"`
	Error   string `json:"error"`
	// Pod and PodPort are what the tunnel reached, which for a Service is
	// neither of the things the user named.
	Pod     string `json:"pod"`
	PodPort int    `json:"podPort"`
	// Note is the last thing the forwarder itself said, which is where a
	// connection that dropped mid-session explains itself.
	Note string `json:"note"`
}

// tunnel is one forward and the handle that stops it. A stopped forward is
// still a tunnel: the record outlives the connection, which is what lets a
// disconnected row offer to reconnect rather than having to be built again.
type tunnel struct {
	record Forward
	cancel context.CancelFunc
}

// PortForwardService opens local ports into the cluster and keeps the list of
// them.
//
// It shares the watcher, and so the client pool, with everything else: a
// forward to a cluster already showing in a tab costs no second connection and
// no second credential exec.
type PortForwardService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	store   *appconfig.Store

	mu       sync.Mutex
	forwards map[string]*tunnel
	// order keeps the list in the order forwards were created, so a row does
	// not move under the pointer because its state changed.
	order  []string
	nextID atomic.Uint64
	// server is set in the web version, where a forward would listen on the
	// pod's localhost -- somewhere no user's browser can reach.
	server bool
}

// NewPortForwardService wires the service up and restores the forwards from
// last time -- as records, not as connections. Nothing dials a cluster here:
// launching the app must not open every tunnel in this list, including the ones
// to clusters behind a VPN nobody is on yet.
func NewPortForwardService(configs *KubeconfigService, watcher *kube.Watcher, store *appconfig.Store) *PortForwardService {
	s := &PortForwardService{
		configs:  configs,
		watcher:  watcher,
		store:    store,
		forwards: map[string]*tunnel{},
	}

	var highest uint64
	for _, saved := range store.PortForwards() {
		record := Forward{
			ID:         saved.ID,
			ContextID:  saved.ContextID,
			Kind:       saved.Kind,
			Namespace:  saved.Namespace,
			Name:       saved.Name,
			RemotePort: saved.RemotePort,
			LocalPort:  saved.LocalPort,
			Random:     saved.Random,
			Browser:    saved.Browser,
			State:      ForwardStopped,
		}
		s.forwards[record.ID] = &tunnel{record: record}
		s.order = append(s.order, record.ID)

		// New ids carry on from the highest saved one, so a reconnect after a
		// restart is the row it was rather than a second row beside it.
		if n, err := strconv.ParseUint(strings.TrimPrefix(record.ID, "pf-"), 10, 64); err == nil && n > highest {
			highest = n
		}
	}
	s.nextID.Store(highest)
	return s
}

// ServiceShutdown closes every tunnel when the app quits. The records stay on
// disk; what goes is the listener on this machine, which would otherwise be
// held by a process that has gone.
func (s *PortForwardService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.forwards {
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
	}
	return nil
}

// List is every forward, live and disconnected alike, in the order they were
// made.
func (s *PortForwardService) List() []Forward {
	if s.server {
		return []Forward{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Forward, 0, len(s.order))
	for _, id := range s.order {
		if t, found := s.forwards[id]; found {
			out = append(out, t.record)
		}
	}
	return out
}

// Ports names what could be forwarded from one object, for the picker.
func (s *PortForwardService) Ports(contextID, kind, namespace, name string) ([]kube.PortOption, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return nil, err
	}
	return s.watcher.ForwardablePorts(kc, kind, namespace, name)
}

// Start opens a new forward and waits for it to come up.
//
// A local port of 0 means "any free one", which is the default the dialog
// offers: a forward is normally reached by clicking the link beside it, and
// choosing the port by hand only matters when something else already expects
// one.
//
// It waits rather than returning immediately because the answer -- which port
// you got -- is the whole point, and a browser cannot be opened on a port that
// has not been decided yet.
func (s *PortForwardService) Start(contextID, kind, namespace, name string, remotePort, localPort int, browser bool) (Forward, error) {
	if s.server {
		return Forward{}, errDesktopOnly
	}
	if _, err := s.resolve(contextID); err != nil {
		return Forward{}, err
	}
	if _, ok := asPort(remotePort, false); !ok {
		return Forward{}, fmt.Errorf("%d is not a port", remotePort)
	}
	// Zero is allowed for the local end alone, where it means "any free one".
	if _, ok := asPort(localPort, true); !ok {
		return Forward{}, fmt.Errorf("%d is not a port", localPort)
	}

	record := Forward{
		ID:         fmt.Sprintf("pf-%d", s.nextID.Add(1)),
		ContextID:  contextID,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		RemotePort: remotePort,
		LocalPort:  localPort,
		Random:     localPort == 0,
		Browser:    browser,
		State:      ForwardStopped,
	}

	s.mu.Lock()
	s.forwards[record.ID] = &tunnel{record: record}
	s.order = append(s.order, record.ID)
	s.mu.Unlock()
	s.save()

	return s.run(record.ID)
}

// Reconnect opens a forward that is not currently up. A forward remembered from
// a previous session is exactly this: a request with no connection under it.
func (s *PortForwardService) Reconnect(id string) (Forward, error) {
	if s.server {
		return Forward{}, errDesktopOnly
	}
	s.mu.Lock()
	t, found := s.forwards[id]
	if found && t.cancel != nil {
		s.mu.Unlock()
		return t.record, nil
	}
	s.mu.Unlock()

	if !found {
		return Forward{}, fmt.Errorf("that forward is no longer in the list")
	}
	return s.run(id)
}

// run brings one forward up, and answers with what it did.
//
// The remembered port is tried first even when it was originally given at
// random, so that a forward you bookmarked comes back on the port you
// bookmarked. If it is taken and the port was never chosen by hand, it falls
// back to another free one rather than failing -- which is the behaviour that
// matches what was actually asked for: "any port".
func (s *PortForwardService) run(id string) (Forward, error) {
	s.mu.Lock()
	t, found := s.forwards[id]
	if !found {
		s.mu.Unlock()
		return Forward{}, fmt.Errorf("that forward is no longer in the list")
	}
	record := t.record
	s.mu.Unlock()

	kc, err := s.resolve(record.ContextID)
	if err != nil {
		s.failed(id, err)
		return s.recordOf(id), err
	}

	// Checked here as well as in Start, because a record can also arrive from
	// the settings file, which is a file somebody can edit. A number that is
	// not a port is refused rather than narrowed into one that is.
	local, ok := asPort(record.LocalPort, true)
	if !ok {
		err := fmt.Errorf("%d is not a port", record.LocalPort)
		s.failed(id, err)
		return s.recordOf(id), err
	}
	if _, ok := asPort(record.RemotePort, false); !ok {
		err := fmt.Errorf("%d is not a port", record.RemotePort)
		s.failed(id, err)
		return s.recordOf(id), err
	}

	err = s.attempt(id, kc, local)
	if err != nil && record.Random && local != 0 && portTaken(err) {
		s.note(id, fmt.Sprintf("port %d is in use, taking another", record.LocalPort))
		err = s.attempt(id, kc, 0)
	}
	if err != nil {
		s.failed(id, err)
		return s.recordOf(id), err
	}
	return s.recordOf(id), nil
}

// attempt runs one forward until it is up, and leaves it running.
//
// The goroutine outlives this function: what it waits for is the tunnel
// becoming usable, and what the goroutine waits for is the tunnel ending, which
// may be hours later.
func (s *PortForwardService) attempt(id string, kc kube.Context, want int32) error {
	s.mu.Lock()
	t, found := s.forwards[id]
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("that forward is no longer in the list")
	}
	record := t.record
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.record.State = ForwardConnecting
	t.record.Error = ""
	s.mu.Unlock()
	s.emit(id)

	up := make(chan struct{})
	ended := make(chan error, 1)

	go func() {
		remote, _ := asPort(record.RemotePort, false)
		err := s.watcher.Forward(ctx, kc,
			record.Kind, record.Namespace, record.Name,
			remote, want,
			func(local int32, at kube.ForwardEndpoint) {
				s.up(id, int(local), at)
				close(up)
			},
			func(text string) { s.note(id, text) },
		)
		// However it ended, the row has to stop saying "active": a tunnel whose
		// pod was deleted goes quiet, and a row that still claimed to be
		// forwarding would be a lie that a browser tab would then confirm.
		s.down(ctx, id, err)
		ended <- err
	}()

	select {
	case <-up:
		return nil
	case err := <-ended:
		if err == nil {
			err = fmt.Errorf("the forward closed before it was ready")
		}
		return err
	case <-time.After(forwardStartTimeout):
		cancel()
		// Wait for the goroutine to record that the tunnel stopped before
		// reporting why it never started. Both write the row's state, and the
		// one worth keeping is the one that says what went wrong -- so it has
		// to be written second.
		select {
		case <-ended:
		case <-time.After(time.Second):
		}
		return fmt.Errorf("the forward did not come up within %s", forwardStartTimeout)
	}
}

// Stop closes a forward's tunnel and leaves the row where it is.
func (s *PortForwardService) Stop(id string) {
	s.mu.Lock()
	t, found := s.forwards[id]
	var cancel context.CancelFunc
	if found {
		cancel = t.cancel
		t.cancel = nil
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// Forget stops a forward and drops it from the list for good.
func (s *PortForwardService) Forget(id string) {
	s.Stop(id)

	s.mu.Lock()
	delete(s.forwards, id)
	for i, held := range s.order {
		if held == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.save()
}

// URL is where a live forward can be reached, empty when it is not up.
//
// https for the ports that conventionally mean it. A guess, but the useful kind:
// a link that opens on http where the far end speaks TLS produces a blank tab
// and no explanation.
func (s *PortForwardService) URL(id string) string {
	record := s.recordOf(id)
	if record.State != ForwardActive || record.LocalPort == 0 {
		return ""
	}
	scheme := "http"
	switch record.RemotePort {
	case 443, 8443, 6443, 9443:
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, record.LocalPort)
}

// Open sends a live forward to the browser.
//
// The URL is built here from what the service knows rather than taken from the
// window, so that nothing the webview says can decide what gets opened.
func (s *PortForwardService) Open(id string) error {
	if s.server {
		return errDesktopOnly
	}
	url := s.URL(id)
	if url == "" {
		return fmt.Errorf("that forward is not connected")
	}
	app := application.Get()
	if app == nil {
		return fmt.Errorf("the application is closing")
	}
	return app.Browser.OpenURL(url)
}

// ---- state -----------------------------------------------------------------

// up records that a tunnel is listening, and on what.
func (s *PortForwardService) up(id string, local int, at kube.ForwardEndpoint) {
	s.mu.Lock()
	if t, found := s.forwards[id]; found {
		t.record.State = ForwardActive
		t.record.LocalPort = local
		t.record.Pod = at.Pod
		t.record.PodPort = int(at.Port)
		t.record.Error = ""
	}
	s.mu.Unlock()
	// The port it came up on is worth remembering even when it was chosen at
	// random: it is what the next reconnect will ask for.
	s.save()
	s.emit(id)
}

// down records that a tunnel has ended, and whether that was asked for.
func (s *PortForwardService) down(ctx context.Context, id string, err error) {
	s.mu.Lock()
	t, found := s.forwards[id]
	if found {
		t.cancel = nil
		switch {
		case ctx.Err() != nil:
			t.record.State = ForwardStopped
			t.record.Error = ""
		case err != nil:
			t.record.State = ForwardFailed
			t.record.Error = err.Error()
		default:
			t.record.State = ForwardStopped
		}
		t.record.Pod = ""
		t.record.PodPort = 0
	}
	s.mu.Unlock()

	if found {
		s.emit(id)
	}
}

// failed records a forward that could not be opened at all.
func (s *PortForwardService) failed(id string, err error) {
	s.mu.Lock()
	if t, found := s.forwards[id]; found {
		t.record.State = ForwardFailed
		t.record.Error = err.Error()
	}
	s.mu.Unlock()
	s.emit(id)
}

// note keeps the last thing the forwarder said about a tunnel.
func (s *PortForwardService) note(id, text string) {
	s.mu.Lock()
	if t, found := s.forwards[id]; found {
		t.record.Note = text
	}
	s.mu.Unlock()
	s.emit(id)
}

func (s *PortForwardService) recordOf(id string) Forward {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, found := s.forwards[id]; found {
		return t.record
	}
	return Forward{}
}

func (s *PortForwardService) emit(id string) {
	record := s.recordOf(id)
	if record.ID == "" {
		return
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(ForwardEvent, record)
	}
}

// save writes the list to the settings file. What is written is the request --
// which object, which port, whether the port was chosen -- because nothing
// about a live connection survives a restart.
func (s *PortForwardService) save() {
	s.mu.Lock()
	out := make([]appconfig.PortForward, 0, len(s.order))
	for _, id := range s.order {
		t, found := s.forwards[id]
		if !found {
			continue
		}
		out = append(out, appconfig.PortForward{
			ID:         t.record.ID,
			ContextID:  t.record.ContextID,
			Kind:       t.record.Kind,
			Namespace:  t.record.Namespace,
			Name:       t.record.Name,
			RemotePort: t.record.RemotePort,
			LocalPort:  t.record.LocalPort,
			Random:     t.record.Random,
			Browser:    t.record.Browser,
		})
	}
	s.mu.Unlock()

	// A settings file that cannot be written is reported where the user can act
	// on it -- by the settings view, which owns that conversation. Here it must
	// not take a working tunnel down with it.
	_, _ = s.store.SetPortForwards(out)
}

// asPort narrows a port to what the wire carries, refusing anything that is not
// one. `free` allows zero, which is how "any free port" is asked for.
//
// The check is the point rather than the conversion: a port is two bytes, and a
// number that could not fit in them -- from a hand-edited settings file, or
// from a window that has been tampered with -- must be refused rather than
// truncated into a port that means something else.
func asPort(n int, free bool) (int32, bool) {
	if n > 65535 || n < 0 || (n == 0 && !free) {
		return 0, false
	}
	return int32(n), true
}

// portTaken reports whether a forward failed because something else holds the
// port. client-go's listener says so in prose, and the wording differs between
// platforms, so both the Go error and the Windows one are matched.
func portTaken(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "address already in use") ||
		strings.Contains(text, "unable to listen on any of the requested ports") ||
		strings.Contains(text, "only one usage of each socket address")
}

func (s *PortForwardService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}
