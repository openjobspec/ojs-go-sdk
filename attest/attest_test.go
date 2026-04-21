package attest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNoneAttestor(t *testing.T) {
	a := NewNoneAttestor()

	if a.Name() != "none" {
		t.Fatalf("expected name %q, got %q", "none", a.Name())
	}

	ctx := context.Background()
	input := AttestInput{
		JobID:      "job-1",
		JobType:    "test.noop",
		ArgsHash:   "sha256:aaa",
		ResultHash: "sha256:bbb",
		Timestamp:  time.Now(),
	}

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Quote == nil || result.Quote.Type != QuoteTypeNone {
		t.Fatalf("expected quote type %q, got %v", QuoteTypeNone, result.Quote)
	}

	receipt := NewReceipt(input, *result)
	if err := a.Verify(ctx, receipt); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestPQCOnlyAttestor_SignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	_ = pub // used implicitly inside attestor

	a := NewPQCOnlyAttestor(priv, "test-key-1")

	if a.Name() != "pqc-only" {
		t.Fatalf("expected name %q, got %q", "pqc-only", a.Name())
	}

	ctx := context.Background()
	input := AttestInput{
		JobID:      "job-42",
		JobType:    "ml.infer",
		ArgsHash:   "sha256:deadbeef",
		ResultHash: "sha256:cafebabe",
		Timestamp:  time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest returned error: %v", err)
	}
	if result.Quote == nil || result.Quote.Type != QuoteTypePQCOnly {
		t.Fatalf("expected quote type %q", QuoteTypePQCOnly)
	}
	if result.Signature.Algorithm != AlgorithmEd25519 {
		t.Fatalf("expected algorithm %q, got %q", AlgorithmEd25519, result.Signature.Algorithm)
	}
	if result.Signature.KeyID != "test-key-1" {
		t.Fatalf("expected key ID %q, got %q", "test-key-1", result.Signature.KeyID)
	}
	if result.Signature.Value == "" {
		t.Fatal("expected non-empty signature value")
	}

	receipt := NewReceipt(input, *result)
	if err := a.Verify(ctx, receipt); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestPQCOnlyAttestor_VerifyBadSig(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	a := NewPQCOnlyAttestor(priv, "test-key-2")
	ctx := context.Background()

	input := AttestInput{
		JobID:      "job-99",
		JobType:    "test.bad",
		ArgsHash:   "sha256:111",
		ResultHash: "sha256:222",
		Timestamp:  time.Now(),
	}

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest returned error: %v", err)
	}

	// Tamper with the signature only: every other field stays exactly what
	// was signed, so this isolates the Ed25519 check itself.
	tampered := make([]byte, ed25519.SignatureSize)
	copy(tampered, "bad-signature-data-that-is-long-enough-for-ed25519-64-bytes!!!xx")
	receipt := NewReceipt(input, *result)
	receipt.Signature.Value = hex.EncodeToString(tampered)

	err = a.Verify(ctx, receipt)
	if err == nil {
		t.Fatal("expected verification to fail with tampered signature")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("error = %v, want a signature verification failure", err)
	}
}

func TestNitroAttestor_NotAvailable(t *testing.T) {
	a := NewNitroAttestor()

	if a.Name() != "aws-nitro" {
		t.Fatalf("expected name %q, got %q", "aws-nitro", a.Name())
	}

	ctx := context.Background()
	_, err := a.Attest(ctx, AttestInput{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}

	err = a.Verify(ctx, Receipt{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}
}

func TestTDXAttestor_NotAvailable(t *testing.T) {
	a := NewTDXAttestor()

	if a.Name() != "intel-tdx" {
		t.Fatalf("expected name %q, got %q", "intel-tdx", a.Name())
	}

	ctx := context.Background()
	_, err := a.Attest(ctx, AttestInput{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}

	err = a.Verify(ctx, Receipt{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}
}

func TestSEVAttestor_NotAvailable(t *testing.T) {
	a := NewSEVAttestor()

	if a.Name() != "amd-sev-snp" {
		t.Fatalf("expected name %q, got %q", "amd-sev-snp", a.Name())
	}

	ctx := context.Background()
	_, err := a.Attest(ctx, AttestInput{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}

	err = a.Verify(ctx, Receipt{})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}
}

// NewPQCOnlyAttestor cannot report a bad key because it returns no error, so
// the key was previously used unchecked: ed25519.Sign panicked on a short key,
// and Verify panicked inside PrivateKey.Public() before an unchecked type
// assertion could even run. Both now fail the job with an ordinary error.
func TestPQCOnlyAttestorRejectsMalformedKey(t *testing.T) {
	ctx := context.Background()
	input := AttestInput{
		JobID:      "job-bad-key",
		JobType:    "test.badkey",
		ArgsHash:   "sha256:aaa",
		ResultHash: "sha256:bbb",
		Timestamp:  time.Now(),
	}

	cases := []struct {
		name string
		key  ed25519.PrivateKey
	}{
		{"nil key", nil},
		{"empty key", ed25519.PrivateKey{}},
		{"truncated key", make(ed25519.PrivateKey, ed25519.PrivateKeySize-1)},
		{"oversized key", make(ed25519.PrivateKey, ed25519.PrivateKeySize+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewPQCOnlyAttestor(tc.key, "key-1")

			result, err := a.Attest(ctx, input)
			if err == nil {
				t.Errorf("Attest() = %v, nil error; want an invalid-key error", result)
			}

			receipt := Receipt{
				JobID:     input.JobID,
				Quote:     &Quote{Type: QuoteTypePQCOnly, Evidence: []byte("evidence")},
				Signature: Signature{Algorithm: AlgorithmEd25519, Value: hex.EncodeToString([]byte("sig"))},
				IssuedAt:  time.Now(),
			}
			if err := a.Verify(ctx, receipt); err == nil {
				t.Error("Verify() = nil error; want an invalid-key error")
			}
		})
	}
}

// A well-formed key must keep working exactly as before.
func TestPQCOnlyAttestorRoundTripWithValidKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	a := NewPQCOnlyAttestor(priv, "key-1")

	ctx := context.Background()
	input := AttestInput{
		JobID:      "job-ok",
		JobType:    "test.ok",
		ArgsHash:   "sha256:aaa",
		ResultHash: "sha256:bbb",
		Timestamp:  time.Now(),
	}

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest() = %v", err)
	}
	receipt := NewReceipt(input, *result)
	if err := a.Verify(ctx, receipt); err != nil {
		t.Errorf("Verify() = %v, want nil", err)
	}
}
