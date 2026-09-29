package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/knownlist"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/updates"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// KnownListEvent tells the window the list of known plugins has changed, so
// Settings and the sidebar's suggestions read it again.
const KnownListEvent = "plugins:known"

// When the list is first fetched after launch, and how often after that: the
// same rhythm as the update check, a little after it so the two do not land
// on GitHub in the same instant.
const (
	knownListDelay = 8 * time.Second
	knownListEvery = 6 * time.Hour
)

// KnownListStatus is where the list of known plugins came from, for Settings
// to say.
type KnownListStatus struct {
	// Source is "built in" until a fetched copy has been accepted, then
	// "fetched".
	Source string `json:"source"`
	// FetchedAt is when the copy in use was fetched, RFC 3339; empty for the
	// built-in list.
	FetchedAt string `json:"fetchedAt"`
	// Error is why the last fetch failed, or what it had to leave out; empty
	// when it went through whole.
	Error string `json:"error"`
	// Enabled is whether the app fetches the list at all right now.
	Enabled bool `json:"enabled"`
}

// knownLister keeps the list of known plugins up to date from the repository.
//
// It lives beside the plugin service because that is what reads the list:
// once a new copy is in, the service's cached catalogue is dropped, since the
// Official badge and the fallback categories of installed plugins are read
// from the list when the catalogue is built.
type knownLister struct {
	fetcher      *knownlist.Fetcher
	path         string
	delay, every time.Duration
	// allowed is asked before every automatic fetch.
	allowed func() bool
	// changed is called once a new list is in use.
	changed func()

	mu        sync.Mutex
	etag      string
	fetchedAt time.Time
	lastErr   string
	stop      context.CancelFunc
}

func newKnownLister(path string, allowed func() bool, changed func()) *knownLister {
	return &knownLister{
		fetcher: knownlist.New("k8sdockside/" + DisplayVersion() + " (+https://github.com/" + updates.Repo + ")"),
		path:    path,
		delay:   knownListDelay,
		every:   knownListEvery,
		allowed: allowed,
		changed: changed,
	}
}

// start puts the copy kept on disk in use straight away -- a launch without a
// network offers what the last one found -- and begins fetching.
func (l *knownLister) start(ctx context.Context) {
	if cached, ok := knownlist.Load(l.path); ok {
		if list, _, err := plugins.ParseFetchedKnown(cached.Body, DisplayVersion()); err == nil {
			plugins.UseFetchedKnown(list)
			l.mu.Lock()
			l.etag, l.fetchedAt = cached.ETag, cached.FetchedAt
			l.mu.Unlock()
			l.changed()
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.stop = cancel
	l.mu.Unlock()
	go l.loop(ctx)
}

func (l *knownLister) shutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stop != nil {
		l.stop()
	}
}

// loop fetches after the delay and then on the interval. Whether it may is
// asked on every turn, so the setting takes effect without a restart.
func (l *knownLister) loop(ctx context.Context) {
	timer := time.NewTimer(l.delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if l.allowed() {
			l.refresh(ctx)
		}
		timer.Reset(l.every)
	}
}

// refresh fetches the list once, and puts it in use if it is sound. A failure
// of any kind keeps the list in use as it is.
func (l *knownLister) refresh(ctx context.Context) {
	l.mu.Lock()
	etag := l.etag
	l.mu.Unlock()

	answer, err := l.fetcher.Fetch(ctx, etag)
	if err != nil {
		l.fail(err.Error())
		return
	}
	if answer.NotModified {
		l.fail("")
		return
	}
	list, skipped, err := plugins.ParseFetchedKnown(answer.Body, DisplayVersion())
	if err != nil {
		l.fail(err.Error())
		return
	}
	plugins.UseFetchedKnown(list)
	now := time.Now()
	// Kept only once it has been accepted, so the copy on disk is always one
	// that parsed.
	_ = knownlist.Save(l.path, knownlist.Cached{Body: answer.Body, ETag: answer.ETag, FetchedAt: now})

	l.mu.Lock()
	l.etag, l.fetchedAt = answer.ETag, now
	l.lastErr = ""
	if len(skipped) > 0 {
		l.lastErr = "left out: " + strings.Join(skipped, "; ")
	}
	l.mu.Unlock()
	l.changed()
}

func (l *knownLister) fail(msg string) {
	l.mu.Lock()
	l.lastErr = msg
	l.mu.Unlock()
}

func (l *knownLister) status(enabled bool) KnownListStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := KnownListStatus{Source: "built in", Error: l.lastErr, Enabled: enabled}
	if !l.fetchedAt.IsZero() {
		st.Source = "fetched"
		st.FetchedAt = l.fetchedAt.Format(time.RFC3339)
	}
	return st
}

// ----- on the plugin service -------------------------------------------------

// ServiceStartup begins keeping the list of known plugins up to date. Wails
// calls it as the app comes up.
func (s *PluginService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.knownOff {
		return nil
	}
	s.known = newKnownLister(s.store.KnownListPath(), s.knownAllowed, s.knownChanged)
	s.known.start(ctx)
	return nil
}

// ServiceShutdown stops fetching the list.
func (s *PluginService) ServiceShutdown() error {
	if s.known != nil {
		s.known.shutdown()
	}
	return nil
}

// knownAllowed is whether the list may be fetched on its own right now. On the
// desktop it goes with the setting that lets the app check for new versions:
// both are the app asking GitHub something without being asked. The web
// version fetches it unless its operator switched such requests off, which is
// knownOff and stops it before it starts.
func (s *PluginService) knownAllowed() bool {
	return s.server || s.store.CheckForUpdates()
}

// knownChanged drops the cached catalogue, whose badges and categories came
// from the old list, and tells the window.
func (s *PluginService) knownChanged() {
	s.forget()
	if app := application.Get(); app != nil {
		app.Event.Emit(KnownListEvent, nil)
	}
}

// KnownListStatus says where the list of known plugins came from.
func (s *PluginService) KnownListStatus() KnownListStatus {
	if s.known == nil {
		return KnownListStatus{Source: "built in"}
	}
	return s.known.status(s.knownAllowed())
}
