package plugins

import (
	"errors"
	"fmt"
	"slices"
)

// Icons are the names a plugin, a view or an action may use as its icon: the
// glyphs the app draws, PATHS in frontend/src/lib/components/Icon.svelte. A
// name that is not one of them draws an empty square that lays out like any
// other, which hides the typo until someone looks closely -- so it is refused
// on load instead. A test keeps this list and that one the same.
var Icons = []string{
	"dashboard", "server", "layers", "bell", "box", "rocket", "database", "repeat",
	"check", "clock", "share", "globe", "sliders", "lock", "unlock", "play",
	"pause", "stop", "power", "drive", "gateway", "route", "grant", "puzzle",
	"terminal", "forward", "chevron-left", "chevron-right", "chevron-down", "copies", "copy", "tick", "scale", "gauge",
	"shield", "priority", "chip", "webhook", "policy", "history", "certificate", "exit", "link", "graph", "user",
	"users", "helm", "refresh", "download", "expand-all", "collapse-all", "sort-asc", "sort-desc",
	"sort-off", "plus", "minus", "folder-plus", "folder", "close", "dot", "edit", "save",
	"chevron-up", "alert", "file", "search", "trash", "undo", "dock-right", "dock-bottom",
	"dock-left", "pin", "settings", "sun", "moon", "monitor", "display", "rows",
	"columns", "type", "restore", "info", "help", "book",
}

// checkIcon says what is wrong with an icon name, or nothing. Empty is fine:
// every icon has a default.
func checkIcon(pluginID, what, icon string) error {
	if icon == "" || slices.Contains(Icons, icon) {
		return nil
	}
	msg := fmt.Sprintf("plugin %q gives %s the icon %q, which is not one of the app's icons", pluginID, what, icon)
	if near := nearest(icon, Icons); near != "" {
		msg += fmt.Sprintf(" -- did you mean %q?", near)
	} else {
		msg += " (see Icons in the plugin docs)"
	}
	return errors.New(msg)
}
