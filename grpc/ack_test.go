// Copyright 2025 The Open Job Spec Authors
// SPDX-License-Identifier: Apache-2.0

package ojsgrpc

import (
	"context"
	"math"
	"strings"
	"testing"
)

// TestAckRejectsUnencodableResult is a regression test: an ACK result that
// structpb could not encode was silently discarded and the job was ACKed with
// no result at all, losing the handler's output without any signal.
func TestAckRejectsUnencodableResult(t *testing.T) {
	tr := &Transport{}

	err := tr.Ack(context.Background(), "job-1", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("Ack must report a result that cannot be encoded")
	}
	if !strings.Contains(err.Error(), "job-1") {
		t.Errorf("error = %v, want it to name the job", err)
	}
	if !strings.Contains(err.Error(), "marshal ack result") {
		t.Errorf("error = %v, want it to identify the marshal failure", err)
	}
}

// TestFetchRejectsOutOfRangeCount guards the int -> int32 narrowing that the
// FetchRequest field requires.
func TestFetchRejectsOutOfRangeCount(t *testing.T) {
	tr := &Transport{}

	// Built at runtime so the literal cannot overflow int on 32-bit platforms;
	// there it wraps negative, which the same guard rejects.
	overLimit := math.MaxInt32
	overLimit++

	for _, count := range []int{-1, overLimit} {
		if _, err := tr.Fetch(context.Background(), []string{"default"}, count, "w1"); err == nil {
			t.Errorf("Fetch(count=%d) must be rejected", count)
		}
	}
}
