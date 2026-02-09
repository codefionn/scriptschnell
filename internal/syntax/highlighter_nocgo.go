//go:build !cgo

package syntax

// Highlighter provides syntax highlighting for code using tree-sitter.
type Highlighter struct{}

// NewHighlighter creates a new syntax highlighter (no-op without CGo).
func NewHighlighter() *Highlighter {
	return &Highlighter{}
}

// Highlight returns code as-is without CGo (tree-sitter unavailable).
func (h *Highlighter) Highlight(code string, language string) (string, error) {
	return code, nil
}
