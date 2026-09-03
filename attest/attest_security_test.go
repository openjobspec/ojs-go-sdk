package attest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// This file covers the hardening of PQCOnlyAttestor's canonical evidence
// scheme: a crypto-random nonce generated fresh per Attest (not derived from
// the digest, which was fully predictable from the same inputs), a versioned
// canonical payload covering every field a receipt claims, and a Verify that
// cross-checks all of it -- version, exact metadata, algorithm/key ID,
// timestamp consistency and freshness, and nonce format -- before ever
// trusting the signature. Each subsection proves a specific attack the
// pre-fix implementation was vulnerable to is now rejected.

// stubClock and stubNonce let tests deterministically control attestNow and
// generateNonce -- the package's private clock/random seams -- and restore
// the real implementations afterward so no other test is affected.
func stubClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := attestNow
	attestNow = func() time.Time { return now }
	t.Cleanup(func() { attestNow = prev })
}

func stubNonce(t *testing.T, sequence ...string) {
	t.Helper()
	prev := generateNonce
	i := 0
	generateNonce = func() (string, error) {
		if i >= len(sequence) {
			t.Fatalf("stubNonce: exhausted %d-value sequence", len(sequence))
		}
		v := sequence[i]
		i++
		return v, nil
	}
	t.Cleanup(func() { generateNonce = prev })
}

func testAttestor(t *testing.T) *PQCOnlyAttestor {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return NewPQCOnlyAttestor(priv, "key-1")
}

func baseInput() AttestInput {
	return AttestInput{
		JobID:      "job-security-1",
		JobType:    "ml.train",
		ArgsHash:   "sha256:aaaa",
		ResultHash: "sha256:bbbb",
		Timestamp:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Jurisdiction: &Jurisdiction{
			Region:     "us-east-1",
			Datacenter: "dc-3",
			Prover:     "prover-a",
		},
		ModelFingerprint: &ModelFingerprint{
			SHA256:      "sha256:model-x",
			RegistryURL: "https://models.example.com/x",
		},
	}
}

// --- Nonce: crypto-random per call, not derived from the digest ---

// TestAttestGeneratesFreshNonceEveryCall is the direct regression test for
// the fix: two attestations of byte-identical input (same args/result hash
// and timestamp) must not produce the same nonce. The pre-fix
// implementation derived the nonce from the digest, so identical input
// always produced the identical, fully predictable "nonce" -- defeating any
// replay defense that assumes nonce uniqueness.
func TestAttestGeneratesFreshNonceEveryCall(t *testing.T) {
	a := testAttestor(t)
	ctx := context.Background()
	input := baseInput()

	r1, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #1: %v", err)
	}
	r2, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #2: %v", err)
	}

	if r1.Quote.Nonce == "" {
		t.Fatal("nonce must not be empty")
	}
	if r1.Quote.Nonce == r2.Quote.Nonce {
		t.Fatalf("two attestations of identical input produced the same nonce %q: nonce is not random", r1.Quote.Nonce)
	}
}

// TestAttestNonceIsCryptoRandomNotDigestDerived proves the nonce is
// independent of the digest inputs by holding the clock fixed (via the
// private seam) and varying only the nonce generator: with the SAME
// timestamp and args/result hashes, the evidence differs only because the
// nonce does, and the two signatures differ as a result -- which could not
// happen if the nonce were still a deterministic function of the other
// fields.
func TestAttestNonceIsCryptoRandomNotDigestDerived(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	stubNonce(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	r1, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #1: %v", err)
	}

	stubNonce(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	r2, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #2: %v", err)
	}

	if r1.Quote.Nonce == r2.Quote.Nonce {
		t.Fatal("stubbed nonces should differ")
	}
	if bytes.Equal(r1.Quote.Evidence, r2.Quote.Evidence) {
		t.Fatal("evidence must differ when only the nonce differs")
	}
	if r1.Signature.Value == r2.Signature.Value {
		t.Fatal("signatures must differ when the signed evidence differs")
	}
}

// --- Deterministic signing payload via the private clock/random seams ---

// TestCanonicalEvidencePayloadIsDeterministicGivenFixedSeams pins the exact
// canonical evidence bytes produced for a fixed input, clock, and nonce --
// only possible because both are seams private tests can override -- and
// decodes them back to assert every field the finding requires is present:
// job ID/type, args/result hashes, quote type, nonce, both issued timestamps,
// algorithm, key ID, jurisdiction, and model fingerprint.
func TestCanonicalEvidencePayloadIsDeterministicGivenFixedSeams(t *testing.T) {
	a := testAttestor(t)
	fixedNow := time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC)
	stubClock(t, fixedNow)
	stubNonce(t, "0123456789abcdef0123456789abcdef")
	ctx := context.Background()
	input := baseInput()

	r1, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #1: %v", err)
	}

	// Re-run with the identical fixed seams: the payload must be byte-for-byte
	// reproducible, which is what "deterministic" means here.
	stubNonce(t, "0123456789abcdef0123456789abcdef")
	r2, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest #2: %v", err)
	}
	if !bytes.Equal(r1.Quote.Evidence, r2.Quote.Evidence) {
		t.Fatalf("evidence differs across identical fixed seams:\n1: %s\n2: %s", r1.Quote.Evidence, r2.Quote.Evidence)
	}

	var decoded canonicalEvidenceV1
	if err := json.Unmarshal(r1.Quote.Evidence, &decoded); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	want := canonicalEvidenceV1{
		Version:                     canonicalEvidenceVersion,
		JobID:                       "job-security-1",
		JobType:                     "ml.train",
		ArgsHash:                    "sha256:aaaa",
		ResultHash:                  "sha256:bbbb",
		QuoteType:                   QuoteTypePQCOnly,
		Nonce:                       "0123456789abcdef0123456789abcdef",
		JobIssuedAt:                 "2026-01-01T00:00:00Z",
		QuoteIssuedAt:               "2026-06-15T08:30:00Z",
		Algorithm:                   AlgorithmEd25519,
		KeyID:                       "key-1",
		JurisdictionRegion:          "us-east-1",
		JurisdictionDatacenter:      "dc-3",
		JurisdictionProver:          "prover-a",
		ModelFingerprintSHA256:      "sha256:model-x",
		ModelFingerprintRegistryURL: "https://models.example.com/x",
	}
	if decoded != want {
		t.Fatalf("decoded canonical evidence = %+v, want %+v", decoded, want)
	}
}

// --- Verify accepts a well-formed, freshly issued receipt ---

func TestVerifyAcceptsFreshReceipt(t *testing.T) {
	a := testAttestor(t)
	now := time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC)
	stubClock(t, now)
	ctx := context.Background()
	input := baseInput()

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)
	if err := a.Verify(ctx, receipt); err != nil {
		t.Fatalf("Verify: %v, want nil", err)
	}
}

// --- Tamper tests: mutating any single claimed field must be rejected ---

func TestVerifyRejectsTamperedReceiptFields(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	sign := func() Receipt {
		result, err := a.Attest(ctx, input)
		if err != nil {
			t.Fatalf("Attest: %v", err)
		}
		return NewReceipt(input, *result)
	}

	cases := []struct {
		name   string
		tamper func(*Receipt)
	}{
		{"job id", func(r *Receipt) { r.JobID = "job-other" }},
		{"job type", func(r *Receipt) { r.JobType = "other.type" }},
		{"args hash", func(r *Receipt) { r.ArgsHash = "sha256:tampered" }},
		{"result hash", func(r *Receipt) { r.ResultHash = "sha256:tampered" }},
		{"quote type", func(r *Receipt) { r.Quote.Type = QuoteTypeAWSNitro }},
		{"quote nonce", func(r *Receipt) { r.Quote.Nonce = strings.Repeat("f", 32) }},
		{"quote issued at", func(r *Receipt) { r.Quote.IssuedAt = r.Quote.IssuedAt.Add(time.Hour) }},
		{"receipt issued at", func(r *Receipt) { r.IssuedAt = r.IssuedAt.Add(time.Hour) }},
		{"quote evidence bytes", func(r *Receipt) {
			tampered := append([]byte(nil), r.Quote.Evidence...)
			tampered[len(tampered)-2] ^= 0xFF // flip a byte inside the JSON payload
			r.Quote.Evidence = tampered
		}},
		{"signature algorithm", func(r *Receipt) { r.Signature.Algorithm = AlgorithmMLDSA65 }},
		{"signature key id", func(r *Receipt) { r.Signature.KeyID = "other-key" }},
		{"jurisdiction region", func(r *Receipt) { r.Jurisdiction.Region = "eu-west-1" }},
		{"jurisdiction removed", func(r *Receipt) { r.Jurisdiction = nil }},
		{"model fingerprint sha256", func(r *Receipt) { r.ModelFingerprint.SHA256 = "sha256:different-model" }},
		{"model fingerprint removed", func(r *Receipt) { r.ModelFingerprint = nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			receipt := sign()
			// Copy the pointer-valued fields so mutating one case's Jurisdiction
			// or Quote does not leak into another case sharing the same sign().
			if receipt.Quote != nil {
				q := *receipt.Quote
				receipt.Quote = &q
			}
			if receipt.Jurisdiction != nil {
				j := *receipt.Jurisdiction
				receipt.Jurisdiction = &j
			}
			if receipt.ModelFingerprint != nil {
				m := *receipt.ModelFingerprint
				receipt.ModelFingerprint = &m
			}

			tc.tamper(&receipt)

			if err := a.Verify(ctx, receipt); err == nil {
				t.Fatalf("Verify accepted a receipt tampered in %q", tc.name)
			}
		})
	}
}

// --- Transplant: a valid (evidence, signature) pair borrowed for a different receipt ---

// TestVerifyRejectsTransplantedEvidence proves a valid Quote+Signature from
// one job cannot be reused by attaching it to a Receipt claiming a different
// job's identity: the pre-fix digest covered only ArgsHash/ResultHash/
// Timestamp, so a receipt whose JobID/Jurisdiction/ModelFingerprint disagreed
// with what was actually signed still verified.
func TestVerifyRejectsTransplantedEvidence(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()

	victim := baseInput()
	victim.JobID = "victim-job"
	victimResult, err := a.Attest(ctx, victim)
	if err != nil {
		t.Fatalf("Attest(victim): %v", err)
	}

	attacker := baseInput()
	attacker.JobID = "attacker-job"
	attacker.Jurisdiction = &Jurisdiction{Region: "attacker-region", Datacenter: "attacker-dc", Prover: "attacker-prover"}

	// The attacker claims their own job/jurisdiction metadata but reuses the
	// victim's genuine (evidence, signature) pair wholesale.
	transplanted := Receipt{
		JobID:            attacker.JobID,
		JobType:          attacker.JobType,
		ArgsHash:         attacker.ArgsHash,
		ResultHash:       attacker.ResultHash,
		Quote:            victimResult.Quote,
		Jurisdiction:     attacker.Jurisdiction,
		ModelFingerprint: victimResult.ModelFingerprint,
		Signature:        victimResult.Signature,
	}

	err = a.Verify(ctx, transplanted)
	if err == nil {
		t.Fatal("expected the transplanted receipt to be rejected")
	}
	if !strings.Contains(err.Error(), "transplanted") && !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want a transplant/mismatch rejection", err)
	}
}

// --- Legacy digest-only evidence must not be accepted ---

// TestVerifyRejectsLegacyDigestOnlyEvidence proves the pre-v1 evidence shape
// (a raw SHA-256 digest, with none of the canonical payload's structure) is
// rejected rather than silently accepted as if it still meant what v1 means.
func TestVerifyRejectsLegacyDigestOnlyEvidence(t *testing.T) {
	a := testAttestor(t)
	ctx := context.Background()
	input := baseInput()

	// Reconstruct exactly what the pre-fix attestDigest produced:
	// SHA-256(ArgsHash || ResultHash || Timestamp), signed directly.
	legacyDigest := legacyAttestDigest(input)
	// Any signature will do: Verify must reject this before the signature is
	// even relevant, because the evidence does not parse as v1 canonical.
	sig := make([]byte, ed25519.SignatureSize)

	receipt := Receipt{
		JobID:      input.JobID,
		JobType:    input.JobType,
		ArgsHash:   input.ArgsHash,
		ResultHash: input.ResultHash,
		Quote: &Quote{
			Type:     QuoteTypePQCOnly,
			Evidence: legacyDigest,
			Nonce:    hex.EncodeToString(legacyDigest[:16]), // the pre-fix digest-derived "nonce"
			IssuedAt: input.Timestamp,
		},
		Signature: Signature{Algorithm: AlgorithmEd25519, Value: hex.EncodeToString(sig), KeyID: "key-1"},
	}

	err := a.Verify(ctx, receipt)
	if err == nil {
		t.Fatal("expected legacy digest-only evidence to be rejected")
	}
	if !strings.Contains(err.Error(), "legacy digest-only evidence is not accepted") {
		t.Fatalf("error = %v, want the legacy-evidence rejection message", err)
	}
}

// legacyAttestDigest reproduces the pre-fix evidence encoding exactly, for
// TestVerifyRejectsLegacyDigestOnlyEvidence.
func legacyAttestDigest(e AttestInput) []byte {
	h := sha256.New()
	h.Write([]byte(e.ArgsHash))
	h.Write([]byte(e.ResultHash))
	ts, _ := e.Timestamp.MarshalText()
	h.Write(ts)
	return h.Sum(nil)
}

// --- Nonce format validation ---

func TestVerifyRejectsMalformedNonce(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	cases := []struct {
		name  string
		nonce string
	}{
		{"empty", ""},
		{"not hex", "not-hex-characters-zzzzzzzzzzzzz"},
		{"too short", "aabbcc"},
		{"too long", strings.Repeat("ab", nonceByteLen+4)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := a.Attest(ctx, input)
			if err != nil {
				t.Fatalf("Attest: %v", err)
			}
			receipt := NewReceipt(input, *result)
			receipt.Quote.Nonce = tc.nonce

			if err := a.Verify(ctx, receipt); err == nil {
				t.Fatalf("Verify accepted a %s nonce %q", tc.name, tc.nonce)
			}
		})
	}
}

// --- Algorithm / key ID mismatch ---

func TestVerifyRejectsAlgorithmMismatch(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)
	receipt.Signature.Algorithm = AlgorithmHybridEdMLDSA

	if err := a.Verify(ctx, receipt); err == nil {
		t.Fatal("expected an algorithm mismatch to be rejected")
	}
}

func TestVerifyRejectsKeyIDMismatch(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)
	receipt.Signature.KeyID = "someone-elses-key"

	if err := a.Verify(ctx, receipt); err == nil {
		t.Fatal("expected a key ID mismatch to be rejected")
	}
}

// TestVerifyRejectsUnverifiableQuoteType proves an attestor only verifies the
// quote type it produces: a receipt claiming a different quote type (even a
// hardware one this attestor cannot check) must not be waved through.
func TestVerifyRejectsUnverifiableQuoteType(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)
	receipt.Quote.Type = QuoteTypeAWSNitro

	if err := a.Verify(ctx, receipt); err == nil {
		t.Fatal("expected an unverifiable quote type to be rejected")
	}
}

// --- Timestamp consistency ---

// TestVerifyRejectsQuoteIssuedBeforeJob proves an attestation cannot claim to
// have been produced before the job it attests to finished.
func TestVerifyRejectsQuoteIssuedBeforeJob(t *testing.T) {
	a := testAttestor(t)
	jobTime := time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC)
	quoteTime := jobTime.Add(-time.Minute) // issued a minute before the job completed
	stubClock(t, quoteTime)
	ctx := context.Background()

	input := baseInput()
	input.Timestamp = jobTime

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)

	// Verify "later", once the quote's own claimed time is no longer in the
	// future relative to the verifier's clock, isolating the consistency
	// check from the freshness one.
	stubClock(t, jobTime.Add(time.Hour))

	err = a.Verify(ctx, receipt)
	if err == nil {
		t.Fatal("expected a quote issued before the job's own timestamp to be rejected")
	}
	if !strings.Contains(err.Error(), "before the job's own timestamp") {
		t.Fatalf("error = %v, want a timestamp-consistency rejection", err)
	}
}

// --- Freshness / stateless replay mitigation ---

// TestVerifyRejectsStaleReceiptAsReplay proves a receipt whose signed
// issuance time is older than the freshness window is rejected -- this
// package's stateless proxy for "this looks like it might be a replay",
// since a bare Verify(ctx, receipt) call has no nonce registry to consult.
func TestVerifyRejectsStaleReceiptAsReplay(t *testing.T) {
	a := testAttestor(t)
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stubClock(t, issuedAt)
	ctx := context.Background()
	input := baseInput()
	input.Timestamp = issuedAt

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)

	// A verifier checking well past DefaultMaxReceiptAge later -- as if the
	// receipt were being replayed long after it was legitimately issued.
	stubClock(t, issuedAt.Add(DefaultMaxReceiptAge+time.Hour))

	err = a.Verify(ctx, receipt)
	if err == nil {
		t.Fatal("expected a stale receipt to be rejected")
	}
	if !strings.Contains(err.Error(), "stale or replayed") {
		t.Fatalf("error = %v, want a freshness rejection", err)
	}
}

// TestVerifyAcceptsReceiptWithinCustomMaxAge proves MaxReceiptAge is
// respected when a caller narrows it below DefaultMaxReceiptAge.
func TestVerifyRejectsWithNarrowedMaxAge(t *testing.T) {
	a := testAttestor(t)
	a.MaxReceiptAge = time.Minute
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stubClock(t, issuedAt)
	ctx := context.Background()
	input := baseInput()
	input.Timestamp = issuedAt

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)

	stubClock(t, issuedAt.Add(2*time.Minute))
	if err := a.Verify(ctx, receipt); err == nil {
		t.Fatal("expected the narrowed MaxReceiptAge to reject a 2-minute-old receipt")
	}

	stubClock(t, issuedAt.Add(30*time.Second))
	if err := a.Verify(ctx, receipt); err != nil {
		t.Fatalf("Verify within the narrowed window: %v, want nil", err)
	}
}

// TestVerifyRejectsFutureIssuedAt proves a quote claiming to have been issued
// after the verifier's own clock is rejected as excessive clock skew, rather
// than treated as fresh.
func TestVerifyRejectsFutureIssuedAt(t *testing.T) {
	a := testAttestor(t)
	issuedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	stubClock(t, issuedAt)
	ctx := context.Background()
	input := baseInput()
	input.Timestamp = issuedAt.Add(-time.Hour)

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)

	// The verifier's clock is now far behind the quote's claimed issuance.
	stubClock(t, issuedAt.Add(-time.Hour))

	err = a.Verify(ctx, receipt)
	if err == nil {
		t.Fatal("expected a quote issued in the future to be rejected")
	}
	if !strings.Contains(err.Error(), "in the future") {
		t.Fatalf("error = %v, want a clock-skew rejection", err)
	}
}

// --- NewReceipt round-trips every AttestResult field ---

func TestNewReceiptCopiesEveryField(t *testing.T) {
	a := testAttestor(t)
	stubClock(t, time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC))
	ctx := context.Background()
	input := baseInput()

	result, err := a.Attest(ctx, input)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	receipt := NewReceipt(input, *result)

	if receipt.JobID != input.JobID || receipt.JobType != input.JobType ||
		receipt.ArgsHash != input.ArgsHash || receipt.ResultHash != input.ResultHash {
		t.Fatalf("NewReceipt did not copy AttestInput fields: %+v", receipt)
	}
	if receipt.Quote != result.Quote || receipt.Signature != result.Signature {
		t.Fatalf("NewReceipt did not copy AttestResult fields: %+v", receipt)
	}
	if receipt.Jurisdiction != result.Jurisdiction || receipt.ModelFingerprint != result.ModelFingerprint {
		t.Fatalf("NewReceipt did not copy Jurisdiction/ModelFingerprint: %+v", receipt)
	}
	if !receipt.IssuedAt.Equal(result.Quote.IssuedAt) {
		t.Fatalf("NewReceipt.IssuedAt = %v, want the quote's issued_at %v", receipt.IssuedAt, result.Quote.IssuedAt)
	}
}
