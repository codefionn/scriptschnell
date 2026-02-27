package tui

import (
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketutil"
)

// DetectSocketServer checks if a socket server is running at the configured path.
// Deprecated: Use socketutil.DetectSocketServer instead.
func DetectSocketServer(cfg *config.Config) bool {
	return socketutil.DetectSocketServer(cfg)
}

// ShouldUseSocketMode determines if TUI should use socket mode based on config and detection.
// Deprecated: Use socketutil.ShouldUseSocketMode instead.
func ShouldUseSocketMode(cfg *config.Config) bool {
	// TUI doesn't have a --no-socket flag, so pass false for noSocket parameter
	if !cfg.Socket.Enabled {
		logger.Info("Socket mode disabled in config")
		return false
	}

	// Check if auto-connect is enabled in config
	if !cfg.Socket.AutoConnect {
		logger.Debug("Socket auto-connect disabled in config")
		return false
	}

	// Auto-detect socket server
	if socketutil.DetectSocketServer(cfg) {
		return true
	}

	return false
}

// GetSocketDetectionInfo returns information about socket detection for logging.
// Deprecated: Use socketutil.GetSocketDetectionInfo instead.
func GetSocketDetectionInfo(cfg *config.Config) string {
	return socketutil.GetSocketDetectionInfo(cfg)
}
