// Copyright 2025 The Open Job Spec Authors
// SPDX-License-Identifier: Apache-2.0

package ojsgrpc

import (
	"context"
	"fmt"

	ojsv1 "github.com/openjobspec/ojs-proto/gen/go/ojs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// Transport provides an OJS gRPC transport.
// Use Dial to create a connected transport or FromConn to wrap an existing connection.
type Transport struct {
	conn   *grpc.ClientConn
	client ojsv1.OJSServiceClient
	auth   string
}

// Option configures the gRPC transport.
type Option func(*options)

type options struct {
	auth     string
	dialOpts []grpc.DialOption
}

// WithAuth sets a Bearer token attached as gRPC metadata on every call.
func WithAuth(token string) Option {
	return func(o *options) { o.auth = token }
}

// WithDialOptions appends gRPC dial options used by Dial.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dialOpts = append(o.dialOpts, opts...) }
}

// Dial creates a gRPC transport connected to the given address.
// The address should be host:port (e.g., "localhost:9090").
func Dial(addr string, opts ...Option) (*Transport, error) {
	cfg := &options{}
	for _, o := range opts {
		o(cfg)
	}

	dialOpts := cfg.dialOpts
	if len(dialOpts) == 0 {
		dialOpts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}

	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("ojsgrpc: dial %s: %w", addr, err)
	}

	return &Transport{
		conn:   conn,
		client: ojsv1.NewOJSServiceClient(conn),
		auth:   cfg.auth,
	}, nil
}

// FromConn wraps an existing gRPC connection.
func FromConn(conn *grpc.ClientConn, opts ...Option) *Transport {
	cfg := &options{}
	for _, o := range opts {
		o(cfg)
	}
	return &Transport{
		conn:   conn,
		client: ojsv1.NewOJSServiceClient(conn),
		auth:   cfg.auth,
	}
}

// Close closes the underlying gRPC connection.
func (t *Transport) Close() error {
	return t.conn.Close()
}

// Client returns the underlying OJS gRPC service client for advanced usage.
func (t *Transport) Client() ojsv1.OJSServiceClient {
	return t.client
}

func (t *Transport) ctx(ctx context.Context) context.Context {
	if t.auth != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+t.auth)
	}
	return ctx
}

// --- Job operations ---

// EnqueueOpts holds optional parameters for gRPC enqueue.
type EnqueueOpts struct {
	Queue    string
	Priority int32
	Tags     []string
}

// Enqueue submits a job via gRPC.
func (t *Transport) Enqueue(ctx context.Context, jobType string, args []any, opts *EnqueueOpts) (*ojsv1.Job, error) {
	argsVals, err := toStructList(args)
	if err != nil {
		return nil, fmt.Errorf("ojsgrpc: marshal args: %w", err)
	}

	req := &ojsv1.EnqueueRequest{
		Type: jobType,
		Args: argsVals,
	}
	if opts != nil {
		req.Options = &ojsv1.EnqueueOptions{
			Queue:    opts.Queue,
			Priority: opts.Priority,
			Tags:     opts.Tags,
		}
	}

	resp, err := t.client.Enqueue(t.ctx(ctx), req)
	if err != nil {
		return nil, err
	}
	return resp.GetJob(), nil
}

// GetJob retrieves a job by ID.
func (t *Transport) GetJob(ctx context.Context, id string) (*ojsv1.Job, error) {
	resp, err := t.client.GetJob(t.ctx(ctx), &ojsv1.GetJobRequest{JobId: id})
	if err != nil {
		return nil, err
	}
	return resp.GetJob(), nil
}

// CancelJob cancels a job by ID.
func (t *Transport) CancelJob(ctx context.Context, id string) (*ojsv1.Job, error) {
	resp, err := t.client.CancelJob(t.ctx(ctx), &ojsv1.CancelJobRequest{JobId: id})
	if err != nil {
		return nil, err
	}
	return resp.GetJob(), nil
}

// Health checks server health.
func (t *Transport) Health(ctx context.Context) (*ojsv1.HealthResponse, error) {
	return t.client.Health(t.ctx(ctx), &ojsv1.HealthRequest{})
}

// --- Worker operations ---

// Fetch requests jobs from the server for processing.
func (t *Transport) Fetch(ctx context.Context, queues []string, count int, workerID string) ([]*ojsv1.Job, error) {
	resp, err := t.client.Fetch(t.ctx(ctx), &ojsv1.FetchRequest{
		Queues:   queues,
		Count:    int32(count),
		WorkerId: workerID,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetJobs(), nil
}

// Ack acknowledges successful job completion.
func (t *Transport) Ack(ctx context.Context, jobID string, result map[string]any) error {
	req := &ojsv1.AckRequest{JobId: jobID}
	if result != nil {
		s, err := structpb.NewStruct(result)
		if err == nil {
			req.Result = s
		}
	}
	_, err := t.client.Ack(t.ctx(ctx), req)
	return err
}

// Nack reports job failure with structured error information.
func (t *Transport) Nack(ctx context.Context, jobID, code, message string, retryable bool) error {
	_, err := t.client.Nack(t.ctx(ctx), &ojsv1.NackRequest{
		JobId: jobID,
		Error: &ojsv1.JobError{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	})
	return err
}

// Heartbeat sends a heartbeat for a job or worker.
// It returns the server-directed worker state.
func (t *Transport) Heartbeat(ctx context.Context, id string, workerID string) (ojsv1.WorkerState, error) {
	resp, err := t.client.Heartbeat(t.ctx(ctx), &ojsv1.HeartbeatRequest{
		Id:       id,
		WorkerId: workerID,
	})
	if err != nil {
		return ojsv1.WorkerState_WORKER_STATE_UNSPECIFIED, err
	}
	return resp.GetDirectedState(), nil
}

// --- Helpers ---

func toStructList(args []any) ([]*structpb.Value, error) {
	result := make([]*structpb.Value, 0, len(args))
	for _, a := range args {
		v, err := structpb.NewValue(a)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}
