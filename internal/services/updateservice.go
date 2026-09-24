package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/k8sdockside/k8sdockside/internal/updates"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// UpdateEvent carries a fresh UpdateStatus to the window whenever a check
// finishes, so the bell in the title bar can change without having asked.
const UpdateEvent = "update:status"

// How long after launch the first check runs, and how often after that.
//
// The delay is so the window is up and the kubeconfigs are read before
// anything is spent on a question that can wait. The interval is the balance
// between hearing about a release the day it is cut and GitHub's sixty
// unauthenticated requests an hour per address, which this shares with every
// other tool on the machine that asks the same API.
const (
	updateCheckDelay = 5 * time.Second
	updateCheckEvery = 6 * time.Hour
)

// UpdateStatus is what the window knows about releases: the one it is running,
// the newest one there is, and whether the user has heard about the difference.
type UpdateStatus struct {
	// Current is the version this binary reports -- see DisplayVersion.
	Current string `json:"current"`
	// Latest is the newest published release, nil until a check has succeeded.
	// It survives a later failure: what was known this morning is still known.
	Latest *updates.Release `json:"latest"`
	// Newer is whether Latest is a later version than Current. Always false
	// for a development build, which has no version to be behind.
	Newer bool `json:"newer"`
	// Unread is Newer, and the user has not yet marked that release as read.
	Unread bool `json:"unread"`
	// CheckedAt is when the last check finished, RFC 3339, empty until one has.
	CheckedAt string `json:"checkedAt"`
	// Error says why the last check failed, in words for the settings page.
	// Empty when it succeeded.
	Error string `json:"error"`
	// Install is what this build is -- "Debian package, amd64" -- so the user
	// can see why the download below is the one offered.
	Install string `json:"install"`
	// Download is the address of Latest's file for this install, empty when
	// no check has succeeded or the release has no file for it. The release
	// page, which always exists, is the fallback.
	Download string `json:"download"`
	// Server is set in the web version, where the app is upgraded by whoever
	// deployed it -- with Helm, usually -- rather than by the person in the
	// browser. It never checks on its own there, and offers no download.
	Server bool `json:"server"`
	// CanCheck is whether a check may be asked for at all. False when the
	// web version's operator has switched checks off -- a cluster that must
	// not reach GitHub, say.
	CanCheck bool `json:"canCheck"`
}

// UpdateService tells the window when a newer release of the app exists.
//
// It is the one service that reaches beyond the user's own machine and
// clusters, and it does so as little as possible: one unauthenticated GET of
// GitHub's public releases endpoint shortly after launch and every few hours
// after, carrying nothing but a User-Agent naming the app and its version. The
// Behaviour settings switch that off; a check the user asks for from the About
// page runs either way, because that is the user asking rather than the app.
//
// What the user has already seen is kept in the settings file rather than
// here, so a release marked as read stays read across restarts, and the bell
// speaks up again only for the next one.
type UpdateService struct {
	store   *appconfig.Store
	checker *updates.Checker
	// current is the version this binary reports, fixed at construction: it
	// cannot change while the process runs, and reading it once keeps every
	// status consistent with the About page.
	current string
	// install is how this build was installed, worked out once at
	// construction for the same reason.
	install updates.Install
	// delay and every are the constants above, held here so a test can shorten
	// them.
	delay, every time.Duration

	mu        sync.Mutex
	latest    *updates.Release
	checkedAt time.Time
	err       string
	// stop ends the background loop; nil until ServiceStartup has begun it.
	stop context.CancelFunc
	// server is set in the web version, which is updated by whoever deploys
	// it rather than by the person using it. There it never asks GitHub on its
	// own: only when somebody presses Check now, to see whether the server is
	// behind -- and never at all when noChecks says the operator forbade it.
	server   bool
	noChecks bool
}

// NewUpdateService wires the service to the settings it reads the preference
// from and records the read version in.
func NewUpdateService(store *appconfig.Store) *UpdateService {
	current := DisplayVersion()
	return &UpdateService{
		store:   store,
		checker: updates.New(current),
		current: current,
		install: updates.DetectInstall(),
		delay:   updateCheckDelay,
		every:   updateCheckEvery,
	}
}

// ServiceStartup begins the background checks. Wails calls it as the app comes
// up, on every service that has it.
func (s *UpdateService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.server {
		return nil
	}
	ctx, s.stop = context.WithCancel(ctx)
	go s.loop(ctx)
	return nil
}

// ServiceShutdown ends the background checks when the app quits.
func (s *UpdateService) ServiceShutdown() error {
	if s.stop != nil {
		s.stop()
	}
	return nil
}

// loop checks once after the launch delay and then on the interval, for as
// long as the app runs. The preference is read on every turn rather than once,
// so switching it off in settings takes effect without a restart -- the next
// tick asks nothing -- and switching it back on does the same.
func (s *UpdateService) loop(ctx context.Context) {
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if s.store.CheckForUpdates() {
			s.check(ctx)
		}
		timer.Reset(s.every)
	}
}

// Status is what is known right now, without asking anything. The window reads
// it as it opens, before the first check has had its chance to run.
func (s *UpdateService) Status() UpdateStatus {
	return s.snapshot()
}

// Check asks GitHub now, whether or not automatic checks are on: pressing the
// button on the About page is the user asking, and the preference is about the
// app asking on its own. The same in the web version, where it is the only
// check there is -- unless the operator switched checks off.
func (s *UpdateService) Check() UpdateStatus {
	if s.noChecks {
		return s.snapshot()
	}
	return s.check(context.Background())
}

// check asks GitHub, records the answer and tells the window.
func (s *UpdateService) check(ctx context.Context) UpdateStatus {
	release, err := s.checker.Latest(ctx)

	s.mu.Lock()
	s.checkedAt = time.Now()
	if err != nil {
		s.err = err.Error()
	} else {
		s.err = ""
		s.latest = &release
	}
	s.mu.Unlock()

	status := s.snapshot()
	if app := application.Get(); app != nil {
		app.Event.Emit(UpdateEvent, status)
	}
	return status
}

// snapshot assembles the status from what is held here and what the settings
// say the user has already read.
func (s *UpdateService) snapshot() UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := UpdateStatus{Current: s.current, Error: s.err, Install: s.install.String(), Server: s.server, CanCheck: !s.noChecks}
	if s.server {
		status.Install = "server"
	}
	if !s.checkedAt.IsZero() {
		status.CheckedAt = s.checkedAt.Format(time.RFC3339)
	}
	if s.latest != nil {
		release := *s.latest
		status.Latest = &release
		status.Newer = updates.Newer(s.current, release.Version)
		status.Unread = status.Newer && s.store.Get().Updates.ReadVersion != release.Version
		if !s.server {
			status.Download = release.Download(s.install)
		}
	}
	return status
}

// MarkRead records that the user has seen the notice about the latest release.
// The bell goes quiet for that version and stays quiet across restarts; a
// newer release after it is news again.
//
// With nothing to mark -- no check has succeeded yet -- it changes nothing and
// answers with the status as it stands rather than an error, since there is
// nothing the user could have done differently.
func (s *UpdateService) MarkRead() (UpdateStatus, error) {
	s.mu.Lock()
	latest := s.latest
	s.mu.Unlock()
	if latest == nil {
		return s.snapshot(), nil
	}
	if _, err := s.store.MarkUpdateRead(latest.Version); err != nil {
		return s.snapshot(), err
	}
	return s.snapshot(), nil
}

// OpenRelease sends the latest release's page to the browser, or the list of
// releases when no check has succeeded yet. The address is built from the tag
// by the checker rather than taken from the window, so nothing the webview
// says can decide what gets opened.
func (s *UpdateService) OpenRelease() error {
	if s.server {
		return errDesktopOnly
	}
	s.mu.Lock()
	target := updates.ReleasesPage
	if s.latest != nil {
		target = s.latest.URL
	}
	s.mu.Unlock()

	app := application.Get()
	if app == nil {
		return errors.New("the application is closing")
	}
	return app.Browser.OpenURL(target)
}

// OpenDownload sends the latest release's file for this install to the
// browser, which downloads it. Like OpenRelease, the address is built here
// from the tag and the file's name, never taken from the window. With nothing
// to download -- no check yet, or no file for this platform -- it fails, and
// the window is expected not to have offered the button.
func (s *UpdateService) OpenDownload() error {
	if s.server {
		return errDesktopOnly
	}
	s.mu.Lock()
	target := ""
	if s.latest != nil {
		target = s.latest.Download(s.install)
	}
	s.mu.Unlock()

	if target == "" {
		return errors.New("there is no download for this platform; open the release page instead")
	}
	app := application.Get()
	if app == nil {
		return errors.New("the application is closing")
	}
	return app.Browser.OpenURL(target)
}
