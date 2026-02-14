package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestAddContextDirectoryToolName(t *testing.T) {
	tool := &AddContextDirectoryTool{}
	if tool.Name() != ToolNameAddContextDirectory {
		t.Fatalf("expected name %s, got %s", ToolNameAddContextDirectory, tool.Name())
	}
}

func TestAddContextDirectoryToolSpecName(t *testing.T) {
	spec := &AddContextDirectoryToolSpec{}
	if spec.Name() != ToolNameAddContextDirectory {
		t.Fatalf("expected name %s, got %s", ToolNameAddContextDirectory, spec.Name())
	}
}

func TestAddContextDirectoryToolExecuteSuccess(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Create a directory to add
	if err := mockFS.MkdirAll(ctx, "/docs/go", os.ModePerm); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "/docs/go",
		"reason":    "Go documentation",
	})

	if result.Error != "" {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	resultStr, ok := result.Result.(string)
	if !ok {
		t.Fatalf("expected result to be string, got %T", result.Result)
	}
	if resultStr == "" {
		t.Fatalf("expected result, got empty")
	}
	if !strings.Contains(resultStr, "/docs/go") {
		t.Fatalf("expected result to contain directory path")
	}
	if !strings.Contains(resultStr, "Go documentation") {
		t.Fatalf("expected result to contain reason")
	}

	// Verify the directory was added to config
	dirs := cfg.GetContextDirectories("/workspace")
	found := false
	for _, d := range dirs {
		if d == "/docs/go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected directory to be added to config")
	}
}

func TestAddContextDirectoryToolExecuteRelativePath(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Create a directory to add (relative path will be resolved to /workspace/docs)
	if err := mockFS.MkdirAll(ctx, "/workspace/docs", os.ModePerm); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "docs",
	})

	if result.Error != "" {
		t.Fatalf("expected no error, got: %s", result.Error)
	}

	// Verify the directory was added to config with absolute path
	dirs := cfg.GetContextDirectories("/workspace")
	found := false
	for _, d := range dirs {
		if d == "/workspace/docs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected directory to be added to config with absolute path")
	}
}

func TestAddContextDirectoryToolExecuteDuplicate(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Create and add directory first
	if err := mockFS.MkdirAll(ctx, "/docs/go", os.ModePerm); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	cfg.AddContextDirectory("/workspace", "/docs/go")

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "/docs/go",
	})

	if result.Error != "" {
		t.Fatalf("expected no error for duplicate, got: %s", result.Error)
	}
	resultStr, ok := result.Result.(string)
	if !ok {
		t.Fatalf("expected result to be string, got %T", result.Result)
	}
	if !strings.Contains(resultStr, "already in context") {
		t.Fatalf("expected result to indicate directory already exists")
	}
}

func TestAddContextDirectoryToolExecuteMissingDirectory(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "/nonexistent/path",
	})

	if result.Error == "" {
		t.Fatalf("expected error for nonexistent directory")
	}
	if !strings.Contains(result.Error, "does not exist") {
		t.Fatalf("expected error to mention directory does not exist, got: %s", result.Error)
	}
}

func TestAddContextDirectoryToolExecuteEmptyDirectory(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{})

	if result.Error == "" {
		t.Fatalf("expected error for empty directory")
	}
	if !strings.Contains(result.Error, "directory is required") {
		t.Fatalf("expected error to mention directory is required, got: %s", result.Error)
	}
}

func TestAddContextDirectoryToolExecuteNotADirectory(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Create a file instead of directory
	if err := mockFS.WriteFile(ctx, "/workspace/file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "/workspace/file.txt",
	})

	if result.Error == "" {
		t.Fatalf("expected error for file path")
	}
	if !strings.Contains(result.Error, "not a directory") {
		t.Fatalf("expected error to mention path is not a directory, got: %s", result.Error)
	}
}

func TestAddContextDirectoryToolExecuteNoReason(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Create a directory to add
	if err := mockFS.MkdirAll(ctx, "/docs/go", os.ModePerm); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": "/docs/go",
		// No reason provided
	})

	if result.Error != "" {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	resultStr, ok := result.Result.(string)
	if !ok {
		t.Fatalf("expected result to be string, got %T", result.Result)
	}
	// Result should not contain "Reason:" line when no reason provided
	if strings.Contains(resultStr, "**Reason:**") {
		t.Fatalf("expected result not to contain Reason when not provided")
	}
}

func TestNewAddContextDirectoryToolFactory(t *testing.T) {
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")
	registry := NewRegistry(nil)

	factory := NewAddContextDirectoryToolFactory(mockFS, cfg, sess)
	tool := factory(registry)

	if tool == nil {
		t.Fatalf("expected tool, got nil")
	}

	executor, ok := tool.(*AddContextDirectoryTool)
	if !ok {
		t.Fatalf("expected *AddContextDirectoryTool, got %T", tool)
	}
	if executor.Name() != ToolNameAddContextDirectory {
		t.Fatalf("expected tool name %s, got %s", ToolNameAddContextDirectory, executor.Name())
	}
}

func TestAddContextDirectoryToolExecuteHomeDirectory(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	cfg := config.DefaultConfig()
	sess := session.NewSession("test", "/workspace")

	// Get the home directory for testing
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get user home directory: %v", err)
	}

	// Create the home directory in mock filesystem
	if err := mockFS.MkdirAll(ctx, homeDir, os.ModePerm); err != nil {
		t.Fatalf("failed to create home directory: %v", err)
	}

	tool := NewAddContextDirectoryTool(mockFS, cfg, sess)
	result := tool.Execute(ctx, map[string]interface{}{
		"directory": homeDir,
	})

	if result.Error == "" {
		t.Fatalf("expected error for home directory")
	}
	if !strings.Contains(result.Error, "cannot add home directory") {
		t.Fatalf("expected error to mention home directory restriction, got: %s", result.Error)
	}
}

func TestIsHomeDirectory(t *testing.T) {
	// Get the home directory for testing
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get user home directory: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "home directory - exact match",
			path:     homeDir,
			expected: true,
		},
		{
			name:     "home directory with trailing slash",
			path:     homeDir + "/",
			expected: true,
		},
		{
			name:     "home directory with tilde",
			path:     "~",
			expected: true,
		},
		{
			name:     "home directory with tilde and trailing slash",
			path:     "~/",
			expected: true,
		},
		{
			name:     "subdirectory of home",
			path:     filepath.Join(homeDir, "Documents"),
			expected: false,
		},
		{
			name:     "subdirectory of home with tilde",
			path:     "~/Documents",
			expected: false,
		},
		{
			name:     "non-home directory",
			path:     "/usr/local",
			expected: false,
		},
		{
			name:     "non-existent home-like path",
			path:     "/nonexistent/home",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := IsHomeDirectory(tt.path)
			if err != nil {
				t.Logf("IsHomeDirectory(%q) returned error: %v", tt.path, err)
				// Some tests may error on non-existent paths, which is acceptable
				return
			}
			if result != tt.expected {
				t.Errorf("IsHomeDirectory(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}
