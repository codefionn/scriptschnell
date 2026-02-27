//go:build !windows

package lockfile

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

// isProcessRunning checks if a process with the given PID is running (Unix)
func isProcessRunning(pid int) (bool, string) {
	// Unix-like systems: Try to send signal 0 (doesn't actually send a signal)
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, "process not found"
	}

	// Check if we can signal the process
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return false, "process has finished"
		}
		// Permission error might mean process exists but we don't have access
		if strings.Contains(err.Error(), "operation not permitted") {
			return true, ""
		}
		return false, "cannot signal process"
	}

	return true, ""
}
