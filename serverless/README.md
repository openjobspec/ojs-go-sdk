# OJS Serverless Adapters

Adapters for running OJS job handlers in serverless environments. Each adapter bridges platform-specific invocation mechanisms into the common `serverless.HandlerFunc` interface.

## Supported Runtimes

| Adapter | Handler Type | Trigger Sources |
|---------|-------------|-----------------|
| **AWS Lambda** | `LambdaHandler` | SQS events, HTTP push (Function URL / API Gateway), direct invocation |
| **Cloudflare Workers** | `CloudflareHandler` | HTTP push, FetchEvent (raw job) |
| **Vercel** | `VercelHandler` | HTTP push (Go serverless functions) |

## Installation

```go
import "github.com/openjobspec/ojs-go-sdk/serverless"
```

No external dependencies are required. All adapters use the Go standard library only.

## Usage

### AWS Lambda with SQS

```go
package main

import (
    "context"
    "os"
    "time"

    "github.com/aws/aws-lambda-go/lambda"
    "github.com/openjobspec/ojs-go-sdk/serverless"
)

// Create handler at package level for warm starts.
var handler = serverless.NewLambdaHandler(
    serverless.WithOJSURL("https://ojs.example.com"),
    serverless.WithTimeout(25*time.Second), // leave 5s margin for Lambda's 30s timeout
    serverless.WithPushSigningSecrets(os.Getenv("OJS_PUSH_SIGNING_SECRET")),
)

func init() {
    handler.Register("email.send", func(ctx context.Context, job serverless.JobEvent) error {
        // ctx carries the timeout deadline — check ctx.Done() for cancellation
        return nil
    })
}

func main() {
    lambda.Start(handler.HandleSQS)
}
```

### AWS Lambda with HTTP Push (Function URL)

```go
func main() {
    // HandleHTTP returns an http.HandlerFunc suitable for Lambda Function URLs
    // or API Gateway HTTP API integrations.
    http.Handle("/", handler.HandleHTTP())
}
```

### AWS Lambda Direct Invocation

```go
func main() {
    lambda.Start(handler.HandleDirect)
}
```

### Cloudflare Workers

```go
package main

import (
    "context"
    "net/http"
    "os"
    "time"

    "github.com/openjobspec/ojs-go-sdk/serverless"
)

var handler = serverless.NewCloudflareHandler(
    serverless.WithCloudflareOJSURL("https://ojs.example.com"),
    serverless.WithCloudflareTimeout(25*time.Second),
    serverless.WithCloudflarePushSigningSecrets(os.Getenv("OJS_PUSH_SIGNING_SECRET")),
)

func init() {
    handler.Register("image.resize", func(ctx context.Context, job serverless.JobEvent) error {
        return nil
    })
}

func main() {
    // ServeHTTP handles OJS push delivery (PushDeliveryRequest format)
    http.Handle("/ojs/worker", handler)

    // HandleFetchEvent handles raw job events (JobEvent format)
    http.HandleFunc("/ojs/fetch", handler.HandleFetchEvent)
}
```

### Vercel Serverless Functions

```go
// api/worker.go
package handler

import (
    "context"
    "net/http"
    "os"
    "time"

    "github.com/openjobspec/ojs-go-sdk/serverless"
)

var h = serverless.NewVercelHandler(
    serverless.WithVercelOJSURL("https://ojs.example.com"),
    serverless.WithVercelTimeout(10*time.Second),
    serverless.WithVercelPushSigningSecrets(os.Getenv("OJS_PUSH_SIGNING_SECRET")),
)

func init() {
    h.Register("report.generate", func(ctx context.Context, job serverless.JobEvent) error {
        // Access Vercel request ID for tracing
        reqID := serverless.VercelRequestID(ctx)
        _ = reqID
        return nil
    })
}

func Handler(w http.ResponseWriter, r *http.Request) {
    h.ServeHTTP(w, r)
}
```

## Configuration

All adapters accept platform-specific options:

| Option | Default | Description |
|--------|---------|-------------|
| `WithTimeout` / `WithCloudflareTimeout` / `WithVercelTimeout` | 30s | Maximum job processing duration. Set to 0 to disable. |
| `WithMaxBodySize` / `WithCloudflareMaxBodySize` / `WithVercelMaxBodySize` | 1 MB | Maximum HTTP request body size. |
| `WithOJSURL` / `WithCloudflareOJSURL` / `WithVercelOJSURL` | (empty) | OJS server URL for callbacks. |
| `WithLogger` / `WithCloudflareLogger` / `WithVercelLogger` | `slog.Default()` | Structured logger. |
| `WithPushSigningSecrets` / platform equivalent | none | One or more OJS push HMAC secrets. Multiple values support rotation. |
| `WithPushFreshnessWindow` / platform equivalent | 5m | Maximum permitted past or future timestamp skew. |

Shared authentication settings can also be supplied with `HandlerOptions` via
`WithHandlerOptions`, `WithCloudflareHandlerOptions`, or
`WithVercelHandlerOptions`.

## Push Authentication

HTTP push endpoints fail closed unless at least one signing secret is
configured. Direct invocation and SQS processing are unaffected.

OJS signs the exact raw request bytes with:

```text
HMAC-SHA256(secret, X-OJS-Timestamp + "." + raw_body)
```

The request must provide a Unix-seconds `X-OJS-Timestamp` and an
`X-OJS-Signature` value in `sha256=<hex>` form. Multiple comma-separated
signatures and multiple configured secrets are accepted during rotation. The
timestamp must be within five minutes of the handler clock unless a different
freshness window is configured.

Read the secret from the deployment platform's secret manager or environment
and pass it explicitly:

```go
handler := serverless.NewLambdaHandler(
    serverless.WithPushSigningSecrets(
        os.Getenv("OJS_PUSH_SIGNING_SECRET_CURRENT"),
        os.Getenv("OJS_PUSH_SIGNING_SECRET_PREVIOUS"),
    ),
)
```

Empty secrets are ignored, so a missing environment variable leaves the
endpoint safely unavailable. Unsigned requests can be accepted only by
explicitly selecting
`WithInsecureAllowUnsignedPushForLocalDevelopment` (or its platform-specific
equivalent); never use that option in a deployed environment.

## Cold Start Considerations

Serverless platforms have cold start latency when a new instance is created. To minimize impact:

1. **Create handlers at package level** — Use `var handler = serverless.NewLambdaHandler(...)` and register handlers in `init()`. This ensures initialization happens once, not per-request.

2. **Track cold start latency** — Use `handler.Initialized()` to measure the time between handler creation and the first request.

3. **Keep imports minimal** — The serverless package has zero external dependencies. Avoid importing heavy packages unless needed.

## Timeout Handling

Each adapter applies a configurable timeout to job processing via `context.WithTimeout`. The timeout creates a derived context, so handlers should check `ctx.Done()` for cancellation:

```go
handler.Register("long.job", func(ctx context.Context, job serverless.JobEvent) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err() // returns context.DeadlineExceeded on timeout
        default:
            // do work
        }
    }
})
```

**Important:** Set the adapter timeout *lower* than your platform's function timeout to allow time for error reporting:

| Platform | Function Timeout | Recommended Adapter Timeout |
|----------|-----------------|----------------------------|
| AWS Lambda | 30s (default) | 25s |
| Cloudflare Workers | 30s (free) / 15min (paid) | 25s / 14min |
| Vercel | 10s (Hobby) / 60s (Pro) | 8s / 55s |

## Error Handling

All adapters distinguish between:

- **Missing server authentication configuration** → HTTP 503
- **Invalid, stale, future, or missing signatures** → HTTP 401
- **Oversized authentication headers** → HTTP 431
- **Oversized request bodies** → HTTP 413
- **Invalid requests** (bad JSON, missing fields) → HTTP 400
- **No handler registered** → returned as a retryable failure
- **Handler errors** → returned as a retryable failure (HTTP 200 with error body)
- **Timeouts** → treated as handler errors (retryable)

For SQS, the Lambda adapter returns partial batch failures (`SQSBatchResponse`) so that SQS only retries failed messages, not the entire batch.

## SAM Template

See `template/template.yaml` for a ready-to-use AWS SAM template that deploys a Lambda function with SQS event source mapping and dead letter queue.
