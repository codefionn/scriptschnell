package fs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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
	// Exists checks if a file exists
	Exists(ctx context.Context, path string) (bool, error)
	// Delete removes a file or empty directory
	Delete(ctx context.Context, path string) error
	// DeleteAll removes a directory and all its contents recursively
	DeleteAll(ctx context.Context, path string) error
	// MkdirAll creates a directory and all parent directories
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	// Move renames or moves a file or directory to a new path
	Move(ctx context.Context, src, dst string) error
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
	closeOnce  sync.Once
}

type dirCacheEntry struct {
	entries   []*FileInfo
	timestamp time.Time
}

// NewCachedFS creates a new cached filesystem with fsnotify
func NewCachedFS(baseDir string, cacheTTL time.Duration, maxEntries int) *CachedFS {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Warning: failed to create file watcher: %v", err)
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
	var err error
	cfs.closeOnce.Do(func() {
		close(cfs.stopWatch)
		if cfs.watcher != nil {
			err = cfs.watcher.Close()
		}
	})
	return err
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
			log.Printf("Filesystem watcher error: %v", err)
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
			log.Printf("CachedFS: failed to add watcher for %s: %v", absPath, err)
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
			log.Printf("CachedFS: failed to add watcher for %s: %v", absPath, err)
		}
	}

	return result, nil
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

func (cfs *CachedFS) DeleteAll(ctx context.Context, path string) error {
	absPath := cfs.absPath(path)
	if err := os.RemoveAll(absPath); err != nil {
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

func (cfs *CachedFS) Move(ctx context.Context, src, dst string) error {
	absSrc := cfs.absPath(src)
	absDst := cfs.absPath(dst)

	// Ensure destination parent exists to match typical mv behavior
	if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
		return err
	}

	if err := os.Rename(absSrc, absDst); err != nil {
		return err
	}

	// Invalidate caches for source and destination parents
	cfs.InvalidateDirCache(filepath.Dir(src))
	cfs.InvalidateDirCache(filepath.Dir(dst))
	return nil
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
		dirs:  map[string]bool{".": true},
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
	delete(mfs.dirs, path)
	return nil
}

func (mfs *MockFS) DeleteAll(ctx context.Context, path string) error {
	mfs.mu.Lock()
	defer mfs.mu.Unlock()

	// Remove the directory itself
	delete(mfs.dirs, path)

	// Remove all files and subdirectories under this path
	prefix := path + "/"
	for filePath := range mfs.files {
		if filePath == path || strings.HasPrefix(filePath, prefix) {
			delete(mfs.files, filePath)
		}
	}
	for dirPath := range mfs.dirs {
		if dirPath == path || strings.HasPrefix(dirPath, prefix) {
			delete(mfs.dirs, dirPath)
		}
	}

	return nil
}

func (mfs *MockFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	mfs.mu.Lock()
	defer mfs.mu.Unlock()

	if path == "" {
		path = "."
	}

	dir := path
	for {
		mfs.dirs[dir] = true
		parent := filepath.Dir(dir)
		if parent == dir || parent == "." || parent == "" {
			mfs.dirs["."] = true
			break
		}
		dir = parent
	}

	return nil
}

func (mfs *MockFS) Move(ctx context.Context, src, dst string) error {
	mfs.mu.Lock()
	defer mfs.mu.Unlock()

	// Helper to ensure destination directories exist
	ensureDirs := func(path string) {
		dir := filepath.Dir(path)
		for dir != "." && dir != "/" && dir != "" {
			mfs.dirs[dir] = true
			dir = filepath.Dir(dir)
		}
		mfs.dirs["."] = true
	}

	// Move file if it exists
	if data, ok := mfs.files[src]; ok {
		delete(mfs.files, src)
		mfs.files[dst] = data
		ensureDirs(dst)
		return nil
	}

	// Move directory (and all children) if it exists
	if mfs.dirs[src] {
		newDirs := make(map[string]bool)
		for dirPath := range mfs.dirs {
			if dirPath == src || strings.HasPrefix(dirPath, src+"/") {
				suffix := strings.TrimPrefix(dirPath, src)
				suffix = strings.TrimPrefix(suffix, "/")
				newPath := filepath.Join(dst, suffix)
				newDirs[newPath] = true
			} else {
				newDirs[dirPath] = true
			}
		}

		newFiles := make(map[string][]byte)
		for filePath, data := range mfs.files {
			if filePath == src || strings.HasPrefix(filePath, src+"/") {
				suffix := strings.TrimPrefix(filePath, src)
				suffix = strings.TrimPrefix(suffix, "/")
				newPath := filepath.Join(dst, suffix)
				newFiles[newPath] = data
			} else {
				newFiles[filePath] = data
			}
		}

		mfs.files = newFiles
		mfs.dirs = newDirs
		ensureDirs(dst)
		return nil
	}

	return os.ErrNotExist
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
