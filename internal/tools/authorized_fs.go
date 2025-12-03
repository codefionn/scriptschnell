package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	internalfs "github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

// AuthorizedFS wraps a FileSystem and enforces authorization rules
// It ensures that sandboxed code can only access files that have been
// previously read via the read_file tool (read-before-write rule)
type AuthorizedFS struct {
	underlying internalfs.FileSystem
	session    *session.Session
	workingDir string
}

// NewAuthorizedFS creates a new authorized filesystem wrapper
func NewAuthorizedFS(underlying internalfs.FileSystem, sess *session.Session, workingDir string) *AuthorizedFS {
	return &AuthorizedFS{
		underlying: underlying,
		session:    sess,
		workingDir: workingDir,
	}
}

// isPathAllowed checks if a path can be accessed based on session rules
func (afs *AuthorizedFS) isPathAllowed(path string, requireRead bool) error {
	if afs.session == nil {
		// No session means no authorization - allow all
		return nil
	}

	// Clean and normalize the path
	cleanPath := filepath.Clean(path)

	// If this is a write operation, check read-before-write rule
	if requireRead {
		if !afs.session.WasFileRead(cleanPath) {
			return fmt.Errorf("access denied: file %s was not read in this session (read-before-write rule)", path)
		}
	}

	return nil
}

// ReadFile reads a file with authorization check
func (afs *AuthorizedFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	// Reading is always allowed - we just track it
	data, err := afs.underlying.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	// Track that this file was read
	if afs.session != nil {
		afs.session.TrackFileRead(path, string(data))
	}

	return data, nil
}

// ReadFileLines reads file lines with authorization check
func (afs *AuthorizedFS) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	// Reading is always allowed - we just track it
	lines, err := afs.underlying.ReadFileLines(ctx, path, from, to)
	if err != nil {
		return nil, err
	}

	// Track that this file was read
	if afs.session != nil {
		content := strings.Join(lines, "\n")
		afs.session.TrackFileRead(path, content)
	}

	return lines, nil
}

// WriteFile writes a file with authorization check (requires prior read)
func (afs *AuthorizedFS) WriteFile(ctx context.Context, path string, data []byte) error {
	// Check if file exists first
	exists, err := afs.underlying.Exists(ctx, path)
	if err != nil {
		return err
	}

	// Only enforce read-before-write for existing files
	if exists {
		if err := afs.isPathAllowed(path, true); err != nil {
			return err
		}
	}

	// Perform the write
	if err := afs.underlying.WriteFile(ctx, path, data); err != nil {
		return err
	}

	// Track that this file was modified
	if afs.session != nil {
		afs.session.TrackFileModified(path)
		// Also track as read so subsequent operations are allowed
		afs.session.TrackFileRead(path, string(data))
	}

	return nil
}

// Stat returns file information (always allowed)
func (afs *AuthorizedFS) Stat(ctx context.Context, path string) (*internalfs.FileInfo, error) {
	return afs.underlying.Stat(ctx, path)
}

// ListDir lists directory contents (always allowed)
func (afs *AuthorizedFS) ListDir(ctx context.Context, path string) ([]*internalfs.FileInfo, error) {
	return afs.underlying.ListDir(ctx, path)
}

// Exists checks if a file exists (always allowed)
func (afs *AuthorizedFS) Exists(ctx context.Context, path string) (bool, error) {
	return afs.underlying.Exists(ctx, path)
}

// Delete removes a file with authorization check (requires prior read)
func (afs *AuthorizedFS) Delete(ctx context.Context, path string) error {
	// Check if it's a directory
	stat, err := afs.underlying.Stat(ctx, path)
	if err != nil {
		return err
	}

	// For files, enforce read-before-write rule
	if !stat.IsDir {
		if err := afs.isPathAllowed(path, true); err != nil {
			return err
		}
	}

	// Perform the delete
	if err := afs.underlying.Delete(ctx, path); err != nil {
		return err
	}

	// Track modification
	if afs.session != nil {
		afs.session.TrackFileModified(path)
	}

	return nil
}

// DeleteAll removes a directory and contents with authorization check
func (afs *AuthorizedFS) DeleteAll(ctx context.Context, path string) error {
	// For recursive deletion, we don't enforce read-before-write on every file
	// but we do track the modification
	if err := afs.underlying.DeleteAll(ctx, path); err != nil {
		return err
	}

	// Track modification
	if afs.session != nil {
		afs.session.TrackFileModified(path)
	}

	return nil
}

// MkdirAll creates directories (always allowed, no authorization needed)
func (afs *AuthorizedFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if err := afs.underlying.MkdirAll(ctx, path, perm); err != nil {
		return err
	}

	// Track modification
	if afs.session != nil {
		afs.session.TrackFileModified(path)
	}

	return nil
}

// Move renames or moves a file/directory with authorization check
func (afs *AuthorizedFS) Move(ctx context.Context, src, dst string) error {
	// Check if source is a file or directory
	stat, err := afs.underlying.Stat(ctx, src)
	if err != nil {
		return err
	}

	// For files, enforce read-before-write rule on source
	if !stat.IsDir {
		if err := afs.isPathAllowed(src, true); err != nil {
			return err
		}
	}

	// Perform the move
	if err := afs.underlying.Move(ctx, src, dst); err != nil {
		return err
	}

	// Track modifications
	if afs.session != nil {
		afs.session.TrackFileModified(src)
		afs.session.TrackFileModified(dst)
	}

	return nil
}
