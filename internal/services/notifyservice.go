package services

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// NotifyOpenEvent says a notification was clicked: which alert, on which
// cluster, so the window can show that alert's details.
const NotifyOpenEvent = "notify:open"

// NotifyOpen is what a clicked notification was about.
type NotifyOpen struct {
	ContextID string `json:"contextId"`
	AlertID   string `json:"alertId"`
}

func init() {
	application.RegisterEvent[NotifyOpen](NotifyOpenEvent)
}

// NotifyStatus says whether the system's notifications can be used, and if
// not, why.
type NotifyStatus struct {
	// Available is whether the system accepted the app as something that may
	// notify at all. On macOS that needs a signed bundle, so a development
	// build answers false here and says so in Reason.
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	// Authorized is whether the user has allowed it. Only meaningful when
	// Available is.
	Authorized bool `json:"authorized"`
}

// NotifyService posts the cluster alerts as the operating system's own
// notifications, so a node going down is heard about with the window behind
// others -- or minimised, or on another desktop.
//
// It wraps Wails' notification service rather than registering that service
// directly, because that one fails the app's start when the system will not
// have it: an unsigned build on macOS, a Linux session with no notification
// daemon. Here that is recorded and reported instead, and the alerts still
// reach the bell in the title bar.
//
// What to say, and when, is the window's decision: it is the window that
// compares one reading of a cluster with the last. This only delivers.
type NotifyService struct {
	// disabled is set in the web version, where "the operating system" is a
	// pod's and nobody would see what it showed.
	disabled bool

	mu      sync.Mutex
	native  *notifications.NotificationService
	missing string
}

// ServiceStartup connects to the system's notifications. A refusal is kept
// as the reason Status reports, never returned: an app that will not start
// because it cannot post notifications has its priorities backwards.
func (s *NotifyService) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		s.missing = "Desktop notifications are not available in the web version."
		return nil
	}
	native := notifications.New()
	if err := native.ServiceStartup(ctx, opts); err != nil {
		s.missing = err.Error()
		return nil
	}
	native.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			return
		}
		contextID, _ := result.Response.UserInfo["contextId"].(string)
		alertID, _ := result.Response.UserInfo["alertId"].(string)
		if contextID == "" && alertID == "" {
			return
		}
		app := application.Get()
		if app == nil {
			return
		}
		// The window comes forward first: a click on a notification is
		// somebody asking to see it, and the details are no use behind
		// another app or in the Dock.
		for _, w := range app.Window.GetAll() {
			if w.IsMinimised() {
				w.UnMinimise()
			}
			w.Show()
			w.Focus()
		}
		app.Event.Emit(NotifyOpenEvent, NotifyOpen{ContextID: contextID, AlertID: alertID})
	})
	s.native = native
	return nil
}

// ServiceShutdown lets go of the system's notifications.
func (s *NotifyService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native != nil {
		return s.native.ServiceShutdown()
	}
	return nil
}

func (s *NotifyService) available() (*notifications.NotificationService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native == nil {
		if s.missing == "" {
			return nil, errors.New("desktop notifications have not started")
		}
		return nil, errors.New(s.missing)
	}
	return s.native, nil
}

// Status reports whether notifications can be posted, for the settings page.
func (s *NotifyService) Status() NotifyStatus {
	native, err := s.available()
	if err != nil {
		return NotifyStatus{Reason: err.Error()}
	}
	authorized, err := native.CheckNotificationAuthorization()
	if err != nil {
		return NotifyStatus{Available: true, Reason: err.Error()}
	}
	return NotifyStatus{Available: true, Authorized: authorized}
}

// RequestPermission asks the user to allow the app's notifications, which
// macOS does once and remembers. It is asked when the setting is turned on,
// rather than at launch, so the question comes when the answer is wanted.
func (s *NotifyService) RequestPermission() (bool, error) {
	native, err := s.available()
	if err != nil {
		return false, err
	}
	return native.RequestNotificationAuthorization()
}

// Send posts one notification. id makes a repeat of the same alert replace
// the one already showing rather than stack beside it; contextID and alertID
// are what a click on it opens.
func (s *NotifyService) Send(id, title, subtitle, body, contextID, alertID string) error {
	native, err := s.available()
	if err != nil {
		return err
	}
	return native.SendNotification(notifications.NotificationOptions{
		ID:       id,
		Title:    title,
		Subtitle: subtitle,
		Body:     body,
		Data:     map[string]any{"contextId": contextID, "alertId": alertID},
	})
}
