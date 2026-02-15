package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockRefactoringAgent is a mock implementation of RefactoringAgent for testing
type MockRefactoringAgent struct {
	results []string
	err     error
}

func (m *MockRefactoringAgent) Refactor(ctx context.Context, objectives []string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestRefactoringAgentToolSpec(t *testing.T) {
	spec := &RefactoringAgentToolSpec{}

	t.Run("Name returns correct tool name", func(t *testing.T) {
		assert.Equal(t, ToolNameRefactoringAgent, spec.Name())
	})

	t.Run("Description is not empty", func(t *testing.T) {
		assert.NotEmpty(t, spec.Description())
		assert.Contains(t, spec.Description(), "refactoring")
	})

	t.Run("Parameters schema is valid", func(t *testing.T) {
		params := spec.Parameters()

		assert.Equal(t, "object", params["type"])

		props, ok := params["properties"].(map[string]interface{})
		require.True(t, ok)

		objectives, ok := props["objectives"].(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, "array", objectives["type"])

		items, ok := objectives["items"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "string", items["type"])

		required, ok := params["required"].([]string)
		require.True(t, ok)
		assert.Contains(t, required, "objectives")
	})
}

func TestRefactoringAgentTool_Execute(t *testing.T) {
	t.Run("valid objectives with single result", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{
			results: []string{"Refactoring completed successfully"},
		}

		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{"Refactor function A"},
		}

		result := tool.Execute(ctx, params)

		assert.Empty(t, result.Error)
		assert.NotNil(t, result.Result)
		output := result.Result.(string)
		assert.Contains(t, output, "Refactoring completed successfully")
		// Single objective doesn't get a header
		assert.NotContains(t, output, "Objective:")
	})

	t.Run("valid objectives with multiple results", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{
			results: []string{"Task 1 done", "Task 2 done"},
		}

		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{"Task 1", "Task 2"},
		}

		result := tool.Execute(ctx, params)

		assert.Empty(t, result.Error)
		assert.NotNil(t, result.Result)
		output := result.Result.(string)
		assert.Contains(t, output, "Task 1 done")
		assert.Contains(t, output, "Task 2 done")
		assert.Contains(t, output, "Objective: Task 1")
		assert.Contains(t, output, "Objective: Task 2")
	})

	t.Run("missing objectives parameter", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{}
		result := tool.Execute(ctx, params)

		assert.Equal(t, "objectives is required", result.Error)
	})

	t.Run("objectives not an array", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": "not an array",
		}
		result := tool.Execute(ctx, params)

		assert.Equal(t, "objectives must be an array", result.Error)
	})

	t.Run("empty objectives array", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{},
		}
		result := tool.Execute(ctx, params)

		assert.Equal(t, "at least one objective is required", result.Error)
	})

	t.Run("objectives array contains empty strings", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{
			results: []string{}, // No results for empty objectives
		}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{"", "  ", "\t"},
		}
		result := tool.Execute(ctx, params)

		// Should return an error because no valid objectives remain after filtering
		assert.Equal(t, "at least one non-empty objective is required", result.Error)
	})

	t.Run("agent returns error", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{
			err: assert.AnError,
		}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{"Refactor this"},
		}
		result := tool.Execute(ctx, params)

		assert.Equal(t, assert.AnError.Error(), result.Error)
	})

	t.Run("filters out empty objectives from array", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{
			results: []string{"Only non-empty processed"},
		}
		tool := NewRefactoringAgentTool(mockAgent)
		ctx := context.Background()

		params := map[string]interface{}{
			"objectives": []interface{}{"", "Valid objective", "  "},
		}
		result := tool.Execute(ctx, params)

		// Should succeed with only the valid objective
		assert.Empty(t, result.Error)
		assert.Contains(t, result.Result.(string), "Only non-empty processed")
	})
}

func TestRefactoringAgentTool_LegacyInterface(t *testing.T) {
	mockAgent := &MockRefactoringAgent{}
	tool := NewRefactoringAgentTool(mockAgent)

	t.Run("Name returns correct tool name", func(t *testing.T) {
		assert.Equal(t, ToolNameRefactoringAgent, tool.Name())
	})

	t.Run("Description is not empty", func(t *testing.T) {
		assert.NotEmpty(t, tool.Description())
	})

	t.Run("Parameters returns valid schema", func(t *testing.T) {
		params := tool.Parameters()
		assert.NotNil(t, params)
		assert.Equal(t, "object", params["type"])
	})
}

func TestRefactoringAgentToolFactory(t *testing.T) {
	t.Run("factory creates tool with agent", func(t *testing.T) {
		mockAgent := &MockRefactoringAgent{}
		factory := NewRefactoringAgentToolFactory(mockAgent)

		registry := NewRegistry(nil)
		executor := factory(registry)

		assert.NotNil(t, executor)
		assert.IsType(t, &RefactoringAgentTool{}, executor)

		tool := executor.(*RefactoringAgentTool)
		assert.Equal(t, mockAgent, tool.agent)
	})
}
