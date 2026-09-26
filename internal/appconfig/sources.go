// Where the kubeconfigs come from, and what the user decided about each
// context in them: files and folders added, files and contexts hidden, and
// the per-context alias, colour, columns and metrics endpoint.
package appconfig

import (
	"errors"
	"path/filepath"
	"slices"
)

// SetContextPrefs records the alias and colour for one context.
func (s *Store) SetContextPrefs(id string, prefs ContextPrefs) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		// A folding override is a preference in its own right, so a context
		// carrying only that one is kept -- see ContextPrefs.isEmpty.
		if prefs.isEmpty() {
			delete(d.Contexts, id)
			return
		}
		d.Contexts[id] = prefs
	})
}

// MetricsEndpoint returns where a context's Prometheus was configured to be,
// empty when the user has not said and discovery should look.
func (s *Store) MetricsEndpoint(contextID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Contexts[contextID].Metrics
}

// SetMetricsEndpoint records where a context's Prometheus is. An empty value
// clears the override rather than recording an empty one, which is what puts
// discovery back in charge.
func (s *Store) SetMetricsEndpoint(contextID, value string) (Settings, error) {
	if contextID == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		prefs := d.Contexts[contextID]
		prefs.Metrics = value
		// The same emptiness rule SetContextPrefs applies: a context with
		// nothing left to say about it is forgotten rather than kept as a blank
		// entry cluttering the settings file.
		if prefs.isEmpty() {
			delete(d.Contexts, contextID)
			return
		}
		d.Contexts[contextID] = prefs
	})
}

// AddManualFile remembers a kubeconfig path the user chose. Adding a path that
// is already known is a no-op rather than an error.
func (s *Store) AddManualFile(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ManualFiles, path) {
			d.ManualFiles = append(d.ManualFiles, path)
		}
	})
}

// RemoveManualFile forgets a path the user added. Auto-discovered files cannot
// be removed this way -- they will simply reappear on the next sync.
func (s *Store) RemoveManualFile(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ManualFiles = slices.DeleteFunc(d.ManualFiles, func(p string) bool { return p == path })
	})
}

// ExcludedFiles returns the discovered kubeconfigs the user has hidden.
func (s *Store) ExcludedFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ExcludedFiles)
}

// ExcludeFile hides a discovered kubeconfig from the sidebar.
func (s *Store) ExcludeFile(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ExcludedFiles, path) {
			d.ExcludedFiles = append(d.ExcludedFiles, path)
		}
	})
}

// UnexcludeFile lets a hidden kubeconfig be discovered again.
func (s *Store) UnexcludeFile(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ExcludedFiles = slices.DeleteFunc(d.ExcludedFiles, func(p string) bool { return p == path })
	})
}

// ExcludedContexts returns the contexts the user has removed one by one.
func (s *Store) ExcludedContexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ExcludedContexts)
}

// ExcludeContext removes one context from the sidebar, leaving its file and
// the other contexts in it where they are.
func (s *Store) ExcludeContext(id string) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ExcludedContexts, id) {
			d.ExcludedContexts = append(d.ExcludedContexts, id)
		}
	})
}

// UnexcludeContext brings back a context the user removed, so the next scan
// lists it again.
//
// The counterpart to ExcludeContext, and there for the same reason
// UnexcludeFile is: a removal that nothing can undo is a removal that has to be
// got right first time, and this one is a click away from the row it hides.
func (s *Store) UnexcludeContext(id string) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		d.ExcludedContexts = slices.DeleteFunc(d.ExcludedContexts, func(c string) bool { return c == id })
	})
}

// ForgetContexts drops the removals of contexts that are no longer in any
// kubeconfig. A removal is only meant to outlast the context it names for as
// long as the file still holds it; once the context is gone, keeping the
// removal would silently swallow the same name the next time it is added.
func (s *Store) ForgetContexts(ids []string) (Settings, error) {
	if len(ids) == 0 {
		return s.Get(), nil
	}
	return s.update(func(d *Settings) {
		d.ExcludedContexts = slices.DeleteFunc(d.ExcludedContexts, func(c string) bool {
			return slices.Contains(ids, c)
		})
	})
}

// ClearExclusionsIn forgets what was hidden inside one directory, so that
// re-adding a folder starts from a clean slate rather than quietly continuing
// to hide files the user can no longer see listed anywhere.
func (s *Store) ClearExclusionsIn(dir string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ExcludedFiles = slices.DeleteFunc(d.ExcludedFiles, func(p string) bool {
			return filepath.Dir(p) == dir
		})
	})
}

// AddManualFolder remembers a directory to scan for kubeconfigs. Adding one
// that is already known is a no-op rather than an error.
func (s *Store) AddManualFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ManualFolders, path) {
			d.ManualFolders = append(d.ManualFolders, path)
		}
	})
}

// RemoveManualFolder stops scanning a directory. The configs found through it
// disappear from the sidebar on the next sync.
func (s *Store) RemoveManualFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ManualFolders = slices.DeleteFunc(d.ManualFolders, func(p string) bool { return p == path })
	})
}
