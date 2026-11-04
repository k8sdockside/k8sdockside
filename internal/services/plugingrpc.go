package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/grpccall"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
)

// gRPC calls for plugins' views, and the Services they open port-forwards to.
//
// The API server's service proxy speaks HTTP/1.1, and gRPC needs HTTP/2, so a
// gRPC service is reached the way `kubectl port-forward svc/...` reaches it:
// a tunnel to one of the Service's pods, on a loopback port of this process.
// A page polls, so the tunnel is kept between calls and closed once it has
// gone unused for a while; one that broke is opened again on the next call.

// grpcIdle is how long a tunnel is kept with no calls through it.
const grpcIdle = 2 * time.Minute

// GRPCAnswer is what a gRPC call came to, for a view.
type GRPCAnswer struct {
	// Service is the Service that answered, as namespace/name:port, and Pod
	// the pod the tunnel reached.
	Service string `json:"service"`
	Pod     string `json:"pod"`
	grpccall.Answer
}

// grpcTunnel is a port-forward kept open to a plugin's gRPC service.
type grpcTunnel struct {
	conn   *grpccall.Conn
	cancel context.CancelFunc
	// done is closed when the tunnel ends, however it ends.
	done    chan struct{}
	service string
	pod     string
	used    time.Time
}

// grpcTunnels are the tunnels open, per context, plugin and service.
type grpcTunnels struct {
	mu   sync.Mutex
	open map[string]*grpcTunnel
	// opening serialises opening one key's tunnel, so a burst of calls
	// shares one rather than racing to open several.
	opening map[string]*sync.Mutex
	stop    chan struct{}
	once    sync.Once
}

func (t *grpcTunnels) init() {
	t.once.Do(func() {
		t.open = map[string]*grpcTunnel{}
		t.opening = map[string]*sync.Mutex{}
		t.stop = make(chan struct{})
		go t.sweep()
	})
}

// sweep closes the tunnels nothing has used for a while.
func (t *grpcTunnels) sweep() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tick.C:
		}
		t.mu.Lock()
		for key, tun := range t.open {
			if time.Since(tun.used) > grpcIdle {
				tun.close()
				delete(t.open, key)
			}
		}
		t.mu.Unlock()
	}
}

func (tun *grpcTunnel) close() {
	tun.cancel()
	tun.conn.Close()
}

func (tun *grpcTunnel) alive() bool {
	select {
	case <-tun.done:
		return false
	default:
		return true
	}
}

// closeAll ends every tunnel, when the app shuts down.
func (t *grpcTunnels) closeAll() {
	t.init()
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, tun := range t.open {
		tun.close()
		delete(t.open, key)
	}
	select {
	case <-t.stop:
	default:
		close(t.stop)
	}
}

// GRPCCall makes a gRPC call, for a plugin's own view, to one of the
// in-cluster gRPC services the plugin declares, and answers with the
// messages it got back as JSON.
//
// The view names only the declared service, one of its declared methods and
// the request; where the call goes is the manifest's, and what the messages
// are is the plugin's descriptors'. timeoutSeconds and maxMessages bound how
// long a stream is read for and how much of it; zero takes the defaults.
func (s *PluginService) GRPCCall(ctx context.Context, contextID, pluginID, serviceID, method, request string, timeoutSeconds, maxMessages int) (GRPCAnswer, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return GRPCAnswer{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return GRPCAnswer{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	g, ok := plugin.GRPC(serviceID)
	if !ok {
		return GRPCAnswer{}, fmt.Errorf("the %s plugin does not declare a gRPC service %q in \"ui\": {\"grpc\": [...]}, so its views cannot call it", plugin.Name, serviceID)
	}
	if !g.Allows(method) {
		return GRPCAnswer{}, fmt.Errorf("the %s plugin may not call %s on %s -- it declares %v", plugin.Name, method, g.Label, g.Methods)
	}
	files, done, ok := plugin.UIFiles()
	if !ok {
		return GRPCAnswer{}, fmt.Errorf("the %s plugin has no ui folder to read its descriptors from", plugin.Name)
	}
	schema, err := plugins.LoadGRPCSchema(files, g)
	done()
	if err != nil {
		return GRPCAnswer{}, fmt.Errorf("%s: %w", g.Label, err)
	}
	kc, ok := s.configs.lookup(contextID)
	if !ok {
		return GRPCAnswer{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}

	call := grpccall.Request{
		Method:     method,
		Descriptor: schema.Methods[method],
		Types:      schema.Types,
		Body:       request,
		Timeout:    time.Duration(max(timeoutSeconds, 0)) * time.Second,
		Messages:   maxMessages,
	}
	key := kc.ID + "\x00" + pluginID + "\x00" + g.ID

	// Once more on a fresh tunnel if the first could not reach the service:
	// the pod it went to may have been replaced since the last call.
	for attempt := 0; ; attempt++ {
		tun, err := s.grpcTunnel(kc, key, pluginID, g)
		if err != nil {
			return GRPCAnswer{}, err
		}
		answer, err := tun.conn.Call(ctx, call)
		if err == nil {
			return GRPCAnswer{Service: tun.service, Pod: tun.pod, Answer: answer}, nil
		}
		if !errors.Is(err, grpccall.ErrTransport) || attempt > 0 {
			return GRPCAnswer{}, fmt.Errorf("%s: %w", g.Label, err)
		}
		s.dropTunnel(key, tun)
	}
}

// grpcTunnel is the open tunnel for a key, opened if there is none.
func (s *PluginService) grpcTunnel(kc kube.Context, key, pluginID string, g plugins.UIGRPC) (*grpcTunnel, error) {
	t := &s.tunnels
	t.init()

	t.mu.Lock()
	gate, ok := t.opening[key]
	if !ok {
		gate = &sync.Mutex{}
		t.opening[key] = gate
	}
	t.mu.Unlock()
	gate.Lock()
	defer gate.Unlock()

	t.mu.Lock()
	if tun, ok := t.open[key]; ok && tun.alive() {
		tun.used = time.Now()
		t.mu.Unlock()
		return tun, nil
	}
	t.mu.Unlock()

	target, port, err := s.servicePort(kc, pluginID, g.AsService())
	if err != nil {
		return nil, err
	}
	tun, err := s.openTunnel(kc, target, port)
	if err != nil {
		return nil, fmt.Errorf("could not open a port-forward to %s: %w", g.Label, err)
	}
	t.mu.Lock()
	if old, ok := t.open[key]; ok {
		old.close()
	}
	t.open[key] = tun
	t.mu.Unlock()
	return tun, nil
}

// dropTunnel closes a tunnel that failed, if it is still the one open.
func (s *PluginService) dropTunnel(key string, tun *grpcTunnel) {
	t := &s.tunnels
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open[key] == tun {
		delete(t.open, key)
	}
	tun.close()
}

// openTunnel forwards a free loopback port to a pod behind the Service, and
// waits for it to listen.
func (s *PluginService) openTunnel(kc kube.Context, target kube.ServiceTarget, port int32) (*grpcTunnel, error) {
	ctx, cancel := context.WithCancel(context.Background())
	type up struct {
		local int32
		at    kube.ForwardEndpoint
	}
	ready := make(chan up, 1)
	ended := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ended <- s.watcher.Forward(ctx, kc, kube.KindServices, target.Namespace, target.Name, port, 0,
			func(local int32, at kube.ForwardEndpoint) { ready <- up{local, at} },
			func(string) {},
		)
	}()
	select {
	case got := <-ready:
		return &grpcTunnel{
			conn:    grpccall.Dial("localhost:" + strconv.Itoa(int(got.local))),
			cancel:  cancel,
			done:    done,
			service: target.Describe(),
			pod:     got.at.Namespace + "/" + got.at.Pod,
			used:    time.Now(),
		}, nil
	case err := <-ended:
		cancel()
		if err == nil {
			err = errors.New("the forward closed before it was ready")
		}
		return nil, err
	case <-time.After(15 * time.Second):
		cancel()
		return nil, errors.New("the forward did not come up within 15s")
	}
}

// servicePort finds a declared Service and the number of its declared port,
// which is what a forward is opened on.
func (s *PluginService) servicePort(kc kube.Context, pluginID string, svc plugins.UIService) (kube.ServiceTarget, int32, error) {
	target, err := s.serviceTarget(kc, pluginID, svc)
	if err != nil {
		return kube.ServiceTarget{}, 0, err
	}
	if n, ok := svc.Port.Number(); ok {
		if n < 1 || n > 65535 {
			return kube.ServiceTarget{}, 0, fmt.Errorf("%d is not a port", n)
		}
		return target, int32(n), nil
	}
	ports, err := s.watcher.ForwardablePorts(kc, kube.KindServices, target.Namespace, target.Name)
	if err != nil {
		return kube.ServiceTarget{}, 0, fmt.Errorf("could not read %s: %w", target.Describe(), err)
	}
	for _, p := range ports {
		if p.Name == string(svc.Port) {
			return target, p.Port, nil
		}
	}
	return kube.ServiceTarget{}, 0, fmt.Errorf("%s/%s has no port named %s", target.Namespace, target.Name, svc.Port)
}

// ForwardTarget is a Service a plugin's view may open a port-forward to,
// found in the cluster: what the app's port-forward dialog is given.
type ForwardTarget struct {
	Label     string `json:"label"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Port      int    `json:"port"`
}

// PluginForwardTarget finds the Service a plugin's declared forward names,
// for the view's request to open it -- which the user confirms, and the
// app's own port-forwards then make.
func (s *PluginService) PluginForwardTarget(contextID, pluginID, forwardID string) (ForwardTarget, error) {
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return ForwardTarget{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return ForwardTarget{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	f, ok := plugin.Forward(forwardID)
	if !ok {
		return ForwardTarget{}, fmt.Errorf("the %s plugin does not declare a forward %q in \"ui\": {\"forwards\": [...]}", plugin.Name, forwardID)
	}
	kc, ok := s.configs.lookup(contextID)
	if !ok {
		return ForwardTarget{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	target, port, err := s.servicePort(kc, pluginID, f.AsService())
	if err != nil {
		return ForwardTarget{}, err
	}
	return ForwardTarget{Label: f.Label, Namespace: target.Namespace, Name: target.Name, Port: int(port)}, nil
}
