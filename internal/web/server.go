package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/gorilla/websocket"
)

//go:embed static/*
var StaticFiles embed.FS

const (
	defaultPort     = 8936
	authTokenLength = 32
)

// Server represents the web server
type Server struct {
	addr            string
	authToken       string
	httpServer      *http.Server
	cfg             *config.Config
	providerMgr     *provider.Manager
	secretsPassword string
	broker          *MessageBroker
	hub             *Hub
	quitChan        chan struct{}
	debug           bool
}

// NewServer creates a new web server
func NewServer(ctx context.Context, cfg *config.Config, providerMgr *provider.Manager, secretsPassword string, debug bool) (*Server, error) {
	// Ensure .js files are served with correct MIME type
	if err := mime.AddExtensionType(".js", "application/javascript"); err != nil {
		logger.Warn("Failed to register .js MIME type: %v", err)
	}

	// Generate auth token
	token, err := generateAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate auth token: %w", err)
	}

	// Create message broker
	broker := NewMessageBroker()

	// Create hub for WebSocket connections
	hub := NewHub()

	srv := &Server{
		addr:            fmt.Sprintf("localhost:%d", defaultPort),
		authToken:       token,
		cfg:             cfg,
		providerMgr:     providerMgr,
		secretsPassword: secretsPassword,
		broker:          broker,
		hub:             hub,
		quitChan:        make(chan struct{}),
		debug:           debug,
	}

	return srv, nil
}

// Start starts the web server
func (s *Server) Start() error {
	// Register MIME type for JavaScript files
	if err := mime.AddExtensionType(".js", "application/javascript"); err != nil {
		logger.Warn("Failed to register .js MIME type: %v", err)
	} else {
		logger.Info("Successfully registered .js MIME type as application/javascript")
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Serve static files from embedded filesystem with explicit MIME type handling
	fileServer := http.FileServer(http.FS(StaticFiles))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set Content-Type header explicitly for .js files
		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript")
		}
		fileServer.ServeHTTP(w, r)
	}))

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Page routes
	mux.HandleFunc("/", s.handleIndex)

	// HTTP server
	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	// Start hub
	go s.hub.Run()

	// Start server in background
	go func() {
		logger.Info("Web server listening on %s", s.addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the web server
func (s *Server) Stop() error {
	logger.Info("Stopping web server...")

	// Stop hub
	s.hub.Stop()

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	return nil
}

// GetURL returns the server URL with auth token
func (s *Server) GetURL() string {
	return fmt.Sprintf("http://%s/?token=%s", s.addr, s.authToken)
}

// OpenBrowser opens the default browser to the server URL
func (s *Server) OpenBrowser() error {
	url := s.GetURL()
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Validate auth token
	queryToken := r.URL.Query().Get("token")
	if queryToken != s.authToken {
		logger.Warn("WebSocket connection rejected: invalid auth token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for local development
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade WebSocket: %v", err)
		return
	}

	// Create client
	client := NewClient(s.hub, conn, s.broker, s.cfg, s.providerMgr, s.secretsPassword, s.debug)

	// Register client
	s.hub.Register(client)

	// Start client
	go client.WritePump()
	go client.ReadPump()
}

// handleIndex handles the index page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Validate auth token
	queryToken := r.URL.Query().Get("token")
	if queryToken != s.authToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Render templ page
	page := Page(s.authToken, "scriptschnell")
	if err := page.Render(r.Context(), w); err != nil {
		logger.Error("Failed to render page: %v", err)
	}
}

// generateAuthToken generates a random auth token
func generateAuthToken() (string, error) {
	bytes := make([]byte, authTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
