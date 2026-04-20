package federation

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
)

// --- Routing Strategy Implementations ---

// AffinityRouter prefers the local region, falling back to the lowest-latency
// healthy region.
type AffinityRouter struct{}

func (r *AffinityRouter) Select(_ context.Context, regions map[string]*regionState, localRegion string) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}
	if localRegion != "" {
		if _, ok := regions[localRegion]; ok {
			return localRegion, nil
		}
	}
	return lowestLatencyRegion(regions)
}

// OverflowRouter selects a region using weighted random selection.
// Higher weight values increase the probability of selection.
type OverflowRouter struct{}

func (r *OverflowRouter) Select(_ context.Context, regions map[string]*regionState, _ string) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}

	totalWeight := 0
	for _, rs := range regions {
		totalWeight += rs.config.Weight
	}
	if totalWeight == 0 {
		return "", ErrNoHealthyRegions
	}

	pick, err := randIntn(totalWeight)
	if err != nil {
		return "", err
	}
	for id, rs := range regions {
		pick -= rs.config.Weight
		if pick < 0 {
			return id, nil
		}
	}

	for id := range regions {
		return id, nil
	}
	return "", ErrNoHealthyRegions
}

// GeoPinRouter requires a specific target region. If the region is
// unavailable, it returns an error.
type GeoPinRouter struct {
	Region string
}

func (r *GeoPinRouter) Select(_ context.Context, regions map[string]*regionState, _ string) (string, error) {
	if r.Region == "" {
		return "", fmt.Errorf("federation: geo-pin routing requires a target region")
	}
	if _, ok := regions[r.Region]; !ok {
		return "", fmt.Errorf("%w: %s", ErrRegionUnavailable, r.Region)
	}
	return r.Region, nil
}

// RoundRobinRouter cycles through healthy regions in a deterministic order.
type RoundRobinRouter struct {
	counter atomic.Uint64
}

func (r *RoundRobinRouter) Select(_ context.Context, regions map[string]*regionState, _ string) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}
	ids := sortedRegionIDs(regions)
	idx := r.counter.Add(1) - 1
	return ids[idx%uint64(len(ids))], nil
}

// LatencyBasedRouter routes every request to the region with the lowest
// observed latency from health checks.
type LatencyBasedRouter struct{}

func (r *LatencyBasedRouter) Select(_ context.Context, regions map[string]*regionState, _ string) (string, error) {
	return lowestLatencyRegion(regions)
}

// ActivePassiveRouter sends all traffic to the Primary region. When the
// primary is unhealthy it falls back to Secondaries in order, then to any
// remaining healthy region.
type ActivePassiveRouter struct {
	Primary     string
	Secondaries []string
}

func (r *ActivePassiveRouter) Select(_ context.Context, regions map[string]*regionState, _ string) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}
	if r.Primary != "" {
		if _, ok := regions[r.Primary]; ok {
			return r.Primary, nil
		}
	}
	for _, id := range r.Secondaries {
		if _, ok := regions[id]; ok {
			return id, nil
		}
	}
	// Fall back to any healthy region (deterministic).
	ids := sortedRegionIDs(regions)
	return ids[0], nil
}

// GeographicRouter routes jobs based on a geographic hint stored in the
// context. Use WithGeoHint to attach the hint before calling Enqueue.
//
// Lookup order:
//  1. Exact match in RegionMapping for the context hint.
//  2. DefaultRegion if set and healthy.
//  3. Any healthy region.
type GeographicRouter struct {
	// RegionMapping maps geographic hints (e.g., "eu", "us") to region IDs.
	RegionMapping map[string]string
	// DefaultRegion is used when no hint matches.
	DefaultRegion string
}

func (r *GeographicRouter) Select(ctx context.Context, regions map[string]*regionState, _ string) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}

	if hint, ok := ctx.Value(geoHintKey{}).(string); ok && hint != "" {
		if regionID, exists := r.RegionMapping[hint]; exists {
			if _, healthy := regions[regionID]; healthy {
				return regionID, nil
			}
		}
	}

	if r.DefaultRegion != "" {
		if _, ok := regions[r.DefaultRegion]; ok {
			return r.DefaultRegion, nil
		}
	}

	// Fall back to any healthy region (deterministic).
	ids := sortedRegionIDs(regions)
	return ids[0], nil
}

// geoHintKey is the context key for geographic routing hints.
type geoHintKey struct{}

// WithGeoHint returns a new context carrying a geographic routing hint.
// The GeographicRouter inspects this value to select a region.
func WithGeoHint(ctx context.Context, hint string) context.Context {
	return context.WithValue(ctx, geoHintKey{}, hint)
}

// --- Helpers ---

func lowestLatencyRegion(regions map[string]*regionState) (string, error) {
	if len(regions) == 0 {
		return "", ErrNoHealthyRegions
	}
	var bestID string
	var bestLatency int64 = 1<<63 - 1
	for id, rs := range regions {
		lat := rs.latency.Load()
		if lat < bestLatency {
			bestLatency = lat
			bestID = id
		}
	}
	if bestID == "" {
		return "", ErrNoHealthyRegions
	}
	return bestID, nil
}

func sortedRegionIDs(regions map[string]*regionState) []string {
	ids := make([]string, 0, len(regions))
	for id := range regions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func strategyName(s RoutingStrategy) string {
	switch s.(type) {
	case *AffinityRouter:
		return StrategyAffinity
	case *OverflowRouter:
		return StrategyOverflow
	case *GeoPinRouter:
		return StrategyGeoPin
	case *RoundRobinRouter:
		return StrategyRoundRobin
	case *LatencyBasedRouter:
		return StrategyLatencyBased
	case *ActivePassiveRouter:
		return StrategyActivePassive
	case *GeographicRouter:
		return StrategyGeographic
	default:
		return "custom"
	}
}
