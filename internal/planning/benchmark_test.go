package planning

import (
	"context"
	"testing"
	
	"github.com/codefionn/scriptschnell/internal/session"
)

// BenchmarkPlanningAgent_SimplePlanning benchmarks basic planning performance
func BenchmarkPlanningAgent_SimplePlanning(b *testing.B) {
	mockFS := NewMockFileSystem()
	mockLLM := NewMockLLMClient(`{
  "plan": ["Step 1: Analyze", "Step 2: Design", "Step 3: Implement"],
  "complete": true
}`)
	
	sess := session.NewSession("benchmark", ".")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Simple benchmark task",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Plan(ctx, req, nil)
		if err != nil {
			b.Fatalf("Planning failed: %v", err)
		}
	}
}

// BenchmarkPlanningAgent_ComplexPlanning benchmarks planning with tool usage
func BenchmarkPlanningAgent_ComplexPlanning(b *testing.B) {
	mockFS := NewMockFileSystem()
	mockFS.AddFile("main.go", "package main\n\nfunc main() {}")
	mockFS.AddFile("config.yaml", "app:\n  name: test")
	
	mockLLM := NewMockLLMClient(
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"main.go\"}"}}]}`,
		`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "search_files", "arguments": "{\"pattern\": \"*.yaml\"}"}}]}`,
		`{
  "plan": [
    "Step 1: Analyze existing code",
    "Step 2: Design solution",
    "Step 3: Implement changes",
    "Step 4: Add tests",
    "Step 5: Update documentation"
  ],
  "complete": true
}`,
	)
	
	sess := session.NewSession("benchmark", ".")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Complex benchmark task with tools",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Plan(ctx, req, nil)
		if err != nil {
			b.Fatalf("Planning failed: %v", err)
		}
	}
}

// BenchmarkPlanningAgent_WithQuestions benchmarks planning with user interaction
func BenchmarkPlanningAgent_WithQuestions(b *testing.B) {
	mockFS := NewMockFileSystem()
	mockLLM := NewMockLLMClient(`{
  "plan": ["Step 1: Gather requirements"],
  "questions": ["What framework should we use?"],
  "needs_input": true,
  "complete": false
}`)
	
	sess := session.NewSession("benchmark", ".")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Interactive benchmark task",
		AllowQuestions: true,
		MaxQuestions:   3,
	}

	userInputCb := func(question string) (string, error) {
		return "User response", nil
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Plan(ctx, req, userInputCb)
		if err != nil {
			b.Fatalf("Planning failed: %v", err)
		}
	}
}

// BenchmarkPlanningAgent_PlanExtraction benchmarks plan extraction performance
func BenchmarkPlanningAgent_PlanExtraction(b *testing.B) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("benchmark", ".")
	mockLLM := NewMockLLMClient("dummy")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	testCases := []string{
		`{"plan": ["Step 1", "Step 2", "Step 3"], "complete": true}`,
		`<answer>{"plan": ["A", "B", "C", "D"], "complete": true}</answer>`,
		"1. First step\n2. Second step\n3. Third step\n4. Fourth step",
		"- Item one\n- Item two\n- Item three",
		"Simple single step plan",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testCase := testCases[i%len(testCases)]
		_ = agent.extractPlan(testCase)
	}
}

// BenchmarkPlanningAgent_ToolRegistry benchmarks tool registry performance
func BenchmarkPlanningAgent_ToolRegistry(b *testing.B) {
	registry := NewPlanningToolRegistry()
	
	// Register multiple tools
	tools := []PlanningTool{
		NewAskUserTool(),
		&MockPlanningTool{name: "read_file"},
		&MockPlanningTool{name: "search_files"},
		&MockPlanningTool{name: "search_file_content"},
		&MockPlanningTool{name: "codebase_investigator"},
	}
	
	for _, tool := range tools {
		registry.Register(tool)
	}

	params := map[string]interface{}{
		"question": "test question",
		"path":     "test.txt",
		"pattern":  "*.go",
		"objective": "test investigation",
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		toolName := []string{"ask_user", "read_file", "search_files", "search_file_content", "codebase_investigator"}[i%5]
		_ = registry.Execute(ctx, toolName, params)
	}
}

// BenchmarkPlanningAgent_JSONSchema benchmarks JSON schema generation
func BenchmarkPlanningAgent_JSONSchema(b *testing.B) {
	registry := NewPlanningToolRegistry()
	
	// Register multiple tools
	for i := 0; i < 10; i++ {
		registry.Register(NewAskUserTool())
		registry.Register(&MockPlanningTool{name: "read_file"})
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.ToJSONSchema()
	}
}

// BenchmarkPlanningAgent_ConcurrentRequests benchmarks concurrent planning performance
func BenchmarkPlanningAgent_ConcurrentRequests(b *testing.B) {
	mockFS := NewMockFileSystem()
	mockLLM := NewMockLLMClient(`{
  "plan": ["Concurrent step 1", "Concurrent step 2"],
  "complete": true
}`)
	
	sess := session.NewSession("benchmark", ".")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Concurrent benchmark task",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := agent.Plan(ctx, req, nil)
			if err != nil {
				b.Fatalf("Planning failed: %v", err)
			}
		}
	})
}

// BenchmarkPlanningAgent_LargeResponse benchmarks performance with large planning responses
func BenchmarkPlanningAgent_LargeResponse(b *testing.B) {
	mockFS := NewMockFileSystem()
	
	// Generate a large plan response
	largePlan := `{
  "plan": [`
	for i := 0; i < 50; i++ {
		largePlan += `"Step ` + string(rune('A'+i%26)) + `: Detailed planning step with lots of information and subtasks"`
		if i < 49 {
			largePlan += ","
		}
	}
	largePlan += `],
  "complete": true
}`
	
	mockLLM := NewMockLLMClient(largePlan)
	sess := session.NewSession("benchmark", ".")
	agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Large response benchmark task",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Plan(ctx, req, nil)
		if err != nil {
			b.Fatalf("Planning failed: %v", err)
		}
	}
}

// BenchmarkPlanningAgent_MemoryUsage benchmarks memory allocation patterns
func BenchmarkPlanningAgent_MemoryUsage(b *testing.B) {
	mockFS := NewMockFileSystem()
	mockLLM := NewMockLLMClient(`{
  "plan": ["Memory test step 1", "Memory test step 2", "Memory test step 3"],
  "complete": true
}`)
	
	b.ReportAllocs()
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		sess := session.NewSession("benchmark", ".")
		agent := NewPlanningAgent("benchmark-agent", mockFS, sess, mockLLM, nil)
		
		req := &PlanningRequest{
			Objective:      "Memory benchmark task",
			AllowQuestions: false,
			MaxQuestions:   0,
		}

		ctx := context.Background()
		_, err := agent.Plan(ctx, req, nil)
		if err != nil {
			b.Fatalf("Planning failed: %v", err)
		}
		
		agent.Close(ctx)
	}
}

// BenchmarkPlanningAgent_ToolExecution benchmarks individual tool execution performance
func BenchmarkPlanningAgent_ToolExecution(b *testing.B) {
	tests := []struct {
		name     string
		tool     PlanningTool
		params   map[string]interface{}
	}{
		{
			name:   "AskUserTool",
			tool:   NewAskUserTool(),
			params: map[string]interface{}{"question": "benchmark question"},
		},
		{
			name:   "SearchFilesTool",
			tool:   &MockPlanningTool{name: "search_files"},
			params: map[string]interface{}{"pattern": "*.go", "max_results": 10},
		},
		{
			name:   "SearchFileContentTool",
			tool:   &MockPlanningTool{name: "search_file_content"},
			params: map[string]interface{}{"pattern": "func", "max_results": 5},
		},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			ctx := context.Background()
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_ = test.tool.Execute(ctx, test.params)
			}
		})
	}
}

// BenchmarkPlanningAgent_FileSystemOperations benchmarks filesystem tool performance
func BenchmarkPlanningAgent_FileSystemOperations(b *testing.B) {
	mockFS := NewMockFileSystem()
	
	// Add test files
	for i := 0; i < 100; i++ {
		mockFS.AddFile(string(rune('a'+i%26))+".go", "package main\n\nfunc test"+string(rune('A'+i%26))+"() {}")
	}
	
	readTool := &MockPlanningTool{name: "read_file"}
	searchTool := &MockPlanningTool{name: "search_files"}
	contentTool := &MockPlanningTool{name: "search_file_content"}
	
	ctx := context.Background()
	
	b.Run("ReadFile", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filename := string(rune('a'+i%26)) + ".go"
			_ = readTool.Execute(ctx, map[string]interface{}{"path": filename})
		}
	})
	
	b.Run("SearchFiles", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = searchTool.Execute(ctx, map[string]interface{}{
				"pattern": "*.go",
				"max_results": 20,
			})
		}
	})
	
	b.Run("SearchContent", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = contentTool.Execute(ctx, map[string]interface{}{
				"pattern": "func",
				"max_results": 10,
			})
		}
	})
}