package tools

import (
	"archive/tar"
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/ulikunitz/xz"
)

const (
	wasmtimeVersion = "28.0.0" // Match the wasmtime-go version we're using
)

// WasmtimeManager handles downloading and caching the wasmtime CLI binary
type WasmtimeManager struct {
	cacheDir       string
	mu             sync.Mutex
	logger         *logger.Logger
	statusCallback func(string) // Callback for TUI status updates
}

// NewWasmtimeManager creates a new wasmtime manager with platform-specific cache directory
func NewWasmtimeManager() (*WasmtimeManager, error) {
	cacheDir, err := getWasmtimeCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	return &WasmtimeManager{
		cacheDir: cacheDir,
		logger:   logger.Global().WithPrefix("wasmtime"),
	}, nil
}

// SetStatusCallback sets the callback function for status updates
func (m *WasmtimeManager) SetStatusCallback(callback func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCallback = callback
}

// updateStatus sends a status update via the callback if set
func (m *WasmtimeManager) updateStatus(status string) {
	m.mu.Lock()
	cb := m.statusCallback
	m.mu.Unlock()

	if cb != nil {
		cb(status)
	}
	m.logger.Info("%s", status)
}

// GetWasmtimeBinary returns the path to the wasmtime binary, downloading if necessary
func (m *WasmtimeManager) GetWasmtimeBinary(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if wasmtime is already cached
	wasmtimePath := filepath.Join(m.cacheDir, wasmtimeVersion, "wasmtime")
	if runtime.GOOS == "windows" {
		wasmtimePath += ".exe"
	}

	if _, err := os.Stat(wasmtimePath); err == nil {
		m.logger.Debug("Using cached wasmtime at %s", wasmtimePath)
		return wasmtimePath, nil
	}

	// Download wasmtime
	m.updateStatus(fmt.Sprintf("Downloading wasmtime %s (first use only, ~15MB)...", wasmtimeVersion))
	if err := m.downloadWasmtime(ctx); err != nil {
		return "", fmt.Errorf("failed to download wasmtime: %w", err)
	}

	m.updateStatus("Wasmtime ready")
	return wasmtimePath, nil
}

// downloadWasmtime downloads and extracts wasmtime to the cache directory
func (m *WasmtimeManager) downloadWasmtime(ctx context.Context) error {
	downloadURL, err := getWasmtimeDownloadURL()
	if err != nil {
		return err
	}

	m.logger.Info("Downloading wasmtime from %s", downloadURL)

	// Create version-specific directory
	versionDir := filepath.Join(m.cacheDir, wasmtimeVersion)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Download to temporary file
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download wasmtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Track download progress
	var bytesRead int64
	totalBytes := resp.ContentLength

	progressReader := &progressReader{
		reader: resp.Body,
		onProgress: func(n int64) {
			bytesRead += n
			if totalBytes > 0 {
				percent := float64(bytesRead) * 100 / float64(totalBytes)
				m.updateStatus(fmt.Sprintf("Downloading wasmtime: %.0f%%", percent))
			}
		},
	}

	// Extract based on platform
	if runtime.GOOS == "windows" {
		if err := m.extractZip(progressReader, versionDir); err != nil {
			return fmt.Errorf("failed to extract wasmtime: %w", err)
		}
	} else {
		if err := m.extractTarXz(progressReader, versionDir); err != nil {
			return fmt.Errorf("failed to extract wasmtime: %w", err)
		}
	}

	m.updateStatus("Extracting wasmtime...")

	// Verify the binary exists
	wasmtimePath := filepath.Join(versionDir, "wasmtime")
	if runtime.GOOS == "windows" {
		wasmtimePath += ".exe"
	}

	if _, err := os.Stat(wasmtimePath); err != nil {
		return fmt.Errorf("wasmtime binary not found after extraction: %w", err)
	}

	// Make executable on Unix systems
	if runtime.GOOS != "windows" {
		if err := os.Chmod(wasmtimePath, 0755); err != nil {
			return fmt.Errorf("failed to make wasmtime executable: %w", err)
		}
	}

	m.logger.Info("Wasmtime installed successfully at %s", wasmtimePath)
	return nil
}

// extractTarXz extracts a .tar.xz archive to the destination directory
func (m *WasmtimeManager) extractTarXz(reader io.Reader, destDir string) error {
	// Decompress xz
	xzReader, err := xz.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to create xz reader: %w", err)
	}

	// Extract tar
	tarReader := tar.NewReader(xzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Get the base filename (remove directory prefix like "wasmtime-v28.0.0-x86_64-linux/")
		filename := filepath.Base(header.Name)

		// Only extract the wasmtime binary
		if filename != "wasmtime" {
			continue
		}

		// Extract to destination
		path := filepath.Join(destDir, filename)

		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		if _, err := io.Copy(file, tarReader); err != nil {
			file.Close()
			return fmt.Errorf("failed to extract file: %w", err)
		}
		file.Close()
	}

	return nil
}

// extractZip extracts a zip archive to the destination directory
func (m *WasmtimeManager) extractZip(reader io.Reader, destDir string) error {
	// Write to temporary file first
	tempFile := filepath.Join(destDir, "wasmtime.zip")
	f, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile)

	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		return fmt.Errorf("failed to write zip: %w", err)
	}
	f.Close()

	// Open and extract zip
	zipReader, err := zip.OpenReader(tempFile)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zipReader.Close()

	for _, file := range zipReader.File {
		if err := m.extractZipFile(file, destDir); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func (m *WasmtimeManager) extractZipFile(file *zip.File, destDir string) error {
	// Get the base filename (remove directory prefix like "wasmtime-v28.0.0-x86_64-windows/")
	filename := filepath.Base(file.Name)

	// Only extract the wasmtime binary
	if filename != "wasmtime.exe" && filename != "wasmtime" {
		return nil
	}

	path := filepath.Join(destDir, filename)

	if file.FileInfo().IsDir() {
		return os.MkdirAll(path, file.Mode())
	}

	fileReader, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer fileReader.Close()

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, fileReader); err != nil {
		return fmt.Errorf("failed to extract file: %w", err)
	}

	return nil
}

// getWasmtimeCacheDir returns the platform-specific cache directory for wasmtime
func getWasmtimeCacheDir() (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case "linux":
		// Use XDG_CACHE_HOME or ~/.cache
		if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
			baseDir = xdgCache
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			baseDir = filepath.Join(home, ".cache")
		}
	case "darwin":
		// macOS: ~/Library/Caches
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(home, "Library", "Caches")
	case "windows":
		// Windows: %LOCALAPPDATA%
		baseDir = os.Getenv("LOCALAPPDATA")
		if baseDir == "" {
			return "", fmt.Errorf("LOCALAPPDATA environment variable not set")
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	cacheDir := filepath.Join(baseDir, "statcode-ai", "wasmtime")
	return cacheDir, nil
}

// getWasmtimeDownloadURL returns the download URL for the current platform
func getWasmtimeDownloadURL() (string, error) {
	const baseURL = "https://github.com/bytecodealliance/wasmtime/releases/download"

	var filename string
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			filename = fmt.Sprintf("wasmtime-v%s-x86_64-linux.tar.xz", wasmtimeVersion)
		case "arm64":
			filename = fmt.Sprintf("wasmtime-v%s-aarch64-linux.tar.xz", wasmtimeVersion)
		default:
			return "", fmt.Errorf("unsupported Linux architecture: %s", runtime.GOARCH)
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			filename = fmt.Sprintf("wasmtime-v%s-x86_64-macos.tar.xz", wasmtimeVersion)
		case "arm64":
			filename = fmt.Sprintf("wasmtime-v%s-aarch64-macos.tar.xz", wasmtimeVersion)
		default:
			return "", fmt.Errorf("unsupported macOS architecture: %s", runtime.GOARCH)
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			filename = fmt.Sprintf("wasmtime-v%s-x86_64-windows.zip", wasmtimeVersion)
		default:
			return "", fmt.Errorf("unsupported Windows architecture: %s", runtime.GOARCH)
		}
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return fmt.Sprintf("%s/v%s/%s", baseURL, wasmtimeVersion, filename), nil
}
