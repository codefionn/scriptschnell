package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/eval/templates"
	"github.com/julienschmidt/httprouter"
)

// Server provides the HTTP interface for the eval web UI
type Server struct {
	service *Service
	port    int
	server  *http.Server
	router  *httprouter.Router
}

// NewServer creates a new eval server
func NewServer(service *Service, port int) *Server {
	s := &Server{
		service: service,
		port:    port,
		router:  httprouter.New(),
	}

	s.setupRoutes()
	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: s.router,
	}

	log.Printf("Starting server on port %d", s.port)
	return s.server.ListenAndServe()
}

// Stop stops the HTTP server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.server.Shutdown(ctx)
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	// Serve static files (CSS, JS)
	s.router.ServeFiles("/static/*filepath", http.Dir("templates/static"))

	// Main routes
	s.router.GET("/", s.handleIndex)
	s.router.GET("/health", s.handleHealth)

	// Setup routes
	s.router.GET("/setup", s.handleSetup)
	s.router.POST("/setup", s.handleSetupSubmit)

	// Model management
	s.router.GET("/models", s.handleModels)
	s.router.POST("/models/refresh", s.handleModelsRefresh)
	s.router.GET("/models/search", s.handleModelsSearch)
	s.router.POST("/models/search", s.handleModelsSearch)
	s.router.POST("/models/select", s.handleModelSelect)
	s.router.POST("/models/deselect", s.handleModelDeselect)

	// Eval definitions
	s.router.POST("/evals-search", s.handleEvalsSearch)
	s.router.GET("/evals", s.handleEvals)
	s.router.POST("/evals/:id/run", s.handleEvalRun)
	s.router.GET("/evals/:id/status", s.handleEvalStatus)

	// Results
	s.router.GET("/results", s.handleResults)
	s.router.GET("/results/:run_id", s.handleResultDetail)
}

// Additional handlers for model management
func (s *Server) handleModelsSearch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var searchQuery string

	// Support both GET (query parameter) and POST (form value) methods
	if r.Method == "GET" {
		searchQuery = r.URL.Query().Get("q")
	} else {
		searchQuery = r.FormValue("search")
	}

	// Get all models first
	allModels, err := s.service.GetModels()
	if err != nil {
		// Create error template response
		props := templates.SearchResultsProps{
			Models:      []templates.Model{},
			SearchQuery: searchQuery,
			Total:       0,
			Error:       fmt.Sprintf("Failed to get models: %v", err),
		}
		w.Header().Set("Content-Type", "text/html")
		if renderErr := templates.SearchResults(props).Render(r.Context(), w); renderErr != nil {
			// Fallback to plain error if template fails
			http.Error(w, props.Error, http.StatusInternalServerError)
		}
		return
	}

	// Filter models based on search query
	var filteredModels []*EvalModel
	if searchQuery == "" {
		filteredModels = allModels
	} else {
		searchLower := strings.ToLower(strings.TrimSpace(searchQuery))
		for _, model := range allModels {
			// Search in model ID, name, provider, and description
			if strings.Contains(strings.ToLower(model.ID), searchLower) ||
				strings.Contains(strings.ToLower(model.Name), searchLower) ||
				strings.Contains(strings.ToLower(model.Provider), searchLower) ||
				strings.Contains(strings.ToLower(model.Description), searchLower) {
				filteredModels = append(filteredModels, model)
			}
		}
	}

	// Convert to template models
	templateModels := make([]templates.Model, len(filteredModels))
	for i, model := range filteredModels {
		templateModels[i] = templates.Model{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
			Selected:      model.Selected,
		}
	}

	// Render using the new SearchResults template
	props := templates.SearchResultsProps{
		Models:      templateModels,
		SearchQuery: searchQuery,
		Total:       len(allModels),
		Error:       "",
	}

	w.Header().Set("Content-Type", "text/html")

	// Check if this is an HTMX request or a direct browser navigation
	isHTMX := r.Header.Get("HX-Request") != ""

	if isHTMX {
		// For HTMX requests, return only the SearchResults component
		if err := templates.SearchResults(props).Render(r.Context(), w); err != nil {
			http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// For direct browser navigation, wrap in full layout with search form
		layoutBody := templates.SearchResultsPage(props)
		layoutProps := templates.LayoutProps{
			Title:      "Search Models - LLM Eval",
			ActivePage: "models",
			Body:       layoutBody,
		}
		if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
			http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
			return
		}
	}
}

func (s *Server) handleEvalsSearch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	searchQuery := r.FormValue("search")

	// Get all evals first
	allEvals, err := s.service.GetEvals()
	if err != nil {
		// Create error template response
		props := templates.EvalsSearchResultsProps{
			Evals:       []templates.EvalDefinition{},
			SearchQuery: searchQuery,
			Total:       0,
			Error:       fmt.Sprintf("Failed to get evals: %v", err),
		}
		w.Header().Set("Content-Type", "text/html")
		if renderErr := templates.EvalsSearchResults(props).Render(r.Context(), w); renderErr != nil {
			// Fallback to plain error if template fails
			http.Error(w, props.Error, http.StatusInternalServerError)
		}
		return
	}

	// Filter evals based on search query
	var filteredEvals []*EvalDefinition
	if searchQuery == "" {
		for _, eval := range allEvals {
			filteredEvals = append(filteredEvals, eval)
		}
	} else {
		searchLower := strings.ToLower(searchQuery)
		for _, eval := range allEvals {
			if strings.Contains(strings.ToLower(eval.ID), searchLower) ||
				strings.Contains(strings.ToLower(eval.Name), searchLower) ||
				strings.Contains(strings.ToLower(eval.Description), searchLower) {
				filteredEvals = append(filteredEvals, eval)
			}
		}
	}

	// Convert to template evals
	templateEvals := make([]templates.EvalDefinition, len(filteredEvals))
	for i, eval := range filteredEvals {
		templateEvals[i] = templates.EvalDefinition{
			ID:       eval.ID,
			Name:     eval.Name,
			Desc:     eval.Description, // Map Description to Desc
			RunFile:  eval.RunFile,
			CLITests: convertCLITestCases(eval.CLITests),
		}
	}

	// Get selected models for eval cards
	selectedModels, err := s.service.GetSelectedModels()
	if err != nil {
		// Create error template response
		props := templates.EvalsSearchResultsProps{
			Evals:       templateEvals,
			SearchQuery: searchQuery,
			Total:       len(allEvals),
			Error:       fmt.Sprintf("Failed to get selected models: %v", err),
		}
		w.Header().Set("Content-Type", "text/html")
		if renderErr := templates.EvalsSearchResults(props).Render(r.Context(), w); renderErr != nil {
			http.Error(w, props.Error, http.StatusInternalServerError)
		}
		return
	}

	// Convert selected models to template models
	templateModels := make([]templates.Model, len(selectedModels))
	for i, model := range selectedModels {
		templateModels[i] = templates.Model{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
			Selected:      model.Selected,
		}
	}

	// Render using the EvalsSearchResults template
	props := templates.EvalsSearchResultsProps{
		Evals:       templateEvals,
		SearchQuery: searchQuery,
		Total:       len(allEvals),
		Error:       "",
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.EvalsSearchResults(props).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleModelsRefresh(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	models, err := s.service.RefreshModels()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to refresh models: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to template models
	templateModels := make([]templates.Model, len(models))
	for i, model := range models {
		templateModels[i] = templates.Model{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
			Selected:      model.Selected,
		}
	}

	// Create props for the SearchResults template (empty search query shows all models)
	props := templates.SearchResultsProps{
		Models:      templateModels,
		SearchQuery: "",
		Total:       len(models),
		Error:       "",
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.SearchResults(props).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleModelSelect(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	modelID := r.FormValue("model_id")
	if modelID == "" {
		http.Error(w, "Model ID is required", http.StatusBadRequest)
		return
	}

	if err := s.service.SelectModel(modelID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to select model: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the updated button HTML showing "Selected" state
	w.Header().Set("Content-Type", "text/html")
	sanitizedID := sanitizeID(modelID)
	fmt.Fprintf(w, `<div class="ml-4 flex-shrink-0" id="model-button-%s">
		<button
			hx-post="/models/deselect"
			hx-vals='{"model_id": "%s"}'
			hx-target="#model-button-%s"
			hx-swap="outerHTML"
			class="px-4 py-2 rounded-md font-medium text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 bg-green-600 text-white hover:bg-green-700 focus:ring-green-500"
		>
			Selected ✓
		</button>
	</div>`, sanitizedID, modelID, sanitizedID)
}

func (s *Server) handleModelDeselect(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	modelID := r.FormValue("model_id")
	if modelID == "" {
		http.Error(w, "Model ID is required", http.StatusBadRequest)
		return
	}

	if err := s.service.DeselectModel(modelID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to deselect model: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the updated button HTML showing "Select" state
	w.Header().Set("Content-Type", "text/html")
	sanitizedID := sanitizeID(modelID)
	fmt.Fprintf(w, `<div class="ml-4 flex-shrink-0" id="model-button-%s">
		<button
			hx-post="/models/select"
			hx-vals='{"model_id": "%s"}'
			hx-target="#model-button-%s"
			hx-swap="outerHTML"
			class="px-4 py-2 rounded-md font-medium text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 bg-blue-600 text-white hover:bg-blue-700 focus:ring-blue-500"
		>
			Select
		</button>
	</div>`, sanitizedID, modelID, sanitizedID)
}

func (s *Server) handleEvalRun(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	evalID := ps.ByName("id")
	if evalID == "" {
		http.Error(w, "Eval ID is required", http.StatusBadRequest)
		return
	}

	runs, err := s.service.RunEval(evalID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to run eval: %v", err), http.StatusInternalServerError)
		return
	}

	// Collect run IDs for polling
	var runIDs []int64
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}

	w.Header().Set("Content-Type", "text/html")
	// Return a loading message that polls for completion
	// We'll poll every 2 seconds and check if all runs are completed
	fmt.Fprintf(w, `<div
		id="eval-status-%s"
		hx-get="/evals/%s/status?runs=%v"
		hx-trigger="load delay:2s"
		hx-swap="outerHTML"
		class="bg-blue-50 border border-blue-200 rounded-lg p-4"
	>
		<div class="flex items-center text-blue-800">
			<svg class="animate-spin -ml-1 mr-3 h-5 w-5 text-blue-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
				<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
				<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
			</svg>
			<div>
				<div class="font-medium">Running evaluation...</div>
				<div class="text-sm">%d runs in progress for %s</div>
			</div>
		</div>
	</div>`, evalID, evalID, runIDs, len(runs), evalID)
}

func (s *Server) handleEvalStatus(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	evalID := ps.ByName("id")
	if evalID == "" {
		http.Error(w, "Eval ID is required", http.StatusBadRequest)
		return
	}

	// Get runs for this eval
	runs, err := s.service.GetEvalRuns(evalID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get eval runs: %v", err), http.StatusInternalServerError)
		return
	}

	if len(runs) == 0 {
		http.Error(w, "No runs found for this eval", http.StatusNotFound)
		return
	}

	// Check if all runs are completed
	allCompleted := true
	anyFailed := false
	completedCount := 0
	failedCount := 0

	for _, run := range runs {
		if run.Status == "completed" {
			completedCount++
		} else if run.Status == "failed" {
			failedCount++
			anyFailed = true
		} else {
			allCompleted = false
		}
	}

	w.Header().Set("Content-Type", "text/html")

	if allCompleted {
		// All runs completed - show success message
		successCount := completedCount
		if anyFailed {
			fmt.Fprintf(w, `<div class="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
				<div class="text-yellow-800">
					<div class="font-medium">⚠️ Evaluation completed with some failures</div>
					<div class="text-sm mt-1">
						%d of %d runs completed successfully, %d failed.
						<a href="/results" class="text-yellow-700 underline ml-2">View detailed results</a>
					</div>
				</div>
			</div>`, successCount, len(runs), failedCount)
		} else {
			fmt.Fprintf(w, `<div class="bg-green-50 border border-green-200 rounded-lg p-4">
				<div class="text-green-800">
					<div class="font-medium">✓ Evaluation completed successfully!</div>
					<div class="text-sm mt-1">
						All %d runs completed for %s.
						<a href="/results" class="text-green-700 underline ml-2">View detailed results</a>
					</div>
				</div>
			</div>`, len(runs), evalID)
		}
	} else {
		// Still running - continue polling
		runningCount := len(runs) - completedCount - failedCount
		fmt.Fprintf(w, `<div
			id="eval-status-%s"
			hx-get="/evals/%s/status"
			hx-trigger="load delay:2s"
			hx-swap="outerHTML"
			class="bg-blue-50 border border-blue-200 rounded-lg p-4"
		>
			<div class="flex items-center text-blue-800">
				<svg class="animate-spin -ml-1 mr-3 h-5 w-5 text-blue-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<div>
					<div class="font-medium">Running evaluation...</div>
					<div class="text-sm">%d running, %d completed, %d failed of %d total runs</div>
				</div>
			</div>
		</div>`, evalID, evalID, runningCount, completedCount, failedCount, len(runs))
	}
}

func (s *Server) handleResultDetail(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	runIDStr := ps.ByName("run_id")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil {
		errorProps := templates.DetailedResultsProps{
			Error: fmt.Sprintf("Invalid run ID format: %s", runIDStr),
		}
		layoutProps := templates.LayoutProps{
			Title:      "Error - LLM Eval",
			ActivePage: "",
			Body:       templates.DetailedResults(errorProps),
		}
		w.Header().Set("Content-Type", "text/html")
		templates.Layout(layoutProps).Render(r.Context(), w)
		return
	}

	// Get the run details
	run, err := s.service.db.GetEvalRun(runID)
	if err != nil {
		errorProps := templates.DetailedResultsProps{
			Error: fmt.Sprintf("Could not find run with ID: %d", runID),
		}
		layoutProps := templates.LayoutProps{
			Title:      "Error - LLM Eval",
			ActivePage: "",
			Body:       templates.DetailedResults(errorProps),
		}
		w.Header().Set("Content-Type", "text/html")
		templates.Layout(layoutProps).Render(r.Context(), w)
		return
	}

	results, err := s.service.GetEvalResults(runID)
	if err != nil {
		errorProps := templates.DetailedResultsProps{
			Error: fmt.Sprintf("Could not load results for run %d: %v", runID, err),
		}
		layoutProps := templates.LayoutProps{
			Title:      "Error - LLM Eval",
			ActivePage: "",
			Body:       templates.DetailedResults(errorProps),
		}
		w.Header().Set("Content-Type", "text/html")
		templates.Layout(layoutProps).Render(r.Context(), w)
		return
	}

	// Calculate summary statistics
	totalTests := len(results)
	passedTests := 0

	for _, result := range results {
		if result.Passed {
			passedTests++
		}
	}

	passRate := 0.0
	if totalTests > 0 {
		passRate = float64(passedTests) / float64(totalTests) * 100
	}

	// Convert to template test results
	templateResults := make([]templates.TestResult, 0, len(results))
	for _, result := range results {
		templateResults = append(templateResults, templates.TestResult{
			RunID:            runID,
			TestCaseID:       result.TestCaseID,
			Passed:           result.Passed,
			ExpectedOutput:   result.ExpectedOutput,
			ActualOutput:     result.ActualOutput,
			ContainerName:    result.ContainerName,
			ErrorInfo:        result.Errors,
			RawOutput:        result.RawOutput,
			DetailedExecInfo: result.DetailedExecutionInfo,
			ExecutionTime:    result.ExecutionTime,
		})
	}

	detailedProps := templates.DetailedResultsProps{
		RunID:         runID,
		EvalID:        run.EvalID,
		ModelID:       run.ModelID,
		StartedAt:     run.StartedAt.Format("2006-01-02 15:04:05"),
		Status:        run.Status,
		AgentOutput:   run.AgentOutput,
		AgentExitCode: run.AgentExitCode,
		TotalTests:    totalTests,
		PassedTests:   passedTests,
		PassRate:      passRate,
		TestResults:   templateResults,
		Error:         "",
	}

	layoutProps := templates.LayoutProps{
		Title:      fmt.Sprintf("Results #%d - LLM Eval", runID),
		ActivePage: "results",
		Body:       templates.DetailedResults(detailedProps),
	}

	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// Handlers implementation will go here...

// handleIndex serves the main dashboard
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.service.GetConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get config: %v", err), http.StatusInternalServerError)
		return
	}

	// If no OpenRouter token is configured, redirect to setup
	if config.OpenRouterToken == "" {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	// Get dashboard statistics
	stats, err := s.service.GetEvalStats("")
	if err != nil {
		stats = []EvalStats{}
	}

	// Calculate totals
	totalRuns := len(stats)
	var totalPassed, totalTests int
	var totalTokens int64
	var totalCost float64

	for _, stat := range stats {
		totalPassed += stat.PassedCount
		totalTests += stat.TotalCount
		totalTokens += int64(stat.TotalTokens)
		totalCost += stat.TotalCost
	}

	var passRate float64
	if totalTests > 0 {
		passRate = float64(totalPassed) / float64(totalTests) * 100
	}

	// Render dashboard using templates
	dashboardProps := templates.DashboardProps{
		Stats: templates.DashboardStats{
			TotalRuns:  totalRuns,
			PassRate:   passRate,
			TokensUsed: totalTokens,
			TotalCost:  totalCost,
		},
	}

	layoutProps := templates.LayoutProps{
		Title:      "Dashboard - LLM Eval",
		ActivePage: "dashboard",
		Body:       templates.Dashboard(dashboardProps),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// API handlers
func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.service.GetConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func (s *Server) handleAPIUpdateConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var config EvalConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.service.SetConfig(&config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func (s *Server) handleAPIModels(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	models, err := s.service.GetModels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func (s *Server) handleAPIEvals(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	eevals, err := s.service.GetEvals()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(eevals)
}

func (s *Server) handleAPIResults(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	stats, err := s.service.GetEvalStats("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleAPIResultDetail(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	runIDStr := ps.ByName("run_id")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}
	results, err := s.service.GetEvalResults(runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleSetup shows the setup form
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.service.GetConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get config: %v", err), http.StatusInternalServerError)
		return
	}

	setupProps := templates.SetupProps{
		Token: config.OpenRouterToken,
	}

	layoutProps := templates.LayoutProps{
		Title:      "Setup - LLM Eval",
		ActivePage: "setup",
		Body:       templates.Setup(setupProps),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleSetupSubmit processes the setup form
func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	token := r.FormValue("token")
	if token == "" {
		http.Error(w, "Token is required", http.StatusBadRequest)
		return
	}

	config := &EvalConfig{OpenRouterToken: token}
	if err := s.service.SetConfig(config); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect to main page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleModels shows the models management page
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Check if there's a search query in the URL
	searchQuery := r.URL.Query().Get("q")

	// If there's a search query, redirect to the search endpoint
	if searchQuery != "" {
		http.Redirect(w, r, "/models/search?q="+searchQuery, http.StatusTemporaryRedirect)
		return
	}

	models, err := s.service.GetModels()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get models: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to template models
	templateModels := make([]templates.Model, len(models))
	for i, model := range models {
		templateModels[i] = templates.Model{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
			Selected:      model.Selected,
		}
	}

	// Create props for the Models template
	props := templates.ModelsProps{
		Models: templateModels,
		Error:  "",
	}

	// Render the Models template within a layout
	w.Header().Set("Content-Type", "text/html")
	layoutProps := templates.LayoutProps{
		Title:      "Models - LLM Eval",
		ActivePage: "models",
		Body:       templates.Models(props),
	}

	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleEvals shows the available evaluations
func (s *Server) handleEvals(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	evals, err := s.service.GetEvals()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get evals: %v", err), http.StatusInternalServerError)
		return
	}

	selectedModels, err := s.service.GetSelectedModels()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get selected models: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to template evals
	templateEvals := make([]templates.EvalDefinition, 0, len(evals))
	for _, evalDef := range evals {
		templateEvals = append(templateEvals, templates.EvalDefinition{
			ID:       evalDef.ID,
			Name:     evalDef.Name,
			Desc:     evalDef.Description,
			RunFile:  evalDef.RunFile,
			CLITests: convertCLITestCases(evalDef.CLITests),
		})
	}

	// Convert to template models
	templateModels := make([]templates.Model, len(selectedModels))
	for i, model := range selectedModels {
		templateModels[i] = templates.Model{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
			Selected:      model.Selected,
		}
	}

	evalsProps := templates.EvalsProps{
		Evals:          templateEvals,
		SelectedModels: templateModels,
		Error:          "",
	}

	layoutProps := templates.LayoutProps{
		Title:      "Evaluations - LLM Eval",
		ActivePage: "evals",
		Body:       templates.Evals(evalsProps),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleResults shows evaluation results
func (s *Server) handleResults(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	stats, err := s.service.GetEvalStats("")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get results: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to template stats
	templateStats := make([]templates.EvalStat, 0, len(stats))
	for _, stat := range stats {
		templateStats = append(templateStats, templates.EvalStat{
			RunID:       int(stat.RunID),
			EvalID:      stat.EvalID,
			ModelID:     stat.ModelID,
			Status:      stat.Status,
			PassedCount: stat.PassedCount,
			TotalCount:  stat.TotalCount,
			PassRate:    stat.PassRate,
			TotalTokens: int64(stat.TotalTokens),
			TotalCost:   stat.TotalCost,
		})
	}

	resultsProps := templates.ResultsProps{
		Stats: templateStats,
		Error: "",
	}

	layoutProps := templates.LayoutProps{
		Title:      "Results - LLM Eval",
		ActivePage: "results",
		Body:       templates.Results(resultsProps),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.Layout(layoutProps).Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// Additional handlers will be implemented...

// convertCLITestCases converts from internal CLITestCase to template CLITest
func convertCLITestCases(testCases []CLITestCase) []templates.CLITest {
	result := make([]templates.CLITest, len(testCases))
	for i, tc := range testCases {
		result[i] = templates.CLITest{
			ID:          tc.ID,
			Name:        tc.ID, // Use ID as name for now
			Description: tc.Description,
			Command:     strings.Join(tc.Args, " "),
			Expected:    tc.Expected,
			Timeout:     tc.Timeout,
		}
	}
	return result
}

// maskToken masks the API token for display
func maskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

// sanitizeID replaces characters that are invalid in CSS selectors
func sanitizeID(id string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		".", "-",
		":", "-",
		"@", "-",
		" ", "-",
		"[", "-",
		"]", "-",
		"(", "-",
		")", "-",
	)
	return replacer.Replace(id)
}
