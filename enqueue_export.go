package ojs

// TestEnqueueConfig is an exported view of resolved enqueue configuration.
// It is provided for extension packages that need to verify their
// EnqueueOption builders produce the expected meta values.
type TestEnqueueConfig struct {
	Queue    string
	Priority int
	Tags     []string
	Meta     map[string]any
}

// ResolveTestEnqueueConfig applies the given options and returns
// the resolved configuration. This is intended for testing extension
// packages such as ml/.
func ResolveTestEnqueueConfig(opts []EnqueueOption) TestEnqueueConfig {
	cfg := resolveEnqueueConfig(opts)
	return TestEnqueueConfig{
		Queue:    cfg.queue,
		Priority: cfg.priority,
		Tags:     cfg.tags,
		Meta:     cfg.meta,
	}
}
