package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/coder/acp-go-sdk"
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

func (afs *ACPFileSystem) resolvePath(path string) (string, bool, error) {
	if path == "" {
		return "", false, fmt.Errorf("invalid path: empty")
	}

	originalPath := path

	if !filepath.IsAbs(path) {
		if afs.workingDir == "" {
			return "", false, fmt.Errorf("path must be absolute (got %q) and no working directory set to resolve", path)
		}
		path = filepath.Join(afs.workingDir, path)
	}

	path = filepath.Clean(path)
	workingDir := filepath.Clean(afs.workingDir)

	// Normalize to absolute paths
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve absolute path for %q: %w", originalPath, err)
	}
	path = absPath

	if workingDir != "" {
		absWD, err := filepath.Abs(workingDir)
		if err != nil {
			return "", false, fmt.Errorf("failed to resolve absolute working directory %q: %w", workingDir, err)
		}
		workingDir = absWD
	}

	// If the resolved path escapes the working directory, fall back to the local filesystem
	if workingDir != "" && path != workingDir && !strings.HasPrefix(path, workingDir+string(os.PathSeparator)) {
		logger.Debug("ACP FS: path %s is outside codebase, using fallback filesystem", originalPath)
		return path, true, nil
	}

	return path, false, nil
}

// ReadFile implements fs.FileSystem using client's readTextFile
func (afs *ACPFileSystem) ReadFile(ctx context.Context, path string) ([]byte, error) {
	logger.Debug("ACP FS: Reading file %s", path)

	resolvedPath, useFallback, err := afs.resolvePath(path)
	if err != nil {
		return nil, err
	}

	if useFallback {
		return afs.fallbackFS.ReadFile(ctx, resolvedPath)
	}

	logger.Debug("ACP FS: requesting ReadTextFile via ACP (session=%s path=%s)", afs.sessionID, resolvedPath)
	// Use the client's filesystem protocol
	result, err := afs.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: acp.SessionId(afs.sessionID),
		Path:      resolvedPath,
	})

	if err != nil {
		logger.Error("ACP FS: Error reading file %s: %v", resolvedPath, err)
		return nil, fmt.Errorf("failed to read file %s via ACP: %w", resolvedPath, err)
	}

	logger.Debug("ACP FS: Successfully read %d bytes from %s", len(result.Content), resolvedPath)
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

	resolvedPath, useFallback, err := afs.resolvePath(path)
	if err != nil {
		return err
	}

	if useFallback {
		return afs.fallbackFS.WriteFile(ctx, resolvedPath, data)
	}

	logger.Debug("ACP FS: requesting WriteTextFile via ACP (session=%s path=%s bytes=%d)", afs.sessionID, resolvedPath, len(data))
	content := string(data)

	// Use the client's filesystem protocol
	_, err = afs.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{
		SessionId: acp.SessionId(afs.sessionID),
		Path:      resolvedPath,
		Content:   content,
	})

	if err != nil {
		logger.Error("ACP FS: Error writing file %s: %v", resolvedPath, err)
		return fmt.Errorf("failed to write file %s via ACP: %w", resolvedPath, err)
	}

	logger.Debug("ACP FS: Successfully wrote file %s", resolvedPath)
	return nil
}

// Stat implements fs.FileSystem - limited implementation for ACP
func (afs *ACPFileSystem) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	return afs.fallbackFS.Stat(ctx, path)
}

// ListDir implements fs.FileSystem - limited implementation for ACP
func (afs *ACPFileSystem) ListDir(ctx context.Context, path string) ([]*fs.FileInfo, error) {
	return afs.fallbackFS.ListDir(ctx, path)
}

// Exists implements fs.FileSystem
func (afs *ACPFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	return afs.fallbackFS.Exists(ctx, path)
}

// Delete implements fs.FileSystem - not supported via ACP
func (afs *ACPFileSystem) Delete(ctx context.Context, path string) error {
	return afs.fallbackFS.Delete(ctx, path)
}

// DeleteAll implements fs.FileSystem - not supported via ACP
func (afs *ACPFileSystem) DeleteAll(ctx context.Context, path string) error {
	return afs.fallbackFS.DeleteAll(ctx, path)
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
