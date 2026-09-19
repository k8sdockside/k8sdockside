package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
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

// LogoTypes are the image formats a plugin's own mark may be in. They are the
// three every webview draws and none of them needs decoding help: vector for
// preference, and two raster formats for a mark that was never drawn as one.
var LogoTypes = []string{".svg", ".png", ".webp"}

// checkLogo says what is wrong with a plugin's logo path, or nothing. Empty is
// fine: a plugin without one is drawn with its icon.
//
// The path is held to the same shape as a view's entry -- relative, inside the
// ui folder -- because it is served from exactly the same place and by the
// same handler. Whether the file is actually there is a question for the
// folder, and is asked in checkEntries.
func checkLogo(pluginID, logo string) error {
	if logo == "" {
		return nil
	}
	if strings.ContainsAny(logo, "\\") || strings.HasPrefix(logo, "/") || !fs.ValidPath(logo) {
		return fmt.Errorf("plugin %q gives its logo as %q, which is not a path inside its ui folder", pluginID, logo)
	}
	ext := strings.ToLower(path.Ext(logo))
	if !slices.Contains(LogoTypes, ext) {
		return fmt.Errorf("plugin %q gives its logo as %q, which is not %s", pluginID, logo, strings.Join(LogoTypes, ", "))
	}
	return nil
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
