package syntax

import (
	"context"
	"fmt"
	"strings"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Validator provides syntax validation for code using tree-sitter parsers.
type Validator struct {
	languages map[string]unsafe.Pointer
}

// SyntaxError represents a single syntax error found during validation.
type SyntaxError struct {
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Message   string `json:"message"`
	ErrorNode string `json:"error_node"` // Type of error node (e.g., "ERROR", "MISSING")
}

// ValidationResult contains the results of syntax validation.
type ValidationResult struct {
	Valid       bool          `json:"valid"`
	Errors      []SyntaxError `json:"errors,omitempty"`
	Language    string        `json:"language"`
	ParsedBytes int           `json:"parsed_bytes"`
}

// NewValidator creates a new syntax validator with support for multiple languages.
func NewValidator() *Validator {
	return &Validator{
		languages: map[string]unsafe.Pointer{
			"go":         tree_sitter_go.Language(),
			"golang":     tree_sitter_go.Language(),
			"python":     tree_sitter_python.Language(),
			"py":         tree_sitter_python.Language(),
			"typescript": tree_sitter_typescript.LanguageTypescript(),
			"ts":         tree_sitter_typescript.LanguageTypescript(),
			"javascript": tree_sitter_typescript.LanguageTypescript(), // TypeScript parser handles JS
			"js":         tree_sitter_typescript.LanguageTypescript(),
			"tsx":        tree_sitter_typescript.LanguageTSX(),
			"jsx":        tree_sitter_typescript.LanguageTSX(),
			"bash":       tree_sitter_bash.Language(),
			"sh":         tree_sitter_bash.Language(),
			"shell":      tree_sitter_bash.Language(),
		},
	}
}

// Validate validates code syntax using tree-sitter.
// Returns a ValidationResult with syntax errors, if any.
func (v *Validator) Validate(code string, language string) (*ValidationResult, error) {
	// Normalize language name
	language = strings.ToLower(strings.TrimSpace(language))

	// Handle empty or whitespace-only files
	if strings.TrimSpace(code) == "" {
		return &ValidationResult{
			Valid:       true,
			Errors:      nil,
			Language:    language,
			ParsedBytes: 0,
		}, nil
	}

	// Get the language grammar
	lang, ok := v.languages[language]
	if !ok {
		return nil, fmt.Errorf("language not supported for validation: %s (supported: %s)",
			language, strings.Join(SupportedValidationLanguages(), ", "))
	}

	// Create parser
	parser := tree_sitter.NewParser()
	defer parser.Close()

	// Set language
	if err := parser.SetLanguage(tree_sitter.NewLanguage(lang)); err != nil {
		return nil, fmt.Errorf("failed to set parser language: %w", err)
	}

	// Parse code
	sourceBytes := []byte(code)
	tree := parser.Parse(sourceBytes, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse code: parser returned nil tree")
	}
	defer tree.Close()

	// Get root node
	root := tree.RootNode()
	if root == nil {
		return nil, fmt.Errorf("failed to get root node from parsed tree")
	}

	// Check if tree has errors
	if !root.HasError() {
		return &ValidationResult{
			Valid:       true,
			Errors:      nil,
			Language:    language,
			ParsedBytes: len(sourceBytes),
		}, nil
	}

	// Tree has errors - collect them
	errors := v.findErrorNodes(root, sourceBytes)

	return &ValidationResult{
		Valid:       len(errors) == 0, // Only valid if we actually found errors but tree says there are errors
		Errors:      errors,
		Language:    language,
		ParsedBytes: len(sourceBytes),
	}, nil
}

// ValidateFile validates a file's syntax.
// This is a convenience method that can be used when the file system is available.
func (v *Validator) ValidateFile(ctx context.Context, readFile func(context.Context, string) ([]byte, error), path string) (*ValidationResult, error) {
	// Detect language from file path
	language := DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("cannot detect language from file: %s", path)
	}

	// Check if language is supported
	if !IsValidationSupported(language) {
		return nil, fmt.Errorf("language not supported for validation: %s", language)
	}

	// Read file
	data, err := readFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Validate
	return v.Validate(string(data), language)
}

// SupportsLanguage checks if the validator supports a given language.
func (v *Validator) SupportsLanguage(language string) bool {
	language = strings.ToLower(strings.TrimSpace(language))
	_, ok := v.languages[language]
	return ok
}

// findErrorNodes recursively traverses the syntax tree to find all ERROR and MISSING nodes.
func (v *Validator) findErrorNodes(node *tree_sitter.Node, source []byte) []SyntaxError {
	var errors []SyntaxError

	// If node is nil, return empty result
	if node == nil {
		return errors
	}

	// Helper function to recursively collect errors
	var traverse func(*tree_sitter.Node)
	traverse = func(n *tree_sitter.Node) {
		if n == nil {
			return
		}

		nodeType := n.Kind()

		// Check if this is an error node - look for tree-sitter error indicators
		if nodeType == "ERROR" || strings.Contains(nodeType, "MISSING") || strings.Contains(nodeType, "ERROR") {
			// Get position information
			startPos := n.StartPosition()
			line := int(startPos.Row) + 1      // Tree-sitter uses 0-based rows
			column := int(startPos.Column) + 1 // Tree-sitter uses 0-based columns

			// Generate error message
			message := v.generateErrorMessage(n, source, nodeType)

			errors = append(errors, SyntaxError{
				Line:      line,
				Column:    column,
				Message:   message,
				ErrorNode: nodeType,
			})
		}

		// Recursively check children
		childCount := n.ChildCount()
		for i := uint(0); i < childCount; i++ {
			child := n.Child(i)
			if child != nil {
				traverse(child)
			}
		}
	}

	traverse(node)

	// Additional safety check: if root has error but we didn't find ERROR nodes,
	// there might be parsing recovery issues - add a general error
	if node.HasError() && len(errors) == 0 {
		rootPos := node.StartPosition()
		errors = append(errors, SyntaxError{
			Line:      int(rootPos.Row) + 1,
			Column:    int(rootPos.Column) + 1,
			Message:   "syntax error: parsing failed with error recovery",
			ErrorNode: "ERROR",
		})
	}

	return errors
}

// generateErrorMessage creates a human-readable error message for an error node.
func (v *Validator) generateErrorMessage(node *tree_sitter.Node, source []byte, nodeType string) string {
	// Get the text of the error node
	startByte := node.StartByte()
	endByte := node.EndByte()

	var errorText string
	if startByte < endByte && endByte <= uint(len(source)) {
		errorText = string(source[startByte:endByte])
		// Truncate if too long
		if len(errorText) > 50 {
			errorText = errorText[:50] + "..."
		}
		// Escape newlines for display
		errorText = strings.ReplaceAll(errorText, "\n", "\\n")
	}

	// Generate message based on node type
	switch {
	case nodeType == "ERROR":
		if errorText != "" {
			return fmt.Sprintf("syntax error near '%s'", errorText)
		}
		return "syntax error"
	case strings.Contains(nodeType, "MISSING"):
		// Extract what's missing from the node type if possible
		missing := strings.TrimPrefix(nodeType, "MISSING")
		missing = strings.Trim(missing, " _")
		if missing != "" {
			return fmt.Sprintf("missing %s", missing)
		}
		return "syntax error: missing token"
	default:
		return fmt.Sprintf("unexpected %s", nodeType)
	}
}
