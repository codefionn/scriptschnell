package tools

import (
	"context"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestSandboxRemoveFile(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	// Create mock filesystem and session
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", "/tmp")

	// Create test file
	testPath := "test_file.txt"
	testContent := []byte("test content")
	if err := mockFS.WriteFile(context.Background(), testPath, testContent); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create sandbox tool
	sandbox := NewSandboxToolWithFS("/tmp", "/tmp", mockFS, sess, nil)

	// Test code that removes a file (must read first due to read-before-write rule)
	code := `
package main

import "fmt"

func main() {
	// Read the file first (required by read-before-write rule)
	content := ReadFile("test_file.txt", 0, 0)
	fmt.Printf("File content: %s\n", content)

	// Now remove it
	if err := RemoveFile("test_file.txt"); err != "" {
		fmt.Printf("Error removing file: %s\n", err)
		return
	}
	fmt.Println("File removed successfully")
}
`

	result := sandbox.Execute(context.Background(), map[string]interface{}{
		"code":    code,
		"timeout": 30,
	})

	if result.Error != "" {
		t.Fatalf("Sandbox execution failed: %s", result.Error)
	}

	// Verify file was removed
	exists, err := mockFS.Exists(context.Background(), testPath)
	if err != nil {
		t.Fatalf("Failed to check if file exists: %v", err)
	}
	if exists {
		t.Errorf("File still exists after RemoveFile")
	}

	t.Logf("Result: %v", result.Result)
}

func TestSandboxRemoveDir(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	// Create mock filesystem and session
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", "/tmp")

	// Create test directory structure
	if err := mockFS.MkdirAll(context.Background(), "test_dir/subdir", 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	if err := mockFS.WriteFile(context.Background(), "test_dir/file.txt", []byte("content")); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create sandbox tool
	sandbox := NewSandboxToolWithFS("/tmp", "/tmp", mockFS, sess, nil)

	// Test code that removes a directory recursively
	code := `
package main

import "fmt"

func main() {
	// Remove directory and all contents
	if err := RemoveDir("test_dir", true); err != "" {
		fmt.Printf("Error removing directory: %s\n", err)
		return
	}
	fmt.Println("Directory removed successfully")
}
`

	result := sandbox.Execute(context.Background(), map[string]interface{}{
		"code":    code,
		"timeout": 30,
	})

	if result.Error != "" {
		t.Fatalf("Sandbox execution failed: %s", result.Error)
	}

	// Verify directory was removed
	exists, err := mockFS.Exists(context.Background(), "test_dir")
	if err != nil {
		t.Fatalf("Failed to check if directory exists: %v", err)
	}
	if exists {
		t.Errorf("Directory still exists after RemoveDir")
	}

	t.Logf("Result: %v", result.Result)
}

func TestSandboxRemoveFileRequiresRead(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	// Create mock filesystem and session
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", "/tmp")

	// Create test file
	testPath := "test_file.txt"
	testContent := []byte("test content")
	if err := mockFS.WriteFile(context.Background(), testPath, testContent); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create sandbox tool
	sandbox := NewSandboxToolWithFS("/tmp", "/tmp", mockFS, sess, nil)

	// Test code that tries to remove without reading first (should fail)
	code := `
package main

import "fmt"

func main() {
	// Try to remove without reading first
	if err := RemoveFile("test_file.txt"); err != "" {
		fmt.Printf("Expected error: %s\n", err)
		return
	}
	fmt.Println("This should not be reached")
}
`

	result := sandbox.Execute(context.Background(), map[string]interface{}{
		"code":    code,
		"timeout": 30,
	})

	if result.Error != "" {
		t.Fatalf("Sandbox execution failed: %s", result.Error)
	}

	// File should still exist because removal should have failed
	exists, err := mockFS.Exists(context.Background(), testPath)
	if err != nil {
		t.Fatalf("Failed to check if file exists: %v", err)
	}
	if !exists {
		t.Errorf("File was removed despite not being read first")
	}

	t.Logf("Result: %v", result.Result)
}
