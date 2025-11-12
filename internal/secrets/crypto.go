package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

const (
	// SecretPrefix is applied to encrypted string fields persisted in config files.
	SecretPrefix = "enc:"
	// payloadVersion allows us to evolve the encryption format while remaining backward compatible.
	payloadVersion = 1
)

var (
	// ErrInvalidPassword is returned when the provided password cannot decrypt the payload.
	ErrInvalidPassword = errors.New("invalid password")
	// ErrInvalidPayload indicates the payload structure is malformed.
	ErrInvalidPayload = errors.New("invalid encrypted payload")
)

// Payload represents encrypted data persisted to disk.
type Payload struct {
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// EncryptBytes encrypts the given data using AES-256-GCM with a password-derived key.
func EncryptBytes(data []byte, password string) (*Payload, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, data, nil)

	return &Payload{
		Version:    payloadVersion,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

// DecryptBytes decrypts the payload with the provided password.
func DecryptBytes(payload *Payload, password string) ([]byte, error) {
	if payload == nil {
		return nil, ErrInvalidPayload
	}
	if payload.Version != payloadVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidPayload, payload.Version)
	}

	salt, err := base64.StdEncoding.DecodeString(payload.Salt)
	if err != nil {
		return nil, fmt.Errorf("%w: decode salt: %v", ErrInvalidPayload, err)
	}
	nonce, err := base64.StdEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: decode nonce: %v", ErrInvalidPayload, err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: decode ciphertext: %v", ErrInvalidPayload, err)
	}

	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: invalid nonce size", ErrInvalidPayload)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPassword, err)
	}
	return plaintext, nil
}

// EncryptString encrypts a string and returns a storage-safe representation with the standard prefix.
func EncryptString(value, password string) (string, error) {
	if value == "" {
		return "", nil
	}

	payload, err := EncryptBytes([]byte(value), password)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	return SecretPrefix + base64.StdEncoding.EncodeToString(raw), nil
}

// DecryptString decrypts a value previously returned by EncryptString. The bool indicates if decryption happened.
func DecryptString(value, password string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}
	if !strings.HasPrefix(value, SecretPrefix) {
		return value, false, nil
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, SecretPrefix))
	if err != nil {
		return "", true, fmt.Errorf("%w: decode payload: %v", ErrInvalidPayload, err)
	}

	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", true, fmt.Errorf("%w: parse payload: %v", ErrInvalidPayload, err)
	}

	plaintext, err := DecryptBytes(&payload, password)
	if err != nil {
		return "", true, err
	}
	return string(plaintext), true, nil
}

// EncodePayload serializes the payload as JSON bytes.
func EncodePayload(payload *Payload) ([]byte, error) {
	if payload == nil {
		return nil, ErrInvalidPayload
	}
	return json.Marshal(payload)
}

// DecodePayload parses JSON bytes into a Payload instance.
func DecodePayload(data []byte) (*Payload, error) {
	var payload Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if payload.Version == 0 || payload.Salt == "" || payload.Nonce == "" || payload.Ciphertext == "" {
		return nil, ErrInvalidPayload
	}
	return &payload, nil
}

func deriveKey(password string, salt []byte) ([]byte, error) {
	key, err := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32) // N=32768
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}
