package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/syntax"
)

// ValidateSyntaxToolSpec is the static specification for the validate_syntax tool
type ValidateSyntaxToolSpec struct{}

func (s *ValidateSyntaxToolSpec) Name() string {
	return ToolNameValidateSyntax
}

func (s *ValidateSyntaxToolSpec) Description() string {
	return "Validate syntax of code using tree-sitter parser. Can validate files or inline code snippets. " +
		"Supported languages: go, python, typescript, javascript, tsx, jsx, bash, sh. " +
		"Returns detailed syntax error information including line/column numbers."
}

func (s *ValidateSyntaxToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to file to validate (mutually exclusive with 'code')",
			},
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Inline code to validate (mutually exclusive with 'path')",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Language for validation (auto-detected from file extension if path provided). Supported: go, python, typescript, javascript, tsx, jsx, bash, rust (future), c (future), cpp (future), java (future), ruby (future), json (future), yaml (future), toml (future)",
			},
		},
		"oneOf": []map[string]interface{}{
			{"required": []string{"path"}},
			{"required": []string{"code", "language"}},
		},
	}
}

// ValidateSyntaxToolExecutor handles execution of the validate_syntax tool
type ValidateSyntaxToolExecutor struct {
	fs        fs.FileSystem
	validator *syntax.Validator
}

func (e *ValidateSyntaxToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// Parse parameters
	path := GetStringParam(params, "path", "")
	code := GetStringParam(params, "code", "")
	language := GetStringParam(params, "language", "")

	// Validate parameter combinations
	if path != "" && code != "" {
		return &ToolResult{Error: "cannot specify both 'path' and 'code' parameters"}
	}

	if path == "" && code == "" {
		return &ToolResult{Error: "must specify either 'path' or 'code' parameter"}
	}

	// Handle file validation
	if path != "" {
		return e.validateFile(ctx, path, language)
	}

	// Handle inline code validation
	if language == "" {
		return &ToolResult{Error: "language parameter is required when validating inline code"}
	}

	return e.validateCode(code, language, "inline code")
}

// validateFile validates a file's syntax
func (e *ValidateSyntaxToolExecutor) validateFile(ctx context.Context, path string, language string) *ToolResult {
	// Auto-detect language if not provided
	if language == "" {
		language = syntax.DetectLanguage(path)
		if language == "" {
			return &ToolResult{
				Error: fmt.Sprintf("cannot detect language from file: %s (consider providing 'language' parameter)", path),
			}
		}
	}

	// Check if language is supported
	if !syntax.IsValidationSupported(language) {
		return &ToolResult{
			Error: fmt.Sprintf("language '%s' is not supported for syntax validation. Supported languages: %s",
				language, strings.Join(syntax.SupportedValidationLanguages(), ", ")),
		}
	}

	// Read file
	data, err := e.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{
			Error: fmt.Sprintf("failed to read file %s: %v", path, err),
		}
	}

	return e.validateCode(string(data), language, path)
}

// validateCode validates code and returns formatted result
func (e *ValidateSyntaxToolExecutor) validateCode(code, language, sourceName string) *ToolResult {
	// Validate syntax
	result, err := e.validator.Validate(code, language)
	if err != nil {
		return &ToolResult{
			Error: fmt.Sprintf("validation error: %v", err),
		}
	}

	// Build result for LLM (JSON format)
	resultMap := map[string]interface{}{
		"valid":        result.Valid,
		"language":     result.Language,
		"parsed_bytes": result.ParsedBytes,
	}

	if !result.Valid {
		resultMap["error_count"] = len(result.Errors)
		errors := make([]map[string]interface{}, len(result.Errors))
		for i, syntaxErr := range result.Errors {
			errors[i] = map[string]interface{}{
				"line":       syntaxErr.Line,
				"column":     syntaxErr.Column,
				"message":    syntaxErr.Message,
				"error_node": syntaxErr.ErrorNode,
			}
		}
		resultMap["errors"] = errors
	}

	// Build UI result (markdown format)
	uiResult := e.formatUIResult(sourceName, result)

	return &ToolResult{
		Result:   resultMap,
		UIResult: uiResult,
	}
}

// formatUIResult creates a user-friendly markdown representation of validation results
func (e *ValidateSyntaxToolExecutor) formatUIResult(sourceName string, result *syntax.ValidationResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🔍 **Syntax Validation:** `%s`\n\n", sourceName))
	sb.WriteString(fmt.Sprintf("📊 **Language:** %s | **Parsed:** %d bytes\n\n", result.Language, result.ParsedBytes))

	if result.Valid {
		sb.WriteString("✅ **Validation Passed** - No syntax errors detected")
		return sb.String()
	}

	// Validation failed
	sb.WriteString(fmt.Sprintf("❌ **Validation Failed** - Found %d syntax error(s)\n\n", len(result.Errors)))

	// List errors (limit to first 10 for readability)
	maxErrors := 10
	for i, syntaxErr := range result.Errors {
		if i >= maxErrors {
			sb.WriteString(fmt.Sprintf("*... and %d more error(s)*\n", len(result.Errors)-maxErrors))
			break
		}

		sb.WriteString(fmt.Sprintf("**Error %d:** Line %d, Column %d\n",
			i+1, syntaxErr.Line, syntaxErr.Column))
		sb.WriteString(fmt.Sprintf("```\n%s\n```\n", syntaxErr.Message))

		if i < len(result.Errors)-1 && i < maxErrors-1 {
			sb.WriteString("\n---\n\n")
		}
	}

	return sb.String()
}

// NewValidateSyntaxToolFactory creates a factory for ValidateSyntaxTool
func NewValidateSyntaxToolFactory(filesystem fs.FileSystem) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return &ValidateSyntaxToolExecutor{
			fs:        filesystem,
			validator: syntax.NewValidator(),
		}
	}
}
