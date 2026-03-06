// Package encryption provides client-side encryption middleware for OJS job args.
//
// It encrypts job arguments before enqueue and decrypts them in the worker,
// ensuring sensitive data is never stored in plaintext in the backend.
//
// Uses AES-256-GCM for encryption with a configurable key provider.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

const (
	// MetaKeyEncrypted marks a job's args as encrypted.
	MetaKeyEncrypted = "ojs.encryption.encrypted"
	// MetaKeyAlgorithm records the encryption algorithm used.
	MetaKeyAlgorithm = "ojs.encryption.algorithm"
	// MetaKeyKeyID records which key was used (for rotation).
	MetaKeyKeyID = "ojs.encryption.key_id"
)

// KeyProvider supplies encryption keys. Implement this for KMS integration.
type KeyProvider interface {
	GetKey(keyID string) ([]byte, error)
	CurrentKeyID() string
}

// StaticKeyProvider uses a single fixed key.
type StaticKeyProvider struct {
	keyID string
	key   []byte
}

// NewStaticKeyProvider creates a key provider with a fixed AES-256 key.
func NewStaticKeyProvider(keyID string, key []byte) (*StaticKeyProvider, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256 (got %d)", len(key))
	}
	return &StaticKeyProvider{keyID: keyID, key: key}, nil
}

func (p *StaticKeyProvider) GetKey(keyID string) ([]byte, error) {
	if keyID != p.keyID {
		return nil, fmt.Errorf("unknown key ID: %s", keyID)
	}
	return p.key, nil
}

func (p *StaticKeyProvider) CurrentKeyID() string { return p.keyID }

// Encrypt encrypts plaintext using AES-256-GCM.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
}

// EncryptMiddleware returns worker middleware that decrypts job args before processing.
func EncryptMiddleware(provider KeyProvider) ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		if ctx.Job.Meta == nil {
			return next(ctx)
		}

		encrypted, ok := ctx.Job.Meta[MetaKeyEncrypted].(bool)
		if !ok || !encrypted {
			return next(ctx)
		}

		keyID, _ := ctx.Job.Meta[MetaKeyKeyID].(string)
		key, err := provider.GetKey(keyID)
		if err != nil {
			return fmt.Errorf("encryption key %s not found: %w", keyID, err)
		}

		// The encrypted payload is stored as a base64 string in RawArgs[0]
		if len(ctx.Job.RawArgs) == 0 {
			return next(ctx)
		}
		encryptedStr, ok := ctx.Job.RawArgs[0].(string)
		if !ok {
			return next(ctx)
		}

		ciphertext, err := base64.StdEncoding.DecodeString(encryptedStr)
		if err != nil {
			return fmt.Errorf("base64 decode error: %w", err)
		}

		plaintext, err := Decrypt(key, ciphertext)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}

		// Restore original args
		var decoded ojs.Args
		if json.Unmarshal(plaintext, &decoded) == nil {
			ctx.Job.Args = decoded
		}

		return next(ctx)
	}
}

// EncryptArgs encrypts job args for enqueue. Call this before client.Enqueue().
func EncryptArgs(provider KeyProvider, args json.RawMessage) (json.RawMessage, map[string]interface{}, error) {
	key, err := provider.GetKey(provider.CurrentKeyID())
	if err != nil {
		return nil, nil, err
	}

	ciphertext, err := Encrypt(key, args)
	if err != nil {
		return nil, nil, fmt.Errorf("encryption failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	encArgs, _ := json.Marshal(encoded)

	meta := map[string]interface{}{
		MetaKeyEncrypted: true,
		MetaKeyAlgorithm: "AES-256-GCM",
		MetaKeyKeyID:     provider.CurrentKeyID(),
	}

	return encArgs, meta, nil
}

