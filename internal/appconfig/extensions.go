// Themes and plugins: the folders they are read from, which plugins are
// switched off or hidden from the suggestions, and what each plugin keeps.
package appconfig

import (
	"errors"
	"fmt"
	"slices"
)

// ThemeFolders returns the extra directories themes are read from.
func (s *Store) ThemeFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ThemeFolders)
}

// AddThemeFolder remembers a directory to read themes from. Adding one that is
// already known is a no-op rather than an error.
func (s *Store) AddThemeFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ThemeFolders, path) {
			d.ThemeFolders = append(d.ThemeFolders, path)
		}
	})
}

// RemoveThemeFolder stops reading themes from a directory. Nothing is deleted
// from disk; the themes simply stop being offered.
func (s *Store) RemoveThemeFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ThemeFolders = slices.DeleteFunc(d.ThemeFolders, func(p string) bool { return p == path })
	})
}

// PluginFolders returns the extra directories plugins are read from.
func (s *Store) PluginFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.PluginFolders)
}

// AddPluginFolder remembers a directory to read plugins from. Adding one that
// is already known is a no-op rather than an error.
func (s *Store) AddPluginFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.PluginFolders, path) {
			d.PluginFolders = append(d.PluginFolders, path)
		}
	})
}

// RemovePluginFolder stops reading plugins from a directory. Nothing is deleted
// from disk; the plugins in it just stop being offered.
func (s *Store) RemovePluginFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.PluginFolders = slices.DeleteFunc(d.PluginFolders, func(p string) bool { return p == path })
	})
}

// DisabledPlugins returns the ids of the plugins the user has switched off.
func (s *Store) DisabledPlugins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.DisabledPlugins)
}

// SetPluginEnabled switches one plugin on or off by id.
//
// It takes the wanted state rather than being a toggle so that the frontend
// cannot drift out of step with the file: a switch that sends "off" twice ends
// up off, where a toggle would end up back on.
func (s *Store) SetPluginEnabled(id string, enabled bool) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("plugin id is required")
	}
	return s.update(func(d *Settings) {
		if enabled {
			d.DisabledPlugins = slices.DeleteFunc(d.DisabledPlugins, func(p string) bool { return p == id })
			return
		}
		if !slices.Contains(d.DisabledPlugins, id) {
			d.DisabledPlugins = append(d.DisabledPlugins, id)
		}
	})
}

// HidePluginSuggestion stops the sidebar suggesting one known plugin, or
// lets it suggest it again. Like SetPluginEnabled it takes the wanted state.
func (s *Store) HidePluginSuggestion(id string, hidden bool) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("plugin id is required")
	}
	return s.update(func(d *Settings) {
		if !hidden {
			d.HiddenPluginSuggestions = slices.DeleteFunc(d.HiddenPluginSuggestions, func(p string) bool { return p == id })
			return
		}
		if !slices.Contains(d.HiddenPluginSuggestions, id) {
			d.HiddenPluginSuggestions = append(d.HiddenPluginSuggestions, id)
		}
	})
}

// Limits on what one plugin's pages may keep on one context: enough for
// folded sections, filters and a few choices, not a database in the
// settings file.
const (
	maxPluginStateKeys  = 64
	maxPluginStateKey   = 128
	maxPluginStateValue = 16 << 10
)

// SetPluginState keeps one value for a plugin's pages on one context, or
// forgets it when value is empty.
func (s *Store) SetPluginState(pluginID, contextID, key, value string) (Settings, error) {
	switch {
	case pluginID == "" || contextID == "":
		return s.Get(), errors.New("plugin id and context id are required")
	case key == "" || len(key) > maxPluginStateKey:
		return s.Get(), fmt.Errorf("a stored key is 1 to %d characters", maxPluginStateKey)
	case len(value) > maxPluginStateValue:
		return s.Get(), fmt.Errorf("a stored value is at most %d KiB, and this one is %d bytes", maxPluginStateValue>>10, len(value))
	}
	if value != "" {
		s.mu.Lock()
		keys := s.data.PluginState[pluginID][contextID]
		_, have := keys[key]
		full := !have && len(keys) >= maxPluginStateKeys
		s.mu.Unlock()
		if full {
			return s.Get(), fmt.Errorf("a plugin may keep at most %d values per cluster; remove one first", maxPluginStateKeys)
		}
	}
	return s.update(func(d *Settings) {
		if value == "" {
			// Indexing nil maps reads as empty, and deleting from one is a no-op.
			delete(d.PluginState[pluginID][contextID], key)
			return
		}
		if d.PluginState == nil {
			d.PluginState = map[string]map[string]map[string]string{}
		}
		if d.PluginState[pluginID] == nil {
			d.PluginState[pluginID] = map[string]map[string]string{}
		}
		if d.PluginState[pluginID][contextID] == nil {
			d.PluginState[pluginID][contextID] = map[string]string{}
		}
		d.PluginState[pluginID][contextID][key] = value
	})
}
