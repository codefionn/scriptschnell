// Package lockfile provides file-based locking for single instance enforcement
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	ErrLockAcquired = errors.New("lock already acquired")
	ErrLocked       = errors.New("process is already running")
)

// Lockfile represents a file-based lock
type Lockfile struct {
	path   string
	file   *os.File
	pid    int
	locked bool
}

// New creates a new lockfile instance
func New(path string) *Lockfile {
	return &Lockfile{
		path: path,
	}
}

// TryAcquire attempts to acquire the lock
func (l *Lockfile) TryAcquire() error {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create lockfile directory: %w", err)
	}

	// Try to create and open the file exclusively
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		// File already exists, check if it's stale
		if os.IsExist(err) {
			// Try to read the existing lockfile
			if stale, reason, lockErr := l.checkStale(); lockErr == nil && stale {
				// Lockfile is stale, remove it and try again
				if removeErr := os.Remove(l.path); removeErr != nil {
					return fmt.Errorf("failed to remove stale lockfile (%s): %w", reason, removeErr)
				}
				// Retry acquiring the lock
				file, err = os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
				if err != nil {
					return fmt.Errorf("failed to create lockfile after removing stale one: %w", err)
				}
				// Retry succeeded, proceed with setting up the lock
				l.file = file
				l.pid = os.Getpid()
				l.locked = true

				// Write our PID and timestamp to the lockfile
				timestamp := time.Now().Format(time.RFC3339)
				content := fmt.Sprintf("%d\n%s\n", l.pid, timestamp)
				if _, err := l.file.WriteString(content); err != nil {
					l.Release()
					return fmt.Errorf("failed to write to lockfile: %w", err)
				}

				// Sync to ensure data is written to disk
				if err := l.file.Sync(); err != nil {
					l.Release()
					return fmt.Errorf("failed to sync lockfile: %w", err)
				}

				return nil
			} else if lockErr != nil {
				return fmt.Errorf("failed to check lockfile staleness: %w", lockErr)
			} else {
				// Lockfile is valid, process is still running
				return fmt.Errorf("%w: %s", ErrLocked, reason)
			}
		}
		return fmt.Errorf("failed to create lockfile: %w", err)
	}

	l.file = file
	l.pid = os.Getpid()
	l.locked = true

	// Write our PID and timestamp to the lockfile
	timestamp := time.Now().Format(time.RFC3339)
	content := fmt.Sprintf("%d\n%s\n", l.pid, timestamp)
	if _, err := l.file.WriteString(content); err != nil {
		l.Release()
		return fmt.Errorf("failed to write to lockfile: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := l.file.Sync(); err != nil {
		l.Release()
		return fmt.Errorf("failed to sync lockfile: %w", err)
	}

	return nil
}

// checkStale checks if the lockfile is stale (process not running)
func (l *Lockfile) checkStale() (bool, string, error) {
	// Read existing lockfile
	data, err := os.ReadFile(l.path)
	if err != nil {
		// Can't read the lockfile, assume it's corrupted and stale
		return true, "cannot read lockfile", nil
	}

	// Parse PID and timestamp
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		return true, "invalid lockfile format", nil
	}

	pidStr := strings.TrimSpace(lines[0])
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return true, "invalid PID in lockfile", nil
	}

	// Check if process is running
	running, reason := isProcessRunning(pid)
	if !running {
		return true, reason, nil
	}

	// Check timestamp if available (stale after 1 hour)
	if len(lines) >= 2 {
		timestampStr := strings.TrimSpace(lines[1])
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err == nil {
			if time.Since(timestamp) > time.Hour {
				return true, "lockfile is older than 1 hour", nil
			}
		}
	}

	return false, fmt.Sprintf("process with PID %d is running", pid), nil
}

// Release releases the lock
func (l *Lockfile) Release() error {
	if !l.locked {
		return nil
	}

	var err error
	if l.file != nil {
		if closeErr := l.file.Close(); closeErr != nil {
			err = closeErr
		}
		l.file = nil
	}

	// Remove the lockfile
	if removeErr := os.Remove(l.path); removeErr != nil && !os.IsNotExist(removeErr) {
		if err != nil {
			err = fmt.Errorf("%v; failed to remove lockfile: %w", err, removeErr)
		} else {
			err = fmt.Errorf("failed to remove lockfile: %w", removeErr)
		}
	}

	l.locked = false
	return err
}

// PID returns the PID that acquired the lock
func (l *Lockfile) PID() int {
	return l.pid
}

// Locked returns true if the lock is held
func (l *Lockfile) Locked() bool {
	return l.locked
}

// Path returns the lockfile path
func (l *Lockfile) Path() string {
	return l.path
}
