package tools

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

// MockFS for testing
type mockContextFS struct {
	files map[string][]byte
	dirs  map[string][]fs.FileInfo
}

func newMockContextFS() *mockContextFS {
	return &mockContextFS{
		files: make(map[string][]byte),
		dirs:  make(map[string][]fs.FileInfo),
	}
}

func (m *mockContextFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockContextFS) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	data, err := m.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}
	return lines[from-1 : to], nil
}

func (m *mockContextFS) WriteFile(ctx context.Context, path string, data []byte) error {
	m.files[path] = data
	return nil
}

func (m *mockContextFS) Exists(ctx context.Context, path string) (bool, error) {
	_, ok := m.files[path]
	if ok {
		return true, nil
	}
	_, ok = m.dirs[path]
	return ok, nil
}

func (m *mockContextFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return &fs.FileInfo{Path: path, IsDir: false, Size: int64(len(m.files[path]))}, nil
	}
	if _, ok := m.dirs[path]; ok {
		return &fs.FileInfo{Path: path, IsDir: true}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockContextFS) ListDir(ctx context.Context, path string) ([]*fs.FileInfo, error) {
	if entries, ok := m.dirs[path]; ok {
		result := make([]*fs.FileInfo, len(entries))
		for i := range entries {
			result[i] = &entries[i]
		}
		return result, nil
	}
	return nil, os.ErrNotExist
}


func newTestSession() *session.Session {
	return &session.Session{
		WorkingDir: "/test/workspace",
	}
}
func (m *mockContextFS) Delete(ctx context.Context, path string) error {
	delete(m.files, path)
	delete(m.dirs, path)
	return nil
}

func (m *mockContextFS) DeleteAll(ctx context.Context, path string) error {
	return m.Delete(ctx, path)
}

func (m *mockContextFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	m.dirs[path] = []fs.FileInfo{}
	return nil
}

func TestDecompressData(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		filename string
		wantErr  bool
	}{
		{
			name:     "plain text file",
			input:    "Hello, World!",
			filename: "/tmp/test.txt",
			wantErr:  false,
		},
		{
			name:     "unknown extension",
			input:    "Some data",
			filename: "/tmp/test.xyz",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.input)
			result, err := decompressData(data, tt.filename)

			if (err != nil) != tt.wantErr {
				t.Errorf("decompressData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !bytes.Equal(result, data) {
				t.Errorf("decompressData() = %v, want %v", result, data)
			}
		})
	}
}

func TestDecompressDataGzip(t *testing.T) {
	// Create gzipped data
	original := []byte("This is test data for gzip compression")
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	_, err := gzWriter.Write(original)
	if err != nil {
		t.Fatalf("Failed to write gzip data: %v", err)
	}
	gzWriter.Close()

	compressed := buf.Bytes()

	// Test decompression
	result, err := decompressData(compressed, "/tmp/test.txt.gz")
	if err != nil {
		t.Fatalf("decompressData() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressData() = %q, want %q", string(result), string(original))
	}
}

func TestSearchContextFilesTool_NoContextDirs(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	tool := NewSearchContextFilesTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"pattern": "*.txt",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error == "" {
		t.Error("Expected error when no context directories configured")
	}

	if !strings.Contains(result.Error, "No context directories configured") {
		t.Errorf("Expected 'No context directories configured' error, got: %s", result.Error)
	}
}

func TestSearchContextFilesTool_BasicSearch(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	// Set up mock filesystem
	contextDir := "/usr/share/doc"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	mockFS.dirs[contextDir] = []fs.FileInfo{
		{Path: contextDir + "/readme.txt", IsDir: false, Size: 100},
		{Path: contextDir + "/manual.txt", IsDir: false, Size: 200},
		{Path: contextDir + "/image.png", IsDir: false, Size: 1000},
	}
	mockFS.files[contextDir+"/readme.txt"] = []byte("This is a readme file")
	mockFS.files[contextDir+"/manual.txt"] = []byte("This is a manual")
	mockFS.files[contextDir+"/image.png"] = []byte{0x89, 0x50, 0x4E, 0x47} // PNG header

	tool := NewSearchContextFilesTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"pattern": "*.txt",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	if !strings.Contains(result.Result.(string), "readme.txt") {
		t.Error("Expected readme.txt in results")
	}

	if !strings.Contains(result.Result.(string), "manual.txt") {
		t.Error("Expected manual.txt in results")
	}

	if strings.Contains(result.Result.(string), "image.png") {
		t.Error("Should not include image.png in results")
	}
}

func TestReadContextFileTool_NoContextDirs(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	tool := NewReadContextFileTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"path": "/some/file.txt",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error == "" {
		t.Error("Expected error when no context directories configured")
	}

	if !strings.Contains(result.Error, "No context directories configured") {
		t.Errorf("Expected 'No context directories configured' error, got: %s", result.Error)
	}
}

func TestReadContextFileTool_FileNotInContextDir(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()
	cfg.AddContextDirectory("/test/workspace", "/usr/share/doc")

	tool := NewReadContextFileTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"path": "/etc/passwd",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error == "" {
		t.Error("Expected error when file not in context directory")
	}

	if !strings.Contains(result.Error, "not within any configured context directory") {
		t.Errorf("Expected 'not within context directory' error, got: %s", result.Error)
	}
}

func TestReadContextFileTool_Success(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	contextDir := "/usr/share/doc"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	filePath := contextDir + "/readme.txt"
	fileContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	mockFS.files[filePath] = []byte(fileContent)

	tool := NewReadContextFileTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"path": filePath,
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	if !strings.Contains(result.Result.(string), "Line 1") {
		t.Error("Expected 'Line 1' in result")
	}

	if !strings.Contains(result.Result.(string), "Line 5") {
		t.Error("Expected 'Line 5' in result")
	}
}

func TestReadContextFileTool_WithLineRange(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	contextDir := "/usr/share/doc"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	filePath := contextDir + "/readme.txt"
	fileContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	mockFS.files[filePath] = []byte(fileContent)

	tool := NewReadContextFileTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"path":      filePath,
		"from_line": 2,
		"to_line":   4,
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	if strings.Contains(result.Result.(string), "Line 1") {
		t.Error("Should not include Line 1")
	}

	if !strings.Contains(result.Result.(string), "Line 2") {
		t.Error("Expected 'Line 2' in result")
	}

	if !strings.Contains(result.Result.(string), "Line 4") {
		t.Error("Expected 'Line 4' in result")
	}

	if strings.Contains(result.Result.(string), "Line 5") {
		t.Error("Should not include Line 5")
	}
}

func TestReadContextFileTool_CompressedFile(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	contextDir := "/usr/share/man"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	// Create compressed content
	original := []byte("NAME\n    test - a test man page\n\nDESCRIPTION\n    This is a test.")
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	gzWriter.Write(original)
	gzWriter.Close()

	filePath := contextDir + "/test.1.gz"
	mockFS.files[filePath] = buf.Bytes()

	tool := NewReadContextFileTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"path": filePath,
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	if !strings.Contains(result.Result.(string), "NAME") {
		t.Error("Expected decompressed content with 'NAME'")
	}

	if !strings.Contains(result.Result.(string), "test man page") {
		t.Error("Expected decompressed content with 'test man page'")
	}
}

func TestGrepContextFilesTool_NoContextDirs(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	tool := NewGrepContextFilesTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"pattern": "test",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error == "" {
		t.Error("Expected error when no context directories configured")
	}

	if !strings.Contains(result.Error, "No context directories configured") {
		t.Errorf("Expected 'No context directories configured' error, got: %s", result.Error)
	}
}

func TestGrepContextFilesTool_BasicGrep(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	contextDir := "/usr/share/doc"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	// Set up files
	mockFS.dirs[contextDir] = []fs.FileInfo{
		{Path: contextDir + "/readme.txt", IsDir: false, Size: 100},
		{Path: contextDir + "/manual.txt", IsDir: false, Size: 100},
	}
	mockFS.files[contextDir+"/readme.txt"] = []byte("This is a test file.\nIt contains test data.\nNothing special here.")
	mockFS.files[contextDir+"/manual.txt"] = []byte("Manual content.\nNo matches here.\nJust documentation.")

	tool := NewGrepContextFilesTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"pattern": "test",
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	// Should find matches in readme.txt
	if !strings.Contains(result.Result.(string), "readme.txt") {
		t.Error("Expected readme.txt in results")
	}

	if !strings.Contains(result.Result.(string), "This is a test file") {
		t.Error("Expected matching line in results")
	}

	// Should not include manual.txt (no matches)
	if strings.Contains(result.Result.(string), "manual.txt") {
		t.Error("Should not include manual.txt (no matches)")
	}
}

func TestGrepContextFilesTool_WithContextLines(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()

	contextDir := "/usr/share/doc"
	cfg.AddContextDirectory("/test/workspace", contextDir)

	mockFS.dirs[contextDir] = []fs.FileInfo{
		{Path: contextDir + "/test.txt", IsDir: false, Size: 100},
	}
	mockFS.files[contextDir+"/test.txt"] = []byte("Line 1\nLine 2\nMATCH HERE\nLine 4\nLine 5")

	tool := NewGrepContextFilesTool(mockFS, cfg, newTestSession())

	params := map[string]interface{}{
		"pattern":       "MATCH",
		"context_lines": 1,
	}

	result := tool.Execute(context.Background(), params)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}

	// Should include line before and after
	if !strings.Contains(result.Result.(string), "Line 2") {
		t.Error("Expected context line before match")
	}

	if !strings.Contains(result.Result.(string), "MATCH HERE") {
		t.Error("Expected matching line")
	}

	if !strings.Contains(result.Result.(string), "Line 4") {
		t.Error("Expected context line after match")
	}

	// Should not include Line 1 (outside context)
	if strings.Contains(result.Result.(string), "Line 1") {
		t.Error("Line 1 should be outside context window")
	}
}

func TestToolParameterValidation(t *testing.T) {
	mockFS := newMockContextFS()
	cfg := config.DefaultConfig()
	cfg.AddContextDirectory("/test/workspace", "/test")

	t.Run("SearchContextFiles missing pattern", func(t *testing.T) {
		tool := NewSearchContextFilesTool(mockFS, cfg, newTestSession())
		params := map[string]interface{}{}
		result := tool.Execute(context.Background(), params)

		if result.Error == "" {
			t.Error("Expected error for missing pattern")
		}
		if !strings.Contains(result.Error, "pattern is required") {
			t.Errorf("Expected 'pattern is required' error, got: %s", result.Error)
		}
	})

	t.Run("GrepContextFiles missing pattern", func(t *testing.T) {
		tool := NewGrepContextFilesTool(mockFS, cfg, newTestSession())
		params := map[string]interface{}{}
		result := tool.Execute(context.Background(), params)

		if result.Error == "" {
			t.Error("Expected error for missing pattern")
		}
		if !strings.Contains(result.Error, "pattern is required") {
			t.Errorf("Expected 'pattern is required' error, got: %s", result.Error)
		}
	})

	t.Run("ReadContextFile missing path", func(t *testing.T) {
		tool := NewReadContextFileTool(mockFS, cfg, newTestSession())
		params := map[string]interface{}{}
		result := tool.Execute(context.Background(), params)

		if result.Error == "" {
			t.Error("Expected error for missing path")
		}
		if !strings.Contains(result.Error, "path is required") {
			t.Errorf("Expected 'path is required' error, got: %s", result.Error)
		}
	})
}
