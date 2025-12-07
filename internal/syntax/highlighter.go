package syntax

import (
	"fmt"
	"strings"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Highlighter provides syntax highlighting for code using tree-sitter
type Highlighter struct {
	languages map[string]unsafe.Pointer
}

// NewHighlighter creates a new syntax highlighter with support for multiple languages
func NewHighlighter() *Highlighter {
	return &Highlighter{
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

// Highlight applies syntax highlighting to code and returns an ANSI-colored string
func (h *Highlighter) Highlight(code string, language string) (string, error) {
	// Normalize language name
	language = strings.ToLower(strings.TrimSpace(language))

	// Get the language grammar
	lang, ok := h.languages[language]
	if !ok {
		// If language not supported, return code as-is
		return code, nil
	}

	// Create parser
	parser := tree_sitter.NewParser()
	defer parser.Close()

	// Set language
	parser.SetLanguage(tree_sitter.NewLanguage(lang))

	// Parse code
	tree := parser.Parse([]byte(code), nil)
	if tree == nil {
		return "", fmt.Errorf("failed to parse code")
	}
	defer tree.Close()

	// Get root node
	root := tree.RootNode()

	// Build highlighted output
	var result strings.Builder
	sourceBytes := []byte(code)

	// Traverse the tree and highlight
	h.highlightNode(root, sourceBytes, &result, 0, uint(len(sourceBytes)))

	return result.String(), nil
}

// highlightNode recursively highlights nodes in the syntax tree
func (h *Highlighter) highlightNode(node *tree_sitter.Node, source []byte, result *strings.Builder, lastPos uint, endPos uint) uint {
	startByte := node.StartByte()
	endByte := node.EndByte()

	// Write any text before this node
	if lastPos < startByte && startByte < endPos {
		result.Write(source[lastPos:startByte])
	}

	// If this is a leaf node, highlight it
	if node.ChildCount() == 0 {
		nodeType := node.Kind()
		color := h.getColorForNodeType(nodeType)
		text := source[startByte:endByte]

		if color != "" {
			result.WriteString(color)
			result.Write(text)
			result.WriteString(colorReset)
		} else {
			result.Write(text)
		}

		return endByte
	}

	// Recursively process children
	pos := lastPos
	if pos < startByte {
		pos = startByte
	}

	childCount := node.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := node.Child(i)
		pos = h.highlightNode(child, source, result, pos, endByte)
	}

	// Write any remaining text after the last child
	if pos < endByte && endByte <= endPos {
		result.Write(source[pos:endByte])
		pos = endByte
	}

	return pos
}

// ANSI color codes
const (
	colorReset     = "\033[0m"
	colorKeyword   = "\033[35m"  // Magenta
	colorString    = "\033[32m"  // Green
	colorNumber    = "\033[33m"  // Yellow
	colorComment   = "\033[90m"  // Bright black (gray)
	colorFunction  = "\033[36m"  // Cyan
	colorType      = "\033[34m"  // Blue
	colorOperator  = "\033[37m"  // White
	colorConstant  = "\033[91m"  // Bright red
	colorVariable  = "\033[97m"  // Bright white
)

// getColorForNodeType returns the ANSI color code for a given node type
func (h *Highlighter) getColorForNodeType(nodeType string) string {
	// Keywords
	if isKeyword(nodeType) {
		return colorKeyword
	}

	// Strings
	if strings.Contains(nodeType, "string") || strings.Contains(nodeType, "template") {
		return colorString
	}

	// Numbers
	if strings.Contains(nodeType, "number") || strings.Contains(nodeType, "int") ||
	   strings.Contains(nodeType, "float") || nodeType == "true" || nodeType == "false" {
		return colorNumber
	}

	// Comments
	if strings.Contains(nodeType, "comment") {
		return colorComment
	}

	// Functions
	if strings.Contains(nodeType, "function") || strings.Contains(nodeType, "method") ||
	   nodeType == "call_expression" {
		return colorFunction
	}

	// Types
	if strings.Contains(nodeType, "type") || nodeType == "type_identifier" {
		return colorType
	}

	// Constants
	if strings.Contains(nodeType, "const") || nodeType == "nil" || nodeType == "null" {
		return colorConstant
	}

	return ""
}

// isKeyword checks if a node type represents a keyword
func isKeyword(nodeType string) bool {
	keywords := []string{
		// Go keywords
		"break", "case", "chan", "const", "continue", "default", "defer", "else",
		"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
		"map", "package", "range", "return", "select", "struct", "switch", "type",
		"var",

		// Python keywords
		"and", "as", "assert", "async", "await", "class", "def", "del", "elif",
		"except", "finally", "from", "global", "in", "is", "lambda", "nonlocal",
		"not", "or", "pass", "raise", "try", "while", "with", "yield",

		// TypeScript/JavaScript keywords
		"abstract", "any", "boolean", "constructor", "declare", "enum", "export",
		"extends", "implements", "instanceof", "keyof", "let", "module", "namespace",
		"never", "new", "number", "private", "protected", "public", "readonly",
		"require", "static", "string", "symbol", "this", "typeof", "unknown", "void",

		// Bash keywords
		"do", "done", "then", "fi", "esac", "until", "function",
	}

	for _, kw := range keywords {
		if nodeType == kw {
			return true
		}
	}

	return false
}
