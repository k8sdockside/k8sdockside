package services

// The command line tools a plugin declares in "ui": {"tools": [...]}: what a
// page may ask about them, read through them, and ask to run. What may be run
// is the manifest's patterns (plugins.UITool.Allows); running it is
// internal/toolcli's. The console and the external terminal are in
// terminalservice.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/plugins"
	"github.com/k8sdockside/k8sdockside/internal/toolcli"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ToolFile is one of a tool's files as a page sees it.
type ToolFile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Path is the file in use: the one the user chose, else the manifest's
	// default. Empty when there is neither.
	Path string `json:"path"`
	// Source is "configured" or "default".
	Source string `json:"source"`
	// Exists is whether there is a file there.
	Exists bool `json:"exists"`
}

// ToolStatus is whether a tool can be run for a cluster, and with what.
type ToolStatus struct {
	ID    string       `json:"id"`
	Label string       `json:"label"`
	Tool  toolcli.Tool `json:"tool"`
	Files []ToolFile   `json:"files"`
}

// ToolPlan is a command a page asked for, written out for the user to read
// before it runs.
type ToolPlan struct {
	// Display is the command line as a person would type it, files included.
	Display string `json:"display"`
}

// invocation is a tool ready to run for one cluster: where it is, and what
// every command is given -- the files' flags, and their environment.
type invocation struct {
	tool   plugins.UITool
	status ToolStatus
	always []string
	env    []string
}

func toolKey(pluginID, toolID string) string { return pluginID + "/" + toolID }

// prepareTool checks a plugin may use a tool and works out how it is run for
// a cluster. A tool that is not installed is an error only with ready set.
func (s *PluginService) prepareTool(contextID, pluginID, toolID string, ready bool) (invocation, error) {
	if s.server {
		return invocation{}, errDesktopOnly
	}
	plugin, ok := s.catalogue().Find(pluginID)
	if !ok {
		return invocation{}, fmt.Errorf("no plugin called %q is installed", pluginID)
	}
	if plugin.Disabled {
		return invocation{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}
	tool, ok := plugin.Tool(toolID)
	if !ok {
		return invocation{}, fmt.Errorf("the %s plugin does not declare a tool %q in \"ui\": {\"tools\": [...]}", plugin.Name, toolID)
	}
	kc, ok := s.configs.lookup(contextID)
	if !ok {
		return invocation{}, fmt.Errorf("unknown context %q -- it may have been removed from the kubeconfig", contextID)
	}

	inv := invocation{tool: tool, status: ToolStatus{ID: tool.ID, Label: tool.Label, Files: []ToolFile{}}}
	inv.status.Tool = toolcli.Locate(tool.Command, tool.Version)
	chosen := s.store.ToolFiles(kc.ID, toolKey(pluginID, toolID))
	for _, f := range tool.Files {
		file := ToolFile{ID: f.ID, Label: f.Label}
		if path := chosen[f.ID]; path != "" {
			file.Path, file.Source = path, "configured"
		} else if f.Default != "" {
			file.Path, file.Source = toolcli.ExpandHome(f.Default), "default"
		}
		if file.Path != "" {
			if info, err := os.Stat(file.Path); err == nil && !info.IsDir() {
				file.Exists = true
			}
		}
		// A default that is not there is left to the tool's own defaults;
		// a file the user chose is passed whether or not it is still there,
		// so the tool says what is wrong with it.
		if file.Path != "" && (file.Exists || file.Source == "configured") {
			if f.Flag != "" {
				inv.always = append(inv.always, f.Flag, file.Path)
			}
			if f.Env != "" {
				inv.env = append(inv.env, f.Env+"="+file.Path)
			}
		}
		inv.status.Files = append(inv.status.Files, file)
	}
	if ready && !inv.status.Tool.Found {
		return inv, fmt.Errorf("%s", inv.status.Tool.Reason)
	}
	return inv, nil
}

// ToolStatus says whether a plugin's tool is installed, and which files it is
// given for this cluster.
func (s *PluginService) ToolStatus(contextID, pluginID, toolID string) (ToolStatus, error) {
	inv, err := s.prepareTool(contextID, pluginID, toolID, false)
	return inv.status, err
}

// ToolChooseFile asks the user, in a native file dialog, which file one of a
// tool's files is for this cluster. Cancelling changes nothing.
func (s *PluginService) ToolChooseFile(contextID, pluginID, toolID, fileID string) (ToolStatus, error) {
	inv, err := s.prepareTool(contextID, pluginID, toolID, false)
	if err != nil {
		return ToolStatus{}, err
	}
	var label, current string
	for _, f := range inv.status.Files {
		if f.ID == fileID {
			label, current = f.Label, f.Path
		}
	}
	if label == "" {
		return ToolStatus{}, fmt.Errorf("%s has no file called %q", inv.tool.Label, fileID)
	}
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Choose the " + label + " for this cluster").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		ShowHiddenFiles(true)
	if current != "" {
		dialog.SetDirectory(filepath.Dir(current))
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return ToolStatus{}, err
	}
	if path != "" {
		if _, err := s.store.SetToolFile(contextID, toolKey(pluginID, toolID), fileID, path); err != nil {
			return ToolStatus{}, err
		}
	}
	return s.ToolStatus(contextID, pluginID, toolID)
}

// ToolForgetFile goes back to the manifest's default for one of a tool's files.
func (s *PluginService) ToolForgetFile(contextID, pluginID, toolID, fileID string) (ToolStatus, error) {
	if _, err := s.prepareTool(contextID, pluginID, toolID, false); err != nil {
		return ToolStatus{}, err
	}
	if _, err := s.store.SetToolFile(contextID, toolKey(pluginID, toolID), fileID, ""); err != nil {
		return ToolStatus{}, err
	}
	return s.ToolStatus(contextID, pluginID, toolID)
}

// ToolExec runs one of the tool's read commands and returns what it wrote.
func (s *PluginService) ToolExec(ctx context.Context, contextID, pluginID, toolID string, args []string) (toolcli.Output, error) {
	inv, err := s.prepareTool(contextID, pluginID, toolID, true)
	if err != nil {
		return toolcli.Output{}, err
	}
	if err := inv.tool.Allows(plugins.ToolRead, args); err != nil {
		return toolcli.Output{}, err
	}
	return toolcli.Run(ctx, inv.status.Tool.Path, append(args, inv.always...), inv.env)
}

// ToolPlan checks a command a page asks to run -- in the dock, or in the
// user's own terminal -- and writes it out for the confirmation. Nothing runs.
func (s *PluginService) ToolPlan(contextID, pluginID, toolID, how string, args []string) (ToolPlan, error) {
	if how != plugins.ToolRun && how != plugins.ToolInteractive {
		return ToolPlan{}, fmt.Errorf("%q is not a way to run a command", how)
	}
	inv, err := s.prepareTool(contextID, pluginID, toolID, true)
	if err != nil {
		return ToolPlan{}, err
	}
	if err := inv.tool.Allows(how, args); err != nil {
		return ToolPlan{}, err
	}
	return ToolPlan{Display: toolcli.Display(inv.tool.Command, append(args, inv.always...))}, nil
}

// checkDefaults refuses console defaults that are not flag and value pairs, or
// that would pass one of the tool's files.
func checkDefaults(tool plugins.UITool, defaults []string) error {
	if len(defaults)%2 != 0 {
		return fmt.Errorf("console defaults are flag and value pairs")
	}
	for i := 0; i < len(defaults); i += 2 {
		if !strings.HasPrefix(defaults[i], "-") {
			return fmt.Errorf("%q is not a flag", defaults[i])
		}
	}
	return tool.PassesNoFile(defaults)
}
