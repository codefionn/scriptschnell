package pprof

import (
	"context"
	"fmt"
	"net"
	"net/http"
	netpprof "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

// Config holds the pprof configuration
type Config struct {
	// HTTP server mode
	HTTPAddr string // HTTP server address (e.g., ":6060", "localhost:6060")

	// File-based mode
	CPUProfile       string // Path to write CPU profile file
	HeapProfile      string // Path to write heap profile file
	GoroutineProfile string // Path to write goroutine profile file
	BlockProfile     string // Path to write block profile file
	MutexProfile     string // Path to write mutex profile file
	TraceProfile     string // Path to write execution trace file

	// Block and mutex profiling rates
	BlockProfileRate     int // Sample 1/n events (default: 1)
	MutexProfileFraction int // Sample 1/n events (default: 1)
}

// Handler manages pprof profiling
type Handler struct {
	config    Config
	server    *http.Server
	listener  net.Listener
	cpuFile   *os.File
	traceFile *os.File

	mu       sync.Mutex
	stopping bool
}

// NewHandler creates a new pprof handler with the given configuration
func NewHandler(config Config) *Handler {
	if config.BlockProfileRate == 0 {
		config.BlockProfileRate = 1
	}
	if config.MutexProfileFraction == 0 {
		config.MutexProfileFraction = 1
	}

	return &Handler{
		config: config,
	}
}

// Start begins profiling based on the configuration
func (h *Handler) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Start CPU profiling if configured
	if h.config.CPUProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.CPUProfile), 0755); err != nil {
			return fmt.Errorf("failed to create directory for CPU profile: %w", err)
		}
		f, err := os.Create(h.config.CPUProfile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile file: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("failed to start CPU profiling: %w", err)
		}
		h.cpuFile = f
	}

	// Start execution tracing if configured
	// Note: Using runtime/trace instead of pprof.StartTrace (deprecated in Go 1.22+)
	if h.config.TraceProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.TraceProfile), 0755); err != nil {
			return fmt.Errorf("failed to create directory for trace profile: %w", err)
		}
		f, err := os.Create(h.config.TraceProfile)
		if err != nil {
			return fmt.Errorf("failed to create trace profile file: %w", err)
		}
		if err := trace.Start(f); err != nil {
			f.Close()
			return fmt.Errorf("failed to start execution tracing: %w", err)
		}
		h.traceFile = f
	}

	// Set block profiling rate if configured
	if h.config.BlockProfile != "" {
		runtime.SetBlockProfileRate(h.config.BlockProfileRate)
	}

	// Set mutex profiling fraction if configured
	if h.config.MutexProfile != "" {
		runtime.SetMutexProfileFraction(h.config.MutexProfileFraction)
	}

	// Start HTTP server if configured
	if h.config.HTTPAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", netpprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", netpprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", netpprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", netpprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", netpprof.Trace)
		mux.Handle("/debug/pprof/goroutine", netpprof.Handler("goroutine"))
		mux.Handle("/debug/pprof/heap", netpprof.Handler("heap"))
		mux.Handle("/debug/pprof/block", netpprof.Handler("block"))
		mux.Handle("/debug/pprof/mutex", netpprof.Handler("mutex"))
		mux.Handle("/debug/pprof/threadcreate", netpprof.Handler("threadcreate"))

		ln, err := net.Listen("tcp", h.config.HTTPAddr)
		if err != nil {
			return fmt.Errorf("failed to bind pprof HTTP server: %w", err)
		}

		h.listener = ln
		h.server = &http.Server{
			Addr:    h.config.HTTPAddr,
			Handler: mux,
		}

		go func() {
			if err := h.server.Serve(h.listener); err != nil && err != http.ErrServerClosed {
				// Log the error (could be improved with proper logging)
				fmt.Fprintf(os.Stderr, "pprof server error: %v\n", err)
			}
		}()
	}

	return nil
}

// Stop stops profiling and writes profile files
func (h *Handler) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stopping {
		return nil
	}
	h.stopping = true

	var errs []error

	// Stop CPU profiling
	if h.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := h.cpuFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close CPU profile: %w", err))
		}
		h.cpuFile = nil
	}

	// Stop execution tracing
	if h.traceFile != nil {
		trace.Stop()
		if err := h.traceFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close trace profile: %w", err))
		}
		h.traceFile = nil
	}

	// Write heap profile
	if h.config.HeapProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.HeapProfile), 0755); err != nil {
			errs = append(errs, fmt.Errorf("failed to create directory for heap profile: %w", err))
		} else {
			f, err := os.Create(h.config.HeapProfile)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to create heap profile file: %w", err))
			} else {
				if err := pprof.WriteHeapProfile(f); err != nil {
					errs = append(errs, fmt.Errorf("failed to write heap profile: %w", err))
				}
				f.Close()
			}
		}
	}

	// Write goroutine profile
	if h.config.GoroutineProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.GoroutineProfile), 0755); err != nil {
			errs = append(errs, fmt.Errorf("failed to create directory for goroutine profile: %w", err))
		} else {
			if err := writeProfile("goroutine", h.config.GoroutineProfile); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Write block profile
	if h.config.BlockProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.BlockProfile), 0755); err != nil {
			errs = append(errs, fmt.Errorf("failed to create directory for block profile: %w", err))
		} else {
			if err := writeProfile("block", h.config.BlockProfile); err != nil {
				errs = append(errs, err)
			}
		}
		runtime.SetBlockProfileRate(0)
	}

	// Write mutex profile
	if h.config.MutexProfile != "" {
		if err := os.MkdirAll(filepath.Dir(h.config.MutexProfile), 0755); err != nil {
			errs = append(errs, fmt.Errorf("failed to create directory for mutex profile: %w", err))
		} else {
			if err := writeProfile("mutex", h.config.MutexProfile); err != nil {
				errs = append(errs, err)
			}
		}
		runtime.SetMutexProfileFraction(0)
	}

	// Stop HTTP server
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown pprof server: %w", err))
		}
		h.server = nil
		h.listener = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("multiple errors occurred: %v", errs)
	}
	return nil
}

// writeProfile writes a named profile to a file
func writeProfile(name, path string) error {
	p := pprof.Lookup(name)
	if p == nil {
		return fmt.Errorf("profile %q not found", name)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create profile file: %w", err)
	}
	defer f.Close()
	if err := p.WriteTo(f, 0); err != nil {
		return fmt.Errorf("failed to write profile: %w", err)
	}
	return nil
}
