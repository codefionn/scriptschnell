//go:build windows

package actor

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

func signalProcessGroup(pgid int, signal string) error {
	_ = pgid
	_ = signal
	return syscall.EWINDOWS
}
