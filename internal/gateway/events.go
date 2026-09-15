package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rogerwesterbo/k8sdockside/internal/session"
)

const (
	// maxEventBytes bounds one event from the app. A snapshot of a big table
	// is the largest thing the app sends, and it runs to megabytes, not to
	// this.
	maxEventBytes = 128 << 20
	// clientGrace is how long a tab's streams outlive its event socket. The
	// runtime reconnects a dropped socket within a second or two; a tab that
	// has not come back after this has been closed or reloaded.
	clientGrace = 45 * time.Second
	// sessionCheckEvery is how often a long-lived socket rechecks that the
	// sign-in it was opened with still stands.
	sessionCheckEvery = 30 * time.Second
)

// events relays the app's event socket to a signed-in browser, dropping every
// event that belongs to a stream another user opened.
func (g *Gateway) events(w http.ResponseWriter, r *http.Request) {
	u, sess, ok := g.current(r)
	if !ok {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	up := g.upstream.Load()
	if up == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	client := r.URL.Query().Get("clientId")

	browser, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: g.ownHosts(r)})
	if err != nil {
		return // Accept has answered already
	}
	defer func() { _ = browser.CloseNow() }()
	// The browser sends nothing on this socket; reading only notices it close.
	ctx, cancel := context.WithCancel(browser.CloseRead(r.Context()))
	defer cancel()

	target := "ws://" + up.Host + "/wails/events"
	if client != "" {
		target += "?clientId=" + url.QueryEscape(client)
	}
	app, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{
		HTTPHeader: http.Header{headerSecret: {g.secret}},
	})
	if err != nil {
		g.log.Warn("could not reach the app's event socket", "err", err)
		_ = browser.Close(websocket.StatusTryAgainLater, "the app is not ready")
		return
	}
	defer func() { _ = app.CloseNow() }()
	app.SetReadLimit(maxEventBytes)

	g.presence.join(u.ID, client)
	defer g.presence.leave(u.ID, client)

	// A socket outlives the request that opened it; one whose sign-in has
	// ended, or whose user has been switched off, is closed.
	go func() {
		tick := time.NewTicker(sessionCheckEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if !g.store.sessionValid(sess.Hash) {
					cancel()
					return
				}
			}
		}
	}()

	for {
		kind, data, err := app.Read(ctx)
		if err != nil {
			break
		}
		if !g.visible(u.ID, data) {
			continue
		}
		if err := browser.Write(ctx, kind, data); err != nil {
			break
		}
	}
	_ = browser.Close(websocket.StatusNormalClosure, "")
}

// visible reports whether an event from the app may go to a user: any event
// that does not belong to a stream, and one that belongs to a stream the user
// opened. An event naming a stream nobody holds goes to nobody.
func (g *Gateway) visible(userID string, raw []byte) bool {
	var event struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return false
	}
	field, owned := g.deps.OwnedEvents[event.Name]
	if !owned {
		return true
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return false
	}
	var id string
	if err := json.Unmarshal(payload[field], &id); err != nil || id == "" {
		return false
	}
	owner, ok := g.deps.Owners.UserOf(id)
	return ok && owner == userID
}

// presence counts the event sockets each tab and each user has open, and
// closes a tab's streams -- or all of a user's -- once the last socket has
// been gone for the grace period.
type presence struct {
	owners *session.Owners
	grace  time.Duration

	mu      sync.Mutex
	counts  map[string]int
	pending map[string]*expiry
}

type expiry struct{ timer *time.Timer }

func newPresence(owners *session.Owners, grace time.Duration) *presence {
	return &presence{owners: owners, grace: grace, counts: map[string]int{}, pending: map[string]*expiry{}}
}

func presenceKeys(user, client string) []string {
	keys := []string{"u:" + user}
	if client != "" {
		keys = append(keys, "c:"+client)
	}
	return keys
}

func (p *presence) join(user, client string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range presenceKeys(user, client) {
		p.counts[key]++
		if e := p.pending[key]; e != nil {
			e.timer.Stop()
			delete(p.pending, key)
		}
	}
}

func (p *presence) leave(user, client string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range presenceKeys(user, client) {
		if p.counts[key]--; p.counts[key] > 0 {
			continue
		}
		delete(p.counts, key)
		e := &expiry{}
		p.pending[key] = e
		e.timer = time.AfterFunc(p.grace, func() { p.expire(key, e) })
	}
}

// expire closes what a tab or user left open, unless they came back, or a
// later departure has set a timer of its own.
func (p *presence) expire(key string, e *expiry) {
	p.mu.Lock()
	if p.pending[key] != e || p.counts[key] > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.pending, key)
	p.mu.Unlock()

	if client, ok := strings.CutPrefix(key, "c:"); ok {
		p.owners.AbandonClient(client)
	} else if user, ok := strings.CutPrefix(key, "u:"); ok {
		p.owners.AbandonUser(user)
	}
}
