//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the child in its own process group so that signals
// (like Ctrl-C) propagate to grandchildren too, and so that the group
// can be cleaned up as a unit.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
