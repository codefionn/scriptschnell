package fs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/statcode-ai/statcode-ai/internal/logger"
)

// FileInfo represents file metadata
type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// FileSystem is an abstraction over filesystem operations
type FileSystem interface {
	// ReadFile reads the entire file
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// ReadFileLines reads specific lines from a file
	ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error)
	// WriteFile writes data to a file
	WriteFile(ctx context.Context, path string, data []byte) error
	// Stat returns file information
	Stat(ctx context.Context, path string) (*FileInfo, error)
	// ListDir lists directory contents
	ListDir(ctx context.Context, path string) ([]*FileInfo, error)
	// ListDirFiltered lists directory contents, filtering out gitignored files
	ListDirFiltered(ctx context.Context, path string) ([]*FileInfo, error)
	// Exists checks if a file exists
	Exists(ctx context.Context, path string) (bool, error)
	// Delete removes a file
	Delete(ctx context.Context, path string) error
	// MkdirAll creates a directory and all parent directories
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
}

// CachedFS is a filesystem implementation with directory listing cache
type CachedFS struct {
	baseDir    string
	dirCache   map[string]*dirCacheEntry
	cacheMu    sync.RWMutex
	cacheTTL   time.Duration
	maxEntries int
	watcher    *fsnotify.Watcher
	stopWatch  chan struct{}
}

type dirCacheEntry struct {
	entries   []*FileInfo
	timestamp time.Time
}

// NewCachedFS creates a new cached filesystem with fsnotify
func NewCachedFS(baseDir string, cacheTTL time.Duration, maxEntries int) *CachedFS {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Global().Warn("failed to create file watcher: %v", err)
	}

	cfs := &CachedFS{
		baseDir:    baseDir,
		dirCache:   make(map[string]*dirCacheEntry),
		cacheTTL:   cacheTTL,
		maxEntries: maxEntries,
		watcher:    watcher,
		stopWatch:  make(chan struct{}),
	}

	// Start watching for file changes
	if watcher != nil {
		go cfs.watchFiles()
	}

	return cfs
}

// Close closes the filesystem watcher
func (cfs *CachedFS) Close() error {
	close(cfs.stopWatch)
	if cfs.watcher != nil {
		return cfs.watcher.Close()
	}
	return nil
}

// watchFiles monitors filesystem events and invalidates cache
func (cfs *CachedFS) watchFiles() {
	for {
		select {
		case <-cfs.stopWatch:
			return
		case event, ok := <-cfs.watcher.Events:
			if !ok {
				return
			}
			// Invalidate directory cache for the parent directory
			dir := filepath.Dir(event.Name)
			cfs.cacheMu.Lock()
			delete(cfs.dirCache, dir)
			cfs.cacheMu.Unlock()
		case err, ok := <-cfs.watcher.Errors:
			if !ok {
				return
			}
			logger.Global().Error("filesystem watcher error: %v", err)
		}
	}
}

// InvalidateDirCache removes a directory from cache
func (cfs *CachedFS) InvalidateDirCache(path string) {
	cfs.cacheMu.Lock()
	defer cfs.cacheMu.Unlock()
	delete(cfs.dirCache, path)
}

// ClearCache removes all entries from cache
func (cfs *CachedFS) ClearCache() {
	cfs.cacheMu.Lock()
	defer cfs.cacheMu.Unlock()
	cfs.dirCache = make(map[string]*dirCacheEntry)
}

func (cfs *CachedFS) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cfs.baseDir, path)
}

func (cfs *CachedFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	absPath := cfs.absPath(path)

	// No caching for file reads - always read from disk
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (cfs *CachedFS) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	data, err := cfs.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0)
	currentLine := 1
	lineStart := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			if currentLine >= from && currentLine <= to {
				lines = append(lines, string(data[lineStart:i]))
			}
			currentLine++
			lineStart = i + 1
			if currentLine > to {
				break
			}
		}
	}

	// Handle last line without newline
	if lineStart < len(data) && currentLine >= from && currentLine <= to {
		lines = append(lines, string(data[lineStart:]))
	}

	if from > currentLine {
		return nil, fmt.Errorf("from line %d exceeds file length %d", from, currentLine-1)
	}

	return lines, nil
}

func (cfs *CachedFS) WriteFile(ctx context.Context, path string, data []byte) error {
	absPath := cfs.absPath(path)

	// Ensure directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return err
	}

	// Invalidate directory cache for parent directory
	cfs.InvalidateDirCache(filepath.Dir(path))

	// Watch the parent directory if not already watched
	if cfs.watcher != nil {
		if err := cfs.watcher.Add(filepath.Dir(absPath)); err != nil {
			logger.Global().Warn("CachedFS: failed to add watcher for %s: %v", absPath, err)
		}
	}

	return nil
}

func (cfs *CachedFS) Stat(ctx context.Context, path string) (*FileInfo, error) {
	absPath := cfs.absPath(path)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	return &FileInfo{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

func (cfs *CachedFS) ListDir(ctx context.Context, path string) ([]*FileInfo, error) {
	absPath := cfs.absPath(path)

	// Check cache first
	cfs.cacheMu.RLock()
	if entry, ok := cfs.dirCache[absPath]; ok {
		if time.Since(entry.timestamp) < cfs.cacheTTL {
			cfs.cacheMu.RUnlock()
			return entry.entries, nil
		}
	}
	cfs.cacheMu.RUnlock()

	// Read from disk
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, err
	}

	result := make([]*FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, &FileInfo{
			Path:    filepath.Join(path, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		})
	}

	// Update cache
	cfs.cacheMu.Lock()
	// Evict old entries if cache is full
	if len(cfs.dirCache) >= cfs.maxEntries {
		// Simple eviction: remove oldest entry
		var oldestKey string
		var oldestTime time.Time
		for k, v := range cfs.dirCache {
			if oldestKey == "" || v.timestamp.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.timestamp
			}
		}
		delete(cfs.dirCache, oldestKey)
	}
	cfs.dirCache[absPath] = &dirCacheEntry{
		entries:   result,
		timestamp: time.Now(),
	}
	cfs.cacheMu.Unlock()

	// Watch this directory for changes
	if cfs.watcher != nil {
		if err := cfs.watcher.Add(absPath); err != nil {
			logger.Global().Warn("CachedFS: failed to add watcher for %s: %v", absPath, err)
		}
	}

	return result, nil
}

// ListDirFiltered lists directory contents, filtering out gitignored files and .git directory
func (cfs *CachedFS) ListDirFiltered(ctx context.Context, path string) ([]*FileInfo, error) {
	absPath := cfs.absPath(path)

	// Check cache first
	cfs.cacheMu.RLock()
	if entry, ok := cfs.dirCache[absPath]; ok {
		if time.Since(entry.timestamp) < cfs.cacheTTL {
			cfs.cacheMu.RUnlock()
			// Apply filtering to cached results
			return cfs.filterEntries(absPath, entry.entries), nil
		}
	}
	cfs.cacheMu.RUnlock()

	// Read from disk
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, err
	}

	// Parse all .gitignore files from baseDir to current directory
	matchers := cfs.loadGitignoreChain(absPath)

	result := make([]*FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		// Calculate relative path from baseDir for gitignore matching
		relPath, err := filepath.Rel(cfs.baseDir, filepath.Join(absPath, entry.Name()))
		if err != nil {
			relPath = entry.Name()
		}

		// Check if file should be ignored by any gitignore in the chain
		if cfs.isIgnored(relPath, entry.IsDir(), matchers) {
			continue
		}

		result = append(result, &FileInfo{
			Path:    filepath.Join(path, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		})
	}

	// Watch this directory for changes
	if cfs.watcher != nil {
		if err := cfs.watcher.Add(absPath); err != nil {
			logger.Global().Warn("CachedFS: failed to add watcher for %s: %v", absPath, err)
		}
	}

	return result, nil
}

// filterEntries filters a list of FileInfo entries based on gitignore rules
func (cfs *CachedFS) filterEntries(absPath string, entries []*FileInfo) []*FileInfo {
	matchers := cfs.loadGitignoreChain(absPath)
	result := make([]*FileInfo, 0, len(entries))

	for _, entry := range entries {
		// Skip .git directory
		if filepath.Base(entry.Path) == ".git" {
			continue
		}

		// Calculate relative path from baseDir for gitignore matching
		relPath, err := filepath.Rel(cfs.baseDir, filepath.Join(absPath, filepath.Base(entry.Path)))
		if err != nil {
			relPath = filepath.Base(entry.Path)
		}

		// Check if file should be ignored
		if cfs.isIgnored(relPath, entry.IsDir, matchers) {
			continue
		}

		result = append(result, entry)
	}

	return result
}

// loadGitignoreChain loads all .gitignore files from baseDir to the given directory
func (cfs *CachedFS) loadGitignoreChain(dir string) []*gitignoreMatcher {
	var matchers []*gitignoreMatcher

	// Walk up from baseDir to dir, collecting .gitignore files
	currentDir := cfs.baseDir
	for {
		gitignorePath := filepath.Join(currentDir, ".gitignore")
		matcher, err := parseGitignore(gitignorePath)
		if err == nil && matcher != nil {
			matchers = append(matchers, matcher)
		}

		// If we've reached the target directory, stop
		if currentDir == dir {
			break
		}

		// Move to next subdirectory towards dir
		relPath, err := filepath.Rel(currentDir, dir)
		if err != nil || relPath == "." {
			break
		}

		// Get the first component of the relative path
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) == 0 {
			break
		}

		currentDir = filepath.Join(currentDir, parts[0])

		// Safety check: don't go outside baseDir
		if !strings.HasPrefix(currentDir, cfs.baseDir) {
			break
		}
	}

	return matchers
}

// isIgnored checks if a path is ignored by any of the gitignore matchers
func (cfs *CachedFS) isIgnored(relPath string, isDir bool, matchers []*gitignoreMatcher) bool {
	for _, matcher := range matchers {
		if matcher.matches(relPath, isDir) {
			return true
		}
	}
	return false
}

func (cfs *CachedFS) Exists(ctx context.Context, path string) (bool, error) {
	absPath := cfs.absPath(path)
	_, err := os.Stat(absPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (cfs *CachedFS) Delete(ctx context.Context, path string) error {
	absPath := cfs.absPath(path)
	if err := os.Remove(absPath); err != nil {
		return err
	}

	// Invalidate directory cache for parent directory
	cfs.InvalidateDirCache(filepath.Dir(path))
	return nil
}

func (cfs *CachedFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	absPath := cfs.absPath(path)
	return os.MkdirAll(absPath, perm)
}

// MockFS is a mock filesystem for testing
type MockFS struct {
	files map[string][]byte
	dirs  map[string]bool
	mu    sync.RWMutex
}

func NewMockFS() *MockFS {
	return &MockFS{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (mfs *MockFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	mfs.mu.RLock()
	defer mfs.mu.RUnlock()

	data, ok := mfs.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (mfs *MockFS) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	data, err := mfs.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0)
	currentLine := 1
	lineStart := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			if currentLine >= from && currentLine <= to {
				lines = append(lines, string(data[lineStart:i]))
			}
			currentLine++
			lineStart = i + 1
			if currentLine > to {
				break
			}
		}
	}

	if lineStart < len(data) && currentLine >= from && currentLine <= to {
		lines = append(lines, string(data[lineStart:]))
	}

	return lines, nil
}

func (mfs *MockFS) WriteFile(ctx context.Context, path string, data []byte) error {
	mfs.mu.Lock()
	defer mfs.mu.Unlock()
	mfs.files[path] = data

	// Automatically create parent directories
	dir := filepath.Dir(path)
	for dir != "." && dir != "/" && dir != "" {
		mfs.dirs[dir] = true
		dir = filepath.Dir(dir)
	}
	mfs.dirs["."] = true

	return nil
}

func (mfs *MockFS) Stat(ctx context.Context, path string) (*FileInfo, error) {
	mfs.mu.RLock()
	defer mfs.mu.RUnlock()

	// Check if it's a directory
	if mfs.dirs[path] {
		return &FileInfo{
			Path:    path,
			Size:    0,
			ModTime: time.Now(),
			IsDir:   true,
		}, nil
	}

	// Check if it's a file
	data, ok := mfs.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}

	return &FileInfo{
		Path:    path,
		Size:    int64(len(data)),
		ModTime: time.Now(),
		IsDir:   false,
	}, nil
}

func (mfs *MockFS) ListDir(ctx context.Context, path string) ([]*FileInfo, error) {
	mfs.mu.RLock()
	defer mfs.mu.RUnlock()

	// Normalize path
	if path == "" {
		path = "."
	}

	// Check if directory exists
	if !mfs.dirs[path] {
		return nil, os.ErrNotExist
	}

	var entries []*FileInfo
	var pathPrefix string
	if path != "." {
		pathPrefix = path + "/"
	}

	// Find all files and directories that are direct children
	seen := make(map[string]bool)

	// Add files
	for filePath := range mfs.files {
		// Skip if not in this directory
		if pathPrefix != "" && !strings.HasPrefix(filePath, pathPrefix) {
			continue
		}

		// Get relative path
		rel := filePath
		if pathPrefix != "" {
			rel = strings.TrimPrefix(filePath, pathPrefix)
		}

		// If it contains a slash, it's in a subdirectory
		if strings.Contains(rel, "/") {
			// Add the subdirectory
			subdir := strings.Split(rel, "/")[0]
			subdirPath := filepath.Join(path, subdir)
			if path == "." {
				subdirPath = subdir
			}
			if !seen[subdirPath] {
				seen[subdirPath] = true
				entries = append(entries, &FileInfo{
					Path:    subdirPath,
					Size:    0,
					ModTime: time.Now(),
					IsDir:   true,
				})
			}
		} else {
			// Direct file in this directory
			if !seen[filePath] {
				seen[filePath] = true
				entries = append(entries, &FileInfo{
					Path:    filePath,
					Size:    int64(len(mfs.files[filePath])),
					ModTime: time.Now(),
					IsDir:   false,
				})
			}
		}
	}

	return entries, nil
}

// ListDirFiltered lists directory contents, filtering out gitignored files and .git directory
func (mfs *MockFS) ListDirFiltered(ctx context.Context, path string) ([]*FileInfo, error) {
	mfs.mu.RLock()
	defer mfs.mu.RUnlock()

	// Normalize path
	if path == "" {
		path = "."
	}

	// Check if directory exists
	if !mfs.dirs[path] {
		return nil, os.ErrNotExist
	}

	// Load gitignore chain
	matchers := mfs.loadGitignoreChain(path)

	var entries []*FileInfo
	var pathPrefix string
	if path != "." {
		pathPrefix = path + "/"
	}

	// Find all files and directories that are direct children
	seen := make(map[string]bool)

	// Add files
	for filePath := range mfs.files {
		// Skip if not in this directory
		if pathPrefix != "" && !strings.HasPrefix(filePath, pathPrefix) {
			continue
		}

		// Get relative path
		rel := filePath
		if pathPrefix != "" {
			rel = strings.TrimPrefix(filePath, pathPrefix)
		}

		// If it contains a slash, it's in a subdirectory
		if strings.Contains(rel, "/") {
			// Add the subdirectory
			subdir := strings.Split(rel, "/")[0]
			subdirPath := filepath.Join(path, subdir)
			if path == "." {
				subdirPath = subdir
			}
			if !seen[subdirPath] {
				seen[subdirPath] = true

				// Skip .git directory
				if subdir == ".git" {
					continue
				}

				// Check gitignore
				if mfs.isIgnored(subdirPath, true, matchers) {
					continue
				}

				entries = append(entries, &FileInfo{
					Path:    subdirPath,
					Size:    0,
					ModTime: time.Now(),
					IsDir:   true,
				})
			}
		} else {
			// Direct file in this directory
			if !seen[filePath] {
				seen[filePath] = true

				// Check gitignore
				if mfs.isIgnored(filePath, false, matchers) {
					continue
				}

				entries = append(entries, &FileInfo{
					Path:    filePath,
					Size:    int64(len(mfs.files[filePath])),
					ModTime: time.Now(),
					IsDir:   false,
				})
			}
		}
	}

	return entries, nil
}

// loadGitignoreChain loads all .gitignore files from root to the given directory
func (mfs *MockFS) loadGitignoreChain(dir string) []*gitignoreMatcher {
	var matchers []*gitignoreMatcher

	// Walk from root (.) to dir, collecting .gitignore files
	currentDir := "."

	// Add root .gitignore if exists
	gitignorePath := ".gitignore"
	if data, ok := mfs.files[gitignorePath]; ok {
		matcher, err := parseGitignoreFromBytes(data)
		if err == nil && matcher != nil {
			matchers = append(matchers, matcher)
		}
	}

	// If we're not at root, walk subdirectories
	if dir != "." && dir != "" {
		parts := strings.Split(dir, string(filepath.Separator))
		for i := range parts {
			currentDir = strings.Join(parts[:i+1], string(filepath.Separator))
			gitignorePath = filepath.Join(currentDir, ".gitignore")

			if data, ok := mfs.files[gitignorePath]; ok {
				matcher, err := parseGitignoreFromBytes(data)
				if err == nil && matcher != nil {
					matchers = append(matchers, matcher)
				}
			}
		}
	}

	return matchers
}

// isIgnored checks if a path is ignored by any of the gitignore matchers
func (mfs *MockFS) isIgnored(path string, isDir bool, matchers []*gitignoreMatcher) bool {
	// Normalize path
	relPath := strings.TrimPrefix(path, "./")

	for _, matcher := range matchers {
		if matcher.matches(relPath, isDir) {
			return true
		}
	}
	return false
}

func (mfs *MockFS) Exists(ctx context.Context, path string) (bool, error) {
	mfs.mu.RLock()
	defer mfs.mu.RUnlock()

	// Check if it's a file
	if _, ok := mfs.files[path]; ok {
		return true, nil
	}

	// Check if it's a directory
	if mfs.dirs[path] {
		return true, nil
	}

	return false, nil
}

func (mfs *MockFS) Delete(ctx context.Context, path string) error {
	mfs.mu.Lock()
	defer mfs.mu.Unlock()
	delete(mfs.files, path)
	return nil
}

func (mfs *MockFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}

// Helper function to copy a file
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
