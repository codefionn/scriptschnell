//go:build windows

package lockfile

import (
	"syscall"
)

// isProcessRunning checks if a process with the given PID is running (Windows)
func isProcessRunning(pid int) (bool, string) {
	// Windows: Try to open the process
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, "process not found"
	}
	syscall.CloseHandle(handle)
	return true, ""
}