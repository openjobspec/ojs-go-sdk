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
	// Spec-compliant meta keys (ojs-encryption.md).
	MetaKeyEncodings = "ojs.codec.encodings"
	MetaKeyKeyID     = "ojs.codec.key_id"

	// EncodingBinaryEncrypted is the encoding value for encrypted payloads.
	EncodingBinaryEncrypted = "binary/encrypted"

	// ArgsKeyEncoded is the key inside an args element that marks it as encoded.
	ArgsKeyEncoded = "ojs_encoded"

	// Legacy meta keys kept for backward-compatible decryption of existing jobs.
	LegacyMetaKeyEncrypted = "ojs.encryption.encrypted"
	LegacyMetaKeyAlgorithm = "ojs.encryption.algorithm"
	LegacyMetaKeyKeyID     = "ojs.encryption.key_id"

	// Deprecated: use MetaKeyEncodings.
	MetaKeyEncrypted = LegacyMetaKeyEncrypted
	// Deprecated: use MetaKeyEncodings with EncodingBinaryEncrypted.
	MetaKeyAlgorithm = LegacyMetaKeyAlgorithm
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
// It supports both spec-compliant meta keys (ojs.codec.*) and legacy keys
// (ojs.encryption.*) so that existing encrypted jobs continue to work.
func EncryptMiddleware(provider KeyProvider) ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		if ctx.Job.Meta == nil {
			return next(ctx)
		}

		keyID, encrypted := detectEncryption(ctx.Job.Meta)
		if !encrypted {
			return next(ctx)
		}

		key, err := provider.GetKey(keyID)
		if err != nil {
			return fmt.Errorf("encryption key %s not found: %w", keyID, err)
		}

		if len(ctx.Job.RawArgs) == 0 {
			return next(ctx)
		}

		encryptedStr, err := extractEncryptedData(ctx.Job.RawArgs[0])
		if err != nil {
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

// detectEncryption checks meta for encryption markers using spec-compliant keys
// first, then falls back to legacy keys for backward compatibility.
func detectEncryption(meta map[string]interface{}) (keyID string, encrypted bool) {
	if encodings, ok := meta[MetaKeyEncodings]; ok {
		if hasEncryptionEncoding(encodings) {
			keyID, _ = meta[MetaKeyKeyID].(string)
			return keyID, true
		}
	}

	// Legacy: ojs.encryption.encrypted + ojs.encryption.key_id
	if enc, ok := meta[LegacyMetaKeyEncrypted].(bool); ok && enc {
		keyID, _ = meta[LegacyMetaKeyKeyID].(string)
		return keyID, true
	}

	return "", false
}

func hasEncryptionEncoding(v interface{}) bool {
	switch encodings := v.(type) {
	case []interface{}:
		for _, e := range encodings {
			if s, ok := e.(string); ok && s == EncodingBinaryEncrypted {
				return true
			}
		}
	case []string:
		for _, s := range encodings {
			if s == EncodingBinaryEncrypted {
				return true
			}
		}
	}
	return false
}

// extractEncryptedData handles both the new args format (object with ojs_encoded
// flag) and the legacy plain base64 string format.
func extractEncryptedData(arg interface{}) (string, error) {
	if m, ok := arg.(map[string]interface{}); ok {
		if encoded, ok := m[ArgsKeyEncoded].(bool); ok && encoded {
			if data, ok := m["data"].(string); ok {
				return data, nil
			}
		}
	}
	if s, ok := arg.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("unrecognized encrypted args format")
}

// EncryptArgs encrypts job args for enqueue. Call this before client.Enqueue().
// Sets spec-compliant meta keys: ojs.codec.encodings and ojs.codec.key_id.
// The encrypted payload is wrapped in an object with ojs_encoded: true.
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
	encPayload := map[string]interface{}{
		ArgsKeyEncoded: true,
		"data":         encoded,
	}
	encArgs, _ := json.Marshal(encPayload)

	meta := map[string]interface{}{
		MetaKeyEncodings: []string{EncodingBinaryEncrypted},
		MetaKeyKeyID:     provider.CurrentKeyID(),
	}

	return encArgs, meta, nil
}

