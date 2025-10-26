package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// TestNewAESEncryptor tests creation of AES encryptor with valid and invalid keys
func TestNewAESEncryptor(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		errorMsg  string
		wantError bool
	}{
		{
			name:      "empty key",
			key:       "",
			wantError: true,
			errorMsg:  "encryption key is empty",
		},
		{
			name:      "invalid base64",
			key:       "not-valid-base64!@#$",
			wantError: true,
			errorMsg:  "base64 decode failed",
		},
		{
			name:      "key too short",
			key:       base64.StdEncoding.EncodeToString(make([]byte, 16)), // 16 bytes = 128 bits
			wantError: true,
			errorMsg:  "must be 32 bytes",
		},
		{
			name:      "key too long",
			key:       base64.StdEncoding.EncodeToString(make([]byte, 64)), // 64 bytes
			wantError: true,
			errorMsg:  "must be 32 bytes",
		},
		{
			name:      "valid 32-byte key",
			key:       base64.StdEncoding.EncodeToString(make([]byte, 32)),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewAESEncryptor(tt.key)
			if tt.wantError {
				if err == nil {
					t.Errorf("NewAESEncryptor() expected error but got nil")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("NewAESEncryptor() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewAESEncryptor() unexpected error = %v", err)
				}
				if enc == nil {
					t.Errorf("NewAESEncryptor() returned nil encryptor")
				}
			}
		})
	}
}

// TestEncryptDecrypt_RoundTrip tests that encryption followed by decryption returns original plaintext
func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	// Generate random 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{
			name:      "short string",
			plaintext: "hello",
		},
		{
			name:      "oauth token",
			plaintext: "ya29.a0AfH6SMBx...",
		},
		{
			name:      "long string",
			plaintext: strings.Repeat("a", 1000),
		},
		{
			name:      "unicode",
			plaintext: "Hello 世界 🌍",
		},
		{
			name:      "special characters",
			plaintext: "!@#$%^&*()_+-={}[]|\\:;\"'<>,.?/~`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := enc.Encrypt([]byte(tt.plaintext))
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Verify ciphertext is not empty
			if len(ciphertext) == 0 {
				t.Errorf("Encrypt() returned empty ciphertext")
			}

			// Verify ciphertext is different from plaintext
			if bytes.Equal(ciphertext, []byte(tt.plaintext)) {
				t.Errorf("Encrypt() returned plaintext unchanged")
			}

			// Decrypt
			decrypted, err := enc.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			// Verify decrypted matches original
			if string(decrypted) != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", string(decrypted), tt.plaintext)
			}
		})
	}
}

// TestEncryptDeterminism tests that encrypting same plaintext twice produces different ciphertexts
// (due to random nonce generation)
func TestEncryptDeterminism(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	plaintext := []byte("test plaintext")

	// Encrypt same plaintext twice
	ciphertext1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	ciphertext2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Verify ciphertexts are different (due to random nonce)
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Errorf("Encrypt() produced identical ciphertexts for same plaintext (should be different due to random nonce)")
	}

	// But both should decrypt to same plaintext
	decrypted1, err := enc.Decrypt(ciphertext1)
	if err != nil {
		t.Fatalf("Decrypt(1) error = %v", err)
	}
	decrypted2, err := enc.Decrypt(ciphertext2)
	if err != nil {
		t.Fatalf("Decrypt(2) error = %v", err)
	}

	if !bytes.Equal(decrypted1, plaintext) || !bytes.Equal(decrypted2, plaintext) {
		t.Errorf("Decrypt() failed to recover original plaintext")
	}
}

// TestDecrypt_InvalidCiphertext tests decryption with corrupted or invalid ciphertext
func TestDecrypt_InvalidCiphertext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	tests := []struct {
		name       string
		errorMsg   string
		ciphertext []byte
	}{
		{
			name:       "empty ciphertext",
			ciphertext: []byte{},
			errorMsg:   "ciphertext is empty",
		},
		{
			name:       "ciphertext too short",
			ciphertext: []byte{1, 2, 3}, // less than nonce size (12 bytes)
			errorMsg:   "ciphertext too short",
		},
		{
			name:       "corrupted ciphertext",
			ciphertext: make([]byte, 50), // random bytes, won't authenticate
			errorMsg:   "authentication or integrity check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decrypt(tt.ciphertext)
			if err == nil {
				t.Errorf("Decrypt() expected error but got nil")
			} else if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Decrypt() error = %v, want error containing %q", err, tt.errorMsg)
			}
		})
	}
}

// TestDecrypt_TamperedCiphertext tests that tampering is detected
func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	plaintext := []byte("sensitive data")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Tamper with ciphertext (flip a bit in the middle)
	if len(ciphertext) > 20 {
		ciphertext[20] ^= 0x01
	}

	// Decryption should fail due to authentication failure
	_, err = enc.Decrypt(ciphertext)
	if err == nil {
		t.Errorf("Decrypt() should fail for tampered ciphertext")
	}
	if !strings.Contains(err.Error(), "authentication or integrity check failed") {
		t.Errorf("Decrypt() error = %v, want error about authentication failure", err)
	}
}

// TestDecrypt_WrongKey tests that decryption fails with wrong key
func TestDecrypt_WrongKey(t *testing.T) {
	// Generate two different keys
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	if _, err := rand.Read(key1); err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	base64Key1 := base64.StdEncoding.EncodeToString(key1)
	base64Key2 := base64.StdEncoding.EncodeToString(key2)

	enc1, err := NewAESEncryptor(base64Key1)
	if err != nil {
		t.Fatalf("NewAESEncryptor(1) error = %v", err)
	}

	enc2, err := NewAESEncryptor(base64Key2)
	if err != nil {
		t.Fatalf("NewAESEncryptor(2) error = %v", err)
	}

	plaintext := []byte("secret message")

	// Encrypt with key1
	ciphertext, err := enc1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Try to decrypt with key2 (should fail)
	_, err = enc2.Decrypt(ciphertext)
	if err == nil {
		t.Errorf("Decrypt() with wrong key should fail")
	}
}

// TestEncrypt_EmptyPlaintext tests encryption of empty plaintext
func TestEncrypt_EmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	_, err = enc.Encrypt([]byte{})
	if err == nil {
		t.Errorf("Encrypt() with empty plaintext should return error")
	}
	if !strings.Contains(err.Error(), "plaintext is empty") {
		t.Errorf("Encrypt() error = %v, want error about empty plaintext", err)
	}
}

// TestEncryptString tests the string convenience wrapper
func TestEncryptString(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	t.Run("empty string", func(t *testing.T) {
		result, err := EncryptString(enc, "")
		if err != nil {
			t.Errorf("EncryptString() error = %v", err)
		}
		if result != "" {
			t.Errorf("EncryptString(\"\") = %q, want empty string", result)
		}
	})

	t.Run("valid string", func(t *testing.T) {
		plaintext := "test-access-token-12345"
		encrypted, err := EncryptString(enc, plaintext)
		if err != nil {
			t.Fatalf("EncryptString() error = %v", err)
		}

		// Verify result is valid base64
		_, err = base64.StdEncoding.DecodeString(encrypted)
		if err != nil {
			t.Errorf("EncryptString() result is not valid base64: %v", err)
		}

		// Verify decryption works
		decrypted, err := DecryptString(enc, encrypted)
		if err != nil {
			t.Fatalf("DecryptString() error = %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("DecryptString() = %q, want %q", decrypted, plaintext)
		}
	})
}

// TestDecryptString tests the string decryption convenience wrapper
func TestDecryptString(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	t.Run("empty string", func(t *testing.T) {
		result, err := DecryptString(enc, "")
		if err != nil {
			t.Errorf("DecryptString() error = %v", err)
		}
		if result != "" {
			t.Errorf("DecryptString(\"\") = %q, want empty string", result)
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, err := DecryptString(enc, "not-valid-base64!@#")
		if err == nil {
			t.Errorf("DecryptString() with invalid base64 should return error")
		}
		if !strings.Contains(err.Error(), "base64 decode failed") {
			t.Errorf("DecryptString() error = %v, want error about base64", err)
		}
	})

	t.Run("valid encrypted string", func(t *testing.T) {
		plaintext := "refresh-token-67890"
		encrypted, err := EncryptString(enc, plaintext)
		if err != nil {
			t.Fatalf("EncryptString() error = %v", err)
		}

		decrypted, err := DecryptString(enc, encrypted)
		if err != nil {
			t.Fatalf("DecryptString() error = %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("DecryptString() = %q, want %q", decrypted, plaintext)
		}
	})
}

// TestEncryptionOverhead measures the ciphertext overhead
func TestEncryptionOverhead(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	base64Key := base64.StdEncoding.EncodeToString(key)

	enc, err := NewAESEncryptor(base64Key)
	if err != nil {
		t.Fatalf("NewAESEncryptor() error = %v", err)
	}

	plaintext := []byte("test")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// GCM overhead: 12 bytes (nonce) + 16 bytes (auth tag) = 28 bytes
	expectedOverhead := 28
	actualOverhead := len(ciphertext) - len(plaintext)

	if actualOverhead != expectedOverhead {
		t.Errorf("Encryption overhead = %d bytes, want %d bytes", actualOverhead, expectedOverhead)
	}
}
