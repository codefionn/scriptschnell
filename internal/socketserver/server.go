package socketserver

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/securemem"
)

// Server represents the Unix socket server
type Server struct {
	cfg              *config.Config
	hub              *Hub
	broker           *MessageBroker
	sessionManager   *SessionManager
	workspaceManager *WorkspaceManager
	listener         net.Listener

	// Dependencies (set via SetDependencies)
	providerMgr     *provider.Manager
	secretsPassword *securemem.String

	// Connection tracking
	connMu    sync.RWMutex
	clients   map[string]*Client
	connCount int
	maxConns  int

	// Control
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
	stopOnce sync.Once

	// Connection ID counter
	connIDCounter int
	connIDMu      sync.Mutex
}

// NewServer creates a new Unix socket server
func NewServer(cfg *config.Config) (*Server, error) {
	server := &Server{
		cfg:      cfg,
		hub:      NewHub(),
		clients:  make(map[string]*Client),
		stopChan: make(chan struct{}),
		maxConns: 10, // Default max connections
	}

	// Load socket configuration from config
	if cfg.Socket.MaxConnections > 0 {
		server.maxConns = cfg.Socket.MaxConnections
	}

	// Create session manager
	sessionMgr, err := NewSessionManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	server.sessionManager = sessionMgr

	// Create workspace manager
	workspaceMgr, err := NewWorkspaceManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace manager: %w", err)
	}
	server.workspaceManager = workspaceMgr

	// Create broker
	server.broker = NewMessageBroker()

	return server, nil
}

// Start starts the Unix socket server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	s.running = true
	s.mu.Unlock()

	// Get socket path
	socketPath := s.cfg.Socket.GetSocketPath()
	if socketPath == "" {
		return fmt.Errorf("socket path is not configured")
	}

	// Expand and validate socket path
	absPath, err := s.prepareSocketPath(socketPath)
	if err != nil {
		return fmt.Errorf("failed to prepare socket path: %w", err)
	}

	// Remove existing socket file if it exists
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket file: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", absPath)
	if err != nil {
		return fmt.Errorf("failed to listen on Unix socket %s: %w", absPath, err)
	}
	s.listener = listener

	// Set socket file permissions if specified
	if s.cfg.Socket.Permissions != "" {
		if err := os.Chmod(absPath, parseFileMode(s.cfg.Socket.Permissions)); err != nil {
			logger.Warn("Failed to set socket permissions: %v", err)
		} else {
			logger.Info("Socket permissions set to %s", s.cfg.Socket.Permissions)
		}
	}

	// Start hub in background
	go s.hub.Run()

	// Start connection accept loop
	go s.acceptLoop(ctx)

	logger.Info("Unix socket server started on %s (max connections: %d)", absPath, s.maxConns)

	return nil
}

// Stop stops the Unix socket server
func (s *Server) Stop() error {
	s.stopOnce.Do(func() {
		logger.Info("Stopping Unix socket server...")

		// Signal all goroutines to stop
		close(s.stopChan)

		// Shutdown session manager
		if s.sessionManager != nil {
			s.sessionManager.Shutdown()
		}

		// Shutdown workspace manager
		if s.workspaceManager != nil {
			s.workspaceManager.Shutdown()
		}

		// Shutdown hub
		s.hub.Shutdown()

		// Close listener
		if s.listener != nil {
			if err := s.listener.Close(); err != nil {
				logger.Error("Error closing socket listener: %v", err)
			}
		}

		// Wait a bit for connections to close gracefully
		time.Sleep(100 * time.Millisecond)

		// Clean up socket file
		socketPath := s.cfg.Socket.GetSocketPath()
		absPath, err := filepath.Abs(socketPath)
		if err == nil {
			if removeErr := os.Remove(absPath); removeErr != nil && !os.IsNotExist(removeErr) {
				logger.Warn("Failed to remove socket file %s: %v", absPath, removeErr)
			} else if removeErr == nil {
				logger.Info("Socket file removed: %s", absPath)
			}
		} else {
			logger.Warn("Failed to resolve socket path for cleanup: %v", err)
		}

		s.mu.Lock()
		s.running = false
		s.mu.Unlock()

		logger.Info("Unix socket server stopped")
	})

	return nil
}

// prepareSocketPath expands and validates the socket path
func (s *Server) prepareSocketPath(socketPath string) (string, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(socketPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
	}

	return absPath, nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("Accept loop stopped via context cancellation")
			return

		case <-s.stopChan:
			logger.Info("Accept loop stopped via stop signal")
			return

		default:
			// Set accept timeout to allow checking stopChan periodically
			if s.listener != nil {
				s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(1 * time.Second))
			}

			conn, err := s.listener.Accept()
			if err != nil {
				// Check if this is a timeout (expected)
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}

				// Check if listener was closed
				if isClosedError(err) {
					logger.Info("Listener closed, exiting accept loop")
					return
				}

				logger.Error("Error accepting connection: %v", err)
				continue
			}

			// Check connection limit
			if !s.checkConnectionLimit() {
				logger.Warn("Connection limit reached, rejecting connection from %s", conn.RemoteAddr())
				conn.Close()
				continue
			}

			// Create and start client handler
			clientID := s.generateConnectionID()
			client := NewClient(clientID, conn, s.hub, s.sessionManager, s.workspaceManager, s.broker)

			// Track client
			s.trackClient(clientID, client)

			// Start client
			client.Start()

			logger.Info("New connection accepted: %s (total: %d)", clientID, s.hub.GetClientCount())
		}
	}
}

// checkConnectionLimit checks if we can accept more connections
func (s *Server) checkConnectionLimit() bool {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.connCount < s.maxConns
}

// trackClient adds a client to tracking
func (s *Server) trackClient(clientID string, client *Client) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	s.clients[clientID] = client
	s.connCount++
}

// untrackClient removes a client from tracking
func (s *Server) untrackClient(clientID string) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if _, ok := s.clients[clientID]; ok {
		delete(s.clients, clientID)
		s.connCount--
	}
}

// generateConnectionID generates a unique connection ID
func (s *Server) generateConnectionID() string {
	s.connIDMu.Lock()
	defer s.connIDMu.Unlock()

	s.connIDCounter++
	return fmt.Sprintf("conn_%d", s.connIDCounter)
}

// GetClientCount returns the number of connected clients
func (s *Server) GetClientCount() int {
	return s.hub.GetClientCount()
}

// GetSessionCount returns the number of active sessions
func (s *Server) GetSessionCount() int {
	return s.hub.GetSessionCount()
}

// GetClient retrieves a client by ID
func (s *Server) GetClient(clientID string) (*Client, bool) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	client, ok := s.clients[clientID]
	return client, ok
}

// Broadcast sends a message to all connected clients
func (s *Server) Broadcast(msg *BaseMessage) {
	s.hub.broadcast <- msg
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// parseFileMode parses an octal file mode string
func parseFileMode(modeStr string) os.FileMode {
	var mode uint64
	_, err := fmt.Sscanf(modeStr, "%o", &mode)
	if err != nil {
		return 0600 // Default to rw-------
	}
	return os.FileMode(mode)
}

// isClosedError checks if an error indicates a closed listener
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "use of closed network connection"
}

// GetHub returns the hub instance (for testing and internal use)
func (s *Server) GetHub() *Hub {
	return s.hub
}

// SetDependencies sets external dependencies (provider manager, secrets password)
func (s *Server) SetDependencies(providerMgr *provider.Manager, secretsPassword *securemem.String) {
	s.providerMgr = providerMgr
	s.secretsPassword = secretsPassword

	// Set dependencies on broker
	if s.broker != nil {
		s.broker.SetDependencies(providerMgr, secretsPassword, s.cfg)
	}
}
