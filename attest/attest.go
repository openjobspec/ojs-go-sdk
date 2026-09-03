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
// # Canonical evidence versioning and the Labs API boundary
//
// The signed payload embedded in [Quote.Evidence] is versioned independently
// of both this package's Go API and ojs-go-sdk's module version: it is
// "canonical evidence payload v1" (the unexported canonicalEvidenceVersion).
// [Attestor.Verify] parses and requires this exact version. It does not
// accept the unversioned, digest-only evidence this package produced before
// this payload existed — a receipt carrying that legacy shape fails to parse
// as v1 and is rejected outright, not silently reinterpreted — and it will
// not accept a hypothetical v2 either without an explicit code change to add
// support for it.
//
// This is the version boundary a caller integrating with the Labs
// attestation API should track: a change to the canonical evidence version is
// the signal that receipts produced by an older ojs-go-sdk are no longer
// verifiable by, and did not sign the same claims as, a newer one —
// independent of any other Labs API or SDK version change. [NoneAttestor] is
// exempt from this boundary: it is the explicit "attestation disabled" choice
// and makes no cryptographic claim to version in the first place.
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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	AlgorithmEd25519       = "ed25519"
	AlgorithmMLDSA65       = "ml-dsa-65"
	AlgorithmHybridEdMLDSA = "hybrid:Ed25519+ML-DSA-65"
)

// DefaultMaxReceiptAge bounds how old a receipt's attestation timestamp may be
// at verification time before Verify rejects it as stale. This is the
// package's stateless mitigation for replay: a receipt has no external
// nonce-registry to consult, so a receipt whose signed issuance time is
// further in the past than this window is treated the same as a replayed one.
const DefaultMaxReceiptAge = 24 * time.Hour

// maxClockSkew bounds how far in the future a receipt's signed issuance time
// may be relative to the verifier's own clock. A quote claiming to have been
// issued after "now" is not a freshness problem but a consistency one: either
// clocks are badly skewed or the timestamp was tampered with.
const maxClockSkew = 5 * time.Minute

// nonceByteLen is the number of random bytes generateNonce draws. Verify
// checks a receipt's nonce decodes to exactly this many bytes, which is the
// verifiable proxy this stateless package has for "entropy": it cannot prove
// a past nonce was drawn from a real CSPRNG, but it can reject anything that
// is not even shaped like one (empty, truncated, or the pre-fix
// digest-derived value, which was 16 bytes of digest material presented as a
// nonce rather than 16 bytes of independent randomness).
const nonceByteLen = 16

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

	// Jurisdiction and ModelFingerprint, when supplied, are embedded in the
	// signed canonical evidence and copied onto the resulting AttestResult
	// unchanged. Both are optional: an attestor with no intrinsic notion of
	// where it ran or which model it used leaves the corresponding
	// AttestResult field nil, exactly as before this field existed.
	Jurisdiction     *Jurisdiction
	ModelFingerprint *ModelFingerprint
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
	Type     string // One of the QuoteType* constants.
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
//
// JobType, ArgsHash, and ResultHash mirror the same-named AttestInput fields
// and are cross-checked by Verify against the signed canonical evidence
// embedded in Quote.Evidence: a Receipt whose top-level claims do not match
// what was actually signed is rejected, which is what makes it unsafe to
// construct a Receipt by hand from only a JobID and a borrowed Quote/
// Signature pair. Use NewReceipt to build one from an AttestInput and the
// AttestResult Attest returned for it.
type Receipt struct {
	JobID            string
	JobType          string
	ArgsHash         string
	ResultHash       string
	Quote            *Quote
	Jurisdiction     *Jurisdiction
	ModelFingerprint *ModelFingerprint
	Signature        Signature
	IssuedAt         time.Time
}

// NewReceipt builds the Receipt a verifier needs from the AttestInput
// originally passed to Attest and the AttestResult it returned, so callers do
// not have to manually copy every field Verify now cross-checks against the
// signed evidence.
func NewReceipt(input AttestInput, result AttestResult) Receipt {
	issuedAt := input.Timestamp
	if result.Quote != nil {
		issuedAt = result.Quote.IssuedAt
	}
	return Receipt{
		JobID:            input.JobID,
		JobType:          input.JobType,
		ArgsHash:         input.ArgsHash,
		ResultHash:       input.ResultHash,
		Quote:            result.Quote,
		Jurisdiction:     result.Jurisdiction,
		ModelFingerprint: result.ModelFingerprint,
		Signature:        result.Signature,
		IssuedAt:         issuedAt,
	}
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
// Canonical evidence payload v1
// ---------------------------------------------------------------------------

// canonicalEvidenceVersion identifies the wire shape signed into
// Quote.Evidence. See the package doc's "Canonical evidence versioning"
// section for the compatibility contract this implies.
const canonicalEvidenceVersion = 1

// canonicalEvidenceV1 is the exact, versioned payload every signing Attestor
// in this package signs and every verifying Attestor re-derives -- never
// trusts verbatim -- from a receipt's own claimed metadata. Field order is
// part of the wire contract (encoding/json marshals struct fields in
// declaration order, deterministically), so it must not be reordered without
// bumping canonicalEvidenceVersion.
//
// This directly closes two classes of attack the pre-v1 raw-digest evidence
// could not: tampering with any receipt field the old digest did not cover
// (JobID, Jurisdiction, ModelFingerprint, KeyID, ...) went undetected because
// Verify never looked at them, and a valid (evidence, signature) pair could be
// transplanted onto a Receipt claiming a different job or origin because
// nothing tied the evidence to the outer receipt's claims.
type canonicalEvidenceV1 struct {
	Version    int    `json:"v"`
	JobID      string `json:"job_id"`
	JobType    string `json:"job_type"`
	ArgsHash   string `json:"args_hash"`
	ResultHash string `json:"result_hash"`

	QuoteType string `json:"quote_type"`
	Nonce     string `json:"nonce"`

	// JobIssuedAt is the job envelope's own timestamp (AttestInput.Timestamp);
	// QuoteIssuedAt is when this specific attestation was produced. Both are
	// RFC 3339 nanosecond text so the encoding is unambiguous and
	// human-readable.
	JobIssuedAt   string `json:"job_issued_at"`
	QuoteIssuedAt string `json:"quote_issued_at"`

	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`

	JurisdictionRegion     string `json:"jurisdiction_region,omitempty"`
	JurisdictionDatacenter string `json:"jurisdiction_datacenter,omitempty"`
	JurisdictionProver     string `json:"jurisdiction_prover,omitempty"`

	ModelFingerprintSHA256      string `json:"model_fingerprint_sha256,omitempty"`
	ModelFingerprintRegistryURL string `json:"model_fingerprint_registry_url,omitempty"`
}

// newCanonicalEvidenceV1 builds the canonical payload for one attestation.
func newCanonicalEvidenceV1(
	envelope AttestInput, quoteType, nonce, algorithm, keyID string, quoteIssuedAt time.Time,
) canonicalEvidenceV1 {
	e := canonicalEvidenceV1{
		Version:       canonicalEvidenceVersion,
		JobID:         envelope.JobID,
		JobType:       envelope.JobType,
		ArgsHash:      envelope.ArgsHash,
		ResultHash:    envelope.ResultHash,
		QuoteType:     quoteType,
		Nonce:         nonce,
		JobIssuedAt:   envelope.Timestamp.UTC().Format(time.RFC3339Nano),
		QuoteIssuedAt: quoteIssuedAt.UTC().Format(time.RFC3339Nano),
		Algorithm:     algorithm,
		KeyID:         keyID,
	}
	if envelope.Jurisdiction != nil {
		e.JurisdictionRegion = envelope.Jurisdiction.Region
		e.JurisdictionDatacenter = envelope.Jurisdiction.Datacenter
		e.JurisdictionProver = envelope.Jurisdiction.Prover
	}
	if envelope.ModelFingerprint != nil {
		e.ModelFingerprintSHA256 = envelope.ModelFingerprint.SHA256
		e.ModelFingerprintRegistryURL = envelope.ModelFingerprint.RegistryURL
	}
	return e
}

// encode returns the exact bytes this payload signs and Quote.Evidence
// carries.
func (e canonicalEvidenceV1) encode() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("attest: encode canonical evidence: %w", err)
	}
	return data, nil
}

// decodeCanonicalEvidence parses the evidence embedded in a receipt's quote.
//
// A receipt carrying the pre-v1 raw-digest evidence -- or anything else that
// is not this exact versioned JSON shape -- fails here rather than being
// silently accepted: legacy digest-only evidence proved nothing about the
// fields it did not cover, so treating it as equivalent to v1 would quietly
// reopen the tampering and transplant holes v1 closes.
func decodeCanonicalEvidence(raw []byte) (canonicalEvidenceV1, error) {
	var e canonicalEvidenceV1
	if err := json.Unmarshal(raw, &e); err != nil {
		return canonicalEvidenceV1{}, fmt.Errorf(
			"attest: receipt evidence is not valid canonical evidence (legacy digest-only evidence is not accepted): %w", err)
	}
	if e.Version != canonicalEvidenceVersion {
		return canonicalEvidenceV1{}, fmt.Errorf(
			"attest: receipt evidence has unsupported version %d, want %d", e.Version, canonicalEvidenceVersion)
	}
	return e, nil
}

// matchesReceipt cross-checks every field a Receipt claims at its top level
// against what is actually embedded in the signed evidence, in a fixed order
// so the first mismatch found is always reported the same way. This is what
// defeats a transplant: swapping the Receipt's JobID, Jurisdiction,
// ModelFingerprint, or key metadata while keeping a valid (evidence,
// signature) pair borrowed from a different attestation changes nothing the
// Ed25519 signature check alone would catch, because that check only proves
// the evidence bytes are unmodified -- not that the receipt describing them
// is honest.
func (e canonicalEvidenceV1) matchesReceipt(r Receipt) error {
	mismatches := []struct {
		field   string
		receipt string
		evince  string
	}{
		{"job_id", r.JobID, e.JobID},
		{"job_type", r.JobType, e.JobType},
		{"args_hash", r.ArgsHash, e.ArgsHash},
		{"result_hash", r.ResultHash, e.ResultHash},
		{"signature.algorithm", r.Signature.Algorithm, e.Algorithm},
		{"signature.key_id", r.Signature.KeyID, e.KeyID},
	}
	if r.Quote != nil {
		mismatches = append(mismatches,
			struct{ field, receipt, evince string }{"quote.type", r.Quote.Type, e.QuoteType},
			struct{ field, receipt, evince string }{"quote.nonce", r.Quote.Nonce, e.Nonce},
		)
	}
	for _, m := range mismatches {
		if m.receipt != m.evince {
			return fmt.Errorf("attest: receipt %s %q does not match signed evidence %q (tampered or transplanted receipt)",
				m.field, m.receipt, m.evince)
		}
	}

	if r.Quote != nil {
		wantQuoteIssuedAt, err := time.Parse(time.RFC3339Nano, e.QuoteIssuedAt)
		if err != nil {
			return fmt.Errorf("attest: signed evidence has malformed quote_issued_at: %w", err)
		}
		if !r.Quote.IssuedAt.Equal(wantQuoteIssuedAt) {
			return fmt.Errorf("attest: receipt quote.issued_at %s does not match signed evidence %s (tampered or transplanted receipt)",
				r.Quote.IssuedAt.Format(time.RFC3339Nano), e.QuoteIssuedAt)
		}
		if !r.IssuedAt.Equal(wantQuoteIssuedAt) {
			return fmt.Errorf("attest: receipt issued_at %s does not match signed evidence %s (tampered or transplanted receipt)",
				r.IssuedAt.Format(time.RFC3339Nano), e.QuoteIssuedAt)
		}
	}

	return e.matchesJurisdictionAndModel(r)
}

// matchesJurisdictionAndModel compares the receipt's Jurisdiction and
// ModelFingerprint pointers against the embedded evidence field by field,
// including the case where the receipt has none but the evidence does (or
// vice versa), which a nil-pointer-only check would miss entirely.
func (e canonicalEvidenceV1) matchesJurisdictionAndModel(r Receipt) error {
	region, datacenter, prover := "", "", ""
	if r.Jurisdiction != nil {
		region, datacenter, prover = r.Jurisdiction.Region, r.Jurisdiction.Datacenter, r.Jurisdiction.Prover
	}
	if region != e.JurisdictionRegion || datacenter != e.JurisdictionDatacenter || prover != e.JurisdictionProver {
		return errors.New("attest: receipt jurisdiction does not match signed evidence (tampered or transplanted receipt)")
	}

	sha256hex, registryURL := "", ""
	if r.ModelFingerprint != nil {
		sha256hex, registryURL = r.ModelFingerprint.SHA256, r.ModelFingerprint.RegistryURL
	}
	if sha256hex != e.ModelFingerprintSHA256 || registryURL != e.ModelFingerprintRegistryURL {
		return errors.New("attest: receipt model fingerprint does not match signed evidence (tampered or transplanted receipt)")
	}
	return nil
}

// checkTimestampConsistency rejects evidence claiming to have been issued
// before the job it attests to had even finished -- internally inconsistent
// regardless of what the current time is.
func (e canonicalEvidenceV1) checkTimestampConsistency() error {
	jobIssuedAt, err := time.Parse(time.RFC3339Nano, e.JobIssuedAt)
	if err != nil {
		return fmt.Errorf("attest: signed evidence has malformed job_issued_at: %w", err)
	}
	quoteIssuedAt, err := time.Parse(time.RFC3339Nano, e.QuoteIssuedAt)
	if err != nil {
		return fmt.Errorf("attest: signed evidence has malformed quote_issued_at: %w", err)
	}
	if quoteIssuedAt.Before(jobIssuedAt) {
		return fmt.Errorf("attest: quote issued_at %s is before the job's own timestamp %s",
			e.QuoteIssuedAt, e.JobIssuedAt)
	}
	return nil
}

// checkFreshness rejects evidence whose signed issuance time is either too
// old (the stateless proxy this package has for "possible replay": with no
// nonce registry to consult, a receipt any older than maxAge is treated the
// same as one being replayed) or implausibly in the future relative to the
// verifier's own clock (attestNow), which points at clock skew or a tampered
// timestamp rather than a legitimately fresh attestation.
func (e canonicalEvidenceV1) checkFreshness(maxAge time.Duration) error {
	quoteIssuedAt, err := time.Parse(time.RFC3339Nano, e.QuoteIssuedAt)
	if err != nil {
		return fmt.Errorf("attest: signed evidence has malformed quote_issued_at: %w", err)
	}
	now := attestNow()
	if age := now.Sub(quoteIssuedAt); age > maxAge {
		return fmt.Errorf("attest: quote issued_at %s is %s old, exceeding the %s freshness window (stale or replayed receipt)",
			e.QuoteIssuedAt, age, maxAge)
	}
	if skew := quoteIssuedAt.Sub(now); skew > maxClockSkew {
		return fmt.Errorf("attest: quote issued_at %s is %s in the future (exceeds %s clock-skew tolerance)",
			e.QuoteIssuedAt, skew, maxClockSkew)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Private clock/random seams
// ---------------------------------------------------------------------------

// attestNow and generateNonce are the package's only sources of "the current
// time" and "fresh randomness". Tests within this package reassign them to
// get deterministic signing payloads and to drive freshness/consistency
// checks without sleeping; production code always uses the real clock and the
// system CSPRNG. Neither is exported: no caller outside this package may
// depend on overriding them.
var (
	attestNow     = time.Now
	generateNonce = func() (string, error) {
		b := make([]byte, nonceByteLen)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("attest: generate nonce: %w", err)
		}
		return hex.EncodeToString(b), nil
	}
)

// validateNonceFormat checks the shape Verify can actually confirm about a
// past nonce: that it decodes to exactly nonceByteLen bytes of hex. This
// cannot prove the bytes came from a CSPRNG, but it does reject anything that
// is not even shaped like one -- in particular the pre-fix scheme, which
// derived a 16-byte "nonce" from the digest itself and so was fully
// predictable from the same ArgsHash/ResultHash/Timestamp every time.
func validateNonceFormat(nonce string) error {
	raw, err := hex.DecodeString(nonce)
	if err != nil {
		return fmt.Errorf("attest: receipt nonce %q is not valid hex: %w", nonce, err)
	}
	if len(raw) != nonceByteLen {
		return fmt.Errorf("attest: receipt nonce has %d bytes, want %d (insufficient entropy or wrong format)",
			len(raw), nonceByteLen)
	}
	return nil
}

// ---------------------------------------------------------------------------
// NoneAttestor
// ---------------------------------------------------------------------------

// NoneAttestor is the default no-op attestor that always succeeds. It makes
// no cryptographic claim at all -- it is the explicit "attestation disabled"
// choice -- so it is exempt from the canonical-evidence version boundary
// documented at the package level.
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
		Jurisdiction:     envelope.Jurisdiction,
		ModelFingerprint: envelope.ModelFingerprint,
		Signature:        Signature{Algorithm: AlgorithmEd25519, Value: "", KeyID: ""},
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

	// MaxReceiptAge overrides DefaultMaxReceiptAge for Verify's freshness
	// check. Zero means "use DefaultMaxReceiptAge"; set it directly on an
	// attestor returned by NewPQCOnlyAttestor to change the window.
	MaxReceiptAge time.Duration
}

// NewPQCOnlyAttestor creates a PQCOnlyAttestor that signs with the given
// Ed25519 private key. keyID is an opaque identifier surfaced in receipts.
func NewPQCOnlyAttestor(privateKey ed25519.PrivateKey, keyID string) *PQCOnlyAttestor {
	return &PQCOnlyAttestor{privateKey: privateKey, keyID: keyID, MaxReceiptAge: DefaultMaxReceiptAge}
}

func (p *PQCOnlyAttestor) Name() string { return "pqc-only" }

// maxReceiptAge is MaxReceiptAge with the zero-value fallback applied, so an
// attestor built as a bare struct literal (as some pre-existing tests do)
// still gets a sane default rather than rejecting every receipt as
// infinitely stale.
func (p *PQCOnlyAttestor) maxReceiptAge() time.Duration {
	if p.MaxReceiptAge <= 0 {
		return DefaultMaxReceiptAge
	}
	return p.MaxReceiptAge
}

func (p *PQCOnlyAttestor) Attest(_ context.Context, envelope AttestInput) (*AttestResult, error) {
	if err := p.checkKey(); err != nil {
		return nil, err
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}
	issuedAt := attestNow()

	evidence := newCanonicalEvidenceV1(envelope, QuoteTypePQCOnly, nonce, AlgorithmEd25519, p.keyID, issuedAt)
	evidenceBytes, err := evidence.encode()
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(p.privateKey, evidenceBytes)

	return &AttestResult{
		Quote: &Quote{
			Type:     QuoteTypePQCOnly,
			Evidence: evidenceBytes,
			Nonce:    nonce,
			IssuedAt: issuedAt,
		},
		Jurisdiction:     envelope.Jurisdiction,
		ModelFingerprint: envelope.ModelFingerprint,
		Signature: Signature{
			Algorithm: AlgorithmEd25519,
			Value:     hex.EncodeToString(sig),
			KeyID:     p.keyID,
		},
	}, nil
}

// Verify performs, in order: key validity, quote presence and type, signature
// algorithm and key ID (against this attestor's own configuration), nonce
// format, canonical-evidence version parsing, exact cross-checks of every
// receipt-claimed field against the signed evidence, the Ed25519 signature
// itself, timestamp consistency, and freshness. Any failure returns
// immediately, so the signature is never even checked against a receipt whose
// metadata has already proven inconsistent with what it claims to be.
func (p *PQCOnlyAttestor) Verify(_ context.Context, receipt Receipt) error {
	if err := p.checkKey(); err != nil {
		return err
	}
	if receipt.Quote == nil {
		return errors.New("attest: receipt has no quote")
	}
	if receipt.Quote.Type != QuoteTypePQCOnly {
		return fmt.Errorf("attest: receipt quote type %q is not verifiable by this pqc-only attestor", receipt.Quote.Type)
	}
	if receipt.Signature.Algorithm != AlgorithmEd25519 {
		return fmt.Errorf("attest: receipt signature algorithm %q is not verifiable by this pqc-only attestor (want %q)",
			receipt.Signature.Algorithm, AlgorithmEd25519)
	}
	if receipt.Signature.KeyID != p.keyID {
		return fmt.Errorf("attest: receipt key ID %q does not match this attestor's key %q", receipt.Signature.KeyID, p.keyID)
	}
	if err := validateNonceFormat(receipt.Quote.Nonce); err != nil {
		return err
	}

	evidence, err := decodeCanonicalEvidence(receipt.Quote.Evidence)
	if err != nil {
		return err
	}
	if err := evidence.matchesReceipt(receipt); err != nil {
		return err
	}

	sig, err := hex.DecodeString(receipt.Signature.Value)
	if err != nil {
		return fmt.Errorf("attest: bad signature hex: %w", err)
	}
	pub, err := p.publicKey()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, receipt.Quote.Evidence, sig) {
		return errors.New("attest: Ed25519 signature verification failed")
	}

	if err := evidence.checkTimestampConsistency(); err != nil {
		return err
	}
	if err := evidence.checkFreshness(p.maxReceiptAge()); err != nil {
		return err
	}
	return nil
}

// checkKey reports whether the configured key can be used by crypto/ed25519.
//
// NewPQCOnlyAttestor cannot reject a malformed key because it returns no error,
// so the size is validated at use. Without this, ed25519.Sign and
// PrivateKey.Public both panic on a short key, turning a configuration mistake
// into a crash inside a worker goroutine.
func (p *PQCOnlyAttestor) checkKey() error {
	if len(p.privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("attest: invalid Ed25519 private key: got %d bytes, want %d",
			len(p.privateKey), ed25519.PrivateKeySize)
	}
	return nil
}

// publicKey derives the verifying key from the configured private key.
func (p *PQCOnlyAttestor) publicKey() (ed25519.PublicKey, error) {
	if err := p.checkKey(); err != nil {
		return nil, err
	}
	pub, ok := p.privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("attest: Ed25519 private key yielded %T, want ed25519.PublicKey",
			p.privateKey.Public())
	}
	return pub, nil
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
