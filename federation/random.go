package federation

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// The federation package draws every random value from the system CSPRNG rather
// than math/rand.
//
// Federation IDs correlate a single logical job across regions and appear in
// cross-region requests, so a value produced by a globally seeded, observable
// PRNG could be predicted or forged by anyone able to reach a peer region.
// Weighted overflow routing shares the same source simply because there is then
// only one randomness path in the package to reason about; it runs once per
// routed job, where a CSPRNG read is far cheaper than the HTTP call it precedes.

// randIntn returns a uniformly distributed value in [0, n).
//
// It reports an error rather than falling back to a weaker source: the callers
// that need a random choice can all surface the failure, and silently degrading
// the randomness of an identifier is exactly the failure mode worth avoiding.
func randIntn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("ojs federation: random bound must be positive, got %d", n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, fmt.Errorf("ojs federation: read random source: %w", err)
	}
	// v is in [0, n) by construction and n is an int, so the result fits.
	return int(v.Int64()), nil
}

// randBytes fills b from the system CSPRNG.
//
// crypto/rand.Read never returns an error as of Go 1.24: it fills the buffer
// entirely or terminates the process. The check keeps that contract explicit so
// a future change cannot quietly hand back a zero-filled buffer.
func randBytes(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("ojs federation: crypto/rand unavailable: " + err.Error())
	}
}
