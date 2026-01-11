package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

func TestParamsToString(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]interface{}
		expected string
	}{
		{
			name:     "empty params",
			params:   map[string]interface{}{},
			expected: "",
		},
		{
			name: "simple string params",
			params: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: "key1=value1 key2=value2",
		},
		{
			name: "mixed types",
			params: map[string]interface{}{
				"string": "test",
				"int":    42,
				"bool":   true,
				"float":  3.14,
			},
			expected: "string=test int=42 bool=true float=3.14",
		},
		{
			name: "nested array",
			params: map[string]interface{}{
				"items": []interface{}{"a", "b", "c"},
				"count": 3,
			},
			expected: "items=[a,b,c] count=3",
		},
		{
			name: "nested map",
			params: map[string]interface{}{
				"config": map[string]interface{}{
					"host": "localhost",
					"port": 8080,
				},
				"debug": true,
			},
			expected: "config={host:localhost,port:8080} debug=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := paramsToString(tt.params)
			// Since map iteration order is not guaranteed, we need to check that all parts are present
			// in any order for tests with multiple keys
			if tt.name == "simple string params" {
				// Check that all expected parts are in the result
				expectedParts := []string{"key1=value1", "key2=value2"}
				for _, part := range expectedParts {
					if !strings.Contains(result, part) {
						t.Errorf("paramsToString() = %q, expected to contain %q", result, part)
					}
				}
			} else if tt.name == "mixed types" {
				// Check that all expected parts are in the result
				expectedParts := []string{"string=test", "int=42", "bool=true", "float=3.14"}
				for _, part := range expectedParts {
					if !strings.Contains(result, part) {
						t.Errorf("paramsToString() = %q, expected to contain %q", result, part)
					}
				}
			} else if tt.name == "nested_map" {
				// Check that config contains the expected parts in any order
				expectedParts := []string{"host:localhost", "port:8080"}
				if !strings.Contains(result, "config={") || !strings.Contains(result, "}") {
					t.Errorf("paramsToString() = %q, expected config to be a map-like string", result)
				}
				for _, part := range expectedParts {
					if !strings.Contains(result, part) {
						t.Errorf("paramsToString() = %q, expected to contain %q", result, part)
					}
				}
				if !strings.Contains(result, "debug=true") {
					t.Errorf("paramsToString() = %q, expected to contain debug=true", result)
				}
			} else {
				if result != tt.expected {
					t.Errorf("paramsToString() = %q, expected %q", result, tt.expected)
				}
			}
		})
	}
}

func TestHasSecrets(t *testing.T) {
	detector := secretdetect.NewDetector()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "no secrets",
			content:  "just regular text without any secrets",
			expected: false,
		},
		{
			name:     "contains OpenAI API key",
			content:  "api_key=sk-1234567890abcdef1234567890abcdef12345678",
			expected: true,
		},
		{
			name:     "contains AWS Access Key",
			content:  "aws_access_key_id=AKIAIOSFODNN7EXAMPLE",
			expected: true,
		},
		{
			name:     "contains GitHub token",
			content:  "token=ghp_1234567890abcdef1234567890abcdef12345678",
			expected: true,
		},
		{
			name:     "contains private key header",
			content:  "-----BEGIN RSA PRIVATE KEY-----",
			expected: true,
		},
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "nil detector",
			content:  "api_key=sk-1234567890abcdef1234567890abcdef12345678",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var det secretdetect.Detector
			if tt.name != "nil detector" {
				det = detector
			}
			result := hasSecrets(det, tt.content)
			if result != tt.expected {
				t.Errorf("hasSecrets() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestExtractSecrets(t *testing.T) {
	detector := secretdetect.NewDetector()

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "no secrets",
			content:  "just regular text",
			expected: 0,
		},
		{
			name:     "one secret",
			content:  "api_key=sk-1234567890abcdef1234567890abcdef12345678",
			expected: 1,
		},
		{
			name:     "multiple secrets",
			content:  "api_key=sk-1234567890abcdef1234567890abcdef12345678 aws_access_key_id=AKIAIOSFODNN7EXAMPLE",
			expected: 2,
		},
		{
			name:     "empty content",
			content:  "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := extractSecrets(detector, tt.content)
			if len(secrets) != tt.expected {
				t.Errorf("extractSecrets() returned %d secrets, expected %d", len(secrets), tt.expected)
			}
		})
	}
}

func TestShouldScanTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{
			name:     "shell tool should be scanned",
			toolName: "shell",
			expected: true,
		},
		{
			name:     "create_file should be scanned",
			toolName: "create_file",
			expected: true,
		},
		{
			name:     "read_file should not be scanned",
			toolName: "read_file",
			expected: false,
		},
		{
			name:     "todo should not be scanned",
			toolName: "todo",
			expected: false,
		},
		{
			name:     "status should not be scanned",
			toolName: "status",
			expected: false,
		},
		{
			name:     "unknown tool should be scanned",
			toolName: "unknown_tool",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldScanTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("shouldScanTool(%q) = %v, expected %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestSecretAwareAuthorizer(t *testing.T) {
	// Mock authorizer that always allows
	mockAuthorizer := &MockAuthorizer{allowed: true}

	// For testing, we'll use nil as the authorization actor since we're testing
	// the wrapper behavior with a mock interface
	saa := NewSecretAwareAuthorizer(mockAuthorizer, nil)

	// Test normal authorization without secrets
	ctx := context.Background()
	decision, err := saa.Authorize(ctx, "test_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if !decision.Allowed {
		t.Errorf("Authorize() allowed = %v, expected true", decision.Allowed)
	}

	// Test authorization with secrets (with nil auth actor, should require user input)
	secrets := []secretdetect.SecretMatch{
		{
			PatternName: "OpenAI API Key",
			MatchedText: "sk-1234567890abcdef1234567890abcdef12345678",
			LineNumber:  1,
			Column:      1,
			Confidence:  1.0,
		},
	}

	decision, err = saa.AuthorizeWithSecrets(ctx, "test_tool", map[string]interface{}{"api_key": "sk-1234567890abcdef1234567890abcdef12345678"}, secrets)
	if err != nil {
		t.Fatalf("AuthorizeWithSecrets() error = %v", err)
	}
	if decision.Allowed {
		t.Errorf("AuthorizeWithSecrets() allowed = %v, expected false", decision.Allowed)
	}
	if !decision.RequiresUserInput {
		t.Errorf("AuthorizeWithSecrets() requiresUserInput = %v, expected true", decision.RequiresUserInput)
	}
}

// Mock implementations for testing

type MockAuthorizer struct {
	allowed bool
}

func (m *MockAuthorizer) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	return &AuthorizationDecision{Allowed: m.allowed}, nil
}
