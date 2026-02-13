package ojs

import (
	"testing"
	"time"
)

func TestRetryPolicyToWire(t *testing.T) {
	jitter := true
	policy := RetryPolicy{
		MaxAttempts:        5,
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 3.0,
		MaxInterval:        10 * time.Minute,
		Jitter:             &jitter,
		NonRetryableErrors: []string{"invalid_"},
	}
	wire := policy.toWire()
	if wire.MaxAttempts != 5 {
		t.Errorf("expected max_attempts=5, got %d", wire.MaxAttempts)
	}
	if wire.InitialIntervalMS != 2000 {
		t.Errorf("expected initial_interval_ms=2000, got %d", wire.InitialIntervalMS)
	}
	if wire.BackoffCoefficient != 3.0 {
		t.Errorf("expected backoff_coefficient=3.0, got %f", wire.BackoffCoefficient)
	}
	if wire.MaxIntervalMS != 600000 {
		t.Errorf("expected max_interval_ms=600000, got %d", wire.MaxIntervalMS)
	}
	if wire.Jitter == nil || !*wire.Jitter {
		t.Error("expected jitter=true")
	}
	if len(wire.NonRetryableErrors) != 1 || wire.NonRetryableErrors[0] != "invalid_" {
		t.Errorf("expected non_retryable_errors=[invalid_], got %v", wire.NonRetryableErrors)
	}
}

func TestRetryPolicyFromWire(t *testing.T) {
	jitter := false
	wire := &retryPolicyWire{
		MaxAttempts:        10,
		InitialIntervalMS:  500,
		BackoffCoefficient: 1.5,
		MaxIntervalMS:      30000,
		Jitter:             &jitter,
	}
	policy := retryPolicyFromWire(wire)
	if policy.MaxAttempts != 10 {
		t.Errorf("expected max_attempts=10, got %d", policy.MaxAttempts)
	}
	if policy.InitialInterval != 500*time.Millisecond {
		t.Errorf("expected initial_interval=500ms, got %v", policy.InitialInterval)
	}
	if policy.MaxInterval != 30*time.Second {
		t.Errorf("expected max_interval=30s, got %v", policy.MaxInterval)
	}
	if policy.Jitter == nil || *policy.Jitter {
		t.Error("expected jitter=false")
	}
}

func TestRetryPolicyFromWireNil(t *testing.T) {
	policy := retryPolicyFromWire(nil)
	if policy != nil {
		t.Error("expected nil policy from nil wire")
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.MaxAttempts != 3 {
		t.Errorf("expected max_attempts=3, got %d", policy.MaxAttempts)
	}
	if policy.InitialInterval != 1*time.Second {
		t.Errorf("expected initial_interval=1s, got %v", policy.InitialInterval)
	}
	if policy.BackoffCoefficient != 2.0 {
		t.Errorf("expected backoff_coefficient=2.0, got %f", policy.BackoffCoefficient)
	}
	if policy.MaxInterval != 5*time.Minute {
		t.Errorf("expected max_interval=5m, got %v", policy.MaxInterval)
	}
	if policy.Jitter == nil || !*policy.Jitter {
		t.Error("expected jitter=true")
	}
}

func TestUniquePolicyToWire(t *testing.T) {
	policy := UniquePolicy{
		Key:        []string{"email"},
		Period:     5 * time.Minute,
		OnConflict: "reject",
	}
	wire := policy.toWire()
	if wire.PeriodMS != 300000 {
		t.Errorf("expected period_ms=300000, got %d", wire.PeriodMS)
	}
	if wire.OnConflict != "reject" {
		t.Errorf("expected on_conflict=reject, got %s", wire.OnConflict)
	}
	if len(wire.Key) != 1 || wire.Key[0] != "email" {
		t.Errorf("expected key=[email], got %v", wire.Key)
	}
}
