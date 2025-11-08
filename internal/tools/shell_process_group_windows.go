//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	// Process groups are handled differently on Windows.
	// We leave the command configuration untouched.
	_ = cmd
}

func getProcessGroupID(cmd *exec.Cmd) int {
	return 0
}

func signalProcessGroup(pgid int, sig syscall.Signal) error {
	_ = pgid
	_ = sig
	return syscall.EWINDOWS
}
