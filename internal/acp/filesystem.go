package acp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
)

// ACPFileSystem implements the fs.FileSystem interface using the ACP client's filesystem protocol
type ACPFileSystem struct {
	conn       *acp.AgentSideConnection
	sessionID  string
	workingDir string
	fallbackFS fs.FileSystem
}

// NewACPFileSystem creates a new ACP filesystem wrapper
func NewACPFileSystem(conn *acp.AgentSideConnection, sessionID string, workingDir string) *ACPFileSystem {
	logger.Debug("ACP FS: creating filesystem wrapper (session=%s workingDir=%s)", sessionID, workingDir)
	return &ACPFileSystem{
		conn:       conn,
		sessionID:  sessionID,
		workingDir: workingDir,
		fallbackFS: fs.NewCachedFS(workingDir, 0, 0), // Create fallback filesystem
	}
}

// isOutsideCodebase determines if a file path is outside the working directory (codebase)
func (afs *ACPFileSystem) isOutsideCodebase(path string) bool {
	// If it's an absolute path, check if it starts with working directory
	if strings.HasPrefix(path, "/") {
		return !strings.HasPrefix(path, afs.workingDir+"/") && path != afs.workingDir
	}

	// For relative paths, check if they try to go outside the working directory
	// using ".." to escape the working directory
	if strings.Contains(path, "..") {
		logger.Debug("ACP FS: rejecting path %s outside codebase", path)
		return true
	}

	return false
}

// ReadFile implements fs.FileSystem using client's readTextFile
func (afs *ACPFileSystem) ReadFile(ctx context.Context, path string) ([]byte, error) {
	logger.Debug("ACP FS: Reading file %s", path)

	// Normalize path to ensure it's relative
	originalPath := path
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return nil, fmt.Errorf("invalid path: empty")
	}

	// Check if file is outside codebase and fallback to normal filesystem
	if afs.isOutsideCodebase(originalPath) {
		logger.Debug("ACP FS: File %s is outside codebase, using fallback filesystem", originalPath)
		return afs.fallbackFS.ReadFile(ctx, originalPath)
	}

	logger.Debug("ACP FS: requesting ReadTextFile via ACP (session=%s path=%s)", afs.sessionID, path)
	// Use the client's filesystem protocol
	result, err := afs.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: acp.SessionId(afs.sessionID),
		Path:      path,
	})

	if err != nil {
		logger.Error("ACP FS: Error reading file %s: %v", path, err)
		return nil, fmt.Errorf("failed to read file %s via ACP: %w", path, err)
	}

	logger.Debug("ACP FS: Successfully read %d bytes from %s", len(result.Content), path)
	return []byte(result.Content), nil
}

// ReadFileLines implements fs.FileSystem by reading the full file and splitting into lines
func (afs *ACPFileSystem) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	data, err := afs.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")

	// Handle empty file case
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}

	// Adjust indices (1-indexed to 0-indexed)
	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}

	if from > to {
		return []string{}, nil
	}

	return lines[from-1 : to], nil
}

// WriteFile implements fs.FileSystem using client's writeTextFile
func (afs *ACPFileSystem) WriteFile(ctx context.Context, path string, data []byte) error {
	logger.Debug("ACP FS: Writing file %s (%d bytes)", path, len(data))

	// Normalize path to ensure it's relative
	originalPath := path
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return fmt.Errorf("invalid path: empty")
	}

	// Check if file is outside codebase and fallback to normal filesystem
	if afs.isOutsideCodebase(originalPath) {
		logger.Debug("ACP FS: File %s is outside codebase, using fallback filesystem", originalPath)
		return afs.fallbackFS.WriteFile(ctx, originalPath, data)
	}

	logger.Debug("ACP FS: requesting WriteTextFile via ACP (session=%s path=%s bytes=%d)", afs.sessionID, path, len(data))
	content := string(data)

	// Use the client's filesystem protocol
	_, err := afs.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{
		SessionId: acp.SessionId(afs.sessionID),
		Path:      path,
		Content:   content,
	})

	if err != nil {
		logger.Error("ACP FS: Error writing file %s: %v", path, err)
		return fmt.Errorf("failed to write file %s via ACP: %w", path, err)
	}

	logger.Debug("ACP FS: Successfully wrote file %s", path)
	return nil
}

// Stat implements fs.FileSystem - limited implementation for ACP
func (afs *ACPFileSystem) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	return afs.fallbackFS.Stat(ctx, path)
}

// ListDir implements fs.FileSystem - limited implementation for ACP
func (afs *ACPFileSystem) ListDir(ctx context.Context, path string) ([]*fs.FileInfo, error) {
	// Check if file is outside codebase and fallback to normal filesystem
	if afs.isOutsideCodebase(path) {
		logger.Debug("ACP FS: ListDir for %s is outside codebase, using fallback filesystem", path)
		return afs.fallbackFS.ListDir(ctx, path)
	}

	// The ACP filesystem protocol doesn't have a direct directory listing method
	// This is a limitation - we would need to implement this via client-specific methods
	// For now, return an error to indicate this operation is not supported
	return nil, fmt.Errorf("directory listing not supported via ACP filesystem protocol")
}

// Exists implements fs.FileSystem
func (afs *ACPFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	return afs.fallbackFS.Exists(ctx, path)
}

// Delete implements fs.FileSystem - not supported via ACP
func (afs *ACPFileSystem) Delete(ctx context.Context, path string) error {
	return afs.fallbackFS.Delete(ctx, path)
}

// MkdirAll implements fs.FileSystem - not supported via ACP
func (afs *ACPFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return afs.fallbackFS.MkdirAll(ctx, path, perm)
}

// Close implements fs.FileSystem - no-op for ACP
func (afs *ACPFileSystem) Close() error {
	// No cleanup needed for ACP filesystem
	return nil
}

// Ensure ACPFileSystem implements fs.FileSystem
var _ fs.FileSystem = (*ACPFileSystem)(nil)
