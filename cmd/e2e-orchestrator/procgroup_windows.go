//go:build windows

package main

import "os/exec"

// setProcGroup is a no-op on Windows. exec.CommandContext already kills
// the direct child when the context is cancelled. Full grandchild cleanup
// on Windows requires job objects, which is out of scope here.
func setProcGroup(cmd *exec.Cmd) {
	_ = cmd
}
