package consts

import "time"

// File operation limits
const (
	// MaxLinesPerRead is the maximum number of lines that can be read from a file at once
	MaxLinesPerRead = 2000
)

// Buffer sizes for various operations
const (
	// BufferSize1KB is 1 kilobyte
	BufferSize1KB = 1024
	// BufferSize64KB is 64 kilobytes
	BufferSize64KB = 64 * 1024
	// BufferSize256KB is 256 kilobytes
	BufferSize256KB = 256 * 1024
	// BufferSize1MB is 1 megabyte
	BufferSize1MB = 1024 * 1024
	// BufferSize10MB is 10 megabytes
	BufferSize10MB = 10 * 1024 * 1024
)

// LLM default configurations
const (
	// DefaultMaxTokens is the default maximum tokens for LLM responses
	DefaultMaxTokens = 1024
)

// Health monitoring intervals (in milliseconds)
const (
	// DefaultHealthCheckInterval is the default interval for health checks
	DefaultHealthCheckInterval = 5000
	// ExtendedHealthCheckInterval is a longer interval for health checks
	ExtendedHealthCheckInterval = 10000
)

// Timeouts for various operations
const (
	// Timeout1Second is a 1 second timeout
	Timeout1Second = 1 * time.Second
	// Timeout2Seconds is a 2 second timeout
	Timeout2Seconds = 2 * time.Second
	// Timeout5Seconds is a 5 second timeout
	Timeout5Seconds = 5 * time.Second
	// Timeout10Seconds is a 10 second timeout
	Timeout10Seconds = 10 * time.Second
	// Timeout30Seconds is a 30 second timeout
	Timeout30Seconds = 30 * time.Second
	// Timeout60Seconds is a 60 second timeout (1 minute)
	Timeout60Seconds = 60 * time.Second
	// Timeout2Minutes is a 2 minute timeout
	Timeout2Minutes = 2 * time.Minute
	// Timeout5Minutes is a 5 minute timeout
	Timeout5Minutes = 5 * time.Minute
	// Timeout10Minutes is a 10 minute timeout
	Timeout10Minutes = 10 * time.Minute
)

// Time durations
const (
	// Duration1Hour is 1 hour
	Duration1Hour = 1 * time.Hour
	// Duration6Hours is 6 hours
	Duration6Hours = 6 * time.Hour
	// Duration24Hours is 24 hours (1 day)
	Duration24Hours = 24 * time.Hour
)

// Retry and attempt limits
const (
	// DefaultMaxRetries is the default number of retries for operations
	DefaultMaxRetries = 5
	// DefaultMaxAttempts is the default maximum attempts for auto-continue
	DefaultMaxAttempts = 3
	// ExtendedMaxAttempts is used for models requiring more attempts
	ExtendedMaxAttempts = 12
	// MaxExtendedAttempts is the maximum attempts for complex models
	MaxExtendedAttempts = 32
)
