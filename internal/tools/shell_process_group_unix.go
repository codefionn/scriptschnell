//go:build !windows

package tools

import (
	"fmt"
	"os/exec"
	"syscall"
)

// configureProcessGroup ensures the command runs in its own process group so
// that signals can be delivered to the entire tree (parent + children).
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func getProcessGroupID(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return 0
	}
	return pgid
}

func signalProcessGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return fmt.Errorf("invalid process group id: %d", pgid)
	}
	return syscall.Kill(-pgid, sig)
}
