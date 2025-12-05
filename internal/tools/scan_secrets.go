package tools

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

// ScanSecretsToolSpec defines the scan_secrets tool
type ScanSecretsToolSpec struct{}

func (s *ScanSecretsToolSpec) Name() string {
	return "scan_secrets"
}

func (s *ScanSecretsToolSpec) Description() string {
	return "Scan text or files for secrets (API keys, tokens, private keys). " +
		"Use this to check content before sending it to an LLM or saving to a file. " +
		"Returns a list of detected secrets with their types and locations."
}

func (s *ScanSecretsToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Text content to scan (optional, if file_path is not provided)",
			},
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to file to scan (optional, if content is not provided)",
			},
			"redact": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, returns the redacted content instead of just the matches. Default: false",
			},
		},
	}
}

// ScanSecretsToolExecutor implements the execution logic
type ScanSecretsToolExecutor struct {
	detector secretdetect.Detector
}

// NewScanSecretsToolFactory creates a new factory for the scan_secrets tool
func NewScanSecretsToolFactory() ToolFactory {
	return func(registry *Registry) ToolExecutor {
		return &ScanSecretsToolExecutor{
			detector: secretdetect.NewDetector(),
		}
	}
}

func (e *ScanSecretsToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	content := GetStringParam(params, "content", "")
	filePath := GetStringParam(params, "file_path", "")
	redact := GetBoolParam(params, "redact", false)

	if content == "" && filePath == "" {
		return &ToolResult{Error: "Either 'content' or 'file_path' must be provided"}
	}

	var matches []secretdetect.SecretMatch
	var err error
	var scannedContent string

	if filePath != "" {
		matches, err = e.detector.ScanFile(filePath)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("Failed to scan file: %v", err)}
		}
		// If redaction is requested for a file, we need to read it first
		if redact {
			// Note: This is a simple implementation. For large files, we might want to stream.
			// But since we need to return the string, we have to load it anyway.
			// The read_file tool limits to 2000 lines, we should probably respect that or warn.
			// But for now, let's just read it.
			// However, since we already scanned it, we know if there are secrets.
			// If we need to redact, we have to read the content.
			// Let's assume the user knows what they are doing if they ask for redaction of a file.
			// But wait, ScanFile returns matches. To redact, we need the content.
			// We can't easily get the content from ScanFile.
			// So we'll read the file separately if redaction is needed.
			// But wait, ScanFile is efficient.
			// If we just want matches, we don't need content.
			// If we want redacted content, we need content.
		}
	} else {
		scannedContent = content
		matches = e.detector.Scan(content)
	}

	if redact {
		if filePath != "" {
			// Read file content for redaction
			// We can use the read_file tool logic or just standard io
			// But we don't have access to fs here easily unless we inject it.
			// Let's avoid reading file content for redaction if possible, or just return matches.
			// Actually, if file_path is used, maybe we shouldn't support redaction return?
			// Or we require the user to read it first?
			// Let's support redaction for 'content' only for now, or read file if we can.
			// But we don't have FS injected.
			// Let's update the factory to inject FS if we want to support file reading for redaction.
			return &ToolResult{Error: "Redaction is only supported for 'content' parameter, not 'file_path'"}
		}
		
		redacted := secretdetect.Redact(scannedContent, matches)
		return &ToolResult{Result: redacted}
	}

	// Return matches
	type MatchResult struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		Line       int    `json:"line"`
		Column     int    `json:"column"`
		FilePath   string `json:"file_path,omitempty"`
		Confidence float64 `json:"confidence"`
	}

	results := make([]MatchResult, 0, len(matches))
	for _, m := range matches {
		results = append(results, MatchResult{
			Type:       m.PatternName,
			Text:       m.MatchedText,
			Line:       m.LineNumber,
			Column:     m.Column,
			FilePath:   m.FilePath,
			Confidence: m.Confidence,
		})
	}

	if len(results) == 0 {
		return &ToolResult{Result: "No secrets detected."}
	}

	return &ToolResult{Result: map[string]interface{}{
		"count":   len(results),
		"matches": results,
		"warning": "Secrets detected! Please redact before sharing.",
	}}
}
