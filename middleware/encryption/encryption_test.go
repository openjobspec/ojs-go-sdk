package encryption

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func testKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestEncryptDecrypt(t *testing.T) {
	key := testKey()
	plaintext := []byte(`["user@example.com", "Welcome!", "Hello world"]`)

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch: %s", decrypted)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := testKey()
	key2 := testKey()

	ciphertext, _ := Encrypt(key1, []byte("secret"))
	_, err := Decrypt(key2, ciphertext)
	if err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestStaticKeyProvider(t *testing.T) {
	key := testKey()
	p, err := NewStaticKeyProvider("key-1", key)
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	if p.CurrentKeyID() != "key-1" {
		t.Errorf("expected key-1, got %s", p.CurrentKeyID())
	}

	got, err := p.GetKey("key-1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if len(got) != 32 {
		t.Error("expected 32-byte key")
	}

	_, err = p.GetKey("unknown")
	if err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestStaticKeyProviderInvalidKey(t *testing.T) {
	_, err := NewStaticKeyProvider("k", make([]byte, 16))
	if err == nil {
		t.Error("expected error for non-256-bit key")
	}
}

func TestEncryptArgs(t *testing.T) {
	key := testKey()
	provider, _ := NewStaticKeyProvider("k1", key)
	args := json.RawMessage(`["hello", 42, true]`)

	encArgs, meta, err := EncryptArgs(provider, args)
	if err != nil {
		t.Fatalf("EncryptArgs: %v", err)
	}

	encodings, ok := meta[MetaKeyEncodings].([]string)
	if !ok || len(encodings) != 1 || encodings[0] != EncodingBinaryEncrypted {
		t.Errorf("expected encodings=[binary/encrypted], got %v", meta[MetaKeyEncodings])
	}
	if meta[MetaKeyKeyID] != "k1" {
		t.Error("expected key_id in meta")
	}

	// Verify the encrypted args contain the ojs_encoded flag
	var payload map[string]interface{}
	if err := json.Unmarshal(encArgs, &payload); err != nil {
		t.Fatalf("expected JSON object in encrypted args: %v", err)
	}
	if payload[ArgsKeyEncoded] != true {
		t.Error("expected ojs_encoded=true in encrypted args")
	}
	if _, ok := payload["data"].(string); !ok {
		t.Error("expected data string in encrypted args")
	}
}

func TestMiddlewareDecryptsArgs(t *testing.T) {
	key := testKey()
	provider, _ := NewStaticKeyProvider("k1", key)
	originalArgs := json.RawMessage(`{"to":"user@example.com","msg":"hello"}`)

	encArgs, meta, _ := EncryptArgs(provider, originalArgs)

	var decryptedArgs ojs.Args
	handler := func(ctx ojs.JobContext) error {
		decryptedArgs = ctx.Job.Args
		return nil
	}

	// Parse the encrypted args object out of the JSON
	var encObj map[string]interface{}
	json.Unmarshal(encArgs, &encObj)

	mw := EncryptMiddleware(provider)
	ctx := ojs.JobContext{
		Job: ojs.Job{
			ID:      "job-1",
			RawArgs: []any{encObj},
			Meta:    meta,
		},
	}

	err := mw(ctx, handler)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	if decryptedArgs["to"] != "user@example.com" {
		t.Errorf("expected decrypted args, got %v", decryptedArgs)
	}
}

func TestMiddlewareSkipsUnencrypted(t *testing.T) {
	provider, _ := NewStaticKeyProvider("k1", testKey())
	mw := EncryptMiddleware(provider)

	originalArgs := ojs.Args{"key": "value"}
	var receivedArgs ojs.Args
	handler := func(ctx ojs.JobContext) error {
		receivedArgs = ctx.Job.Args
		return nil
	}

	ctx := ojs.JobContext{Job: ojs.Job{ID: "j1", Args: originalArgs}}
	mw(ctx, handler)

	if receivedArgs["key"] != "value" {
		t.Error("unencrypted args should pass through unchanged")
	}
}

func TestMiddlewareDecryptsLegacyFormat(t *testing.T) {
	key := testKey()
	provider, _ := NewStaticKeyProvider("k1", key)
	originalArgs := json.RawMessage(`{"to":"legacy@example.com","msg":"old"}`)

	// Simulate a job encrypted with the old format: plain base64 string + legacy meta keys
	encKey, _ := provider.GetKey("k1")
	ciphertext, _ := Encrypt(encKey, originalArgs)
	encStr := base64.StdEncoding.EncodeToString(ciphertext)

	legacyMeta := map[string]interface{}{
		LegacyMetaKeyEncrypted: true,
		LegacyMetaKeyAlgorithm: "AES-256-GCM",
		LegacyMetaKeyKeyID:     "k1",
	}

	var decryptedArgs ojs.Args
	handler := func(ctx ojs.JobContext) error {
		decryptedArgs = ctx.Job.Args
		return nil
	}

	mw := EncryptMiddleware(provider)
	ctx := ojs.JobContext{
		Job: ojs.Job{
			ID:      "legacy-job",
			RawArgs: []any{encStr},
			Meta:    legacyMeta,
		},
	}

	err := mw(ctx, handler)
	if err != nil {
		t.Fatalf("middleware (legacy): %v", err)
	}

	if decryptedArgs["to"] != "legacy@example.com" {
		t.Errorf("expected decrypted legacy args, got %v", decryptedArgs)
	}
}
