package recorder

import (
	"context"
	"runtime"
	"time"
)

// Middleware returns a function wrapper that automatically records
// execution traces for every invocation. Use it to wrap job handlers
// so all executions are captured for the Replay Studio.
//
// Usage:
//
//	rec := recorder.New()
//	handler := recorder.Middleware(rec, originalHandler)
//	// handler has the same signature but records to rec
func Middleware(rec *Recorder, fn func(ctx context.Context, args any) (any, error)) func(ctx context.Context, args any) (any, error) {
	return func(ctx context.Context, args any) (any, error) {
		return recordExecution(rec, fn, ctx, args)
	}
}

// TypedMiddleware is a generic version of Middleware that preserves
// the argument and return types through the wrapper, preventing
// type-safety loss from the any-based Middleware.
//
// Usage:
//
//	rec := recorder.New()
//	handler := recorder.TypedMiddleware[MyArgs, MyResult](rec, originalHandler)
func TypedMiddleware[A, R any](rec *Recorder, fn func(ctx context.Context, args A) (R, error)) func(ctx context.Context, args A) (R, error) {
	return func(ctx context.Context, args A) (R, error) {
		funcName := callerName()
		start := time.Now()
		result, err := fn(ctx, args)
		dur := time.Since(start).Milliseconds()

		if err != nil {
			rec.RecordError(funcName, args, err, dur)
		} else {
			rec.RecordCall(funcName, args, result, dur)
		}
		return result, err
	}
}

func recordExecution(rec *Recorder, fn func(ctx context.Context, args any) (any, error), ctx context.Context, args any) (any, error) {
	funcName := callerName()
	start := time.Now()
	result, err := fn(ctx, args)
	dur := time.Since(start).Milliseconds()

	if err != nil {
		rec.RecordError(funcName, args, err, dur)
	} else {
		rec.RecordCall(funcName, args, result, dur)
	}
	return result, err
}

func callerName() string {
	if pc, _, _, ok := runtime.Caller(2); ok {
		if f := runtime.FuncForPC(pc); f != nil {
			return f.Name()
		}
	}
	return "handler"
}
