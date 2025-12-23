package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EvalServer manages the evaluation system and exposes an HTTP API.
type EvalServer struct {
	config      *EvalConfig
	evalRunner  *EvalRunner
	evalContext context.Context
	evalCancel  context.CancelFunc
	evalRunning bool
	evalMux     sync.RWMutex
	httpMux     *http.ServeMux
}

// NewEvalServer constructs a server using the provided configuration.
func NewEvalServer(config *EvalConfig) (*EvalServer, error) {
	if config == nil {
		return nil, fmt.Errorf("eval config is required")
	}

	runner, err := NewEvalRunner(config)
	if err != nil {
		return nil, fmt.Errorf("could not create eval runner: %w", err)
	}

	server := &EvalServer{
		config:     config,
		evalRunner: runner,
		httpMux:    http.NewServeMux(),
	}
	server.registerRoutes()
	return server, nil
}

// NewEvalServerFromPath loads configuration from disk and builds a server.
func NewEvalServerFromPath(configPath string) (*EvalServer, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return NewEvalServer(config)
}

// registerRoutes attaches the HTTP handlers to the server mux.
func (s *EvalServer) registerRoutes() {
	s.httpMux.HandleFunc("/api/eval/start", s.handleEvalStart)
	s.httpMux.HandleFunc("/api/eval/stop", s.handleEvalStop)
	s.httpMux.HandleFunc("/api/eval/status", s.handleEvalStatus)
	s.httpMux.HandleFunc("/api/eval/results", s.handleEvalResults)
	s.httpMux.HandleFunc("/api/eval/summary", s.handleEvalSummary)
}

// Start launches the HTTP server on the provided port.
func (s *EvalServer) Start(port string) error {
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting eval-tests server on port %s", port)
	log.Printf("Open http://localhost:%s/api/eval/status to access the API", port)

	return http.ListenAndServe(":"+port, s.httpMux)
}

// Handler exposes the server mux for embedding or testing.
func (s *EvalServer) Handler() http.Handler {
	return s.httpMux
}

// HTTP Handlers
func (s *EvalServer) handleEvalStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.evalMux.Lock()
	defer s.evalMux.Unlock()

	if s.evalRunning {
		http.Error(w, "Evaluation is already running", http.StatusConflict)
		return
	}

	// Rebuild the runner for a fresh evaluation run.
	runner, err := NewEvalRunner(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create eval runner: %v", err), http.StatusInternalServerError)
		return
	}
	s.evalRunner = runner

	// Create context and start evaluation
	s.evalContext, s.evalCancel = context.WithCancel(context.Background())
	s.evalRunning = true

	go func(ctx context.Context) {
		defer func() {
			s.evalMux.Lock()
			s.evalRunning = false
			s.evalMux.Unlock()
		}()

		if err := runner.Run(ctx); err != nil {
			log.Printf("Evaluation failed: %v", err)
		}
	}(s.evalContext)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Evaluation started",
	})
}

func (s *EvalServer) handleEvalStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.evalMux.Lock()
	defer s.evalMux.Unlock()

	if !s.evalRunning {
		http.Error(w, "No evaluation is running", http.StatusNotFound)
		return
	}

	if s.evalCancel != nil {
		s.evalCancel()
	}

	s.evalRunning = false

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Evaluation stopped",
	})
}

func (s *EvalServer) handleEvalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.evalMux.RLock()
	running := s.evalRunning
	s.evalMux.RUnlock()

	if s.evalRunner == nil {
		http.Error(w, "Evaluation runner is not initialized", http.StatusInternalServerError)
		return
	}

	status := s.evalRunner.GetProgress()
	statusData := map[string]interface{}{
		"running":  running,
		"progress": status,
		"config": map[string]interface{}{
			"models":     s.config.Models,
			"test_cases": s.config.TestCases,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusData)
}

func (s *EvalServer) handleEvalResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.evalMux.RLock()
	running := s.evalRunning
	s.evalMux.RUnlock()

	if running {
		http.Error(w, "Evaluation is still running", http.StatusConflict)
		return
	}

	if s.evalRunner == nil {
		http.Error(w, "Evaluation runner is not initialized", http.StatusInternalServerError)
		return
	}

	results := s.evalRunner.GetResults()

	// Parse query parameters
	format := r.URL.Query().Get("format")
	download := r.URL.Query().Get("download") == "true"

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		if download {
			w.Header().Set("Content-Disposition", "attachment; filename=eval_results.csv")
		}

		// Write CSV header
		fmt.Fprintf(w, "model_id,model_name,test_case_id,test_case_name,category,success,error,start_time,end_time,duration_ms,prompt_tokens,completion_tokens,total_tokens,prompt_cost,completion_cost,total_cost,currency,temperature,max_tokens,response\n")

		// Write data rows
		for _, result := range results {
			errorStr := result.Error
			if errorStr != "" {
				errorStr = `"` + strings.ReplaceAll(errorStr, `"`, `""`) + `"`
			}

			response := result.Response
			if response != "" {
				response = `"` + strings.ReplaceAll(response, `"`, `""`) + `"`
			}

			fmt.Fprintf(w, "%s,%s,%s,%s,%s,%t,%s,%s,%s,%d,%d,%d,%d,%.6f,%.6f,%.6f,%s,%.1f,%d,%s\n",
				result.ModelID, result.ModelName, result.TestCaseID,
				result.TestCaseName, result.TestCategory, result.Success, errorStr,
				result.StartTime.Format(time.RFC3339),
				result.EndTime.Format(time.RFC3339),
				result.Duration.Milliseconds(),
				result.PromptTokens, result.CompletionTokens, result.TotalTokens,
				result.PromptCost, result.CompletionCost, result.TotalCost, result.Currency,
				result.Temperature, result.MaxTokens, response)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if download {
		w.Header().Set("Content-Disposition", "attachment; filename=eval_results.json")
	}
	_ = json.NewEncoder(w).Encode(results)
}

func (s *EvalServer) handleEvalSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.evalMux.RLock()
	running := s.evalRunning
	s.evalMux.RUnlock()

	if s.evalRunner == nil {
		http.Error(w, "Evaluation runner is not initialized", http.StatusInternalServerError)
		return
	}

	summary := s.evalRunner.GenerateSummary()
	summaryData := map[string]interface{}{
		"running": running,
		"summary": summary,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summaryData)
}
