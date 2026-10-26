//go:build windows

package procattr

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideAsksForNoWindow(t *testing.T) {
	cmd := exec.Command("git", "--version")
	Hide(cmd)
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("SysProcAttr = %+v, want HideWindow and CREATE_NO_WINDOW", cmd.SysProcAttr)
	}
}

func TestHideKeepsFlagsAlreadySet(t *testing.T) {
	cmd := exec.Command("git", "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	Hide(cmd)
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("Hide dropped a creation flag the caller had set")
	}
}
