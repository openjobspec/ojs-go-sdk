// Package attest provides verifiable compute attestation for OJS jobs.
//
// This package is part of OJS Labs — forward-looking R&D that is not
// part of the core release train. APIs may change between minor versions.
//
// It defines the [Attestor] interface and concrete implementations for
// hardware-backed (AWS Nitro, Intel TDX, AMD SEV-SNP) and software-only
// (PQC / Ed25519) attestation. A [NoneAttestor] is provided as the default
// no-op implementation.
//
// Usage:
//
//	import "github.com/openjobspec/ojs-go-sdk/attest"
//
//	a := attest.NewNoneAttestor()
//	result, err := a.Attest(ctx, attest.AttestInput{
//	    JobID:      "01J...",
//	    JobType:    "ml.train",
//	    ArgsHash:   "sha256:abc...",
//	    ResultHash: "sha256:def...",
//	    Timestamp:  time.Now(),
//	})
package attest

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotAvailable is returned when a hardware attestation mechanism
// (Nitro, TDX, SEV-SNP) is not available on the current platform.
var ErrNotAvailable = errors.New("attest: hardware attestation not available on this platform")

// QuoteType constants identify the attestation hardware/software envelope.
const (
	QuoteTypeAWSNitro  = "aws-nitro-v1"
	QuoteTypeIntelTDX  = "intel-tdx-v4"
	QuoteTypeAMDSEVSNP = "amd-sev-snp-v2"
	QuoteTypePQCOnly   = "pqc-only"
	QuoteTypeNone      = "none"
)

// SignatureAlgorithm constants for the Signature.Algorithm field.
const (
	AlgorithmEd25519        = "ed25519"
	AlgorithmMLDSA65        = "ml-dsa-65"
	AlgorithmHybridEdMLDSA  = "hybrid:Ed25519+ML-DSA-65"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// AttestInput is the envelope handed to an Attestor for signing.
type AttestInput struct {
	JobID      string
	JobType    string
	ArgsHash   string
	ResultHash string
	Timestamp  time.Time
}

// AttestResult is returned by a successful attestation.
type AttestResult struct {
	Quote            *Quote
	Jurisdiction     *Jurisdiction
	ModelFingerprint *ModelFingerprint
	Signature        Signature
}

// Quote carries the attestation evidence produced by the TEE or software layer.
type Quote struct {
	Type     string    // One of the QuoteType* constants.
	Evidence []byte
	Nonce    string
	IssuedAt time.Time
}

// Jurisdiction records where the attestation was produced.
type Jurisdiction struct {
	Region     string
	Datacenter string
	Prover     string
}

// ModelFingerprint captures an ML model identity for auditability.
type ModelFingerprint struct {
	SHA256      string
	RegistryURL string
}

// Signature is the cryptographic signature over the attestation.
type Signature struct {
	Algorithm string // One of the Algorithm* constants.
	Value     string // Hex-encoded signature bytes.
	KeyID     string
}

// Receipt bundles everything a verifier needs.
type Receipt struct {
	JobID            string
	Quote            *Quote
	Jurisdiction     *Jurisdiction
	ModelFingerprint *ModelFingerprint
	Signature        Signature
	IssuedAt         time.Time
}

// ---------------------------------------------------------------------------
// Attestor interface
// ---------------------------------------------------------------------------

// Attestor is the interface implemented by all attestation back-ends.
type Attestor interface {
	// Name returns a human-readable identifier for this attestor.
	Name() string

	// Attest produces an attestation result for the given input.
	Attest(ctx context.Context, envelope AttestInput) (*AttestResult, error)

	// Verify checks a previously produced receipt.
	Verify(ctx context.Context, receipt Receipt) error
}

// ---------------------------------------------------------------------------
// NoneAttestor
// ---------------------------------------------------------------------------

// NoneAttestor is the default no-op attestor that always succeeds.
type NoneAttestor struct{}

// NewNoneAttestor returns a NoneAttestor.
func NewNoneAttestor() *NoneAttestor { return &NoneAttestor{} }

func (n *NoneAttestor) Name() string { return "none" }

func (n *NoneAttestor) Attest(_ context.Context, envelope AttestInput) (*AttestResult, error) {
	return &AttestResult{
		Quote: &Quote{
			Type:     QuoteTypeNone,
			Evidence: nil,
			Nonce:    "",
			IssuedAt: envelope.Timestamp,
		},
		Signature: Signature{Algorithm: AlgorithmEd25519, Value: "", KeyID: ""},
	}, nil
}

func (n *NoneAttestor) Verify(_ context.Context, _ Receipt) error { return nil }

// ---------------------------------------------------------------------------
// PQCOnlyAttestor
// ---------------------------------------------------------------------------

// PQCOnlyAttestor provides software-only post-quantum-ready attestation.
// Today it signs with Ed25519; the Algorithm field is set to "ed25519" so
// callers can distinguish it from a future ML-DSA-65 upgrade.
type PQCOnlyAttestor struct {
	privateKey ed25519.PrivateKey
	keyID      string
}

// NewPQCOnlyAttestor creates a PQCOnlyAttestor that signs with the given
// Ed25519 private key. keyID is an opaque identifier surfaced in receipts.
func NewPQCOnlyAttestor(privateKey ed25519.PrivateKey, keyID string) *PQCOnlyAttestor {
	return &PQCOnlyAttestor{privateKey: privateKey, keyID: keyID}
}

func (p *PQCOnlyAttestor) Name() string { return "pqc-only" }

func (p *PQCOnlyAttestor) Attest(_ context.Context, envelope AttestInput) (*AttestResult, error) {
	digest := attestDigest(envelope)
	sig := ed25519.Sign(p.privateKey, digest)

	return &AttestResult{
		Quote: &Quote{
			Type:     QuoteTypePQCOnly,
			Evidence: digest,
			Nonce:    hex.EncodeToString(digest[:16]),
			IssuedAt: envelope.Timestamp,
		},
		Signature: Signature{
			Algorithm: AlgorithmEd25519,
			Value:     hex.EncodeToString(sig),
			KeyID:     p.keyID,
		},
	}, nil
}

func (p *PQCOnlyAttestor) Verify(_ context.Context, receipt Receipt) error {
	if receipt.Quote == nil {
		return errors.New("attest: receipt has no quote")
	}
	sig, err := hex.DecodeString(receipt.Signature.Value)
	if err != nil {
		return fmt.Errorf("attest: bad signature hex: %w", err)
	}
	pub := p.privateKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pub, receipt.Quote.Evidence, sig) {
		return errors.New("attest: Ed25519 signature verification failed")
	}
	return nil
}

// attestDigest computes SHA-256(ArgsHash || ResultHash || Timestamp).
func attestDigest(e AttestInput) []byte {
	h := sha256.New()
	h.Write([]byte(e.ArgsHash))
	h.Write([]byte(e.ResultHash))
	ts, _ := e.Timestamp.MarshalText() // RFC 3339
	h.Write(ts)
	sum := h.Sum(nil)
	return sum
}

// ---------------------------------------------------------------------------
// NitroAttestor (AWS Nitro Enclaves stub)
// ---------------------------------------------------------------------------

// NitroAttestor is a placeholder for AWS Nitro Enclave attestation.
type NitroAttestor struct{}

// NewNitroAttestor returns a NitroAttestor.
func NewNitroAttestor() *NitroAttestor { return &NitroAttestor{} }

func (n *NitroAttestor) Name() string { return "aws-nitro" }

func (n *NitroAttestor) Attest(_ context.Context, _ AttestInput) (*AttestResult, error) {
	return nil, ErrNotAvailable
}

func (n *NitroAttestor) Verify(_ context.Context, _ Receipt) error {
	return ErrNotAvailable
}

// ---------------------------------------------------------------------------
// TDXAttestor (Intel TDX stub)
// ---------------------------------------------------------------------------

// TDXAttestor is a placeholder for Intel Trust Domain Extensions attestation.
type TDXAttestor struct{}

// NewTDXAttestor returns a TDXAttestor.
func NewTDXAttestor() *TDXAttestor { return &TDXAttestor{} }

func (t *TDXAttestor) Name() string { return "intel-tdx" }

func (t *TDXAttestor) Attest(_ context.Context, _ AttestInput) (*AttestResult, error) {
	return nil, ErrNotAvailable
}

func (t *TDXAttestor) Verify(_ context.Context, _ Receipt) error {
	return ErrNotAvailable
}

// ---------------------------------------------------------------------------
// SEVAttestor (AMD SEV-SNP stub)
// ---------------------------------------------------------------------------

// SEVAttestor is a placeholder for AMD SEV-SNP attestation.
type SEVAttestor struct{}

// NewSEVAttestor returns a SEVAttestor.
func NewSEVAttestor() *SEVAttestor { return &SEVAttestor{} }

func (s *SEVAttestor) Name() string { return "amd-sev-snp" }

func (s *SEVAttestor) Attest(_ context.Context, _ AttestInput) (*AttestResult, error) {
	return nil, ErrNotAvailable
}

func (s *SEVAttestor) Verify(_ context.Context, _ Receipt) error {
	return ErrNotAvailable
}

// compile-time interface checks
var (
	_ Attestor = (*NoneAttestor)(nil)
	_ Attestor = (*PQCOnlyAttestor)(nil)
	_ Attestor = (*NitroAttestor)(nil)
	_ Attestor = (*TDXAttestor)(nil)
	_ Attestor = (*SEVAttestor)(nil)
)

// ensure crypto import is used
var _ = crypto.SHA256
