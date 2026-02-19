package federation

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- Routing Strategy Tests ---

func TestAffinityRouter_PrefersLocalRegion(t *testing.T) {
	regions := makeTestRegions("us-east-1", "eu-west-1")

	r := &AffinityRouter{}
	got, err := r.Select(context.Background(), regions, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "us-east-1" {
		t.Errorf("got %q, want %q", got, "us-east-1")
	}
}

func TestAffinityRouter_FallsBackToLowestLatency(t *testing.T) {
	regions := makeTestRegions("us-east-1", "eu-west-1")
	regions["us-east-1"].latency.Store(100_000_000) // 100ms
	regions["eu-west-1"].latency.Store(50_000_000)  // 50ms

	r := &AffinityRouter{}
	got, err := r.Select(context.Background(), regions, "ap-south-1") // local not in map
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "eu-west-1" {
		t.Errorf("got %q, want %q (lowest latency)", got, "eu-west-1")
	}
}

func TestAffinityRouter_NoRegionsError(t *testing.T) {
	r := &AffinityRouter{}
	_, err := r.Select(context.Background(), map[string]*regionState{}, "us-east-1")
	if !errors.Is(err, ErrNoHealthyRegions) {
		t.Errorf("got %v, want ErrNoHealthyRegions", err)
	}
}

func TestOverflowRouter_SelectsFromHealthyRegions(t *testing.T) {
	regions := makeTestRegions("us-east-1", "eu-west-1", "ap-south-1")

	r := &OverflowRouter{}
	selected := make(map[string]int)
	for i := 0; i < 100; i++ {
		got, err := r.Select(context.Background(), regions, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		selected[got]++
	}

	// All three regions should have been selected at least once with equal weights.
	for _, id := range []string{"us-east-1", "eu-west-1", "ap-south-1"} {
		if selected[id] == 0 {
			t.Errorf("region %q was never selected in 100 iterations", id)
		}
	}
}

func TestOverflowRouter_RespectsWeight(t *testing.T) {
	regions := makeTestRegions("us-east-1", "eu-west-1")
	regions["us-east-1"].config.Weight = 9
	regions["eu-west-1"].config.Weight = 1

	r := &OverflowRouter{}
	selected := make(map[string]int)
	for i := 0; i < 1000; i++ {
		got, err := r.Select(context.Background(), regions, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		selected[got]++
	}

	// us-east-1 (weight 9) should be selected much more than eu-west-1 (weight 1).
	if selected["us-east-1"] < selected["eu-west-1"] {
		t.Errorf("expected us-east-1 (%d) to be selected more than eu-west-1 (%d)",
			selected["us-east-1"], selected["eu-west-1"])
	}
}

func TestOverflowRouter_NoRegionsError(t *testing.T) {
	r := &OverflowRouter{}
	_, err := r.Select(context.Background(), map[string]*regionState{}, "")
	if !errors.Is(err, ErrNoHealthyRegions) {
		t.Errorf("got %v, want ErrNoHealthyRegions", err)
	}
}

func TestGeoPinRouter_SelectsTargetRegion(t *testing.T) {
	regions := makeTestRegions("us-east-1", "eu-west-1")

	r := &GeoPinRouter{Region: "eu-west-1"}
	got, err := r.Select(context.Background(), regions, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "eu-west-1" {
		t.Errorf("got %q, want %q", got, "eu-west-1")
	}
}

func TestGeoPinRouter_ErrorWhenRegionNotAvailable(t *testing.T) {
	regions := makeTestRegions("us-east-1")

	r := &GeoPinRouter{Region: "eu-west-1"}
	_, err := r.Select(context.Background(), regions, "us-east-1")
	if !errors.Is(err, ErrRegionUnavailable) {
		t.Errorf("got %v, want ErrRegionUnavailable", err)
	}
}

func TestGeoPinRouter_ErrorWhenNoRegionSpecified(t *testing.T) {
	regions := makeTestRegions("us-east-1")

	r := &GeoPinRouter{Region: ""}
	_, err := r.Select(context.Background(), regions, "us-east-1")
	if err == nil {
		t.Error("expected error for empty region, got nil")
	}
}

// --- Circuit Breaker / Failover Tests ---

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
		WithRegion(RegionConfig{ID: "r2", URL: "http://localhost:2"}),
		WithFailureThreshold(3),
	)

	rs := fc.regions["r1"]
	for i := 0; i < 3; i++ {
		fc.recordFailure(rs)
	}

	if rs.cbState.Load() != circuitOpen {
		t.Errorf("expected circuit breaker to be open, got state %d", rs.cbState.Load())
	}
	if rs.healthy.Load() {
		t.Error("expected region to be unhealthy after circuit breaker opens")
	}
}

func TestCircuitBreaker_RemainsClosedBelowThreshold(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
		WithFailureThreshold(5),
	)

	rs := fc.regions["r1"]
	for i := 0; i < 4; i++ {
		fc.recordFailure(rs)
	}

	if rs.cbState.Load() != circuitClosed {
		t.Errorf("expected circuit breaker to remain closed, got state %d", rs.cbState.Load())
	}
	if !rs.healthy.Load() {
		t.Error("expected region to still be healthy below threshold")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
		WithFailureThreshold(2),
		WithCooldownPeriod(10 * time.Millisecond),
	)

	rs := fc.regions["r1"]

	// Open the circuit breaker.
	for i := 0; i < 2; i++ {
		fc.recordFailure(rs)
	}
	if rs.cbState.Load() != circuitOpen {
		t.Fatal("expected circuit breaker to be open")
	}

	// Wait for cooldown.
	time.Sleep(20 * time.Millisecond)

	// checkRegion should transition to half-open and attempt a probe.
	// Since the server is unreachable, it will fail and re-open.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	fc.checkRegion(ctx, rs)

	// After a failed probe, circuit should be open again (failures incremented).
	state := rs.cbState.Load()
	if state != circuitOpen {
		t.Errorf("expected circuit to re-open after failed probe, got state %d", state)
	}
}

func TestHealthyRegions_FiltersUnhealthy(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
		WithRegion(RegionConfig{ID: "r2", URL: "http://localhost:2"}),
		WithRegion(RegionConfig{ID: "r3", URL: "http://localhost:3"}),
	)

	// Mark r2 as unhealthy.
	fc.regions["r2"].healthy.Store(false)

	healthy := fc.healthyRegions()
	if _, ok := healthy["r2"]; ok {
		t.Error("expected r2 to be filtered from healthy regions")
	}
	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy regions, got %d", len(healthy))
	}
}

// --- Region Discovery Tests ---

func TestNew_ReturnsErrorWithNoRegions(t *testing.T) {
	_, err := New()
	if !errors.Is(err, ErrNoRegions) {
		t.Errorf("got %v, want ErrNoRegions", err)
	}
}

func TestRegions_ReturnsAllConfiguredRegions(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "us-east-1", URL: "http://localhost:1"}),
		WithRegion(RegionConfig{ID: "eu-west-1", URL: "http://localhost:2"}),
	)

	ids := fc.Regions()
	if len(ids) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(ids))
	}

	found := make(map[string]bool)
	for _, id := range ids {
		found[id] = true
	}
	if !found["us-east-1"] || !found["eu-west-1"] {
		t.Errorf("expected us-east-1 and eu-west-1, got %v", ids)
	}
}

func TestRegionClient_ReturnsClientForKnownRegion(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "us-east-1", URL: "http://localhost:1"}),
	)

	client, err := fc.RegionClient("us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestRegionClient_ErrorForUnknownRegion(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "us-east-1", URL: "http://localhost:1"}),
	)

	_, err := fc.RegionClient("nonexistent")
	if !errors.Is(err, ErrRegionNotFound) {
		t.Errorf("got %v, want ErrRegionNotFound", err)
	}
}

func TestIsHealthy_ReflectsRegionState(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
	)

	if !fc.IsHealthy("r1") {
		t.Error("expected r1 to be healthy initially")
	}

	fc.regions["r1"].healthy.Store(false)
	if fc.IsHealthy("r1") {
		t.Error("expected r1 to be unhealthy after marking")
	}
}

func TestIsHealthy_ReturnsFalseForUnknownRegion(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
	)

	if fc.IsHealthy("nonexistent") {
		t.Error("expected false for unknown region")
	}
}

func TestWithRegionClient_UsesProvidedClient(t *testing.T) {
	// Verify that WithRegionClient stores the provided client.
	fc, _ := New(
		WithRegion(RegionConfig{ID: "placeholder", URL: "http://localhost:1"}),
	)

	// Add a region with a custom client via re-initialization.
	fc2, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
		WithRegionClient("r2", fc.regions["placeholder"].client, RegionConfig{URL: "http://localhost:2", Weight: 5}),
	)

	if fc2.regions["r2"].config.Weight != 5 {
		t.Errorf("expected weight 5, got %d", fc2.regions["r2"].config.Weight)
	}
}

func TestDefaultStrategy_IsAffinity(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
	)

	name := strategyName(fc.strategy)
	if name != StrategyAffinity {
		t.Errorf("expected default strategy %q, got %q", StrategyAffinity, name)
	}
}

func TestWithStrategy_SetsStrategy(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{StrategyAffinity, StrategyAffinity},
		{StrategyOverflow, StrategyOverflow},
		{StrategyGeoPin, StrategyGeoPin},
	}

	for _, tt := range tests {
		fc, _ := New(
			WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1"}),
			WithStrategy(tt.input),
		)
		got := strategyName(fc.strategy)
		if got != tt.want {
			t.Errorf("WithStrategy(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFederationID_IsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateFederationID()
		if seen[id] {
			t.Fatalf("duplicate federation ID: %s", id)
		}
		seen[id] = true
	}
}

func TestDefaultWeight_AppliedWhenZero(t *testing.T) {
	fc, _ := New(
		WithRegion(RegionConfig{ID: "r1", URL: "http://localhost:1", Weight: 0}),
	)

	if fc.regions["r1"].config.Weight != DefaultWeight {
		t.Errorf("expected default weight %d, got %d", DefaultWeight, fc.regions["r1"].config.Weight)
	}
}

// --- Helpers ---

func makeTestRegions(ids ...string) map[string]*regionState {
	regions := make(map[string]*regionState, len(ids))
	for _, id := range ids {
		rs := &regionState{
			config: RegionConfig{
				ID:     id,
				URL:    "http://" + id + ".example.com",
				Weight: 1,
			},
		}
		rs.healthy.Store(true)
		rs.cbState.Store(circuitClosed)
		rs.latency.Store(int64(50 * time.Millisecond))
		regions[id] = rs
	}
	return regions
}
