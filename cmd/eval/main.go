package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/codefionn/scriptschnell/internal/eval"
)

var (
	port           = flag.Int("port", 8080, "Port to run the web server on")
	dbPath         = flag.String("db", "", "Path to SQLite database (defaults to ~/.config/scriptschnell/eval.db)")
	evalDir        = flag.String("eval-dir", "internal/eval/definitions", "Directory containing eval definitions")
	logAssistant   = flag.Bool("log-assistant", false, "Log assistant messages to console as JSON")
)

func main() {
	flag.Parse()

	// Determine database path
	if *dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		configDir := filepath.Join(homeDir, ".config", "scriptschnell")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			log.Fatalf("Failed to create config directory: %v", err)
		}
		*dbPath = filepath.Join(configDir, "eval.db")
	}

	// Initialize eval service
	evalService, err := eval.NewService(*dbPath, *evalDir)
	if err != nil {
		log.Fatalf("Failed to initialize eval service: %v", err)
	}
	defer evalService.Close()

	// Set assistant logging option
	evalService.SetLogAssistant(*logAssistant)

	// Create server
	server := eval.NewServer(evalService, *port)

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("Starting eval web UI on http://localhost%s\n", addr)
	log.Printf("Database: %s", *dbPath)
	log.Printf("Eval directory: %s", *evalDir)

	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
