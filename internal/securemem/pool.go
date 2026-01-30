// Package securemem provides memory-protected storage for sensitive data
// using memguard to prevent data from being read via debugger, memory dump, or swap.
package securemem

import (
	"fmt"
	"strings"
	"sync"

	"github.com/awnumar/memguard"
)

// Pool manages a collection of secure strings with lifecycle tracking.
// This is useful for managing secrets throughout an application's lifetime.
type Pool struct {
	mu    sync.RWMutex
	items map[string]*String
}

// NewPool creates a new secure string pool.
func NewPool() *Pool {
	return &Pool{
		items: make(map[string]*String),
	}
}

// Set stores a value in the pool with the given key.
// If a value already exists for the key, it is securely destroyed first.
func (p *Pool) Set(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Destroy existing value if present
	if existing, ok := p.items[key]; ok {
		existing.Destroy()
	}

	// Store new value
	p.items[key] = NewString(value)
}

// SetFromBytes stores bytes in the pool with the given key.
func (p *Pool) SetFromBytes(key string, value []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Destroy existing value if present
	if existing, ok := p.items[key]; ok {
		existing.Destroy()
	}

	// Store new value
	p.items[key] = NewStringFromBytes(value)
}

// Get retrieves a value from the pool.
// Returns nil if the key doesn't exist.
func (p *Pool) Get(key string) *String {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if s, ok := p.items[key]; ok {
		return s.Clone() // Return a copy to avoid exposing the reference
	}
	return nil
}

// GetString retrieves a value from the pool as a plain string.
// WARNING: The returned string lives in regular memory.
func (p *Pool) GetString(key string) string {
	if s := p.Get(key); s != nil {
		val := s.String()
		s.Destroy() // Clean up the clone
		return val
	}
	return ""
}

// GetBytes retrieves a value from the pool as bytes.
// WARNING: The returned bytes live in regular memory.
func (p *Pool) GetBytes(key string) []byte {
	if s := p.Get(key); s != nil {
		val := s.Bytes()
		s.Destroy() // Clean up the clone
		return val
	}
	return nil
}

// Has returns true if the key exists in the pool.
func (p *Pool) Has(key string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.items[key]
	return ok
}

// Delete securely removes a value from the pool.
func (p *Pool) Delete(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if s, ok := p.items[key]; ok {
		s.Destroy()
		delete(p.items, key)
	}
}

// Clear securely removes all values from the pool.
func (p *Pool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, s := range p.items {
		s.Destroy()
		delete(p.items, key)
	}
}

// Keys returns all keys in the pool.
func (p *Pool) Keys() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	keys := make([]string, 0, len(p.items))
	for key := range p.items {
		keys = append(keys, key)
	}
	return keys
}

// Count returns the number of items in the pool.
func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.items)
}

// WithValue executes a function with access to a value from the pool.
// The function receives the plaintext string and should not retain references to it.
func (p *Pool) WithValue(key string, fn func(string)) {
	s := p.Get(key)
	if s != nil {
		defer s.Destroy()
		s.WithValue(fn)
	}
}

// WithBytes executes a function with access to bytes from the pool.
// The function receives the plaintext bytes and should not retain references to them.
func (p *Pool) WithBytes(key string, fn func([]byte)) {
	s := p.Get(key)
	if s != nil {
		defer s.Destroy()
		s.WithBytes(fn)
	}
}

// String returns a safe representation of the pool (keys only, no values).
func (p *Pool) String() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	keys := make([]string, 0, len(p.items))
	for key := range p.items {
		keys = append(keys, key)
	}
	return fmt.Sprintf("SecurePool{%s}", strings.Join(keys, ", "))
}

// globalPool is the default global pool for application-wide secrets.
var (
	globalPool     *Pool
	globalPoolOnce sync.Once
)

// GlobalPool returns the global secure string pool.
// This is a convenient singleton for managing application secrets.
func GlobalPool() *Pool {
	globalPoolOnce.Do(func() {
		globalPool = NewPool()
	})
	return globalPool
}

// Init initializes the memguard library.
// This should be called once at application startup, preferably in main().
func Init() {
	// Initialize memguard with default settings
	memguard.CatchInterrupt()
}

// Cleanup securely destroys all secure memory and exits the application.
// This is typically called before application exit.
func Cleanup() {
	// Destroy all secure memory
	GlobalPool().Clear()

	// Purge memguard's internal buffers
	memguard.Purge()
}

// SecureWipe wipes a byte slice from memory.
// This is a convenience wrapper around memguard.WipeBytes.
func SecureWipe(data []byte) {
	memguard.WipeBytes(data)
}

// SecureWipeString wipes a string from memory.
// Note: Strings in Go are immutable, so this creates a new empty string
// and allows the old one to be garbage collected.
func SecureWipeString(s *string) {
	if s != nil {
		*s = ""
	}
}
