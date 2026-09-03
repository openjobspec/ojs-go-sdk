package federation

import (
	"regexp"
	"strings"
	"testing"
)

// uuidV7Pattern matches the canonical UUID text form with the version nibble
// fixed at 7 and the variant nibble in the RFC 9562 0b10xx range.
var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestFederationIDIsUUIDv7Shaped(t *testing.T) {
	for i := 0; i < 200; i++ {
		id := generateFederationID()
		if !uuidV7Pattern.MatchString(id) {
			t.Fatalf("generateFederationID() = %q, want UUIDv7 layout %s", id, uuidV7Pattern)
		}
	}
}

// The 48-bit timestamp prefix is big-endian, so IDs minted later must sort at or
// after earlier ones as plain strings. That is the whole point of a v7 layout.
func TestFederationIDIsTimeOrdered(t *testing.T) {
	prev := generateFederationID()
	for i := 0; i < 500; i++ {
		next := generateFederationID()
		if strings.Compare(next[:13], prev[:13]) < 0 {
			t.Fatalf("timestamp prefix went backwards: %q then %q", prev, next)
		}
		prev = next
	}
}

// The random suffix must actually vary. Before this change the entropy came from
// the process-global math/rand; the check guards against a regression to a
// constant or zero-filled tail.
func TestFederationIDRandomSuffixVaries(t *testing.T) {
	seen := make(map[string]bool)
	const n = 500
	for i := 0; i < n; i++ {
		// Everything after the 48-bit timestamp prefix.
		seen[generateFederationID()[14:]] = true
	}
	if len(seen) < n {
		t.Errorf("random suffix collided: %d distinct values out of %d", len(seen), n)
	}
}

func TestRandIntnRejectsNonPositiveBound(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := randIntn(n); err == nil {
			t.Errorf("randIntn(%d) = nil error, want error", n)
		}
	}
}

func TestRandIntnStaysInRange(t *testing.T) {
	const bound = 7
	seen := make(map[int]bool)
	for i := 0; i < 2000; i++ {
		v, err := randIntn(bound)
		if err != nil {
			t.Fatalf("randIntn(%d) = %v", bound, err)
		}
		if v < 0 || v >= bound {
			t.Fatalf("randIntn(%d) = %d, want [0, %d)", bound, v, bound)
		}
		seen[v] = true
	}
	if len(seen) != bound {
		t.Errorf("randIntn(%d) produced %d of %d possible values", bound, len(seen), bound)
	}
}
