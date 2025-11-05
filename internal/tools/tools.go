package tools

import (
	"context"
	"encoding/json"
	"strings"
)

// Tool represents an LLM tool
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Parameters map[string]interface{} `json:"parameters"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ID                     string      `json:"id"`
	Result                 interface{} `json:"result"`
	Error                  string      `json:"error,omitempty"`
	RequiresUserInput      bool        `json:"requires_user_input,omitempty"`      // If true, user approval is needed
	AuthReason             string      `json:"auth_reason,omitempty"`              // Reason for requiring authorization
	SuggestedCommandPrefix string      `json:"suggested_command_prefix,omitempty"` // Suggested prefix to remember for future use
}

// Registry manages available tools
type Registry struct {
	tools      map[string]Tool
	authorizer Authorizer
}

// NewRegistry creates a new tool registry with an optional authorizer
func NewRegistry(authorizer Authorizer) *Registry {
	return &Registry{
		tools:      make(map[string]Tool),
		authorizer: authorizer,
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// RemoveByPrefix unregisters tools whose names share the provided prefix.
func (r *Registry) RemoveByPrefix(prefix string) {
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
		}
	}
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tools
func (r *Registry) List() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, tool)
	}
	return result
}

// ExecuteWithApproval executes a tool call, bypassing authorization (used when user has manually approved)
func (r *Registry) ExecuteWithApproval(ctx context.Context, call *ToolCall) *ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	// Skip authorization check - user has already approved

	result, err := tool.Execute(ctx, call.Parameters)
	if err != nil {
		return &ToolResult{
			ID:    call.ID,
			Error: err.Error(),
		}
	}

	return &ToolResult{
		ID:     call.ID,
		Result: result,
	}
}

// Execute executes a tool call
func (r *Registry) Execute(ctx context.Context, call *ToolCall) *ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	if r.authorizer != nil {
		decision, err := r.authorizer.Authorize(ctx, call.Name, call.Parameters)
		if err != nil {
			return &ToolResult{
				ID:    call.ID,
				Error: "authorization error: " + err.Error(),
			}
		}

		if decision != nil && !decision.Allowed {
			if decision.RequiresUserInput {
				// Signal that user approval is needed
				return &ToolResult{
					ID:                     call.ID,
					RequiresUserInput:      true,
					AuthReason:             decision.Reason,
					SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
				}
			}
			// Hard denial (no user input option)
			return &ToolResult{
				ID:                     call.ID,
				Error:                  decision.Reason,
				SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
			}
		}
	}

	result, err := tool.Execute(ctx, call.Parameters)
	if err != nil {
		return &ToolResult{
			ID:    call.ID,
			Error: err.Error(),
		}
	}

	return &ToolResult{
		ID:     call.ID,
		Result: result,
	}
}

// ToJSONSchema converts tools to JSON schema format for LLM
func (r *Registry) ToJSONSchema() []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  tool.Parameters(),
			},
		})
	}
	return schemas
}

// Helper function to get string parameter
func GetStringParam(params map[string]interface{}, key string, defaultVal string) string {
	if val, ok := params[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

// Helper function to get int parameter
func GetIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case json.Number:
			if i, err := v.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

// Helper function to get bool parameter
func GetBoolParam(params map[string]interface{}, key string, defaultVal bool) bool {
	if val, ok := params[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultVal
}
