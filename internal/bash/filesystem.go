package bash

import (
	"io"
	"os"
	"time"
)

// VirtualFilesystem defines the interface for virtual filesystem operations
type VirtualFilesystem interface {
	// File operations
	Open(path string) (VirtualFile, error)
	Create(path string) (VirtualFile, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Delete(path string) error
	Stat(path string) (FileInfo, error)
	Rename(oldpath, newpath string) error
	Copy(src, dst string) error
	
	// Directory operations
	Mkdir(path string, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Rmdir(path string) error
	ListDir(path string) ([]FileInfo, error)
	Walk(path string, walkFn WalkFunc) error
	
	// Path operations
	Abs(path string) (string, error)
	Rel(basepath, targpath string) (string, error)
	Join(elem ...string) string
	Split(path string) (dir, file string)
	Clean(path string) string
	Exists(path string) bool
	IsDir(path string) bool
	IsFile(path string) bool
	
	// Permission operations
	Chmod(path string, mode os.FileMode) error
	Chown(path string, uid, gid int) error
	
	// Symlink operations
	Symlink(oldname, newname string) error
	Readlink(path string) (string, error)
	Lstat(path string) (FileInfo, error)
	
	// Temp operations
	TempDir(dir, prefix string) (string, error)
	TempFile(dir, pattern string) (VirtualFile, error)
	
	// Working directory
	Getwd() (string, error)
	Chdir(path string) error
}

// VirtualFile represents a virtual file handle
type VirtualFile interface {
	io.Reader
	io.Writer
	io.Closer
	io.Seeker
	
	Name() string
	Stat() (FileInfo, error)
	Sync() error
	Truncate(size int64) error
}

// FileInfo contains file metadata
type FileInfo interface {
	Name() string       // Base name of the file
	Size() int64        // Length in bytes
	Mode() os.FileMode  // File mode bits
	ModTime() time.Time // Modification time
	IsDir() bool        // Is directory
	Sys() interface{}   // Underlying data source (can return nil)
}

// WalkFunc is the type of the function called for each file or directory
type WalkFunc func(path string, info FileInfo, err error) error

// InMemoryFilesystem implements VirtualFilesystem in memory
type InMemoryFilesystem struct {
	root     *InMemoryNode
	wd       string
	umask    os.FileMode
	uid, gid int
}

// InMemoryNode represents a node in the in-memory filesystem
type InMemoryNode struct {
	name     string
	content  []byte
	children map[string]*InMemoryNode
	mode     os.FileMode
	modTime  time.Time
	uid, gid int
	symlink  string
	isDir    bool
	parent   *InMemoryNode
}

// NewInMemoryFilesystem creates a new in-memory filesystem
func NewInMemoryFilesystem() *InMemoryFilesystem {
	now := time.Now()
	root := &InMemoryNode{
		name:     "/",
		children: make(map[string]*InMemoryNode),
		mode:     os.ModeDir | 0755,
		modTime:  now,
		isDir:    true,
	}
	
	return &InMemoryFilesystem{
		root:  root,
		wd:    "/",
		umask: 022,
		uid:   os.Getuid(),
		gid:   os.Getgid(),
	}
}

// InMemoryFile represents an open file in the in-memory filesystem
type InMemoryFile struct {
	node    *InMemoryNode
	offset  int64
	closed  bool
	name    string
}

// NewInMemoryFile creates a new in-memory file handle
func NewInMemoryFile(name string, node *InMemoryNode) *InMemoryFile {
	return &InMemoryFile{
		node:   node,
		name:   name,
		closed: false,
	}
}

// Read implements io.Reader
func (f *InMemoryFile) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.offset >= int64(len(f.node.content)) {
		return 0, io.EOF
	}
	n = copy(p, f.node.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

// Write implements io.Writer
func (f *InMemoryFile) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	
	// Extend file if necessary
	end := f.offset + int64(len(p))
	if end > int64(cap(f.node.content)) {
		newContent := make([]byte, len(f.node.content), end+1024)
		copy(newContent, f.node.content)
		f.node.content = newContent
	}
	if end > int64(len(f.node.content)) {
		f.node.content = f.node.content[:end]
	}
	
	n = copy(f.node.content[f.offset:], p)
	f.offset += int64(n)
	f.node.modTime = time.Now()
	return n, nil
}

// Close implements io.Closer
func (f *InMemoryFile) Close() error {
	f.closed = true
	return nil
}

// Seek implements io.Seeker
func (f *InMemoryFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = f.offset + offset
	case io.SeekEnd:
		newOffset = int64(len(f.node.content)) + offset
	default:
		return 0, os.ErrInvalid
	}
	
	if newOffset < 0 {
		return 0, os.ErrInvalid
	}
	
	f.offset = newOffset
	return newOffset, nil
}

// Name returns the file name
func (f *InMemoryFile) Name() string {
	return f.name
}

// Stat returns file info
func (f *InMemoryFile) Stat() (FileInfo, error) {
	if f.closed {
		return nil, os.ErrClosed
	}
	return &InMemoryFileInfo{node: f.node}, nil
}

// Sync syncs the file (no-op for in-memory)
func (f *InMemoryFile) Sync() error {
	return nil
}

// Truncate truncates the file
func (f *InMemoryFile) Truncate(size int64) error {
	if f.closed {
		return os.ErrClosed
	}
	if size < 0 {
		return os.ErrInvalid
	}
	if size > int64(len(f.node.content)) {
		// Extend
		newContent := make([]byte, size)
		copy(newContent, f.node.content)
		f.node.content = newContent
	} else {
		// Truncate
		f.node.content = f.node.content[:size]
	}
	f.node.modTime = time.Now()
	return nil
}

// InMemoryFileInfo implements FileInfo for in-memory nodes
type InMemoryFileInfo struct {
	node *InMemoryNode
}

func (i *InMemoryFileInfo) Name() string       { return i.node.name }
func (i *InMemoryFileInfo) Size() int64        { return int64(len(i.node.content)) }
func (i *InMemoryFileInfo) Mode() os.FileMode  { return i.node.mode }
func (i *InMemoryFileInfo) ModTime() time.Time { return i.node.modTime }
func (i *InMemoryFileInfo) IsDir() bool        { return i.node.isDir }
func (i *InMemoryFileInfo) Sys() interface{}   { return nil }

// FilesystemError represents a filesystem error
type FilesystemError struct {
	Op   string
	Path string
	Err  error
}

func (e *FilesystemError) Error() string {
	return e.Op + " " + e.Path + ": " + e.Err.Error()
}

func (e *FilesystemError) Unwrap() error {
	return e.Err
}
