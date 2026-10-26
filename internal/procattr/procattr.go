// Package procattr sets how the processes the app starts in the background
// are created.
//
// The app is a GUI program, so on Windows it has no console of its own, and
// every console program it starts -- git, helm, a plugin's tool -- would be
// given a new console window that flashes up for as long as the command runs.
// A plugin install runs git several times, and a page asking a tool for
// status runs it every refresh, so those windows pile up. Hide stops that.
package procattr

import "os/exec"

// Hide keeps cmd from opening a window of its own. Its output still reaches
// whatever cmd.Stdout and cmd.Stderr are set to. Call it before Start, Run or
// Output. It does nothing outside Windows.
func Hide(cmd *exec.Cmd) {
	hide(cmd)
}
