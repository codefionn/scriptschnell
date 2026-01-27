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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
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

// New tests for embedded archive functionality

// TestExtractTarGzData tests extraction of tar.gz data from memory
func TestExtractTarGzData(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create test tar.gz data with the "tinygo/" prefix
	var buf bytes.Buffer

	// Create gzip writer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add some test files
	testFiles := []struct {
		name    string
		content string
	}{
		{"tinygo/bin/tinygo", "tinygo binary content"},
		{"tinygo/pkg/path/to/file.txt", "some package file"},
		{"tinygo/README.md", "TinyGo README"},
	}

	for _, tf := range testFiles {
		hdr := &tar.Header{
			Name:     tf.name,
			Mode:     0644,
			Size:     int64(len(tf.content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("Failed to write header: %v", err)
		}
		if _, err := tw.Write([]byte(tf.content)); err != nil {
			t.Fatalf("Failed to write content: %v", err)
		}
	}

	// Add a directory
	dirHdr := &tar.Header{
		Name:     "tinygo/some/dir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	if err := tw.WriteHeader(dirHdr); err != nil {
		t.Fatalf("Failed to write directory header: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Extract the archive
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractTarGzData(buf.Bytes(), destDir); err != nil {
		t.Fatalf("Failed to extract tar.gz: %v", err)
	}

	// Verify files were extracted (without "tinygo/" prefix)
	expectedFiles := []string{
		"bin/tinygo",
		"pkg/path/to/file.txt",
		"README.md",
		"some/dir",
	}

	for _, expectedFile := range expectedFiles {
		expectedPath := filepath.Join(destDir, expectedFile)
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", expectedPath)
		}
	}

	// Verify content
	tinygoPath := filepath.Join(destDir, "bin/tinygo")
	content, err := os.ReadFile(tinygoPath)
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}
	if string(content) != "tinygo binary content" {
		t.Errorf("Content mismatch: expected %q, got %q", "tinygo binary content", string(content))
	}
}

// TestExtractTarGzDataWithEmptyPrefix tests handling of files with empty prefix
func TestExtractTarGzDataWithEmptyPrefix(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create tar.gz without "tinygo/" prefix
	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	testFile := "direct/file.txt"
	hdr := &tar.Header{
		Name:     testFile,
		Mode:     0644,
		Size:     int64(9),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}
	if _, err := tw.Write([]byte("test data")); err != nil {
		t.Fatalf("Failed to write content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Extract
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractTarGzData(buf.Bytes(), destDir); err != nil {
		t.Fatalf("Failed to extract tar.gz: %v", err)
	}

	// Verify file exists
	expectedPath := filepath.Join(destDir, testFile)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected file %s to exist", expectedPath)
	}
}

// TestExtractTarGzDataInvalidGzip tests error handling for invalid gzip data
func TestExtractTarGzDataInvalidGzip(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Try to extract invalid gzip data
	invalidData := []byte("not a valid gzip file")

	destDir := filepath.Join(tmpDir, "extracted")
	err := mgr.extractTarGzData(invalidData, destDir)

	if err == nil {
		t.Error("Expected error when extracting invalid gzip data")
	}
}

// TestExtractTarGzDataInvalidTar tests error handling for invalid tar data
func TestExtractTarGzDataInvalidTar(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create valid gzip but invalid tar data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte("not valid tar")); err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	destDir := filepath.Join(tmpDir, "extracted")
	err := mgr.extractTarGzData(buf.Bytes(), destDir)

	// The tar reader will fail when trying to read the first header
	// The error might vary, but it should fail
	if err == nil {
		t.Error("Expected error when extracting invalid tar data")
	}
}

// TestExtractZipData tests extraction of zip data from memory
func TestExtractZipData(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create test zip data with "tinygo/" prefix
	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	testFiles := []struct {
		name    string
		content string
		isDir   bool
	}{
		{"tinygo/bin/tinygo.exe", "tinygo binary content", false},
		{"tinygo/pkg/path/to/file.txt", "some package file", false},
		{"tinygo/README.md", "TinyGo README", false},
		{"tinygo/some/dir/", "", true},
	}

	for _, tf := range testFiles {
		if tf.isDir {
			// Create directory
			header := &zip.FileHeader{
				Name:   tf.name,
				Method: zip.Store,
			}
			header.SetMode(0755)
			_, err := w.CreateHeader(header)
			if err != nil {
				t.Fatalf("Failed to create directory entry: %v", err)
			}
		} else {
			// Create file
			writer, err := w.Create(tf.name)
			if err != nil {
				t.Fatalf("Failed to create file entry: %v", err)
			}
			if _, err := writer.Write([]byte(tf.content)); err != nil {
				t.Fatalf("Failed to write content: %v", err)
			}
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}

	// Extract the archive
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractZipData(buf.Bytes(), destDir, "embedded.zip"); err != nil {
		t.Fatalf("Failed to extract zip: %v", err)
	}

	// Verify files were extracted (without "tinygo/" prefix)
	expectedFiles := []string{
		"bin/tinygo.exe",
		"pkg/path/to/file.txt",
		"README.md",
		"some/dir",
	}

	for _, expectedFile := range expectedFiles {
		expectedPath := filepath.Join(destDir, expectedFile)
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", expectedPath)
		}
	}

	// Verify content
	tinygoPath := filepath.Join(destDir, "bin/tinygo.exe")
	content, err := os.ReadFile(tinygoPath)
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}
	if string(content) != "tinygo binary content" {
		t.Errorf("Content mismatch: expected %q, got %q", "tinygo binary content", string(content))
	}
}

// TestExtractZipDataWindowsPathHandling tests handling of Windows paths
func TestExtractZipDataWindowsPathHandling(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create zip with Windows-style paths
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// Use Windows-style path separators
	testFile := "tinygo\\bin\\tinygo.exe"
	writer, err := w.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create file entry: %v", err)
	}
	if _, err := writer.Write([]byte("test content")); err != nil {
		t.Fatalf("Failed to write content: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}

	// Extract
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractZipData(buf.Bytes(), destDir, "embedded.zip"); err != nil {
		t.Fatalf("Failed to extract zip: %v", err)
	}

	// Verify file exists
	// On non-Windows systems, backslashes in zip entries might be preserved in filenames
	// Check both possibilities: normalized path and path with backslashes
	expectedPathNormal := filepath.Join(destDir, "bin", "tinygo.exe")
	expectedPathBackslash := filepath.Join(destDir, "bin\\tinygo.exe")

	found := false
	for _, expectedPath := range []string{expectedPathNormal, expectedPathBackslash} {
		info, err := os.Stat(expectedPath)
		if err == nil && !info.IsDir() {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected file to exist at %s or %s", expectedPathNormal, expectedPathBackslash)
		// Print what actually exists for debugging
		filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				t.Logf("Found: %s", path)
			}
			return nil
		})
	}
}

// TestExtractZipDataInvalidZip tests error handling for invalid zip data
func TestExtractZipDataInvalidZip(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Try to extract invalid zip data
	invalidData := []byte("not a valid zip file")

	destDir := filepath.Join(tmpDir, "extracted")
	err := mgr.extractZipData(invalidData, destDir, "embedded.zip")

	if err == nil {
		t.Error("Expected error when extracting invalid zip data")
	}
}

// TestExtractZipDataEmptyZip tests handling of empty zip archive
func TestExtractZipDataEmptyZip(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create empty zip
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}

	// Extract - should succeed but create no files
	destDir := filepath.Join(tmpDir, "extracted")
	// Ensure the destination directory exists before extraction
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("Failed to create destination directory: %v", err)
	}

	if err := mgr.extractZipData(buf.Bytes(), destDir, "empty.zip"); err != nil {
		t.Fatalf("Failed to extract empty zip: %v", err)
	}

	// Verify directory exists but is empty
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected empty directory, found %d entries", len(entries))
	}
}

// TestTinyGoManagerStatusCallback tests status callback functionality
func TestTinyGoManagerStatusCallback(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewTinyGoManager()
	if err != nil {
		t.Fatalf("Failed to create TinyGo manager: %v", err)
	}

	// Override cache dir for testing
	mgr.cacheDir = tmpDir

	// Set status callback
	statusUpdates := []string{}
	var mu sync.Mutex
	mgr.SetStatusCallback(func(status string) {
		mu.Lock()
		defer mu.Unlock()
		statusUpdates = append(statusUpdates, status)
	})

	// Test updateStatus (non-locked version)
	mgr.updateStatus("test status 1")

	// Test updateStatusLocked (would be called from within GetTinyGoBinary)
	mgr.mu.Lock()
	mgr.updateStatusLocked("test status 2")
	mgr.mu.Unlock()

	// Verify callbacks were received
	mu.Lock()
	defer mu.Unlock()
	if len(statusUpdates) != 2 {
		t.Errorf("Expected 2 status updates, got %d", len(statusUpdates))
	}

	// Note: Order may vary due to concurrency
	for i, update := range statusUpdates {
		if update == "" {
			t.Errorf("Status update %d is empty", i)
		}
	}
}

// TestExtractLargeArchive tests extraction of a larger archive
func TestExtractLargeArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large archive test in short mode")
	}

	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create a larger archive with many files
	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	numFiles := 100
	for i := 0; i < numFiles; i++ {
		fileName := filepath.Join("tinygo", "dir", "subdir", fmt.Sprintf("file%d.txt", i))
		content := fmt.Sprintf("content of file %d\n", i)

		hdr := &tar.Header{
			Name:     fileName,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("Failed to write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Extract
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractTarGzData(buf.Bytes(), destDir); err != nil {
		t.Fatalf("Failed to extract archive: %v", err)
	}

	// Verify all files were extracted
	for i := 0; i < numFiles; i++ {
		expectedPath := filepath.Join(destDir, "dir", "subdir", fmt.Sprintf("file%d.txt", i))
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", expectedPath)
		}
	}
}

// TestExtractWithNestedDirectories tests extraction with deeply nested directories
func TestExtractWithNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &TinyGoManager{
		cacheDir: tmpDir,
		logger:   logger.Global().WithPrefix("test"),
	}

	// Create archive with deeply nested paths
	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	nestedPath := "tinygo/very/deep/nested/path/to/file.txt"
	content := "nested file content"

	hdr := &tar.Header{
		Name:     nestedPath,
		Mode:     0644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Extract
	destDir := filepath.Join(tmpDir, "extracted")
	if err := mgr.extractTarGzData(buf.Bytes(), destDir); err != nil {
		t.Fatalf("Failed to extract archive: %v", err)
	}

	// Verify nested file exists (without "tinygo/" prefix)
	expectedPath := filepath.Join(destDir, "very/deep/nested/path/to/file.txt")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected nested file %s to exist", expectedPath)
	}
}
