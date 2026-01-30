// Package securemem provides memory-protected storage for sensitive data
// using memguard to prevent data from being read via debugger, memory dump, or swap.
package securemem

import (
	"crypto/subtle"

	"github.com/awnumar/memguard"
)

// String is a secure string wrapper that stores sensitive data in encrypted memory.
type String struct {
	buf     *memguard.LockedBuffer
	invalid bool
}

// NewString creates a new secure string from the given plaintext.
// The plaintext is immediately stored in encrypted memory.
func NewString(plaintext string) *String {
	return &String{
		buf: memguard.NewBufferFromBytes([]byte(plaintext)),
	}
}

// NewStringFromBytes creates a new secure string from the given bytes.
// NOTE: memguard may wipe the input slice for security.
func NewStringFromBytes(data []byte) *String {
	return &String{
		buf: memguard.NewBufferFromBytes(data),
	}
}

// String returns the plaintext string value.
// WARNING: The returned string is a copy that lives in regular (non-secure) memory.
// Callers should ensure this copy is zeroed when no longer needed.
func (s *String) String() string {
	if s == nil || s.invalid || s.buf == nil {
		return ""
	}
	return string(s.buf.Bytes())
}

// Bytes returns the plaintext bytes value.
// WARNING: The returned bytes are a copy that lives in regular (non-secure) memory.
// Callers should ensure this copy is zeroed when no longer needed.
func (s *String) Bytes() []byte {
	if s == nil || s.invalid || s.buf == nil {
		return nil
	}
	b := s.buf.Bytes()
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// CopyTo copies the secure string's value into the provided byte slice.
// This avoids allocating new memory for the plaintext.
// Returns the number of bytes copied.
func (s *String) CopyTo(dst []byte) int {
	if s == nil || s.invalid || s.buf == nil {
		return 0
	}
	b := s.buf.Bytes()
	copy(dst, b)
	return len(b)
}

// IsEmpty returns true if the string is empty or invalid.
func (s *String) IsEmpty() bool {
	if s == nil || s.invalid || s.buf == nil {
		return true
	}
	return len(s.buf.Bytes()) == 0
}

// Len returns the length of the string.
func (s *String) Len() int {
	if s == nil || s.invalid || s.buf == nil {
		return 0
	}
	return len(s.buf.Bytes())
}

// Equal returns true if the secure string equals the given plaintext string.
// This comparison is done in constant time.
func (s *String) Equal(other string) bool {
	if s == nil || s.invalid || s.buf == nil {
		return other == ""
	}
	return subtle.ConstantTimeCompare(s.buf.Bytes(), []byte(other)) == 1
}

// EqualSecure returns true if two secure strings are equal.
// This comparison is done in constant time.
func (s *String) EqualSecure(other *String) bool {
	if s == nil || s.invalid {
		return other == nil || other.invalid
	}
	if other == nil || other.invalid {
		return false
	}
	return subtle.ConstantTimeCompare(s.buf.Bytes(), other.buf.Bytes()) == 1
}

// Destroy securely wipes the string from memory.
// After calling this, the string should not be used.
func (s *String) Destroy() {
	if s == nil || s.invalid {
		return
	}
	if s.buf != nil {
		s.buf.Destroy()
		s.buf = nil
	}
	s.invalid = true
}

// Clone creates a copy of the secure string.
func (s *String) Clone() *String {
	if s == nil || s.invalid || s.buf == nil {
		return NewString("")
	}
	b := s.buf.Bytes()
	data := make([]byte, len(b))
	copy(data, b)
	return NewStringFromBytes(data)
}

// Map applies a function to the plaintext value while keeping it in secure memory.
// The function receives the plaintext bytes and should not retain references to them.
// The result is stored in a new secure string.
func (s *String) Map(fn func([]byte) []byte) *String {
	if s == nil || s.invalid || s.buf == nil {
		return NewString("")
	}
	result := fn(s.buf.Bytes())
	defer memguard.WipeBytes(result)
	return NewStringFromBytes(result)
}

// WithValue executes a function with access to the plaintext value.
// The function receives the plaintext string and should not retain references to it.
func (s *String) WithValue(fn func(string)) {
	if s == nil || s.invalid || s.buf == nil {
		return
	}
	fn(string(s.buf.Bytes()))
}

// WithBytes executes a function with access to the plaintext bytes.
// The function receives the plaintext bytes and should not retain references to them.
func (s *String) WithBytes(fn func([]byte)) {
	if s == nil || s.invalid || s.buf == nil {
		return
	}
	b := s.buf.Bytes()
	copyBytes := make([]byte, len(b))
	copy(copyBytes, b)
	defer memguard.WipeBytes(copyBytes)
	fn(copyBytes)
}
