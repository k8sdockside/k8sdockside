package session

import (
	"context"
	"sync"
	"time"
)

// releaseGrace is how long a finished stream's ID stays filed under its owner.
// A stream's last event -- the one saying it has ended -- is emitted just
// before the stream is released, and is delivered afterwards; forgetting the
// owner at once would leave that event with nobody to go to.
const releaseGrace = 30 * time.Second

// Owners remembers which user, and which browser tab, opened each stream: every
// terminal, log view, table subscription, drain and search. The gateway asks it
// who an event belongs to before passing the event on, and the services ask it
// whether the caller may touch a stream before they write to it or close it.
//
// A nil *Owners is the desktop app's, and every method is a no-op on it that
// answers as if the caller owned everything.
type Owners struct {
	mu     sync.Mutex
	claims map[string]*claim
	grace  time.Duration
}

type claim struct {
	user   string
	client string
	// cleanup closes the stream. It is run when the tab that opened the stream
	// goes away without closing it, and cleared once the stream has ended.
	cleanup func()
	// released is set once the stream has ended; the claim is kept for the
	// grace period so the stream's last events still find their owner.
	released *time.Timer
}

// NewOwners returns an empty registry.
func NewOwners() *Owners {
	return &Owners{claims: map[string]*claim{}, grace: releaseGrace}
}

// Claim files id under the user and browser tab a call is being made for,
// with the function that closes the stream should that tab go away first.
//
// A call made for nobody claims nothing: the desktop has nobody to keep apart.
// Claiming an ID another user holds fails, which only an ID the caller chose
// can do -- every other ID is minted fresh by the service.
func (o *Owners) Claim(ctx context.Context, id string, cleanup func()) error {
	if o == nil {
		return nil
	}
	u, ok := FromContext(ctx)
	if !ok {
		return nil
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if held, found := o.claims[id]; found {
		if held.user != u.ID {
			return ErrTaken
		}
		if held.released != nil {
			held.released.Stop()
		}
	}
	o.claims[id] = &claim{user: u.ID, client: Client(ctx), cleanup: cleanup}
	return nil
}

// Allowed reports whether the caller may act on id. A call made for nobody may;
// a user may act only on what that user claimed. An ID nobody holds is
// refused: it is either finished or was never this user's.
func (o *Owners) Allowed(ctx context.Context, id string) bool {
	if o == nil {
		return true
	}
	u, ok := FromContext(ctx)
	if !ok {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	held, found := o.claims[id]
	return found && held.user == u.ID
}

// Release records that a stream has ended, and forgets its owner after the
// grace period. Releasing an ID twice, or one never claimed, does nothing.
func (o *Owners) Release(id string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	held, found := o.claims[id]
	if !found || held.released != nil {
		return
	}
	held.cleanup = nil
	held.released = time.AfterFunc(o.grace, func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.claims[id] == held {
			delete(o.claims, id)
		}
	})
}

// UserOf returns the ID of the user holding id.
func (o *Owners) UserOf(id string) (string, bool) {
	if o == nil {
		return "", false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	held, found := o.claims[id]
	if !found {
		return "", false
	}
	return held.user, true
}

// AbandonClient closes every stream a browser tab opened and has not closed.
// The gateway calls it once a tab's event socket has been gone long enough
// that it is not coming back: the page was closed or reloaded, and whatever it
// had open -- a node shell's privileged pod above all -- must not outlive it.
func (o *Owners) AbandonClient(client string) {
	if client == "" {
		return
	}
	o.abandon(func(c *claim) bool { return c.client == client })
}

// AbandonUser closes every stream a user has open, in every tab: for a user who
// has signed out, been removed, or has no tab left connected.
func (o *Owners) AbandonUser(user string) {
	if user == "" {
		return
	}
	o.abandon(func(c *claim) bool { return c.user == user })
}

// abandon runs the cleanup of every live claim that matches. The cleanups run
// outside the lock: each one closes a stream, and a closing stream releases
// its claim, which takes the lock again.
func (o *Owners) abandon(match func(*claim) bool) {
	if o == nil {
		return
	}
	var cleanups []func()
	o.mu.Lock()
	for _, c := range o.claims {
		if c.released == nil && c.cleanup != nil && match(c) {
			cleanups = append(cleanups, c.cleanup)
			c.cleanup = nil
		}
	}
	o.mu.Unlock()
	for _, cleanup := range cleanups {
		cleanup()
	}
}
