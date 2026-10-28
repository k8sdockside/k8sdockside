package services

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// The keyboards typing into virtual machines' displays: one per open Console
// tab that has been typed into, held open while the tab is, so a key press is
// a message on a connection that is already there rather than a handshake.
// See kube.OpenVMKeyboard.
type vmKeyboards struct {
	mu   sync.Mutex
	open map[string]*kube.VMKeyboard
}

// OpenVMKeyboard connects a keyboard to a virtual machine's display and
// returns the handle keys are sent under.
func (s *TerminalService) OpenVMKeyboard(ctx context.Context, contextID, namespace, name string) (string, error) {
	kc, err := s.resolve(contextID)
	if err != nil {
		return "", err
	}
	// Not the call's context: the connection outlives this call by as long
	// as the tab is open.
	live, cancel := context.WithCancel(context.Background())
	kb, err := s.watcher.OpenVMKeyboard(live, kc, namespace, name)
	if err != nil {
		cancel()
		return "", err
	}

	id := fmt.Sprintf("kbd-%d", s.nextID.Add(1))
	s.keyboards.mu.Lock()
	if s.keyboards.open == nil {
		s.keyboards.open = map[string]*kube.VMKeyboard{}
	}
	s.keyboards.open[id] = kb
	s.keyboards.mu.Unlock()
	_ = s.owners.Claim(ctx, id, func() { s.closeKeyboard(id) })

	go func() {
		<-kb.Done()
		cancel()
		s.keyboards.mu.Lock()
		delete(s.keyboards.open, id)
		s.keyboards.mu.Unlock()
		s.owners.Release(id)
	}()
	return id, nil
}

// SendVMKeys types keys into the display a keyboard is connected to.
func (s *TerminalService) SendVMKeys(ctx context.Context, id string, keys []kube.VMKey) error {
	if !s.owners.Allowed(ctx, id) {
		return errors.New("no such keyboard")
	}
	s.keyboards.mu.Lock()
	kb := s.keyboards.open[id]
	s.keyboards.mu.Unlock()
	if kb == nil {
		return errors.New("the connection to the display has closed")
	}
	return kb.Send(keys)
}

// CloseVMKeyboard hangs up a keyboard.
func (s *TerminalService) CloseVMKeyboard(ctx context.Context, id string) {
	if !s.owners.Allowed(ctx, id) {
		return
	}
	s.closeKeyboard(id)
}

func (s *TerminalService) closeKeyboard(id string) {
	s.keyboards.mu.Lock()
	kb := s.keyboards.open[id]
	s.keyboards.mu.Unlock()
	if kb != nil {
		kb.Close()
	}
}

// closeKeyboards hangs up every keyboard, when the app quits.
func (s *TerminalService) closeKeyboards() {
	s.keyboards.mu.Lock()
	open := make([]*kube.VMKeyboard, 0, len(s.keyboards.open))
	for _, kb := range s.keyboards.open {
		open = append(open, kb)
	}
	s.keyboards.mu.Unlock()
	for _, kb := range open {
		kb.Close()
	}
}
