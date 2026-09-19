//go:build !server

package main

import (
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/appconfig"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func defaultOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{Width: defaultWidth, Height: defaultHeight}
}

func TestPlaceWindowWithNothingSaved(t *testing.T) {
	opts := defaultOptions()
	placeWindow(&opts, appconfig.Window{})

	if opts.Width != defaultWidth || opts.Height != defaultHeight || opts.InitialPosition != application.WindowCentered {
		t.Errorf("options = %dx%d at %v, want the default size, centred", opts.Width, opts.Height, opts.InitialPosition)
	}
}

func TestPlaceWindowWhereItWasLeft(t *testing.T) {
	opts := defaultOptions()
	placeWindow(&opts, appconfig.Window{X: -1500, Y: 60, Width: 1280, Height: 800, Maximised: true})

	if opts.InitialPosition != application.WindowXY || opts.X != -1500 || opts.Y != 60 {
		t.Errorf("position = %v (%d, %d), want (-1500, 60)", opts.InitialPosition, opts.X, opts.Y)
	}
	if opts.Width != 1280 || opts.Height != 800 {
		t.Errorf("size = %dx%d, want 1280x800", opts.Width, opts.Height)
	}
	// Maximising waits until the window is at its place.
	if opts.StartState != application.WindowStateNormal {
		t.Errorf("start state = %v, want normal", opts.StartState)
	}
}

func TestPlaceWindowIgnoresASizeBelowTheMinimum(t *testing.T) {
	opts := defaultOptions()
	placeWindow(&opts, appconfig.Window{X: 10, Y: 10, Width: 200, Height: 100})

	if opts.Width != defaultWidth || opts.InitialPosition != application.WindowCentered {
		t.Errorf("options = %dx%d at %v, want the default size, centred", opts.Width, opts.Height, opts.InitialPosition)
	}
}

func TestReachable(t *testing.T) {
	// A laptop screen with a menu bar, and a second display to its left.
	screens := []*application.Screen{
		{WorkArea: application.Rect{X: 0, Y: 25, Width: 1512, Height: 920}},
		{WorkArea: application.Rect{X: -1920, Y: 0, Width: 1920, Height: 1080}},
	}
	cases := []struct {
		name   string
		bounds application.Rect
		want   bool
	}{
		{"on the primary screen", application.Rect{X: 100, Y: 60, Width: 1200, Height: 800}, true},
		{"on the second display", application.Rect{X: -1500, Y: 100, Width: 1200, Height: 800}, true},
		{"straddling the two", application.Rect{X: -600, Y: 100, Width: 1200, Height: 800}, true},
		{"on a display no longer there", application.Rect{X: 3000, Y: 100, Width: 1200, Height: 800}, false},
		{"only a sliver of the title bar showing", application.Rect{X: 1450, Y: 100, Width: 1200, Height: 800}, false},
		{"title bar below the bottom of the screens", application.Rect{X: 100, Y: 1100, Width: 1200, Height: 800}, false},
	}
	for _, tc := range cases {
		if got := reachable(tc.bounds, screens); got != tc.want {
			t.Errorf("%s: reachable = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !reachable(application.Rect{X: 5000, Y: 5000, Width: 1200, Height: 800}, nil) {
		t.Error("with no screens known, the window should be left where it is")
	}
}
