package tools

import (
	"context"
	"testing"
)

// Test types defined at package level
type testToolSpec struct{}

func (s *testToolSpec) Name() string        { return "test_tool" }
func (s *testToolSpec) Description() string { return "A test tool" }
func (s *testToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type":        "string",
				"description": "Test input",
			},
		},
		"required": []string{"input"},
	}
}

type testToolExecutor struct {
	dependency string
}

func (e *testToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	input := GetStringParam(params, "input", "")
	return &ToolResult{
		Result: map[string]interface{}{
			"input":      input,
			"dependency": e.dependency,
		},
	}
}

// Legacy tool type for testing backward compatibility
type legacyTestTool struct{}

func (t *legacyTestTool) Name() string        { return "legacy_tool" }
func (t *legacyTestTool) Description() string { return "Legacy tool" }
func (t *legacyTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *legacyTestTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return &ToolResult{Result: "legacy"}
}

// TestToolSpecPattern verifies the new ToolSpec + ToolFactory pattern works correctly
func TestToolSpecPattern(t *testing.T) {

	// Create a factory
	testFactory := func(dependency string) ToolFactory {
		return func(reg *Registry) ToolExecutor {
			return &testToolExecutor{dependency: dependency}
		}
	}

	// Test 1: Register spec with factory
	t.Run("RegisterSpec", func(t *testing.T) {
		registry := NewRegistry(nil)
		spec := &testToolSpec{}
		factory := testFactory("test-dep-1")

		registry.RegisterSpec(spec, factory)

		// Verify spec is registered
		executor, ok := registry.GetExecutor("test_tool")
		if !ok {
			t.Fatal("Expected executor to be registered")
		}
		if executor == nil {
			t.Fatal("Expected executor to be non-nil")
		}
	})

	// Test 2: Execute with spec/factory pattern
	t.Run("Execute", func(t *testing.T) {
		registry := NewRegistry(nil)
		spec := &testToolSpec{}
		factory := testFactory("test-dep-2")

		registry.RegisterSpec(spec, factory)

		call := &ToolCall{
			ID:   "test-call-1",
			Name: "test_tool",
			Parameters: map[string]interface{}{
				"input": "hello",
			},
		}

		result := registry.Execute(context.Background(), call)
		if result.Error != "" {
			t.Fatalf("Expected no error, got: %s", result.Error)
		}

		resultMap, ok := result.Result.(map[string]interface{})
		if !ok {
			t.Fatal("Expected result to be map")
		}

		if resultMap["input"] != "hello" {
			t.Errorf("Expected input=hello, got: %v", resultMap["input"])
		}
		if resultMap["dependency"] != "test-dep-2" {
			t.Errorf("Expected dependency=test-dep-2, got: %v", resultMap["dependency"])
		}
	})

	// Test 3: JSON schema generation
	t.Run("ToJSONSchema", func(t *testing.T) {
		registry := NewRegistry(nil)
		spec := &testToolSpec{}
		factory := testFactory("test-dep-3")

		registry.RegisterSpec(spec, factory)

		schemas := registry.ToJSONSchema()
		if len(schemas) != 1 {
			t.Fatalf("Expected 1 schema, got: %d", len(schemas))
		}

		schema := schemas[0]
		if schema["type"] != "function" {
			t.Errorf("Expected type=function, got: %v", schema["type"])
		}

		function, ok := schema["function"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected function to be map")
		}

		if function["name"] != "test_tool" {
			t.Errorf("Expected name=test_tool, got: %v", function["name"])
		}
		if function["description"] != "A test tool" {
			t.Errorf("Expected description=A test tool, got: %v", function["description"])
		}
	})

	// Test 4: Multiple executors with same spec but different dependencies
	t.Run("MultipleExecutorsWithSameSpec", func(t *testing.T) {
		spec := &testToolSpec{}

		registry1 := NewRegistry(nil)
		factory1 := testFactory("registry-1-dep")
		registry1.RegisterSpec(spec, factory1)

		registry2 := NewRegistry(nil)
		factory2 := testFactory("registry-2-dep")
		registry2.RegisterSpec(spec, factory2)

		call := &ToolCall{
			ID:   "test-call-2",
			Name: "test_tool",
			Parameters: map[string]interface{}{
				"input": "test",
			},
		}

		// Execute with registry 1
		result1 := registry1.Execute(context.Background(), call)
		resultMap1 := result1.Result.(map[string]interface{})
		if resultMap1["dependency"] != "registry-1-dep" {
			t.Errorf("Expected registry-1-dep, got: %v", resultMap1["dependency"])
		}

		// Execute with registry 2
		result2 := registry2.Execute(context.Background(), call)
		resultMap2 := result2.Result.(map[string]interface{})
		if resultMap2["dependency"] != "registry-2-dep" {
			t.Errorf("Expected registry-2-dep, got: %v", resultMap2["dependency"])
		}
	})

	// Test 5: Backward compatibility with legacy Tool interface
	t.Run("BackwardCompatibility", func(t *testing.T) {
		registry := NewRegistry(nil)
		legacy := &legacyTestTool{}
		registry.Register(legacy)

		// Verify it works with old Get method
		tool, ok := registry.Get("legacy_tool")
		if !ok || tool == nil {
			t.Fatal("Expected legacy tool to be registered")
		}

		// Verify it works with new GetExecutor method
		executor, ok := registry.GetExecutor("legacy_tool")
		if !ok || executor == nil {
			t.Fatal("Expected legacy tool executor to be available")
		}

		// Verify execution works
		call := &ToolCall{
			ID:         "legacy-call",
			Name:       "legacy_tool",
			Parameters: map[string]interface{}{},
		}
		result := registry.Execute(context.Background(), call)
		if result.Result != "legacy" {
			t.Errorf("Expected result=legacy, got: %v", result.Result)
		}

		// Verify JSON schema generation works
		schemas := registry.ToJSONSchema()
		if len(schemas) != 1 {
			t.Fatalf("Expected 1 schema, got: %d", len(schemas))
		}
	})
}

// Benchmark types
type benchToolSpec struct{}

func (s *benchToolSpec) Name() string {
	return "bench"
}

func (s *benchToolSpec) Description() string {
	return "bench"
}

func (s *benchToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

type benchToolExecutor struct{}

func (e *benchToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return &ToolResult{Result: "ok"}
}

type benchLegacyTool struct{}

func (t *benchLegacyTool) Name() string {
	return "bench"
}

func (t *benchLegacyTool) Description() string {
	return "bench"
}

func (t *benchLegacyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func (t *benchLegacyTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return &ToolResult{Result: "ok"}
}

// BenchmarkToolSpecVsLegacy compares performance of new pattern vs legacy
func BenchmarkToolSpecVsLegacy(b *testing.B) {
	b.Run("NewPattern", func(b *testing.B) {
		registry := NewRegistry(nil)
		spec := &benchToolSpec{}
		factory := func(reg *Registry) ToolExecutor { return &benchToolExecutor{} }
		registry.RegisterSpec(spec, factory)

		call := &ToolCall{
			ID:         "bench-call",
			Name:       "bench",
			Parameters: map[string]interface{}{},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = registry.Execute(context.Background(), call)
		}
	})

	b.Run("LegacyPattern", func(b *testing.B) {
		registry := NewRegistry(nil)
		tool := &benchLegacyTool{}
		registry.Register(tool)

		call := &ToolCall{
			ID:         "bench-call",
			Name:       "bench",
			Parameters: map[string]interface{}{},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = registry.Execute(context.Background(), call)
		}
	})
}
