package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TestPlanningAgent_Integration_WithRealTools tests the planning agent with actual tool execution
func TestPlanningAgent_Integration_WithRealTools(t *testing.T) {
	// Setup mock filesystem with realistic project structure
	mockFS := NewMockFileSystem()
	mockFS.AddFile("go.mod", "module github.com/example/test\n\ngo 1.21\n\nrequire github.com/gin-gonic/gin v1.9.0")
	mockFS.AddFile("main.go", "package main\n\nimport \"github.com/gin-gonic/gin\"\n\nfunc main() {\n\tr := gin.Default()\n\tr.GET(\"/health\", func(c *gin.Context) {\n\t\tc.JSON(200, gin.H{\"status\": \"ok\"})\n\t})\n\tr.Run(\":8080\")\n}")
	mockFS.AddFile("README.md", "# Test API\n\nA simple REST API built with Gin.\n\n## Endpoints\n\n- GET /health - Health check")
	mockFS.AddFile("config.yaml", "server:\n  port: 8080\n  host: localhost\nlogging:\n  level: info")

	// Mock LLM that simulates realistic planning behavior
	mockLLM := NewMockLLMClient(
		// First response: Use tools to investigate codebase
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"go.mod\"}"}}]}`,
		// Second response: Continue investigation
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"main.go\"}"}}]}`,
		// Third response: Generate final plan
		`<answer>{
  "plan": [
    "Step 1: Analyze existing Go project structure and dependencies",
    "Step 2: Review current API implementation in main.go",
    "Step 3: Design logging integration strategy",
    "Step 4: Add structured logging middleware",
    "Step 5: Update configuration to support logging levels",
    "Step 6: Add logging to existing endpoints",
    "Step 7: Write tests for logging functionality",
    "Step 8: Update documentation"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("integration-test", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "Add comprehensive logging to this Go REST API",
		Context:        "The API currently has basic endpoints but no logging",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify comprehensive plan was generated
	if !response.Complete {
		t.Error("Expected plan to be complete")
	}
	if response.NeedsInput {
		t.Error("Expected plan to not need input")
	}
	if len(response.Plan) < 5 {
		t.Errorf("Expected at least 5 plan steps, got %d", len(response.Plan))
	}

	// Verify plan contains relevant steps
	planContent := strings.Join(response.Plan, " ")
	expectedKeywords := []string{"logging", "API", "test", "documentation"}
	for _, keyword := range expectedKeywords {
		if !strings.Contains(strings.ToLower(planContent), strings.ToLower(keyword)) {
			t.Errorf("Expected plan to contain keyword '%s'", keyword)
		}
	}

	// Cleanup
	agent.Close(ctx)
}

// TestPlanningAgent_Integration_WithQuestions tests interactive planning with user questions
func TestPlanningAgent_Integration_WithQuestions(t *testing.T) {
	mockFS := NewMockFileSystem()
	mockFS.AddFile("package.json", `{"name": "test-app", "version": "1.0.0"}`)

	// Mock LLM that asks questions before planning using tool calls
	mockLLM := NewMockLLMClient(
		// First response: Use ask_user tool to ask questions
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"What type of database do you want to use?\"}"}}]}`,
		// Second response: Continue with more questions
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"Do you need authentication and authorization?\"}"}}]}`,
		// Third response: Generate final plan
		`<answer>{
  "plan": [
    "Step 1: Set up PostgreSQL database connection",
    "Step 2: Install required dependencies (pgx, gin-jwt)",
    "Step 3: Create database models and migrations",
    "Step 4: Implement JWT authentication middleware",
    "Step 5: Create user management endpoints",
    "Step 6: Add authorization checks to existing routes",
    "Step 7: Write database and API tests",
    "Step 8: Add connection pooling and error handling"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("interactive-test", mockFS, sess, mockLLM, nil)

	// Track questions asked
	var questionsAsked []string
	userInputCb := func(question string) (string, error) {
		questionsAsked = append(questionsAsked, question)
		// Simulate user responses
		if strings.Contains(question, "database") {
			return "PostgreSQL", nil
		}
		if strings.Contains(question, "authentication") {
			return "Yes, JWT authentication needed", nil
		}
		return "User response", nil
	}

	req := &PlanningRequest{
		Objective:      "Add database and authentication to this Node.js application",
		Context:        "Currently has basic package.json but no backend",
		AllowQuestions: true,
		MaxQuestions:   5,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, userInputCb)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify questions were asked
	if len(questionsAsked) == 0 {
		t.Error("Expected questions to be asked")
	}
	if len(questionsAsked) > 3 {
		t.Errorf("Expected at most 3 questions, got %d", len(questionsAsked))
	}

	// Verify final plan was generated
	if !response.Complete {
		t.Error("Expected final plan to be complete")
	}
	if len(response.Plan) < 5 {
		t.Errorf("Expected at least 5 plan steps, got %d", len(response.Plan))
	}

	// Cleanup
	agent.Close(ctx)
}

// TestPlanningAgent_Integration_ToolChaining tests complex tool usage scenarios
func TestPlanningAgent_Integration_ToolChaining(t *testing.T) {
	mockFS := NewMockFileSystem()

	// Create a realistic project structure
	files := map[string]string{
		"Dockerfile":                     "FROM golang:1.21-alpine\nWORKDIR /app\nCOPY . .\nRUN go build\nCMD [\"./app\"]",
		"internal/server/server.go":      "package server\n\nimport \"fmt\"\n\ntype Server struct {\n\tport int\n}\n\nfunc NewServer(port int) *Server {\n\treturn &Server{port: port}\n}\n\nfunc (s *Server) Start() error {\n\treturn fmt.Errorf(\"not implemented\")\n}",
		"internal/server/server_test.go": "package server\n\nimport \"testing\"\n\nfunc TestNewServer(t *testing.T) {\n\ts := NewServer(8080)\n\tif s.port != 8080 {\n\t\tt.Errorf(\"Expected port 8080, got %d\", s.port)\n\t}\n}",
		"Makefile":                       "build:\n\tgo build -o app ./cmd/main.go\n\ntest:\n\tgo test ./...\n\ndocker:\n\tdocker build -t test-app .",
	}

	for path, content := range files {
		mockFS.AddFile(path, content)
	}

	// Mock LLM that uses multiple tools to investigate
	mockLLM := NewMockLLMClient(
		// First: Search for Go files
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "search_files", "arguments": "{\"pattern\": \"**/*.go\"}"}}]}`,
		// Second: Read main server file
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"internal/server/server.go\"}"}}]}`,
		// Third: Search for test files
		`{"tool_calls": [{"id": "call_3", "type": "function", "function": {"name": "search_files", "arguments": "{\"pattern\": \"**/*_test.go\"}"}}]}`,
		// Fourth: Investigate codebase structure
		`{"tool_calls": [{"id": "call_4", "type": "function", "function": {"name": "codebase_investigator", "arguments": "{\"objective\": \"understand the project architecture and testing setup\"}"}}]}`,
		// Fifth: Generate comprehensive plan
		`<answer>{
  "plan": [
    "Step 1: Complete the server implementation in internal/server/server.go",
    "Step 2: Add HTTP handlers and routing logic",
    "Step 3: Implement graceful shutdown and error handling",
    "Step 4: Enhance test coverage in server_test.go",
    "Step 5: Add integration tests for the full HTTP server",
    "Step 6: Update Dockerfile for proper multistage build",
    "Step 7: Create docker-compose.yml for development",
    "Step 8: Update Makefile with new targets",
    "Step 9: Add configuration management",
    "Step 10: Write API documentation"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("tool-chaining-test", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "Complete this Go web server project and make it production-ready",
		Context:        "The project has basic structure but incomplete implementation",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify comprehensive plan
	if !response.Complete {
		t.Error("Expected plan to be complete")
	}
	if len(response.Plan) < 8 {
		t.Errorf("Expected at least 8 plan steps for production-ready, got %d", len(response.Plan))
	}

	// Verify plan covers different aspects
	planContent := strings.Join(response.Plan, " ")
	aspects := []string{"server", "test", "docker", "documentation"}
	for _, aspect := range aspects {
		if !strings.Contains(strings.ToLower(planContent), aspect) {
			t.Errorf("Expected plan to cover aspect: %s", aspect)
		}
	}

	// Cleanup
	agent.Close(ctx)
}

// TestPlanningAgent_Integration_ErrorHandling tests error scenarios in integration
func TestPlanningAgent_Integration_ErrorHandling(t *testing.T) {
	mockFS := NewMockFileSystem()
	// Don't add any files to simulate empty project

	// Mock LLM that encounters tool errors
	mockLLM := NewMockLLMClient(
		// First: Try to read a file that doesn't exist
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"main.go\"}"}}]}`,
		// Second: Try to search files (returns empty)
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "search_files", "arguments": "{\"pattern\": \"*.go\"}"}}]}`,
		// Third: Generate plan despite errors
		`<answer>{
  "plan": [
    "Step 1: Initialize new Go project with go.mod",
    "Step 2: Create main.go with basic server structure",
    "Step 3: Add basic HTTP endpoints",
    "Step 4: Set up project directory structure",
    "Step 5: Add initial tests and documentation"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("error-handling-test", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "Create a new Go web service from scratch",
		Context:        "Starting with empty directory",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Should still generate a plan despite tool errors
	if !response.Complete {
		t.Error("Expected plan to be complete despite tool errors")
	}
	if len(response.Plan) == 0 {
		t.Error("Expected plan steps even with tool errors")
	}

	// Verify plan is appropriate for empty project
	planContent := strings.Join(response.Plan, " ")
	if !strings.Contains(strings.ToLower(planContent), "initialize") {
		t.Error("Expected plan to include project initialization")
	}

	// Cleanup
	agent.Close(ctx)
}

// TestPlanningAgent_Integration_ConcurrentRequests tests concurrent planning requests
func TestPlanningAgent_Integration_ConcurrentRequests(t *testing.T) {
	mockFS := NewMockFileSystem()
	mockFS.AddFile("main.go", "package main\n\nfunc main() {}")

	mockLLM := NewMockLLMClient(
		`<answer>{"plan": ["Step 1: Analyze code"], "complete": true}</answer>`,
		`<answer>{"plan": ["Step 2: Design solution"], "complete": true}</answer>`,
		`<answer>{"plan": ["Step 3: Implement"], "complete": true}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("concurrent-test", mockFS, sess, mockLLM, nil)

	ctx := context.Background()

	// Run multiple planning requests concurrently
	const numRequests = 3
	results := make(chan *PlanningResponse, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &PlanningRequest{
				Objective:      "Test objective " + string(rune('A'+id)),
				AllowQuestions: false,
				MaxQuestions:   0,
			}
			response, err := agent.Plan(ctx, req, nil)
			if err != nil {
				errors <- err
			} else {
				results <- response
			}
		}(i)
	}

	// Collect results
	var responses []*PlanningResponse
	for i := 0; i < numRequests; i++ {
		select {
		case response := <-results:
			responses = append(responses, response)
		case err := <-errors:
			t.Errorf("Concurrent planning failed: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent planning results")
		}
	}

	// Verify all requests completed
	if len(responses) != numRequests {
		t.Errorf("Expected %d responses, got %d", numRequests, len(responses))
	}

	for i, response := range responses {
		if !response.Complete {
			t.Errorf("Response %d should be complete", i)
		}
		if len(response.Plan) == 0 {
			t.Errorf("Response %d should have plan steps", i)
		}
	}

	// Cleanup
	agent.Close(ctx)
}

// TestPlanningAgent_Integration_LargeProject tests planning with larger codebase
func TestPlanningAgent_Integration_LargeProject(t *testing.T) {
	mockFS := NewMockFileSystem()

	// Simulate a larger project with many files
	largeFiles := map[string]string{
		"cmd/api/main.go":                           "package main\n\nfunc main() { startServer() }",
		"internal/auth/jwt.go":                      "package auth\n\ntype JWT struct { secret string }",
		"internal/auth/middleware.go":               "package auth\n\nfunc AuthMiddleware() {}",
		"internal/database/postgres.go":             "package database\n\ntype Postgres struct { conn string }",
		"internal/database/migrations/001_init.sql": "CREATE TABLE users (id SERIAL);",
		"internal/handlers/user.go":                 "package handlers\n\ntype UserHandler struct { db Database }",
		"internal/handlers/auth.go":                 "package handlers\n\ntype AuthHandler struct {}",
		"internal/models/user.go":                   "package models\n\ntype User struct { ID int }",
		"internal/config/config.go":                 "package config\n\ntype Config struct { Port int }",
		"pkg/utils/hash.go":                         "package utils\n\nfunc Hash(s string) string { return s }",
		"pkg/utils/validation.go":                   "package utils\n\nfunc Validate(s string) bool { return true }",
		"tests/integration/api_test.go":             "package integration\n\nfunc TestAPI() {}",
		"tests/unit/auth_test.go":                   "package auth\n\nfunc TestJWT() {}",
		"docs/api.yaml":                             "openapi: 3.0.0\ninfo:\n  title: Test API",
		"scripts/migrate.sh":                        "#!/bin/bash\necho 'Running migrations'",
		"deploy/docker/Dockerfile":                  "FROM golang:1.21",
		"deploy/k8s/deployment.yaml":                "apiVersion: apps/v1\nkind: Deployment",
	}

	for path, content := range largeFiles {
		mockFS.AddFile(path, content)
	}

	// Mock LLM that investigates the large project
	mockLLM := NewMockLLMClient(
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "search_files", "arguments": "{\"pattern\": \"**/*.go\", \"max_results\": 20}"}}]}`,
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "codebase_investigator", "arguments": "{\"objective\": \"analyze this large Go microservice project structure\"}"}}]}`,
		`<answer>{
  "plan": [
    "Step 1: Review existing microservice architecture and identify gaps",
    "Step 2: Complete database layer implementation with proper connection pooling",
    "Step 3: Implement missing authentication and authorization features",
    "Step 4: Add comprehensive API handlers for all CRUD operations",
    "Step 5: Enhance middleware for logging, metrics, and error handling",
    "Step 6: Complete unit and integration test coverage",
    "Step 7: Set up CI/CD pipeline with automated testing",
    "Step 8: Optimize Docker images for production deployment",
    "Step 9: Configure Kubernetes deployment with health checks",
    "Step 10: Add monitoring and observability (Prometheus, Grafana)",
    "Step 11: Implement rate limiting and caching strategies",
    "Step 12: Create comprehensive API documentation and examples"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}</answer>`,
	)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("large-project-test", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "Complete this large Go microservice project and make it production-ready",
		Context:        "This is a complex microservice with multiple components",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify comprehensive plan for large project
	if !response.Complete {
		t.Error("Expected plan to be complete")
	}
	if len(response.Plan) < 10 {
		t.Errorf("Expected at least 10 plan steps for large project, got %d", len(response.Plan))
	}

	// Verify plan covers enterprise-level concerns
	planContent := strings.Join(response.Plan, " ")
	enterpriseConcerns := []string{"production", "monitoring", "deployment", "testing", "documentation"}
	for _, concern := range enterpriseConcerns {
		if !strings.Contains(strings.ToLower(planContent), concern) {
			t.Errorf("Expected enterprise plan to include: %s", concern)
		}
	}

	// Cleanup
	agent.Close(ctx)
}
