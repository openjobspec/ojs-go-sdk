// Copyright 2026 The Open Job Spec Authors
// SPDX-License-Identifier: Apache-2.0

package ojsgrpc

import "testing"

func TestToStructList(t *testing.T) {
	vals, err := toStructList([]any{"hello", 42.0, true, nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 4 {
		t.Fatalf("expected 4 values, got %d", len(vals))
	}
	if vals[0].GetStringValue() != "hello" {
		t.Errorf("expected 'hello', got %v", vals[0])
	}
	if vals[1].GetNumberValue() != 42.0 {
		t.Errorf("expected 42.0, got %v", vals[1])
	}
	if vals[2].GetBoolValue() != true {
		t.Errorf("expected true, got %v", vals[2])
	}
}

func TestToStructList_Empty(t *testing.T) {
	vals, err := toStructList([]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected 0 values, got %d", len(vals))
	}
}

func TestToStructList_MapArg(t *testing.T) {
	args := []any{map[string]any{"key": "value", "count": 10.0}}
	vals, err := toStructList(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 1 {
		t.Fatalf("expected 1 value, got %d", len(vals))
	}
	m := vals[0].GetStructValue()
	if m == nil {
		t.Fatal("expected struct value")
	}
	if m.Fields["key"].GetStringValue() != "value" {
		t.Errorf("expected key='value'")
	}
}

func TestWithAuth(t *testing.T) {
	opts := &options{}
	WithAuth("my-token")(opts)
	if opts.auth != "my-token" {
		t.Errorf("expected auth='my-token', got %q", opts.auth)
	}
}

func TestEnqueueOpts(t *testing.T) {
	o := &EnqueueOpts{
		Queue:    "ml-training",
		Priority: 100,
		Tags:     []string{"gpu", "training"},
	}
	if o.Queue != "ml-training" {
		t.Errorf("expected queue='ml-training', got %q", o.Queue)
	}
	if o.Priority != 100 {
		t.Errorf("expected priority=100, got %d", o.Priority)
	}
	if len(o.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(o.Tags))
	}
}
