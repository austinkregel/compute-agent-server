package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keySize is AES-256's key size in bytes.
const keySize = 32

// LoadOrCreateKey reads a base64-encoded AES-256 key from path, generating
// and persisting a new random one (0600) on first run if the file doesn't
// exist yet. This gives SMS content encryption-at-rest by default without
// requiring an operator to provision a key up front — mirroring the
// allowlist state file's "seed on first run" convention
// (server/internal/allowlist).
func LoadOrCreateKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil {
			return nil, fmt.Errorf("decode sms encryption key: %w", decodeErr)
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("sms encryption key at %s is %d bytes, want %d", path, len(key), keySize)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read sms encryption key: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate sms encryption key: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create sms encryption key dir: %w", err)
		}
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write sms encryption key: %w", err)
	}
	return key, nil
}

// seal encrypts plaintext with AES-256-GCM, returning nonce||ciphertext.
func seal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// open decrypts a nonce||ciphertext blob produced by seal.
func open(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext shorter than nonce")
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, data, nil)
}
