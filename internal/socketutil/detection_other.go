//go:build !linux && !darwin

package socketutil

import (
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// detectSocketServer is the platform-specific implementation for non-Unix systems.
// On Windows and other non-Unix platforms, Unix sockets are not supported,
// so this always returns false.
func detectSocketServer(cfg *config.Config) bool {
	logger.Debug("Socket server detection skipped: Unix sockets not supported on this platform")
	return false
}
