package securemem

import (
	"testing"
)

func TestNewString(t *testing.T) {
	plaintext := "test-secret-123"
	s := NewString(plaintext)
	defer s.Destroy()

	if s == nil {
		t.Fatal("NewString returned nil")
	}

	if s.String() != plaintext {
		t.Errorf("expected %q, got %q", plaintext, s.String())
	}

	if s.Len() != len(plaintext) {
		t.Errorf("expected length %d, got %d", len(plaintext), s.Len())
	}
}

func TestNewStringFromBytes(t *testing.T) {
	original := []byte{0x01, 0x02, 0x03, 0x04}
	expected := make([]byte, len(original))
	copy(expected, original) // Save expected values before memguard wipes the input
	s := NewStringFromBytes(original)
	defer s.Destroy()

	if s == nil {
		t.Fatal("NewStringFromBytes returned nil")
	}

	result := s.Bytes()
	if len(result) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(result))
	}

	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("byte %d: expected %x, got %x", i, expected[i], result[i])
		}
	}
}

func TestStringEqual(t *testing.T) {
	s1 := NewString("secret")
	defer s1.Destroy()

	if !s1.Equal("secret") {
		t.Error("Equal should return true for matching strings")
	}

	if s1.Equal("different") {
		t.Error("Equal should return false for non-matching strings")
	}
}

func TestStringEqualSecure(t *testing.T) {
	s1 := NewString("secret")
	defer s1.Destroy()

	s2 := NewString("secret")
	defer s2.Destroy()

	if !s1.EqualSecure(s2) {
		t.Error("EqualSecure should return true for matching strings")
	}

	s3 := NewString("different")
	defer s3.Destroy()

	if s1.EqualSecure(s3) {
		t.Error("EqualSecure should return false for non-matching strings")
	}
}

func TestStringClone(t *testing.T) {
	original := NewString("original")
	defer original.Destroy()

	clone := original.Clone()
	defer clone.Destroy()

	if original.String() != clone.String() {
		t.Error("clone should have same value as original")
	}

	// Modifying the clone shouldn't affect the original
	clone.Destroy()
	if original.String() != "original" {
		t.Error("destroying clone should not affect original")
	}
}

func TestStringMap(t *testing.T) {
	s := NewString("hello world")
	defer s.Destroy()

	upper := s.Map(func(b []byte) []byte {
		result := make([]byte, len(b))
		for i, c := range b {
			if c >= 'a' && c <= 'z' {
				result[i] = c - 32
			} else {
				result[i] = c
			}
		}
		return result
	})
	defer upper.Destroy()

	if upper.String() != "HELLO WORLD" {
		t.Errorf("expected 'HELLO WORLD', got '%s'", upper.String())
	}
}

func TestStringWithValue(t *testing.T) {
	s := NewString("test-value")
	defer s.Destroy()

	var captured string
	s.WithValue(func(str string) {
		captured = str
	})

	if captured != "test-value" {
		t.Errorf("expected 'test-value', got '%s'", captured)
	}
}

func TestStringDestroy(t *testing.T) {
	s := NewString("to-be-destroyed")
	s.Destroy()

	if !s.invalid {
		t.Error("string should be marked as invalid after destroy")
	}

	if s.String() != "" {
		t.Error("destroyed string should return empty")
	}
}

func TestStringEmpty(t *testing.T) {
	s1 := NewString("")
	defer s1.Destroy()

	if !s1.IsEmpty() {
		t.Error("empty string should return true for IsEmpty")
	}

	s2 := NewString("not-empty")
	defer s2.Destroy()

	if s2.IsEmpty() {
		t.Error("non-empty string should return false for IsEmpty")
	}

	var s3 *String
	if !s3.IsEmpty() {
		t.Error("nil string should return true for IsEmpty")
	}
}

func TestPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pool test in short mode")
	}
	pool := NewPool()
	defer pool.Clear()

	// Test Set and Get
	pool.Set("key1", "value1")
	if !pool.Has("key1") {
		t.Error("pool should contain key1")
	}

	value := pool.GetString("key1")
	if value != "value1" {
		t.Errorf("expected 'value1', got '%s'", value)
	}

	// Test SetFromBytes and GetBytes
	pool.SetFromBytes("key2", []byte{0x05, 0x06, 0x07})
	bytes := pool.GetBytes("key2")
	if len(bytes) != 3 || bytes[0] != 0x05 {
		t.Error("GetBytes should return correct bytes")
	}

	// Test Delete
	pool.Delete("key1")
	if pool.Has("key1") {
		t.Error("key1 should be deleted")
	}

	// Test Keys
	keys := pool.Keys()
	if len(keys) != 1 || keys[0] != "key2" {
		t.Errorf("expected 1 key 'key2', got %v", keys)
	}

	// Test Count
	if pool.Count() != 1 {
		t.Errorf("expected count 1, got %d", pool.Count())
	}

	// Test Clear
	pool.Clear()
	if pool.Count() != 0 {
		t.Error("pool should be empty after clear")
	}
}

func TestPoolWithReplacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pool test in short mode")
	}
	pool := NewPool()
	defer pool.Clear()

	pool.Set("key", "value1")
	pool.Set("key", "value2")

	value := pool.GetString("key")
	if value != "value2" {
		t.Errorf("expected 'value2', got '%s'", value)
	}
}

func TestGlobalPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping global pool test in short mode")
	}
	// Clear the global pool first
	GlobalPool().Clear()

	GlobalPool().Set("global-key", "global-value")

	value := GlobalPool().GetString("global-key")
	if value != "global-value" {
		t.Errorf("expected 'global-value', got '%s'", value)
	}

	// Clean up
	GlobalPool().Clear()
}

func TestSecureWipe(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	SecureWipe(data)

	// After wiping, all bytes should be zero
	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d should be zero after wipe, got %x", i, b)
		}
	}
}

func TestSecureWipeString(t *testing.T) {
	s := "secret-string"
	SecureWipeString(&s)

	if s != "" {
		t.Errorf("string should be empty after wipe, got '%s'", s)
	}
}
