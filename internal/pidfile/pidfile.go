// Package pidfile provides PID file management for daemon processes
package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Pidfile represents a PID file
type Pidfile struct {
	path string
}

// New creates a new PID file instance
func New(path string) *Pidfile {
	return &Pidfile{
		path: path,
	}
}

// Write writes the current PID to the PID file
func (p *Pidfile) Write() error {
	// Ensure directory exists
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create pidfile directory: %w", err)
	}

	// Create and write PID file
	pid := os.Getpid()
	content := strconv.Itoa(pid)

	if err := os.WriteFile(p.path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write pidfile: %w", err)
	}

	return nil
}

// Read reads the PID from the PID file
func (p *Pidfile) Read() (int, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return 0, fmt.Errorf("failed to read pidfile: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in pidfile: %w", err)
	}

	return pid, nil
}

// Remove removes the PID file
func (p *Pidfile) Remove() error {
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pidfile: %w", err)
	}
	return nil
}

// Path returns the PID file path
func (p *Pidfile) Path() string {
	return p.path
}

// Exists checks if the PID file exists
func (p *Pidfile) Exists() bool {
	_, err := os.Stat(p.path)
	return !os.IsNotExist(err)
}
