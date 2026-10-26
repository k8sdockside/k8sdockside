package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/localshell"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/session"
	"github.com/k8sdockside/k8sdockside/internal/termapp"
	"github.com/k8sdockside/k8sdockside/internal/toolcli"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// TerminalEvent is the event a session's output arrives on. One event carries
// whatever gathered since the last one; the payload names the session.
const TerminalEvent = "terminal:data"

// Registering the event gives the binding generator the payload's type, the
// same way SnapshotEvent does in resourceservice.go.
func init() {
	application.RegisterEvent[kube.TerminalChunk](TerminalEvent)
}

// How output is gathered before it crosses to the window. A terminal printing a
// large file writes far faster than a window can be told about it, and one
// event per read would do to the bridge what one event per log line would.
// Sixteen milliseconds is a frame; eight kilobytes is more than a screenful.
const (
	terminalBatchBytes = 8 << 10
	terminalBatchEvery = 16 * time.Millisecond
)

// TerminalSession is what a newly opened terminal reports back: the handle its
// output arrives under, and what it ended up attached to.
//
// The target is worth returning rather than assuming. "Shell into this
// Deployment" resolves to one of its pods, and a node shell resolves to a pod
// that did not exist a moment ago -- in both cases the window is looking at
// something the user did not name, and it should be able to say so.
type TerminalSession struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	// Node is set for a node shell, naming the machine rather than the pod that
	// is only the way into it.
	Node string `json:"node"`
	// Created names the pod a debug session made -- a copy, or a pod on a
	// node -- and Kept says it outlives the session, so the terminal can
	// offer to delete it once the session is over.
	Created string `json:"created"`
	Kept    bool   `json:"kept"`
}

// ExternalTerminals is what the settings view needs to offer the choice: the
// emulators found on this machine, and whether the thing they would run is
// actually installed.
type ExternalTerminals struct {
	Terminals []termapp.Terminal `json:"terminals"`
	// Kubectl is where kubectl is, empty when it was not found.
	Kubectl string `json:"kubectl"`
	// Reason explains an empty Kubectl in words the settings view can show.
	Reason string `json:"reason"`
}

// TerminalService opens shells: in a container, or on a node.
//
// It is its own service for the reason LogService is: a terminal has a
// lifetime. It is opened, it streams both ways, and it is closed, and the
// registry that makes closing possible has nothing to do with serving tables.
// It shares the watcher, and so the client pool, with everything else.
type TerminalService struct {
	configs *KubeconfigService
	watcher *kube.Watcher
	// store is read rather than trusting the frontend for the shell list, the
	// node image and the namespace it is created in. What a privileged pod is
	// made of is a setting the user chose, not a parameter the window passes.
	store *appconfig.Store
	// owners files each session under whoever opened it, in the web version,
	// where one user's shell must not answer another's keystrokes.
	owners *session.Owners
	// server is set in the web version, which has no terminal emulator of the
	// user's to hand a shell to.
	server bool
	// plugins answers which command line tools a plugin declares, for the
	// tool console. See OpenTool.
	plugins *PluginService
	// scratch is where a local terminal's temporary kubeconfigs are written,
	// and shells the machine the local terminal looks for shells on. Both
	// are left zero outside tests: see kubeconfigDir and shellSystem.
	scratch string
	shells  *localshell.System

	mu       sync.Mutex
	sessions map[string]*live
	nextID   atomic.Uint64
}

// NewTerminalService wires the service to the kubeconfig cache it resolves
// context IDs against, the watcher whose clients it borrows, and the settings
// it reads the terminal preferences from.
func NewTerminalService(configs *KubeconfigService, watcher *kube.Watcher, store *appconfig.Store) *TerminalService {
	return &TerminalService{
		configs:  configs,
		watcher:  watcher,
		store:    store,
		sessions: map[string]*live{},
	}
}

// ServiceShutdown closes every session when the app quits. A node shell left
// running would leave a privileged pod behind it, which is the one thing this
// feature must not do.
func (s *TerminalService) ServiceShutdown() error {
	s.mu.Lock()
	open := make([]*live, 0, len(s.sessions))
	for _, sess := range s.sessions {
		open = append(open, sess)
	}
	s.sessions = map[string]*live{}
	s.mu.Unlock()

	// Stopping a local terminal ends its process tree and removes its
	// temporary kubeconfig there and then; see OpenLocal.
	for _, sess := range open {
		sess.stop()
	}
	// A node shell deletes its pod on the way out, and that is a request to a
	// cluster rather than a channel close. Give it a moment to be made: the
	// alternative is a privileged pod outliving the app that made it.
	time.Sleep(500 * time.Millisecond)
	return nil
}

// Containers names what a terminal could attach to, for the picker above it.
// The same list a log view uses -- a container is a container.
func (s *TerminalService) Containers(contextID, kind, namespace, name string) ([]kube.ContainerRef, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return nil, err
	}
	return s.watcher.LogContainers(kc, kind, namespace, name)
}

// Open starts a shell in a container and returns the session its output will
// arrive under.
//
// An empty pod means "work out which one": a workload resolves to one of its
// running pods. An empty container means the pod's first, which is what
// `kubectl exec` picks without -c.
func (s *TerminalService) Open(ctx context.Context, contextID, kind, namespace, name, pod, container string) (TerminalSession, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return TerminalSession{}, err
	}

	target := kube.ExecTarget{Namespace: namespace, Pod: pod, Container: container}
	if target.Pod == "" || target.Container == "" {
		// Resolved here rather than inside the goroutine so that "this
		// Deployment has no running pod" is an error the button can report,
		// not a terminal that opens onto a message.
		target, err = s.watcher.ExecTarget(kc, kind, namespace, name, container)
		if err != nil {
			return TerminalSession{}, err
		}
	}

	prefs := s.store.Get().Preferences.Terminal
	id, sess := s.register(ctx)

	go func() {
		defer s.finished(id)
		err := s.watcher.Shell(sess.ctx, kc, target, prefs.Shells, sess.in, sess.batch, sess.sizes)
		s.ended(id, sess, err)
	}()

	return TerminalSession{
		ID:        id,
		Namespace: target.Namespace,
		Pod:       target.Pod,
		Container: target.Container,
	}, nil
}

// OpenNode starts a shell on a node, by way of a privileged pod created on it.
//
// It returns as soon as the session exists rather than when the pod is running:
// creating a pod and pulling an image take long enough that a window with
// nothing in it would look broken, and the terminal says what it is doing while
// it waits.
func (s *TerminalService) OpenNode(ctx context.Context, contextID, node string) (TerminalSession, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return TerminalSession{}, err
	}

	prefs := s.store.Get().Preferences.Terminal
	spec := kube.NodeShellSpec{Namespace: prefs.NodeNamespace, Image: prefs.NodeImage}
	id, sess := s.register(ctx)

	s.say(id, fmt.Sprintf(
		"Creating a privileged pod on %s from %s, in namespace %s. It is deleted when this terminal closes.",
		node, spec.Image, spec.Namespace,
	))

	go func() {
		defer s.finished(id)
		err := s.watcher.NodeShell(sess.ctx, kc, node, spec, prefs.Shells, sess.in, sess.batch, sess.sizes,
			func(target kube.ExecTarget) {
				s.say(id, fmt.Sprintf("Pod %s/%s is running. You are chrooted into the node's filesystem.",
					target.Namespace, target.Pod))
			})
		s.ended(id, sess, err)
	}()

	return TerminalSession{ID: id, Node: node, Namespace: spec.Namespace}, nil
}

// OpenDebug starts a `kubectl debug` session in the dock: an ephemeral
// container in a running pod, a copy of a pod, or a pod on a node, with a
// terminal attached to the debug container.
//
// It returns once the request has been checked and the names decided, rather
// than when the container runs, for the reason OpenNode does: pulling a debug
// image takes long enough that a window with nothing in it would look broken.
func (s *TerminalService) OpenDebug(ctx context.Context, contextID string, req kube.DebugRequest) (TerminalSession, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return TerminalSession{}, err
	}
	plan, err := s.planDebug(req)
	if err != nil {
		return TerminalSession{}, err
	}

	id, sess := s.register(ctx)
	s.say(id, describeDebug(plan))

	go func() {
		defer s.finished(id)
		err := s.watcher.Debug(sess.ctx, kc, plan, sess.in, sess.batch, sess.sizes,
			func(line string) { s.say(id, line) })
		s.ended(id, sess, err)
	}()

	out := TerminalSession{ID: id, Namespace: plan.Namespace, Pod: plan.Pod, Container: plan.Container}
	if plan.Mode == kube.DebugNode {
		out.Node = plan.Name
	}
	if plan.Creates() {
		out.Created = plan.Pod
		out.Kept = plan.Keep
	}
	return out, nil
}

// planDebug fills a request in from the settings -- the image, the profile and,
// for a node, the namespace the node shell uses -- and checks it.
func (s *TerminalService) planDebug(req kube.DebugRequest) (kube.DebugPlan, error) {
	prefs := s.store.Get().Preferences
	if req.Profile == "" {
		req.Profile = prefs.Debug.Profile
	}
	if req.Mode == kube.DebugNode && strings.TrimSpace(req.Namespace) == "" {
		req.Namespace = prefs.Terminal.NodeNamespace
	}
	plan, err := req.Plan()
	if err != nil {
		return plan, err
	}
	// The image is left empty in the plan when the request left it so: in a
	// copy, that is how changing an existing container keeps its own image.
	// A new container runs the settings' default instead.
	plan.FallbackImage = prefs.Debug.Image
	return plan, nil
}

// DebugInfo is what the launch form needs to know about a pod: its containers,
// and whether it runs as non-root, which some profiles' capabilities do not
// survive.
func (s *TerminalService) DebugInfo(contextID, namespace, name string) (kube.DebugInfo, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return kube.DebugInfo{}, err
	}
	return s.watcher.DebugInfo(kc, namespace, name)
}

// describeDebug is the first line a debug session shows: what is about to be
// made in the cluster, in so many words.
func describeDebug(plan kube.DebugPlan) string {
	image := plan.Image
	if image == "" {
		image = plan.FallbackImage
	}
	fate := "It is deleted when this terminal closes."
	if plan.Keep {
		fate = "It is kept when this terminal closes."
	}
	switch plan.Mode {
	case kube.DebugCopy:
		line := fmt.Sprintf("Copying pod %s/%s to %s, with debug container %s (profile %s). %s",
			plan.Namespace, plan.Name, plan.Pod, plan.Container, plan.Profile, fate)
		if plan.Replace {
			line += fmt.Sprintf(" The original pod %s is deleted once the copy exists.", plan.Name)
		}
		return line
	case kube.DebugNode:
		return fmt.Sprintf("Creating pod %s/%s on node %s from %s (profile %s). %s",
			plan.Namespace, plan.Pod, plan.Name, image, plan.Profile, fate)
	}
	target := ""
	if plan.Target != "" {
		target = ", sharing the processes of " + plan.Target
	}
	return fmt.Sprintf("Adding ephemeral container %s from %s to pod %s/%s (profile %s%s).",
		plan.Container, image, plan.Namespace, plan.Name, plan.Profile, target)
}

// DeleteDebugPod removes a pod a debug session made and kept. Only a pod
// carrying the debug label is removed: anything else is refused.
func (s *TerminalService) DeleteDebugPod(contextID, namespace, name string) error {
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	return s.watcher.DeleteDebugPod(kc, namespace, name)
}

// DebugPresets are the debug images offered without being added, for the
// settings page and the launch form.
func (s *TerminalService) DebugPresets() []appconfig.DebugImage {
	return appconfig.DebugPresets()
}

// LaunchDebug is OpenDebug in the user's own terminal, by way of kubectl
// debug. kubectl does not delete a copy or a node pod when it ends, so neither
// does this: Keep is what kubectl always does.
func (s *TerminalService) LaunchDebug(contextID string, req kube.DebugRequest) error {
	if s.server {
		return errDesktopOnly
	}
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	kubectl, err := termapp.Kubectl()
	if err != nil {
		return err
	}
	plan, err := s.planDebug(req)
	if err != nil {
		return err
	}
	if strings.TrimSpace(plan.Custom) != "" {
		return fmt.Errorf("a custom container spec is only supported in the app's own terminal -- kubectl reads it from a file")
	}

	prefs := s.store.Get().Preferences.Terminal
	args := append(connectArgs(kc), debugArgs(plan)...)
	title := fmt.Sprintf("debug %s — %s", plan.Name, kc.Name)
	return termapp.Launch(prefs.External, title, append([]string{kubectl}, args...))
}

// debugArgs is the kubectl debug command line for a plan. --custom is not
// here: kubectl reads it from a file, and LaunchDebug refuses a plan that has
// one rather than write the user's spec to disk.
func debugArgs(plan kube.DebugPlan) []string {
	subject := plan.Name
	if plan.Mode == kube.DebugNode {
		subject = "node/" + plan.Name
	}
	args := []string{"debug", subject}
	if plan.Attaches() {
		args = append(args, "-it")
	} else {
		args = append(args, "--attach=false")
	}
	args = append(args, "-n", plan.Namespace, "--profile="+plan.Profile)

	image := plan.Image
	if image == "" && (plan.Mode != kube.DebugCopy || plan.Container == "") {
		image = plan.FallbackImage
	}
	if image != "" {
		args = append(args, "--image="+image)
	}
	if plan.ImagePullPolicy != "" {
		args = append(args, "--image-pull-policy="+plan.ImagePullPolicy)
	}
	for _, pair := range plan.Env {
		args = append(args, "--env="+pair)
	}

	switch plan.Mode {
	case kube.DebugEphemeral:
		args = append(args, "--container="+plan.Container)
		if plan.Target != "" {
			args = append(args, "--target="+plan.Target)
		}
	case kube.DebugCopy:
		args = append(args,
			"--container="+plan.Container,
			"--copy-to="+plan.CopyTo,
			fmt.Sprintf("--share-processes=%t", plan.ShareProcesses == nil || *plan.ShareProcesses))
		if plan.SameNode {
			args = append(args, "--same-node")
		}
		if plan.Replace {
			args = append(args, "--replace")
		}
		for _, pair := range plan.SetImages {
			args = append(args, "--set-image="+strings.TrimSpace(pair))
		}
		for _, keep := range []struct {
			flag string
			on   bool
		}{
			{"--keep-labels", plan.KeepLabels},
			{"--keep-annotations", plan.KeepAnnotations},
			{"--keep-liveness", plan.KeepLiveness},
			{"--keep-readiness", plan.KeepReadiness},
			{"--keep-startup", plan.KeepStartup},
		} {
			if keep.on {
				args = append(args, keep.flag)
			}
		}
		if plan.KeepInitContainers != nil && !*plan.KeepInitContainers {
			args = append(args, "--keep-init-containers=false")
		}
	}
	if len(plan.Command) > 0 {
		if plan.ArgumentsOnly {
			args = append(args, "--arguments-only")
		}
		args = append(args, "--")
		args = append(args, plan.Command...)
	}
	return args
}

// Send passes what was typed to the shell. The data is base64 for the same
// reason the output is: a terminal carries bytes, and a paste is as likely to
// hold something that is not text as not.
func (s *TerminalService) Send(ctx context.Context, sessionID, data string) error {
	s.mu.Lock()
	sess, found := s.sessions[sessionID]
	s.mu.Unlock()
	// Someone else's session answers as a closed one would: whether it exists
	// is not this caller's to learn.
	if !found || !s.owners.Allowed(ctx, sessionID) {
		return fmt.Errorf("this terminal has closed")
	}

	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("that could not be read as input: %w", err)
	}
	_, err = sess.stdin.Write(raw)
	return err
}

// Resize tells the shell how big its window is now, so that anything drawing a
// full screen -- an editor, top, a pager -- draws it the right size.
//
// A size that nothing is waiting for is dropped rather than queued: the current
// size is the only one that matters, and a queue of stale ones would replay
// every intermediate width of a window somebody dragged.
func (s *TerminalService) Resize(ctx context.Context, sessionID string, cols, rows int) {
	if !s.owners.Allowed(ctx, sessionID) {
		return
	}
	// A terminal is measured in cells, and the protocol carries each dimension
	// in two bytes. Anything outside that did not come from a window somebody
	// is looking at, and narrowing it would send the far end a size that is not
	// the one it was told about.
	if cols <= 0 || rows <= 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return
	}

	s.mu.Lock()
	sess, found := s.sessions[sessionID]
	s.mu.Unlock()
	if !found {
		return
	}
	select {
	case sess.sizes <- kube.TerminalSize{Cols: uint16(cols), Rows: uint16(rows)}:
	default:
	}
}

// Close ends one session.
func (s *TerminalService) Close(ctx context.Context, sessionID string) {
	if !s.owners.Allowed(ctx, sessionID) {
		return
	}
	s.closeSession(sessionID)
}

// closeSession ends a session whoever asks: the window through Close, or the
// web version cleaning up behind a tab that went away without closing it.
func (s *TerminalService) closeSession(id string) {
	s.mu.Lock()
	sess, found := s.sessions[id]
	s.mu.Unlock()
	if found {
		sess.stop()
	}
}

// Externals lists the terminal emulators on this machine, for the settings
// view, together with whether kubectl -- which is what actually runs over
// there -- was found.
func (s *TerminalService) Externals() ExternalTerminals {
	if s.server {
		return ExternalTerminals{Terminals: []termapp.Terminal{}, Reason: errDesktopOnly.Error()}
	}
	out := ExternalTerminals{Terminals: termapp.Available()}
	path, err := termapp.Kubectl()
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	out.Kubectl = path
	return out
}

// Launch opens a shell in an external terminal instead of in the dock.
//
// What runs over there is kubectl: this app's connection to a cluster lives in
// this process and cannot be handed to another one, so the other terminal is
// pointed at the same kubeconfig and context and left to make its own.
func (s *TerminalService) Launch(contextID, kind, namespace, name, pod, container string) error {
	if s.server {
		return errDesktopOnly
	}
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	kubectl, err := termapp.Kubectl()
	if err != nil {
		return err
	}

	target := kube.ExecTarget{Namespace: namespace, Pod: pod, Container: container}
	if target.Pod == "" || target.Container == "" {
		target, err = s.watcher.ExecTarget(kc, kind, namespace, name, container)
		if err != nil {
			return err
		}
	}

	prefs := s.store.Get().Preferences.Terminal
	args := append(connectArgs(kc),
		"exec", "-it", "-n", target.Namespace, target.Pod, "-c", target.Container, "--")
	args = append(args, shellChain(prefs.Shells)...)

	title := fmt.Sprintf("%s/%s — %s", target.Namespace, target.Pod, kc.Name)
	return termapp.Launch(prefs.External, title, append([]string{kubectl}, args...))
}

// LaunchNode is Launch for a node, which kubectl reaches with `debug node/…`
// -- the same privileged pod this app would create, made by kubectl instead so
// that the pod belongs to the process that will clean it up.
func (s *TerminalService) LaunchNode(contextID, node string) error {
	if s.server {
		return errDesktopOnly
	}
	kc, err := s.resolve(contextID)
	if err != nil {
		return err
	}
	kubectl, err := termapp.Kubectl()
	if err != nil {
		return err
	}

	prefs := s.store.Get().Preferences.Terminal
	args := append(connectArgs(kc),
		"debug", "node/"+node, "-it",
		"--image="+prefs.NodeImage,
		"-n", prefs.NodeNamespace,
		"--")
	args = append(args, "chroot", "/host")
	args = append(args, shellChain(prefs.Shells)...)

	return termapp.Launch(prefs.External, node+" — "+kc.Name, append([]string{kubectl}, args...))
}

// OpenTool opens a console in the dock for one of a plugin's command line
// tools: a terminal that runs that tool's commands for this cluster, already
// given the files the user chose for it. defaults are flag and value pairs
// added to a command that does not give the flag; initial, when given, is a
// run command the user has just confirmed, run first. See toolcli.Console.
func (s *TerminalService) OpenTool(ctx context.Context, contextID, pluginID, toolID string, defaults, initial []string) (TerminalSession, error) {
	return s.openTool(ctx, contextID, pluginID, toolID, defaults, initial, plugins.ToolRun)
}

// OpenToolInteractive is LaunchTool in the dock: one of a plugin tool's
// interactive commands -- one that draws a full screen, like `talosctl
// dashboard` -- run first in a tool console, behind a real pseudo-terminal.
// The command is checked against the tool's interactive allow-list exactly as
// LaunchTool checks it.
func (s *TerminalService) OpenToolInteractive(ctx context.Context, contextID, pluginID, toolID string, args []string) (TerminalSession, error) {
	return s.openTool(ctx, contextID, pluginID, toolID, nil, args, plugins.ToolInteractive)
}

// openTool opens a tool console; initial, when given, must be allowed as the
// given kind of command.
func (s *TerminalService) openTool(ctx context.Context, contextID, pluginID, toolID string, defaults, initial []string, kind string) (TerminalSession, error) {
	if s.server || s.plugins == nil {
		return TerminalSession{}, errDesktopOnly
	}
	inv, err := s.plugins.prepareTool(contextID, pluginID, toolID, true)
	if err != nil {
		return TerminalSession{}, err
	}
	if err := checkDefaults(inv.tool, defaults); err != nil {
		return TerminalSession{}, err
	}
	if len(initial) > 0 {
		if err := inv.tool.Allows(kind, initial); err != nil {
			return TerminalSession{}, err
		}
	}

	id, sess := s.register(ctx)
	console := &toolcli.Console{
		Path:     inv.status.Tool.Path,
		Name:     inv.tool.Command,
		Always:   inv.always,
		Defaults: defaults,
		Env:      inv.env,
		Banner: strings.TrimSpace(fmt.Sprintf("%s %s — %s · type ? for help, exit to close",
			inv.tool.Label, inv.status.Tool.Version, strings.Join(inv.always, " "))),
	}
	// The window's size, for the commands the console runs behind a
	// pseudo-terminal.
	go func() {
		for {
			select {
			case <-sess.ctx.Done():
				return
			case size := <-sess.sizes:
				console.Resize(size.Cols, size.Rows)
			}
		}
	}()
	go func() {
		defer s.finished(id)
		err := console.Run(sess.ctx, sess.in, sess.batch, initial)
		s.ended(id, sess, err)
	}()
	return TerminalSession{ID: id}, nil
}

// LaunchTool opens one of a plugin tool's interactive commands -- one that
// draws a full screen -- in the user's own terminal.
func (s *TerminalService) LaunchTool(contextID, pluginID, toolID string, args []string) error {
	if s.server || s.plugins == nil {
		return errDesktopOnly
	}
	inv, err := s.plugins.prepareTool(contextID, pluginID, toolID, true)
	if err != nil {
		return err
	}
	if err := inv.tool.Allows(plugins.ToolInteractive, args); err != nil {
		return err
	}
	command := append([]string{inv.status.Tool.Path}, args...)
	command = append(command, inv.always...)
	prefs := s.store.Get().Preferences.Terminal
	return termapp.Launch(prefs.External, inv.tool.Label, command)
}

// connectArgs point kubectl at exactly the context this window is showing.
// Explicitly, rather than relying on whatever the current-context happens to
// be: the app's whole premise is that several clusters are open at once, and a
// terminal that opened on the wrong one would be the worst kind of bug.
func connectArgs(kc kube.Context) []string {
	return []string{"--kubeconfig", kc.File, "--context", kc.Name}
}

// shellChain builds a command that tries each shell in turn.
//
// The terminal in the dock can attempt one shell, see it fail and try the next,
// because it stays in the conversation. An external kubectl gets one command,
// so the trying has to happen inside the container -- hence a loop, run by the
// last candidate in the list, which is the one most likely to exist (and is
// `sh` unless the user has said otherwise).
func shellChain(shells []string) []string {
	if len(shells) == 0 {
		shells = kube.DefaultShells
	}
	names := make([]string, 0, len(shells))
	for _, shell := range shells {
		names = append(names, "'"+strings.ReplaceAll(shell, "'", `'\''`)+"'")
	}
	script := fmt.Sprintf(
		`for s in %s; do command -v "$s" >/dev/null 2>&1 && exec "$s"; done; echo 'none of these shells are in this container: %s'; exit 127`,
		strings.Join(names, " "), strings.Join(shells, ", "),
	)
	return []string{shells[len(shells)-1], "-c", script}
}

// ---- sessions --------------------------------------------------------------

// live is one open terminal: the context that ends it, the pipe the window's
// keystrokes are written into, the channel its size changes go down, and the
// batcher its output comes back through.
type live struct {
	ctx    context.Context
	cancel context.CancelFunc
	// in is the shell's stdin, and stdin the end this process writes to. One
	// pipe, held from both sides: keystrokes arrive as calls from the window.
	in    io.Reader
	stdin *io.PipeWriter
	sizes chan kube.TerminalSize
	batch *chunker
	// once guards the shutdown, which both the window (Close) and the stream
	// ending on its own can reach.
	once sync.Once

	// cleanups run when the session stops, however it stops: a local
	// terminal's process and its temporary kubeconfig are released here, so
	// that closing the tab and quitting the app do it synchronously rather
	// than by way of a goroutine that may not get the chance.
	mu       sync.Mutex
	stopped  bool
	cleanups []func()
}

// stop ends a session. Cancelling the context hangs up on the cluster; closing
// the pipe unblocks the copy that is reading from it.
func (l *live) stop() {
	l.once.Do(func() {
		l.cancel()
		_ = l.stdin.Close()
		l.mu.Lock()
		l.stopped = true
		cleanups := l.cleanups
		l.cleanups = nil
		l.mu.Unlock()
		for _, f := range cleanups {
			f()
		}
	})
}

// atStop runs f when the session stops, or now if it already has.
func (l *live) atStop(f func()) {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		f()
		return
	}
	l.cleanups = append(l.cleanups, f)
	l.mu.Unlock()
}

// register makes a session and files it under a new id, claimed for the caller
// before anything can be written to it.
func (s *TerminalService) register(caller context.Context) (string, *live) {
	id := fmt.Sprintf("term-%d", s.nextID.Add(1))
	// A fresh ID cannot be anyone else's, so there is no error to report.
	_ = s.owners.Claim(caller, id, func() { s.closeSession(id) })
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	sizes := make(chan kube.TerminalSize, 1)

	batch := newChunker(func(data []byte) {
		s.push(kube.TerminalChunk{SessionID: id, Data: base64.StdEncoding.EncodeToString(data)})
	})
	go batch.run(ctx)

	sess := &live{
		ctx:    ctx,
		cancel: cancel,
		in:     reader,
		stdin:  writer,
		sizes:  sizes,
		batch:  batch,
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return id, sess
}

// ended reports how a session finished. Whether it was closed or broke, the
// window is told: a terminal that simply stops is indistinguishable from a
// shell waiting for input.
func (s *TerminalService) ended(id string, sess *live, err error) {
	sess.batch.flush()
	done := kube.TerminalChunk{SessionID: id, Done: true}
	if err != nil && sess.ctx.Err() == nil {
		done.Error = err.Error()
	}
	sess.stop()
	s.push(done)
}

// say writes a line of the app's own words into a session, marked apart from
// the shell's output so it cannot be mistaken for it.
func (s *TerminalService) say(id, text string) {
	s.push(kube.TerminalChunk{
		SessionID: id,
		Data:      base64.StdEncoding.EncodeToString([]byte("\x1b[2m" + text + "\x1b[0m\r\n")),
	})
}

// finished drops a session once it has ended, however it ended.
func (s *TerminalService) finished(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, found := s.sessions[id]; found {
		sess.stop()
		delete(s.sessions, id)
	}
	s.owners.Release(id)
}

func (s *TerminalService) push(chunk kube.TerminalChunk) {
	if app := application.Get(); app != nil {
		app.Event.Emit(TerminalEvent, chunk)
	}
}

func (s *TerminalService) resolve(contextID string) (kube.Context, error) {
	ctx, ok := s.configs.lookup(contextID)
	if !ok {
		return kube.Context{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}
	return ctx, nil
}

// ---- batching --------------------------------------------------------------

// chunker gathers a shell's output and hands it over in groups: when there is a
// screenful of it, or on a tick, whichever comes first. The same arrangement
// the log batcher uses, and for the same reason -- except that here the unit is
// bytes rather than lines, because a terminal has no lines: it has a stream in
// which a newline is just another byte.
type chunker struct {
	emit func([]byte)

	mu  sync.Mutex
	buf []byte
}

func newChunker(emit func([]byte)) *chunker {
	return &chunker{emit: emit}
}

func (c *chunker) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.buf = append(c.buf, p...)
	full := len(c.buf) >= terminalBatchBytes
	c.mu.Unlock()

	if full {
		c.flush()
	}
	return len(p), nil
}

func (c *chunker) flush() {
	c.mu.Lock()
	data := c.buf
	c.buf = nil
	c.mu.Unlock()

	if len(data) > 0 {
		c.emit(data)
	}
}

func (c *chunker) run(ctx context.Context) {
	tick := time.NewTicker(terminalBatchEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			c.flush()
			return
		case <-tick.C:
			c.flush()
		}
	}
}
