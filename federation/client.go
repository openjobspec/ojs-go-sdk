package federation

import (
	"context"
	"fmt"
	"sync"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// FederatedClient wraps multiple region clients with routing and failover.
type FederatedClient struct {
	regions      map[string]*regionState
	localRegion  string
	strategy     RoutingStrategy
	healthInterval   time.Duration
	failureThreshold int32
	cooldownPeriod   time.Duration

	mu       sync.RWMutex
	stopOnce sync.Once
	stopped  chan struct{}
}

// Option configures the FederatedClient.
type Option func(*FederatedClient)

// WithRegion adds a region to the federation.
func WithRegion(cfg RegionConfig) Option {
	return func(fc *FederatedClient) {
		if cfg.Weight <= 0 {
			cfg.Weight = DefaultWeight
		}
		client, err := ojs.NewClient(cfg.URL)
		if err != nil {
			return
		}
		rs := &regionState{
			config: cfg,
			client: client,
		}
		rs.healthy.Store(true)
		rs.cbState.Store(circuitClosed)
		fc.regions[cfg.ID] = rs
	}
}

// WithRegionClient adds a region with a pre-configured OJS client.
func WithRegionClient(id string, client *ojs.Client, cfg RegionConfig) Option {
	return func(fc *FederatedClient) {
		if cfg.Weight <= 0 {
			cfg.Weight = DefaultWeight
		}
		cfg.ID = id
		rs := &regionState{
			config: cfg,
			client: client,
		}
		rs.healthy.Store(true)
		rs.cbState.Store(circuitClosed)
		fc.regions[id] = rs
	}
}

// WithLocalRegion sets the preferred local region for affinity routing.
func WithLocalRegion(id string) Option {
	return func(fc *FederatedClient) {
		fc.localRegion = id
	}
}

// WithStrategy sets the default routing strategy by name.
func WithStrategy(name string) Option {
	return func(fc *FederatedClient) {
		switch name {
		case StrategyAffinity:
			fc.strategy = &AffinityRouter{}
		case StrategyOverflow:
			fc.strategy = &OverflowRouter{}
		case StrategyGeoPin:
			fc.strategy = &GeoPinRouter{}
		case StrategyRoundRobin:
			fc.strategy = &RoundRobinRouter{}
		case StrategyLatencyBased:
			fc.strategy = &LatencyBasedRouter{}
		case StrategyActivePassive:
			fc.strategy = &ActivePassiveRouter{}
		case StrategyGeographic:
			fc.strategy = &GeographicRouter{}
		}
	}
}

// WithCustomStrategy sets a custom routing strategy implementation.
func WithCustomStrategy(s RoutingStrategy) Option {
	return func(fc *FederatedClient) {
		fc.strategy = s
	}
}

// WithHealthInterval sets the interval between health checks.
func WithHealthInterval(d time.Duration) Option {
	return func(fc *FederatedClient) {
		fc.healthInterval = d
	}
}

// WithFailureThreshold sets the number of consecutive failures before
// a circuit breaker opens.
func WithFailureThreshold(n int) Option {
	return func(fc *FederatedClient) {
		fc.failureThreshold = int32(n)
	}
}

// WithCooldownPeriod sets the duration a circuit breaker stays open.
func WithCooldownPeriod(d time.Duration) Option {
	return func(fc *FederatedClient) {
		fc.cooldownPeriod = d
	}
}

// New creates a new FederatedClient with the given options.
func New(opts ...Option) (*FederatedClient, error) {
	fc := &FederatedClient{
		regions:          make(map[string]*regionState),
		strategy:         &AffinityRouter{},
		healthInterval:   DefaultHealthInterval,
		failureThreshold: int32(DefaultFailureThreshold),
		cooldownPeriod:   DefaultCooldownPeriod,
		stopped:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(fc)
	}
	if len(fc.regions) == 0 {
		return nil, ErrNoRegions
	}
	return fc, nil
}

// NewFromConfig creates a FederatedClient from a declarative FederationConfig.
func NewFromConfig(cfg FederationConfig) (*FederatedClient, error) {
	var opts []Option
	for _, r := range cfg.Regions {
		opts = append(opts, WithRegion(r))
	}
	if cfg.LocalRegion != "" {
		opts = append(opts, WithLocalRegion(cfg.LocalRegion))
	}
	if cfg.RoutingPolicy.Strategy != "" {
		opts = append(opts, WithStrategy(cfg.RoutingPolicy.Strategy))
	}
	if cfg.HealthInterval > 0 {
		opts = append(opts, WithHealthInterval(cfg.HealthInterval))
	}
	if cfg.FailureThreshold > 0 {
		opts = append(opts, WithFailureThreshold(cfg.FailureThreshold))
	}
	if cfg.CooldownPeriod > 0 {
		opts = append(opts, WithCooldownPeriod(cfg.CooldownPeriod))
	}
	return New(opts...)
}

// StartHealthChecks begins periodic health monitoring of all regions.
// It blocks until ctx is cancelled. Call this in a goroutine.
func (fc *FederatedClient) StartHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(fc.healthInterval)
	defer ticker.Stop()

	// Run an immediate check.
	fc.checkAllRegions(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-fc.stopped:
			return
		case <-ticker.C:
			fc.checkAllRegions(ctx)
		}
	}
}

// Stop halts health checking.
func (fc *FederatedClient) Stop() {
	fc.stopOnce.Do(func() {
		close(fc.stopped)
	})
}

// checkAllRegions performs a health check on every region.
func (fc *FederatedClient) checkAllRegions(ctx context.Context) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	var wg sync.WaitGroup
	for _, rs := range fc.regions {
		rs := rs
		wg.Add(1)
		go func() {
			defer wg.Done()
			fc.checkRegion(ctx, rs)
		}()
	}
	wg.Wait()
}

// checkRegion performs a single health check against a region.
func (fc *FederatedClient) checkRegion(ctx context.Context, rs *regionState) {
	cbState := rs.cbState.Load()

	// If circuit is open, check if cooldown has expired.
	if cbState == circuitOpen {
		lastFail := time.Unix(0, rs.lastFailure.Load())
		if time.Since(lastFail) < fc.cooldownPeriod {
			return // Still in cooldown.
		}
		rs.cbState.Store(circuitHalfOpen)
	}

	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	status, err := rs.client.Health(checkCtx)
	elapsed := time.Since(start)

	if err != nil || status.Status != "ok" {
		fc.recordFailure(rs)
		return
	}

	// Healthy response.
	rs.healthy.Store(true)
	rs.latency.Store(elapsed.Nanoseconds())
	rs.failures.Store(0)
	rs.cbState.Store(circuitClosed)
}

// recordFailure increments the failure counter and opens the circuit breaker
// if the threshold is exceeded.
func (fc *FederatedClient) recordFailure(rs *regionState) {
	rs.lastFailure.Store(time.Now().UnixNano())
	count := rs.failures.Add(1)
	if count >= fc.failureThreshold {
		rs.cbState.Store(circuitOpen)
		rs.healthy.Store(false)
	}
}

// healthyRegions returns a map of regions whose circuit breaker is not open.
func (fc *FederatedClient) healthyRegions() map[string]*regionState {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	healthy := make(map[string]*regionState)
	for id, rs := range fc.regions {
		if rs.healthy.Load() {
			healthy[id] = rs
		}
	}
	return healthy
}

// RegionClient returns the underlying OJS client for a specific region.
func (fc *FederatedClient) RegionClient(regionID string) (*ojs.Client, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	rs, ok := fc.regions[regionID]
	if !ok {
		return nil, ErrRegionNotFound
	}
	return rs.client, nil
}

// Regions returns the list of configured region IDs.
func (fc *FederatedClient) Regions() []string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	ids := make([]string, 0, len(fc.regions))
	for id := range fc.regions {
		ids = append(ids, id)
	}
	return ids
}

// IsHealthy reports whether a region is currently considered healthy.
func (fc *FederatedClient) IsHealthy(regionID string) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	rs, ok := fc.regions[regionID]
	if !ok {
		return false
	}
	return rs.healthy.Load()
}

// --- Enqueue operations ---

// EnqueueWithRegion enqueues a job using federation routing.
// Federation metadata is injected into the job's meta.
func (fc *FederatedClient) EnqueueWithRegion(ctx context.Context, jobType string, args ojs.Args, opts ...ojs.EnqueueOption) (*ojs.Job, string, error) {
	healthy := fc.healthyRegions()
	if len(healthy) == 0 {
		return nil, "", ErrNoHealthyRegions
	}

	regionID, err := fc.strategy.Select(ctx, healthy, fc.localRegion)
	if err != nil {
		return nil, "", err
	}

	rs, ok := healthy[regionID]
	if !ok {
		return nil, "", ErrRegionUnavailable
	}

	fedID := generateFederationID()
	fedMeta := map[string]any{
		metaPrefix + "federation_id":   fedID,
		metaPrefix + "region":          regionID,
		metaPrefix + "region_affinity": strategyName(fc.strategy),
	}
	opts = append(opts, ojs.WithMeta(fedMeta))

	job, err := rs.client.Enqueue(ctx, jobType, args, opts...)
	if err != nil {
		fc.recordFailure(rs)
		return nil, "", err
	}
	return job, regionID, nil
}

// EnqueueToRegion enqueues a job to a specific region (geo-pin).
func (fc *FederatedClient) EnqueueToRegion(ctx context.Context, regionID string, jobType string, args ojs.Args, opts ...ojs.EnqueueOption) (*ojs.Job, error) {
	fc.mu.RLock()
	rs, ok := fc.regions[regionID]
	fc.mu.RUnlock()
	if !ok {
		return nil, ErrRegionNotFound
	}
	if !rs.healthy.Load() {
		return nil, ErrRegionUnavailable
	}

	fedID := generateFederationID()
	fedMeta := map[string]any{
		metaPrefix + "federation_id":   fedID,
		metaPrefix + "region":          regionID,
		metaPrefix + "region_affinity": StrategyGeoPin,
	}
	opts = append(opts, ojs.WithMeta(fedMeta))

	job, err := rs.client.Enqueue(ctx, jobType, args, opts...)
	if err != nil {
		fc.recordFailure(rs)
		return nil, err
	}
	return job, nil
}

// FetchFromAnyRegion fetches jobs from all healthy regions, returning the
// first non-empty result. This enables workers to process jobs across regions.
func (fc *FederatedClient) FetchFromAnyRegion(ctx context.Context) (string, error) {
	healthy := fc.healthyRegions()
	if len(healthy) == 0 {
		return "", ErrNoHealthyRegions
	}

	// Prefer local region first.
	if fc.localRegion != "" {
		if rs, ok := healthy[fc.localRegion]; ok {
			if _, err := rs.client.Health(ctx); err == nil {
				return fc.localRegion, nil
			}
		}
	}

	// Fall back to lowest-latency healthy region.
	var bestID string
	var bestLatency int64 = 1<<63 - 1
	for id, rs := range healthy {
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

// GetJob retrieves a job by ID, searching across all healthy regions.
func (fc *FederatedClient) GetJob(ctx context.Context, id string) (*ojs.Job, string, error) {
	healthy := fc.healthyRegions()
	for regionID, rs := range healthy {
		job, err := rs.client.GetJob(ctx, id)
		if err != nil {
			continue
		}
		return job, regionID, nil
	}
	return nil, "", fmt.Errorf("federation: job %s not found in any region", id)
}
