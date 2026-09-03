package federation

import (
	"math"
	"strings"
	"testing"
)

// TestNewReportsInvalidRegionURL is a regression test: a region whose URL
// failed to parse was silently dropped, so a typo produced either a smaller
// federation than configured or a bare ErrNoRegions with no cause.
func TestNewReportsInvalidRegionURL(t *testing.T) {
	_, err := New(
		WithRegion(RegionConfig{ID: "us-east", URL: "http://ojs.example.com"}),
		WithRegion(RegionConfig{ID: "eu-west", URL: "ftp://not-http"}),
	)
	if err == nil {
		t.Fatal("New() must report an invalid region URL instead of dropping the region")
	}
	if !strings.Contains(err.Error(), "eu-west") {
		t.Errorf("error = %v, want it to name the offending region", err)
	}
}

func TestNewSucceedsWithValidRegions(t *testing.T) {
	fc, err := New(
		WithRegion(RegionConfig{ID: "us-east", URL: "http://a.example.com"}),
		WithRegion(RegionConfig{ID: "eu-west", URL: "http://b.example.com"}),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	defer fc.Stop()
	if len(fc.Regions()) != 2 {
		t.Errorf("regions = %v, want 2", fc.Regions())
	}
}

// TestRegionsAreSorted locks deterministic ordering: the previous map traversal
// returned a different order on every call.
func TestRegionsAreSorted(t *testing.T) {
	fc, err := New(
		WithRegion(RegionConfig{ID: "us-west", URL: "http://c.example.com"}),
		WithRegion(RegionConfig{ID: "ap-south", URL: "http://a.example.com"}),
		WithRegion(RegionConfig{ID: "eu-west", URL: "http://b.example.com"}),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	defer fc.Stop()

	want := []string{"ap-south", "eu-west", "us-west"}
	for i := 0; i < 20; i++ {
		got := fc.Regions()
		if len(got) != len(want) {
			t.Fatalf("Regions() = %v, want %v", got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("Regions() = %v, want %v (stable order required)", got, want)
			}
		}
	}
}

func TestNewWithNoRegionsStillFails(t *testing.T) {
	if _, err := New(); err != ErrNoRegions {
		t.Errorf("New() = %v, want ErrNoRegions", err)
	}
}

// WithFailureThreshold used to narrow its argument to int32 unchecked, so a
// value above math.MaxInt32 wrapped to a negative threshold and every circuit
// breaker opened on the very first failure. New now reports it instead.
func TestWithFailureThresholdRejectsOutOfRange(t *testing.T) {
	if math.MaxInt <= math.MaxInt32 {
		t.Skip("int is 32-bit on this platform; the overflow is unrepresentable")
	}

	for _, n := range []int{-1, math.MaxInt32 + 1} {
		_, err := New(
			WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
			WithFailureThreshold(n),
		)
		if err == nil {
			t.Fatalf("New(WithFailureThreshold(%d)) = nil error, want out-of-range error", n)
		}
		if !strings.Contains(err.Error(), "failure threshold") {
			t.Errorf("New(WithFailureThreshold(%d)) = %v, want a failure-threshold error", n, err)
		}
	}
}

func TestWithFailureThresholdAcceptsInRange(t *testing.T) {
	for _, n := range []int{0, 1, math.MaxInt32} {
		fc, err := New(
			WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
			WithFailureThreshold(n),
		)
		if err != nil {
			t.Fatalf("New(WithFailureThreshold(%d)) = %v", n, err)
		}
		if got := int(fc.failureThreshold); got != n {
			t.Errorf("failureThreshold = %d, want %d", got, n)
		}
		fc.Stop()
	}
}

func TestNewFromConfigRejectsNegativeFailureThreshold(t *testing.T) {
	_, err := NewFromConfig(FederationConfig{
		Regions: []RegionConfig{
			{ID: "r1", URL: "http://localhost:1"},
		},
		FailureThreshold: -1,
	})
	if err == nil {
		t.Fatal("NewFromConfig() = nil error, want negative failure-threshold error")
	}
	for _, want := range []string{"invalid config", "failure threshold", "-1", "non-negative"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("NewFromConfig() error = %q, want context %q", err, want)
		}
	}
}

func TestNewFromConfigFailureThresholdDefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		want      int
	}{
		{name: "zero uses default", threshold: 0, want: DefaultFailureThreshold},
		{name: "positive applies option", threshold: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, err := NewFromConfig(FederationConfig{
				Regions: []RegionConfig{
					{ID: "r1", URL: "http://localhost:1"},
				},
				FailureThreshold: tt.threshold,
			})
			if err != nil {
				t.Fatalf("NewFromConfig() = %v", err)
			}
			defer fc.Stop()

			if got := int(fc.failureThreshold); got != tt.want {
				t.Errorf("failureThreshold = %d, want %d", got, tt.want)
			}
		})
	}
}
