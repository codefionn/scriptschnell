package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"none", LevelNone},
		{"NONE", LevelNone},
		{"invalid", LevelInfo}, // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelNone, "NONE"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("Level(%d).String() = %q, want %q", tt.level, result, tt.expected)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	// Create temp directory for test logs
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	// Create logger
	logger, err := New(LevelInfo, logPath, "test")
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write some logs
	logger.Info("test message")
	logger.Debug("should not appear")

	// Close to flush
	logger.Close()

	// Read log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	// Check that info message appears
	if !strings.Contains(contentStr, "test message") {
		t.Errorf("Log file missing info message")
	}

	// Check that debug message does not appear (level is INFO)
	if strings.Contains(contentStr, "should not appear") {
		t.Errorf("Log file contains debug message when level is INFO")
	}

	// Check prefix
	if !strings.Contains(contentStr, "[test]") {
		t.Errorf("Log file missing prefix")
	}
}

func TestLoggerWithPrefix(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	logger, err := New(LevelInfo, logPath, "parent")
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Create child logger with additional prefix
	childLogger := logger.WithPrefix("child")
	childLogger.Info("test message")

	logger.Close()

	// Read log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	// Check that combined prefix appears
	if !strings.Contains(contentStr, "[parent:child]") {
		t.Errorf("Log file missing combined prefix, got: %s", contentStr)
	}
}

func TestLoggerDisabled(t *testing.T) {
	// Create logger with LevelNone
	logger, err := New(LevelNone, "", "test")
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// These should not panic or error
	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")
}

func TestSetLevel(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	logger, err := New(LevelInfo, logPath, "")
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Initial level is INFO
	logger.Info("info1")
	logger.Debug("debug1")

	// Change to DEBUG
	logger.SetLevel(LevelDebug)
	logger.Info("info2")
	logger.Debug("debug2")

	logger.Close()

	// Read log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	// debug1 should not appear, debug2 should appear
	if strings.Contains(contentStr, "debug1") {
		t.Errorf("debug1 should not appear (level was INFO)")
	}
	if !strings.Contains(contentStr, "debug2") {
		t.Errorf("debug2 should appear (level changed to DEBUG)")
	}
	if !strings.Contains(contentStr, "info1") || !strings.Contains(contentStr, "info2") {
		t.Errorf("info messages should always appear")
	}
}

func TestGlobalLogger(t *testing.T) {
	// Global logger should always work even if not initialized
	logger := Global()
	if logger == nil {
		t.Errorf("Global() returned nil")
	}

	// Should not panic
	Debug("debug")
	Info("info")
	Warn("warn")
	Error("error")
}
