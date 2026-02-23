// Copyright 2025 The Open Job Spec Authors
// SPDX-License-Identifier: Apache-2.0

// Package ojsgrpc provides an optional gRPC transport for the OJS Go SDK.
//
// This package wraps the generated OJS gRPC service client to provide a
// convenient API for submitting jobs, fetching work, and managing the job
// lifecycle over gRPC instead of HTTP.
//
// It lives in its own Go module so that the main ojs-go-sdk remains
// zero-dependency — users who only need the HTTP transport never pull in
// gRPC or protobuf libraries.
//
// Basic usage:
//
//	t, err := ojsgrpc.Dial("localhost:9090")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer t.Close()
//
//	job, err := t.Enqueue(ctx, "email.send", []any{"user@example.com"}, nil)
package ojsgrpc
