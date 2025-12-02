package tools

// TinyGo Manager Tests
//
// This file contains tests for the TinyGo manager functionality:
// 1. Unit tests: Fast tests that verify URL generation, cache directory logic, etc.
// 2. Integration tests: Slow tests that actually download TinyGo (~50MB)
//
// Integration tests require:
// - Network access to download TinyGo from GitHub releases
// - Disk space for the cached TinyGo binary
//
// To run only fast unit tests (default): go test ./internal/tools
// To run integration tests: go test -run Integration ./internal/tools -integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTinyGoManager_GetCacheDir(t *testing.T) {
	cacheDir, err := getTinyGoCacheDir()
	if err != nil {
		t.Fatalf("Failed to get cache directory: %v", err)
	}

	// Verify the path contains expected components
	switch runtime.GOOS {
	case "linux":
		if !contains(cacheDir, ".cache") {
			t.Errorf("Expected Linux cache dir to contain .cache, got: %s", cacheDir)
		}
	case "darwin":
		if !contains(cacheDir, "Library/Caches") {
			t.Errorf("Expected macOS cache dir to contain Library/Caches, got: %s", cacheDir)
		}
	case "windows":
		// Windows can vary, just check it's not empty
		if cacheDir == "" {
			t.Errorf("Expected non-empty cache dir for Windows")
		}
	}

	if !contains(cacheDir, "scriptschnell") || !contains(cacheDir, "tinygo") {
		t.Errorf("Expected cache dir to contain statcode-ai/tinygo, got: %s", cacheDir)
	}
}

func TestTinyGoManager_GetDownloadURL(t *testing.T) {
	mgr, err := NewTinyGoManager()
	if err != nil {
		t.Fatalf("Failed to create TinyGo manager: %v", err)
	}

	url, fileName, err := mgr.getTinyGoDownloadURL()
	if err != nil {
		t.Fatalf("Failed to get download URL: %v", err)
	}

	// Verify URL structure
	expectedPrefix := "https://github.com/tinygo-org/tinygo/releases/download/v" + tinyGoVersion
	if !contains(url, expectedPrefix) {
		t.Errorf("Expected URL to start with %s, got: %s", expectedPrefix, url)
	}

	// Verify filename contains version
	if !contains(fileName, tinyGoVersion) {
		t.Errorf("Expected filename to contain version %s, got: %s", tinyGoVersion, fileName)
	}

	// Verify platform-specific extension
	switch runtime.GOOS {
	case "linux", "darwin":
		if !contains(fileName, ".tar.gz") {
			t.Errorf("Expected Unix filename to end with .tar.gz, got: %s", fileName)
		}
	case "windows":
		if !contains(fileName, ".zip") {
			t.Errorf("Expected Windows filename to end with .zip, got: %s", fileName)
		}
	}
}

func TestTinyGoManager_BinaryPath(t *testing.T) {
	mgr, err := NewTinyGoManager()
	if err != nil {
		t.Fatalf("Failed to create TinyGo manager: %v", err)
	}

	expectedBinaryName := "tinygo"
	if runtime.GOOS == "windows" {
		expectedBinaryName = "tinygo.exe"
	}

	expectedPath := filepath.Join(mgr.cacheDir, tinyGoVersion, "bin", expectedBinaryName)

	// This is the path that would be returned after download
	t.Logf("Expected TinyGo binary path: %s", expectedPath)
}

// TestTinyGoManager_Download tests the actual download (integration test)
// This test is skipped by default as it downloads ~50MB and takes several minutes
func TestIntegration_TinyGoManager_Download(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping TinyGo download integration test - run with: go test -run Integration ./internal/tools -short=false")
	}

	// Create a temporary cache directory for testing
	tmpDir, err := os.MkdirTemp("", "tinygo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Download TinyGo
	t.Log("Starting TinyGo download (this may take a few minutes)...")
	binaryPath, err := mgr.GetTinyGoBinary(ctx)
	if err != nil {
		t.Fatalf("Failed to get TinyGo binary: %v", err)
	}

	// Verify binary exists
	if _, err := os.Stat(binaryPath); err != nil {
		t.Errorf("TinyGo binary does not exist at %s: %v", binaryPath, err)
	}

	// Verify binary is in the expected location
	expectedBinaryName := "tinygo"
	if runtime.GOOS == "windows" {
		expectedBinaryName = "tinygo.exe"
	}
	expectedPath := filepath.Join(tmpDir, tinyGoVersion, "bin", expectedBinaryName)

	if binaryPath != expectedPath {
		t.Errorf("Expected binary path %s, got %s", expectedPath, binaryPath)
	}

	t.Logf("Successfully downloaded TinyGo to: %s", binaryPath)

	// Test that subsequent calls use the cached version
	binaryPath2, err := mgr.GetTinyGoBinary(ctx)
	if err != nil {
		t.Fatalf("Failed to get cached TinyGo binary: %v", err)
	}

	if binaryPath != binaryPath2 {
		t.Errorf("Expected cached binary path to be the same")
	}

	t.Log("Successfully verified cached TinyGo binary")
}

// TestTinyGoManager_CheckPATH tests the PATH detection functionality
func TestTinyGoManager_CheckPATH(t *testing.T) {
	mgr, err := NewTinyGoManager()
	if err != nil {
		t.Fatalf("Failed to create TinyGo manager: %v", err)
	}

	// Test checking for tinygo in PATH
	path, err := mgr.checkTinyGoInPATH()

	// The result depends on whether tinygo is installed in the system
	if err != nil {
		t.Logf("TinyGo not found in PATH (expected if not installed): %v", err)
	} else {
		t.Logf("Found TinyGo in PATH at: %s", path)

		// Verify we can execute it
		cmd := exec.Command(path, "version")
		output, err := cmd.Output()
		if err != nil {
			t.Errorf("Found tinygo but failed to execute: %v", err)
		}
		t.Logf("TinyGo version: %s", string(output))
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
