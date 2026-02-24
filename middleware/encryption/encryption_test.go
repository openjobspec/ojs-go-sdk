package encryption

import (
	"crypto/rand"
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

	if meta[MetaKeyEncrypted] != true {
		t.Error("expected encrypted=true in meta")
	}
	if meta[MetaKeyKeyID] != "k1" {
		t.Error("expected key_id in meta")
	}

	// Verify the encrypted args are different from original
	if string(encArgs) == string(args) {
		t.Error("encrypted args should differ")
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

	// Parse the encrypted args string out of the JSON
	var encStr string
	json.Unmarshal(encArgs, &encStr)

	mw := EncryptMiddleware(provider)
	ctx := ojs.JobContext{
		Job: ojs.Job{
			ID:      "job-1",
			RawArgs: []any{encStr},
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
