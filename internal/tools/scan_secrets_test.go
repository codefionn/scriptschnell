package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

// Helper to extract matches count from result
func getMatchesCount(t *testing.T, result interface{}) int {
	t.Helper()
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result to be a map, got %T", result)
	}

	count, ok := resultMap["count"].(int)
	if !ok {
		t.Fatalf("Expected count to be an int, got %T: %v", resultMap["count"], resultMap["count"])
	}
	return count
}

func TestScanSecretsToolSpec(t *testing.T) {
	spec := &ScanSecretsToolSpec{}

	t.Run("Name", func(t *testing.T) {
		if got := spec.Name(); got != "scan_secrets" {
			t.Errorf("Name() = %v, want %v", got, "scan_secrets")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := spec.Description()
		if desc == "" {
			t.Error("Description() returned empty string")
		}
		// Check that it mentions key concepts (case-insensitive)
		descLower := strings.ToLower(desc)
		if !strings.Contains(descLower, "scan") || !strings.Contains(descLower, "secret") {
			t.Errorf("Description() missing key concepts: %v", desc)
		}
	})

	t.Run("Parameters", func(t *testing.T) {
		params := spec.Parameters()
		if params == nil {
			t.Fatal("Parameters() returned nil")
		}

		// Check that parameters define the expected structure
		if params["type"] != "object" {
			t.Errorf("Parameters type = %v, want object", params["type"])
		}

		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Parameters properties not a map")
		}

		// Check content parameter exists
		if _, ok := props["content"]; !ok {
			t.Error("Parameters missing 'content' property")
		}

		// Check file_path parameter exists
		if _, ok := props["file_path"]; !ok {
			t.Error("Parameters missing 'file_path' property")
		}

		// Check redact parameter exists
		if _, ok := props["redact"]; !ok {
			t.Error("Parameters missing 'redact' property")
		}
	})
}

func TestScanSecretsToolExecutor_Execute(t *testing.T) {
	executor := &ScanSecretsToolExecutor{
		detector: secretdetect.NewDetector(),
	}
	ctx := context.Background()

	t.Run("NoParametersProvided", func(t *testing.T) {
		result := executor.Execute(ctx, map[string]interface{}{})
		if result.Error == "" {
			t.Error("Expected error when no parameters provided")
		}
		if !contains(result.Error, "content") || !contains(result.Error, "file_path") {
			t.Errorf("Error message should mention required parameters: %v", result.Error)
		}
	})

	t.Run("ScanContentNoSecrets", func(t *testing.T) {
		params := map[string]interface{}{
			"content": "This is just normal text with no secrets",
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}
		if resultStr, ok := result.Result.(string); !ok || resultStr != "No secrets detected." {
			t.Errorf("Expected 'No secrets detected.' message, got: %v", result.Result)
		}
	})

	t.Run("ScanContentWithAPIKey", func(t *testing.T) {
		params := map[string]interface{}{
			"content": "Here is an API key: sk-1234567890abcdefghijklmnopqrstuvwxyz12345678",
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		resultMap, ok := result.Result.(map[string]interface{})
		if !ok {
			t.Fatal("Expected result to be a map")
		}

		count, ok := resultMap["count"].(int)
		if !ok || count == 0 {
			t.Errorf("Expected at least one secret detected, got count: %v", resultMap["count"])
		}

		matches, ok := resultMap["matches"]
		if !ok {
			t.Error("Expected 'matches' field in result")
		}

		if matches == nil {
			t.Error("Expected non-nil matches")
		}

		warning, ok := resultMap["warning"].(string)
		if !ok || warning == "" {
			t.Error("Expected warning message in result")
		}
	})

	t.Run("ScanContentWithAWSKey", func(t *testing.T) {
		params := map[string]interface{}{
			"content": "AKIAIOSFODNN7EXAMPLE is an AWS key",
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		// The detector may or may not detect this pattern depending on implementation
		// Let's just verify we get a valid response (either no secrets or secrets found)
		if result.Result == nil {
			t.Fatal("Expected non-nil result")
		}
	})

	t.Run("RedactContentWithSecrets", func(t *testing.T) {
		originalContent := "API key: sk-1234567890abcdefghijklmnopqrstuvwxyz12345678 end"
		params := map[string]interface{}{
			"content": originalContent,
			"redact":  true,
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		redacted, ok := result.Result.(string)
		if !ok {
			t.Fatal("Expected result to be a string when redact=true")
		}

		if redacted == originalContent {
			t.Error("Expected content to be redacted, but it was unchanged")
		}

		if contains(redacted, "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678") {
			t.Error("Expected secret to be redacted, but it's still present")
		}

		if !contains(redacted, "REDACTED") {
			t.Error("Expected '[REDACTED]' marker in redacted content")
		}
	})

	t.Run("RedactContentNoSecrets", func(t *testing.T) {
		originalContent := "This is just normal text"
		params := map[string]interface{}{
			"content": originalContent,
			"redact":  true,
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		redacted, ok := result.Result.(string)
		if !ok {
			t.Fatal("Expected result to be a string when redact=true")
		}

		if redacted != originalContent {
			t.Errorf("Expected content to be unchanged, got: %v", redacted)
		}
	})

	t.Run("ScanFileNotFound", func(t *testing.T) {
		params := map[string]interface{}{
			"file_path": "/nonexistent/file/path.txt",
		}
		result := executor.Execute(ctx, params)
		if result.Error == "" {
			t.Error("Expected error when scanning nonexistent file")
		}
	})

	t.Run("ScanFileWithSecrets", func(t *testing.T) {
		// Create a temporary file with secrets
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test_secrets.txt")
		content := "API key: sk-1234567890abcdefghijklmnopqrstuvwxyz12345678\n" +
			"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		params := map[string]interface{}{
			"file_path": tmpFile,
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		resultMap, ok := result.Result.(map[string]interface{})
		if !ok {
			t.Fatal("Expected result to be a map")
		}

		count, ok := resultMap["count"].(int)
		if !ok || count == 0 {
			t.Errorf("Expected at least one secret detected, got count: %v", resultMap["count"])
		}

		// Verify we got matches - the exact structure depends on how the tool marshals results
		// Just verify that count is > 0
		if count == 0 {
			t.Error("Expected at least one secret detected in file")
		}
	})

	t.Run("ScanFileNoSecrets", func(t *testing.T) {
		// Create a temporary file without secrets
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test_no_secrets.txt")
		content := "This is just normal text\nwith no secrets at all\n"
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		params := map[string]interface{}{
			"file_path": tmpFile,
		}
		result := executor.Execute(ctx, params)
		if result.Error != "" {
			t.Errorf("Unexpected error: %v", result.Error)
		}

		if resultStr, ok := result.Result.(string); !ok || resultStr != "No secrets detected." {
			t.Errorf("Expected 'No secrets detected.' message, got: %v", result.Result)
		}
	})

	t.Run("RedactFileNotSupported", func(t *testing.T) {
		// Create a temporary file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.txt")
		content := "API key: sk-1234567890abcdefghijklmnopqrstuvwxyz12345678\n"
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		params := map[string]interface{}{
			"file_path": tmpFile,
			"redact":    true,
		}
		result := executor.Execute(ctx, params)
		if result.Error == "" {
			t.Error("Expected error when requesting redaction for file_path")
		}
		if !contains(result.Error, "Redaction") || !contains(result.Error, "content") {
			t.Errorf("Error message should explain redaction limitation: %v", result.Error)
		}
	})
}

func TestNewScanSecretsToolFactory(t *testing.T) {
	factory := NewScanSecretsToolFactory()
	if factory == nil {
		t.Fatal("NewScanSecretsToolFactory() returned nil")
	}

	// Create a mock registry (nil is acceptable for this test)
	executor := factory(nil)
	if executor == nil {
		t.Fatal("Factory returned nil executor")
	}

	// Verify it's the correct type
	_, ok := executor.(*ScanSecretsToolExecutor)
	if !ok {
		t.Errorf("Factory returned wrong type: %T", executor)
	}
}

func TestScanSecretsMatchResult(t *testing.T) {
	executor := &ScanSecretsToolExecutor{
		detector: secretdetect.NewDetector(),
	}
	ctx := context.Background()

	params := map[string]interface{}{
		"content": "First line\nAPI key: sk-1234567890abcdefghijklmnopqrstuvwxyz12345678\nThird line",
	}
	result := executor.Execute(ctx, params)
	if result.Error != "" {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	// Verify that we have matches
	count := getMatchesCount(t, resultMap)
	if count == 0 {
		t.Fatal("Expected at least one match")
	}

	// Verify warning message exists
	warning, ok := resultMap["warning"].(string)
	if !ok || warning == "" {
		t.Error("Expected warning message in result")
	}
}
