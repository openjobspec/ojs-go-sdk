// Package federation provides multi-region federation for OJS clients.
//
// A FederatedClient wraps multiple standard OJS clients -- one per region --
// and routes jobs based on configurable strategies: affinity (prefer local),
// overflow (least-loaded), round-robin, latency-based, active-passive,
// geographic, or geo-pin (require specific region).
//
// Federation is composable: it requires no backend changes. Any conforming
// OJS server can participate in a federated topology.
//
// Example:
//
//	fc, err := federation.New(
//	    federation.WithRegion(federation.RegionConfig{ID: "us-east-1", URL: "http://us-east.example.com"}),
//	    federation.WithRegion(federation.RegionConfig{ID: "eu-west-1", URL: "http://eu-west.example.com"}),
//	    federation.WithLocalRegion("us-east-1"),
//	    federation.WithStrategy(federation.StrategyAffinity),
//	)
package federation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// Routing strategy constants.
const (
	StrategyAffinity      = "affinity"
	StrategyOverflow      = "overflow"
	StrategyGeoPin        = "geo-pin"
	StrategyRoundRobin    = "round-robin"
	StrategyLatencyBased  = "latency-based"
	StrategyActivePassive = "active-passive"
	StrategyGeographic    = "geographic"
)

// Circuit breaker states.
const (
	circuitClosed   = 0
	circuitOpen     = 1
	circuitHalfOpen = 2
)

// Default configuration values.
const (
	DefaultHealthInterval   = 10 * time.Second
	DefaultFailureThreshold = 5
	DefaultCooldownPeriod   = 30 * time.Second
	DefaultWeight           = 1
)

// Federation metadata key prefix.
const metaPrefix = "ojs.federation."

// Sentinel errors.
var (
	ErrNoRegions         = errors.New("federation: no regions configured")
	ErrNoHealthyRegions  = errors.New("federation: no healthy regions available")
	ErrRegionNotFound    = errors.New("federation: region not found in registry")
	ErrRegionUnavailable = errors.New("federation: target region is unavailable")
	ErrLocalRegionUnset  = errors.New("federation: local region not configured")
)

// RegionConfig describes a single OJS region in the federation.
type RegionConfig struct {
	// ID is the unique identifier for this region (e.g., "us-east-1").
	ID string

	// URL is the base URL of the OJS server in this region.
	URL string

	// Priority determines failover order; lower values are preferred.
	Priority int

	// Weight is the routing weight for weighted load balancing. Default: 1.
	Weight int

	// Tags are arbitrary labels for filtering (e.g., "gpu", "high-memory").
	Tags []string
}

// FederationConfig holds the complete declarative configuration for a
// federated client. Use NewFromConfig to create a FederatedClient from it.
type FederationConfig struct {
	Regions        []RegionConfig
	LocalRegion    string
	RoutingPolicy  RoutingPolicy
	HealthInterval time.Duration
	// FailureThreshold is the number of consecutive failures before opening a
	// region's circuit breaker. Zero uses DefaultFailureThreshold; negative
	// values are invalid.
	FailureThreshold int
	CooldownPeriod   time.Duration
}

// RoutingPolicy defines the routing strategy and fallback behavior.
type RoutingPolicy struct {
	// Strategy is one of the Strategy* constants.
	Strategy string

	// FallbackBehavior controls what happens when the preferred region is
	// unavailable: "next-healthy" (default) tries another region,
	// "error" returns an error immediately.
	FallbackBehavior string

	// CrossRegionTimeout is the maximum time allowed for cross-region
	// enqueue operations. Zero means no additional timeout.
	CrossRegionTimeout time.Duration
}

// regionState tracks the runtime state of a region.
type regionState struct {
	config  RegionConfig
	client  *ojs.Client
	healthy atomic.Bool
	latency atomic.Int64 // nanoseconds, from last health check

	// Circuit breaker state.
	cbState     atomic.Int32 // circuitClosed, circuitOpen, circuitHalfOpen
	failures    atomic.Int32
	lastFailure atomic.Int64 // unix nano
}

// RoutingStrategy selects a target region for a job.
type RoutingStrategy interface {
	// Select returns the region ID where the job should be enqueued.
	// The regions map contains only healthy regions.
	Select(ctx context.Context, regions map[string]*regionState, localRegion string) (string, error)
}

// generateFederationID produces a time-ordered unique ID in the UUIDv7 layout
// of RFC 9562 section 5.7: a 48-bit big-endian Unix millisecond timestamp, the
// version nibble, the variant bits, and 74 bits from the system CSPRNG.
//
// It is built byte-wise from a single 16-byte buffer rather than from packed
// integer fields. That keeps every field width exact — the previous form
// narrowed a 64-bit clock and a 32-bit nanosecond count into uint32/uint16
// without bounding them — and it takes the entropy from crypto/rand, because a
// federation ID identifies a job across regions and must not be guessable.
//
// It remains dependency-free: only the standard library is used.
func generateFederationID() string {
	// Low 48 bits of the Unix millisecond clock. Masking keeps the value
	// non-negative and exactly 48 bits wide without narrowing a 64-bit clock
	// into a uint32, which is what the previous form did.
	ms := time.Now().UnixMilli() & 0xFFFFFFFFFFFF

	var rnd [10]byte
	randBytes(rnd[:])
	rnd[0] = (rnd[0] & 0x0F) | 0x70 // version 7
	rnd[2] = (rnd[2] & 0x3F) | 0x80 // variant 0b10

	return fmt.Sprintf("%08x-%04x-%x-%x-%x",
		ms>>16, ms&0xFFFF, rnd[0:2], rnd[2:4], rnd[4:10])
}

// NewHTTPClient is a convenience function that creates an *http.Client
// suitable for cross-region communication with reasonable timeouts.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}
