package syntax

import (
	"path/filepath"
	"strings"
)

// DetectLanguage determines the programming language from file extension or filename.
// This is shared across syntax highlighting and validation to ensure consistency.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyw":
		return "python"
	case ".ts":
		return "typescript"
	case ".js", ".mjs":
		return "javascript"
	case ".tsx":
		return "tsx"
	case ".jsx":
		return "jsx"
	case ".sh", ".bash", ".zsh", ".fish":
		return "bash"
	case ".rs":
		return "rust"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".h":
		return "cpp"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".md", ".markdown":
		return "markdown"
	case ".dockerfile":
		return "dockerfile"
	case ".ini":
		return "ini"
	case ".cfg", ".conf":
		return "config"
	default:
		// Check for specific filenames without extensions
		base := strings.ToLower(filepath.Base(path))
		switch base {
		case "dockerfile", ".dockerignore":
			return "dockerfile"
		case "makefile":
			return "makefile"
		case "readme", "license":
			return "markdown"
		default:
			return ""
		}
	}
}

// SupportedValidationLanguages returns the list of languages supported for syntax validation.
// This is a subset of all detected languages, limited to those with tree-sitter grammars.
func SupportedValidationLanguages() []string {
	return []string{
		"go",
		"python",
		"typescript",
		"javascript",
		"tsx",
		"jsx",
		"bash",
		// Additional languages will be added in Phase 4:
		// rust, c, cpp, java, ruby, json, yaml, toml
	}
}

// IsValidationSupported checks if a language has tree-sitter validation support.
func IsValidationSupported(language string) bool {
	supported := SupportedValidationLanguages()
	for _, lang := range supported {
		if lang == language {
			return true
		}
	}
	return false
}
