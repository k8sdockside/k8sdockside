//go:build !server

package main

import (
	"log"
	"sync"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The main window opens where it was last left, at the size it was left, and
// maximised if it was.

const (
	// Wide enough for the sidebar, a tab bar and a docked detail panel side by
	// side without anything collapsing.
	defaultWidth  = 1440
	defaultHeight = 900
	minWidth      = 960
	minHeight     = 600
	// titleBarHeight matches the height of the frontend's own top bar, which is
	// also the strip the window is dragged by.
	titleBarHeight = 44
	// saveAfter is how long the window has to stand still before where it is
	// gets written: a drag reports every step of the way.
	saveAfter = 500 * time.Millisecond
)

// placeWindow starts the window at the place it was last left, when there is
// one on record; with none it opens centred at the default size. Maximising
// is left to settle: a window maximised before it is put at its place would
// be moved off the edges it was maximised to.
func placeWindow(opts *application.WebviewWindowOptions, saved appconfig.Window) {
	if saved.Width < minWidth || saved.Height < minHeight {
		return
	}
	opts.Width, opts.Height = saved.Width, saved.Height
	opts.InitialPosition = application.WindowXY
	opts.X, opts.Y = saved.X, saved.Y
}

// reachable reports whether enough of the window's title bar lies on one of
// the screens for it to be taken hold of and dragged. A window last left on a
// display that is no longer connected is not. With no screens to go by, the
// window is left where it is.
func reachable(bounds application.Rect, screens []*application.Screen) bool {
	if len(screens) == 0 {
		return true
	}
	for _, screen := range screens {
		area := screen.WorkArea
		wide := min(bounds.X+bounds.Width, area.X+area.Width) - max(bounds.X, area.X)
		tall := min(bounds.Y+titleBarHeight, area.Y+area.Height) - max(bounds.Y, area.Y)
		if wide >= 100 && tall > 0 {
			return true
		}
	}
	return false
}

// windowKeeper keeps the settings file in step with where the main window is
// and how big. The place is read as the window moves -- a closing window can
// no longer be asked -- and written once it has stood still for a moment,
// and at the latest when the app quits.
type windowKeeper struct {
	app       *application.App
	window    *application.WebviewWindow
	settings  *appconfig.Store
	maximised bool

	mu    sync.Mutex
	place appconfig.Window
	timer *time.Timer
}

// keepWindow starts following the window. Its save is what the app should
// call on the way out.
func keepWindow(app *application.App, window *application.WebviewWindow, saved appconfig.Window, settings *appconfig.Store) *windowKeeper {
	k := &windowKeeper{app: app, window: window, settings: settings, maximised: saved.Maximised, place: saved}
	window.OnWindowEvent(events.Common.WindowDidMove, k.moved)
	window.OnWindowEvent(events.Common.WindowDidResize, k.moved)
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { k.save() })
	// The page being up is the first moment the app is sure to have the
	// screens to hand; a reload of it is not another start.
	var once sync.Once
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) { once.Do(k.settle) })
	return k
}

// settle finishes placing the window: one whose place is on no screen that
// is connected now is centred, and one that was left maximised is maximised
// again -- after being put at its place, so un-maximising goes back there.
func (k *windowKeeper) settle() {
	if !reachable(k.window.Bounds(), k.app.Screen.GetAll()) {
		k.window.Center()
	}
	if k.maximised {
		k.window.Maximise()
	}
}

// moved takes note of where the window is now. A maximised window keeps the
// place it had before, which is where un-maximising returns to; full screen
// and minimised are not places to come back to at all.
func (k *windowKeeper) moved(*application.WindowEvent) {
	if k.window.IsMinimised() || k.window.IsFullscreen() {
		return
	}
	maximised := k.window.IsMaximised()
	var bounds application.Rect
	if !maximised {
		// A window already gone reports no bounds at all.
		if bounds = k.window.Bounds(); bounds.Width == 0 || bounds.Height == 0 {
			return
		}
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if maximised {
		k.place.Maximised = true
	} else {
		k.place = appconfig.Window{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height}
	}
	if k.timer != nil {
		k.timer.Stop()
	}
	k.timer = time.AfterFunc(saveAfter, k.save)
}

// save writes the place down, if it has changed since it last was.
func (k *windowKeeper) save() {
	k.mu.Lock()
	if k.timer != nil {
		k.timer.Stop()
		k.timer = nil
	}
	place := k.place
	k.mu.Unlock()

	if place.Width == 0 || place == k.settings.Get().Window {
		return
	}
	if _, err := k.settings.SetWindow(place); err != nil {
		log.Printf("k8sdockside: remembering where the window is: %v", err)
	}
}
