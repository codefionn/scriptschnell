package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level represents a logging level
type Level int

const (
	// LevelDebug is the most verbose logging level
	LevelDebug Level = iota
	// LevelInfo logs informational messages
	LevelInfo
	// LevelWarn logs warnings
	LevelWarn
	// LevelError logs errors
	LevelError
	// LevelNone disables all logging
	LevelNone
)

// String returns string representation of log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelNone:
		return "NONE"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a string into a Level
func ParseLevel(s string) Level {
	switch s {
	case "debug", "DEBUG":
		return LevelDebug
	case "info", "INFO":
		return LevelInfo
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn
	case "error", "ERROR":
		return LevelError
	case "none", "NONE":
		return LevelNone
	default:
		return LevelInfo
	}
}

// Logger provides structured logging capabilities
type Logger struct {
	mu       sync.RWMutex
	level    Level
	logger   *log.Logger
	prefix   string
	file     *os.File
	disabled bool
}

var (
	globalLogger *Logger
	once         sync.Once
)

// Init initializes the global logger
func Init(level Level, logPath string) error {
	var err error
	once.Do(func() {
		globalLogger, err = New(level, logPath, "")
	})
	return err
}

// New creates a new Logger instance
func New(level Level, logPath string, prefix string) (*Logger, error) {
	l := &Logger{
		level:  level,
		prefix: prefix,
	}

	// If logging is disabled or path is empty, use a no-op writer
	if level == LevelNone || logPath == "" {
		l.logger = log.New(io.Discard, "", 0)
		l.disabled = true
		return l, nil
	}

	// Ensure log directory exists
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in append mode
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	l.file = file
	l.logger = log.New(file, "", 0)

	return l, nil
}

// Global returns the global logger instance
func Global() *Logger {
	if globalLogger == nil {
		// Create a default logger that writes to discard if not initialized
		globalLogger = &Logger{
			level:    LevelNone,
			logger:   log.New(io.Discard, "", 0),
			disabled: true,
		}
	}
	return globalLogger
}

// WithPrefix creates a new logger with an additional prefix
func (l *Logger) WithPrefix(prefix string) *Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newPrefix := prefix
	if l.prefix != "" {
		newPrefix = l.prefix + ":" + prefix
	}

	return &Logger{
		level:    l.level,
		logger:   l.logger,
		prefix:   newPrefix,
		file:     l.file,
		disabled: l.disabled,
	}
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// log is the internal logging function
func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.disabled || level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)

	prefix := l.prefix
	if prefix != "" {
		prefix = "[" + prefix + "] "
	}

	logLine := fmt.Sprintf("%s [%s] %s%s", timestamp, level.String(), prefix, msg)
	l.logger.Println(logLine)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Info logs an informational message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Close closes the logger and its underlying file
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Global logging functions for convenience

// Debug logs a debug message using the global logger
func Debug(format string, args ...interface{}) {
	Global().Debug(format, args...)
}

// Info logs an informational message using the global logger
func Info(format string, args ...interface{}) {
	Global().Info(format, args...)
}

// Warn logs a warning message using the global logger
func Warn(format string, args ...interface{}) {
	Global().Warn(format, args...)
}

// Error logs an error message using the global logger
func Error(format string, args ...interface{}) {
	Global().Error(format, args...)
}
