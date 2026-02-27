//go:build linux || darwin

package socketutil

import (
	"context"
	"os"
	"path/filepath"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// detectSocketServerUnix performs socket server detection on Unix-like systems.
// It checks if a socket server is running at the configured path.
func detectSocketServerUnix(cfg *config.Config) bool {
	socketPath := cfg.Socket.GetSocketPath()

	// Check if socket path is configured
	if socketPath == "" {
		logger.Debug("No socket path configured, skipping detection")
		return false
	}

	// Expand ~ to home directory
	if len(socketPath) > 0 && socketPath[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logger.Warn("Failed to expand ~ for socket path: %v", err)
			return false
		}
		socketPath = filepath.Join(homeDir, socketPath[1:])
	}

	// Check if socket file exists
	stat, err := os.Stat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("Socket file does not exist: %s", socketPath)
		} else {
			logger.Debug("Error checking socket file: %v", err)
		}
		return false
	}

	// Check if it's a socket
	if stat.Mode()&os.ModeSocket == 0 {
		logger.Debug("File exists but is not a socket: %s", socketPath)
		return false
	}

	// Try to connect to verify the server is actually running
	ctx, cancel := context.WithTimeout(context.Background(), SocketDetectionTimeout)
	defer cancel()

	client, err := socketclient.NewClient(socketPath)
	if err != nil {
		logger.Debug("Failed to create socket client: %v", err)
		return false
	}
	defer client.Close()

	if err := client.Connect(ctx); err != nil {
		logger.Debug("Socket exists but connection failed: %v", err)
		return false
	}

	logger.Info("Detected active socket server at: %s", socketPath)
	return true
}

// detectSocketServer is the platform-specific implementation for Unix-like systems.
func detectSocketServer(cfg *config.Config) bool {
	return detectSocketServerUnix(cfg)
}
