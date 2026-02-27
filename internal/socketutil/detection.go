// Package socketutil provides shared utilities for socket server detection and connection.
package socketutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// SocketDetectionTimeout is how long to wait for socket detection
const SocketDetectionTimeout = 1 * time.Second

// DetectSocketServer checks if a socket server is running at the configured path.
// On Linux and other Unix-like systems, it performs the following checks:
//  1. Verifies socket path is configured
//  2. Expands ~ to home directory if present
//  3. Checks if socket file exists
//  4. Verifies the file is actually a socket
//  5. Attempts an actual connection to verify the server is responding
//
// On non-Unix platforms (e.g., Windows), this function always returns false
// as Unix domain sockets are not supported.
//
// Returns true if an active socket server is detected, false otherwise.
func DetectSocketServer(cfg *config.Config) bool {
	// Use platform-specific detection implementation
	return detectSocketServer(cfg)
}

// ShouldUseSocketMode determines if the application should use socket mode based on
// configuration and auto-detection.
//
// Parameters:
//   - cfg: The application configuration
//   - noSocket: If true, socket mode is explicitly disabled (e.g., via --no-socket flag)
//
// Returns true if socket mode should be used, false otherwise.
func ShouldUseSocketMode(cfg *config.Config, noSocket bool) bool {
	// Check if socket mode is explicitly disabled
	if noSocket {
		logger.Info("Socket mode disabled via flag")
		return false
	}

	// Check if socket mode is enabled in config
	if !cfg.Socket.Enabled {
		logger.Debug("Socket mode disabled in config")
		return false
	}

	// Check if auto-connect is enabled in config
	if !cfg.Socket.AutoConnect {
		logger.Debug("Socket auto-connect disabled in config")
		return false
	}

	// Auto-detect socket server (platform-specific)
	if DetectSocketServer(cfg) {
		return true
	}

	return false
}

// GetSocketDetectionInfo returns information about socket detection for logging.
// This provides a human-readable description of the socket configuration and
// detection status.
func GetSocketDetectionInfo(cfg *config.Config) string {
	socketPath := cfg.Socket.GetSocketPath()
	info := fmt.Sprintf("Socket path: %s", socketPath)

	if socketPath == "" {
		info += " (not configured)"
	} else {
		// Expand ~ for display
		displayPath := socketPath
		if len(socketPath) > 0 && socketPath[0] == '~' {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				displayPath = filepath.Join(homeDir, socketPath[1:])
			}
		}

		if _, err := os.Stat(displayPath); err != nil {
			if os.IsNotExist(err) {
				info += " (not found)"
			} else {
				info += fmt.Sprintf(" (error: %v)", err)
			}
		} else {
			if DetectSocketServer(cfg) {
				info += " (active server detected)"
			} else {
				info += " (exists but server not responding)"
			}
		}
	}

	return info
}

// ConnectToSocket attempts to connect to the socket server at the configured path.
// Returns the connected client or an error if connection fails.
func ConnectToSocket(cfg *config.Config) (*socketclient.Client, error) {
	socketPath := cfg.Socket.GetSocketPath()
	if socketPath == "" {
		return nil, fmt.Errorf("socket path not configured")
	}

	client, err := socketclient.NewClient(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), SocketDetectionTimeout)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to socket server: %w", err)
	}

	return client, nil
}
