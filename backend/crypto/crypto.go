// Package crypto provides encryption and decryption for sensitive data at rest,
// primarily OAuth tokens. It implements AES-256-GCM authenticated encryption
// with support for key rotation via encryption metadata.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encryptor defines the interface for encrypting and decrypting data.
// Implementations must provide authenticated encryption (AEAD) to ensure
// both confidentiality and integrity of the ciphertext.
type Encryptor interface {
	// Encrypt transforms plaintext into ciphertext with authentication tag.
	// Returns base64-encoded ciphertext for database storage.
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt verifies and transforms ciphertext back to plaintext.
	// Returns error if authentication fails or ciphertext is corrupted.
	Decrypt(ciphertext []byte) ([]byte, error)
}

// AESEncryptor implements Encryptor using AES-256-GCM.
// The 256-bit key provides strong security suitable for long-term storage.
// GCM mode provides both encryption and authentication (AEAD).
type AESEncryptor struct {
	key []byte // 32 bytes for AES-256
}

// NewAESEncryptor creates an encryptor from a base64-encoded 32-byte key.
// The key should be generated using a cryptographically secure random source:
//   openssl rand -base64 32
//
// Returns error if the key is not exactly 32 bytes after decoding.
func NewAESEncryptor(base64Key string) (*AESEncryptor, error) {
	if base64Key == "" {
		return nil, fmt.Errorf("encryption key is empty")
	}

	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: base64 decode failed: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("invalid encryption key: must be 32 bytes (256 bits), got %d bytes", len(key))
	}

	return &AESEncryptor{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns the result as
// raw bytes in the format: nonce || ciphertext || auth_tag
//
// The nonce (12 bytes) is randomly generated per encryption and prepended to
// the ciphertext. GCM automatically appends a 16-byte authentication tag.
//
// Callers should base64-encode the result before storing in text columns.
func (e *AESEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("plaintext is empty")
	}

	// Create AES cipher block
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Create GCM mode wrapper (provides AEAD)
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	// Generate random nonce (12 bytes for GCM)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt and authenticate: nonce || ciphertext || tag
	// Seal appends ciphertext+tag to nonce (nonce is used as IV, no AAD provided)
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// Decrypt decrypts and authenticates ciphertext encrypted by Encrypt.
// Returns error if:
//   - ciphertext is too short (missing nonce or tag)
//   - authentication tag verification fails (tampering detected)
//   - decryption fails for any other reason
func (e *AESEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("ciphertext is empty")
	}

	// Create AES cipher block
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Create GCM mode wrapper
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: expected at least %d bytes, got %d", nonceSize, len(ciphertext))
	}

	// Extract nonce from prefix
	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]

	// Decrypt and verify authentication tag
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Don't expose internal error details that might leak information
		return nil, fmt.Errorf("decryption failed: authentication or integrity check failed")
	}

	return plaintext, nil
}

// EncryptString is a convenience wrapper that encrypts a string and returns
// base64-encoded ciphertext suitable for database text columns.
func EncryptString(enc Encryptor, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	ciphertext, err := enc.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString is a convenience wrapper that base64-decodes and decrypts
// a string from database storage.
func DecryptString(enc Encryptor, base64Ciphertext string) (string, error) {
	if base64Ciphertext == "" {
		return "", nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(base64Ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
