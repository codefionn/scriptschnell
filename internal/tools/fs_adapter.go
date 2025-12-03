package tools

import (
	"context"
	"io"
	"io/fs"
	"path"
	"time"

	internalfs "github.com/codefionn/scriptschnell/internal/fs"
)

// fsAdapter adapts our internal FileSystem interface to the standard Go fs.FS interface
// This allows us to mount our filesystem into wazero using WithFS
type fsAdapter struct {
	filesystem internalfs.FileSystem
	ctx        context.Context
}

// NewFSAdapter creates an adapter that wraps our FileSystem to implement fs.FS
func NewFSAdapter(ctx context.Context, filesystem internalfs.FileSystem) fs.FS {
	return &fsAdapter{
		filesystem: filesystem,
		ctx:        ctx,
	}
}

// Open implements fs.FS interface
func (a *fsAdapter) Open(name string) (fs.File, error) {
	// Clean the path
	name = path.Clean(name)

	// Check if it's a directory
	stat, err := a.filesystem.Stat(a.ctx, name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	if stat.IsDir {
		// Return directory file
		entries, err := a.filesystem.ListDir(a.ctx, name)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &dirFile{
			name:    name,
			stat:    stat,
			entries: entries,
		}, nil
	}

	// Read file content
	content, err := a.filesystem.ReadFile(a.ctx, name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}

	return &regularFile{
		name:    name,
		stat:    stat,
		content: content,
		reader:  nil, // will be created on first Read
	}, nil
}

// regularFile implements fs.File for regular files
type regularFile struct {
	name    string
	stat    *internalfs.FileInfo
	content []byte
	reader  io.Reader
	closed  bool
}

func (f *regularFile) Stat() (fs.FileInfo, error) {
	return &fileInfoAdapter{f.stat, f.name}, nil
}

func (f *regularFile) Read(b []byte) (int, error) {
	if f.closed {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrClosed}
	}
	if f.reader == nil {
		f.reader = io.NopCloser(io.NewSectionReader(newBytesReaderAt(f.content), 0, int64(len(f.content))))
	}
	return f.reader.Read(b)
}

func (f *regularFile) Close() error {
	f.closed = true
	return nil
}

// dirFile implements fs.File for directories
type dirFile struct {
	name    string
	stat    *internalfs.FileInfo
	entries []*internalfs.FileInfo
	offset  int
	closed  bool
}

func (d *dirFile) Stat() (fs.FileInfo, error) {
	return &fileInfoAdapter{d.stat, d.name}, nil
}

func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

func (d *dirFile) Close() error {
	d.closed = true
	return nil
}

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.closed {
		return nil, &fs.PathError{Op: "readdir", Path: d.name, Err: fs.ErrClosed}
	}

	if n <= 0 {
		// Read all remaining entries
		var entries []fs.DirEntry
		for d.offset < len(d.entries) {
			entries = append(entries, &dirEntryAdapter{d.entries[d.offset]})
			d.offset++
		}
		return entries, nil
	}

	// Read up to n entries
	var entries []fs.DirEntry
	for i := 0; i < n && d.offset < len(d.entries); i++ {
		entries = append(entries, &dirEntryAdapter{d.entries[d.offset]})
		d.offset++
	}

	if len(entries) == 0 {
		return nil, io.EOF
	}

	return entries, nil
}

// fileInfoAdapter adapts our FileInfo to fs.FileInfo
type fileInfoAdapter struct {
	info *internalfs.FileInfo
	name string
}

func (fi *fileInfoAdapter) Name() string {
	if fi.name != "" {
		return path.Base(fi.name)
	}
	return path.Base(fi.info.Path)
}

func (fi *fileInfoAdapter) Size() int64 {
	return fi.info.Size
}

func (fi *fileInfoAdapter) Mode() fs.FileMode {
	if fi.info.IsDir {
		return fs.ModeDir | 0755
	}
	return 0644
}

func (fi *fileInfoAdapter) ModTime() time.Time {
	return fi.info.ModTime
}

func (fi *fileInfoAdapter) IsDir() bool {
	return fi.info.IsDir
}

func (fi *fileInfoAdapter) Sys() interface{} {
	return nil
}

// dirEntryAdapter adapts our FileInfo to fs.DirEntry
type dirEntryAdapter struct {
	info *internalfs.FileInfo
}

func (de *dirEntryAdapter) Name() string {
	return path.Base(de.info.Path)
}

func (de *dirEntryAdapter) IsDir() bool {
	return de.info.IsDir
}

func (de *dirEntryAdapter) Type() fs.FileMode {
	if de.info.IsDir {
		return fs.ModeDir
	}
	return 0
}

func (de *dirEntryAdapter) Info() (fs.FileInfo, error) {
	return &fileInfoAdapter{de.info, de.info.Path}, nil
}

// bytesReaderAt implements io.ReaderAt for []byte
type bytesReaderAt struct {
	b []byte
}

func newBytesReaderAt(b []byte) *bytesReaderAt {
	return &bytesReaderAt{b: b}
}

func (r *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, &fs.PathError{Op: "read", Err: fs.ErrInvalid}
	}
	if off >= int64(len(r.b)) {
		return 0, io.EOF
	}
	n = copy(p, r.b[off:])
	if n < len(p) {
		err = io.EOF
	}
	return
}
