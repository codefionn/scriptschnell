package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/logger"
)

const (
	tinyGoVersion = "0.39.0"
	tinyGoBaseURL = "https://github.com/tinygo-org/tinygo/releases/download"
)

// TinyGoManager handles downloading and caching TinyGo compiler
type TinyGoManager struct {
	cacheDir       string
	mu             sync.Mutex
	logger         *logger.Logger
	statusCallback func(string) // Callback for status updates (e.g., to TUI)
	cachedPath     string       // memoized binary path once resolved
}

// NewTinyGoManager creates a new TinyGo manager with platform-specific cache directory
func NewTinyGoManager() (*TinyGoManager, error) {
	cacheDir, err := getTinyGoCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine cache directory: %w", err)
	}

	return &TinyGoManager{
		cacheDir: cacheDir,
		logger:   logger.Global().WithPrefix("tinygo"),
	}, nil
}

// SetStatusCallback sets a callback function for status updates
func (m *TinyGoManager) SetStatusCallback(callback func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCallback = callback
}

// updateStatus sends a status update if callback is set
func (m *TinyGoManager) updateStatus(status string) {
	m.mu.Lock()
	callback := m.statusCallback
	m.mu.Unlock()

	if callback != nil {
		callback(status)
	}
}

// getTinyGoCacheDir returns the platform-specific cache directory for TinyGo
func getTinyGoCacheDir() (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case "linux":
		// Prefer XDG cache home, fall back to ~/.cache
		cacheHome := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
		if cacheHome == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			cacheHome = filepath.Join(homeDir, ".cache")
		}
		baseDir = filepath.Join(cacheHome, "statcode-ai", "tinygo")
	case "darwin":
		// On macOS, use ~/Library/Caches/statcode-ai/tinygo
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, "Library", "Caches", "statcode-ai", "tinygo")
	case "windows":
		// On Windows, use %LOCALAPPDATA%\statcode-ai\tinygo
		localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if localAppData == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		baseDir = filepath.Join(localAppData, "statcode-ai", "tinygo")
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return baseDir, nil
}

// checkTinyGoInPATH checks if tinygo is available in the system PATH
func (m *TinyGoManager) checkTinyGoInPATH() (string, error) {
	// Try to find tinygo in PATH
	path, err := exec.LookPath("tinygo")
	if err != nil {
		// tinygo not found in PATH
		return "", err
	}

	// Verify the found binary is working by checking its version
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "version")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Warn("Found tinygo in PATH at %s but version check failed: %v", path, err)
		return "", fmt.Errorf("tinygo binary found but not working properly")
	}

	m.logger.Debug("Using system tinygo at %s, version: %s", path, strings.TrimSpace(string(output)))
	return path, nil
}

// GetTinyGoBinary returns the path to the TinyGo binary, downloading it if necessary
func (m *TinyGoManager) GetTinyGoBinary(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger == nil {
		m.logger = logger.Global().WithPrefix("tinygo")
	}

	// Return previously resolved path if it is still valid on disk
	if m.cachedPath != "" {
		if fileInfo, statErr := os.Stat(m.cachedPath); statErr == nil && !fileInfo.IsDir() {
			if runtime.GOOS == "windows" || fileInfo.Mode()&0111 != 0 {
				m.logger.Debug("Using memoized TinyGo path %s", m.cachedPath)
				return m.cachedPath, nil
			}
			m.logger.Warn("Cached TinyGo binary at %s is no longer executable, recalculating", m.cachedPath)
		} else {
			m.logger.Debug("Cached TinyGo path %s became invalid: %v", m.cachedPath, statErr)
		}
		m.cachedPath = ""
	}

	// First, check if tinygo is available in PATH
	if systemTinyGo, err := m.checkTinyGoInPATH(); err == nil {
		m.logger.Info("Using system TinyGo from PATH: %s", systemTinyGo)
		m.cachedPath = systemTinyGo
		return systemTinyGo, nil
	} else {
		m.logger.Debug("TinyGo not found in PATH, will use bundled version: %v", err)
	}

	// Check if TinyGo is already cached
	// Check if TinyGo is already cached
	tinyGoPath := filepath.Join(m.cacheDir, tinyGoVersion, "bin", "tinygo")
	if runtime.GOOS == "windows" {
		tinyGoPath += ".exe"
	}

	// Check if binary exists and is executable
	if fileInfo, err := os.Stat(tinyGoPath); err == nil && !fileInfo.IsDir() {
		// Verify it's executable
		if runtime.GOOS != "windows" {
			if fileInfo.Mode()&0111 == 0 {
				m.logger.Warn("TinyGo binary exists but is not executable, re-downloading")
			} else {
				m.logger.Debug("Using cached TinyGo at %s", tinyGoPath)
				m.cachedPath = tinyGoPath
				return tinyGoPath, nil
			}
		} else {
			m.logger.Debug("Using cached TinyGo at %s", tinyGoPath)
			m.cachedPath = tinyGoPath
			return tinyGoPath, nil
		}
	}

	// Download and extract TinyGo
	m.logger.Info("TinyGo not found in cache, downloading version %s...", tinyGoVersion)
	m.updateStatus(fmt.Sprintf("Downloading TinyGo %s (first use only, ~50MB)...", tinyGoVersion))

	if err := m.downloadTinyGo(ctx); err != nil {
		m.updateStatus("")
		return "", fmt.Errorf("failed to download TinyGo: %w", err)
	}

	m.logger.Info("TinyGo downloaded and cached successfully")
	m.updateStatus("TinyGo download complete")

	// Clear status after a brief moment
	go func() {
		time.Sleep(2 * time.Second)
		m.updateStatus("")
	}()

	m.cachedPath = tinyGoPath
	return tinyGoPath, nil
}

// downloadTinyGo downloads and extracts the TinyGo distribution for the current platform
func (m *TinyGoManager) downloadTinyGo(ctx context.Context) error {
	// Determine platform-specific download URL
	downloadURL, fileName, err := m.getTinyGoDownloadURL()
	if err != nil {
		return err
	}

	m.logger.Info("Downloading TinyGo from %s", downloadURL)

	// Create temporary file for download
	tmpFile, err := os.CreateTemp("", "tinygo-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	defer tmpFile.Close()

	// Download the file
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download TinyGo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Copy to temp file with progress updates
	m.logger.Info("Downloading TinyGo binary (this may take a minute)...")

	// Create a progress writer to update status periodically
	totalBytes := resp.ContentLength
	var downloaded int64
	lastUpdate := time.Now()

	progressReader := &progressReader{
		reader: resp.Body,
		onProgress: func(n int64) {
			downloaded += n
			// Update status every second or when complete
			if time.Since(lastUpdate) > time.Second || downloaded == totalBytes {
				lastUpdate = time.Now()
				if totalBytes > 0 {
					percent := float64(downloaded) / float64(totalBytes) * 100
					m.updateStatus(fmt.Sprintf("Downloading TinyGo %s... %.0f%%", tinyGoVersion, percent))
				} else {
					mb := float64(downloaded) / (1024 * 1024)
					m.updateStatus(fmt.Sprintf("Downloading TinyGo %s... %.1f MB", tinyGoVersion, mb))
				}
			}
		},
	}

	if _, err := io.Copy(tmpFile, progressReader); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	// Extract to cache directory
	m.logger.Info("Extracting TinyGo...")
	m.updateStatus(fmt.Sprintf("Extracting TinyGo %s...", tinyGoVersion))
	extractDir := filepath.Join(m.cacheDir, tinyGoVersion)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if runtime.GOOS == "windows" {
		return m.extractZip(tmpPath, extractDir, fileName)
	}
	return m.extractTarGz(tmpPath, extractDir)
}

// getTinyGoDownloadURL returns the download URL and filename for the current platform
func (m *TinyGoManager) getTinyGoDownloadURL() (string, string, error) {
	var fileName string

	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			fileName = fmt.Sprintf("tinygo%s.linux-amd64.tar.gz", tinyGoVersion)
		case "arm64":
			fileName = fmt.Sprintf("tinygo%s.linux-arm64.tar.gz", tinyGoVersion)
		case "arm":
			fileName = fmt.Sprintf("tinygo%s.linux-armhf.tar.gz", tinyGoVersion)
		default:
			return "", "", fmt.Errorf("unsupported Linux architecture: %s", runtime.GOARCH)
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			fileName = fmt.Sprintf("tinygo%s.darwin-amd64.tar.gz", tinyGoVersion)
		case "arm64":
			fileName = fmt.Sprintf("tinygo%s.darwin-arm64.tar.gz", tinyGoVersion)
		default:
			return "", "", fmt.Errorf("unsupported macOS architecture: %s", runtime.GOARCH)
		}
	case "windows":
		if runtime.GOARCH != "amd64" {
			return "", "", fmt.Errorf("unsupported Windows architecture: %s", runtime.GOARCH)
		}
		fileName = fmt.Sprintf("tinygo%s.windows-amd64.zip", tinyGoVersion)
	default:
		return "", "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	url := fmt.Sprintf("%s/v%s/%s", tinyGoBaseURL, tinyGoVersion, fileName)
	return url, fileName, nil
}

// extractTarGz extracts a tar.gz file to the destination directory
func (m *TinyGoManager) extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Remove the top-level "tinygo" directory from the path
		targetPath := strings.TrimPrefix(header.Name, "tinygo/")
		if targetPath == "" {
			continue
		}

		target := filepath.Join(destDir, targetPath)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to extract file: %w", err)
			}
			outFile.Close()
		}
	}

	return nil
}

// extractZip extracts a zip file to the destination directory (Windows)
func (m *TinyGoManager) extractZip(archivePath, destDir, fileName string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		// Remove the top-level "tinygo" directory from the path
		targetPath := strings.TrimPrefix(f.Name, "tinygo/")
		targetPath = strings.TrimPrefix(targetPath, "tinygo\\")
		if targetPath == "" {
			continue
		}

		target := filepath.Join(destDir, targetPath)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			outFile.Close()
			rc.Close()
			return fmt.Errorf("failed to extract file: %w", err)
		}

		outFile.Close()
		rc.Close()
	}

	return nil
}

// CleanCache removes the TinyGo cache directory
func (m *TinyGoManager) CleanCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Cleaning TinyGo cache at %s", m.cacheDir)
	m.cachedPath = ""
	return os.RemoveAll(m.cacheDir)
}

// GetCacheSize returns the size of the TinyGo cache in bytes
func (m *TinyGoManager) GetCacheSize() (int64, error) {
	var size int64

	err := filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader     io.Reader
	onProgress func(int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.onProgress(int64(n))
	}
	return n, err
}
