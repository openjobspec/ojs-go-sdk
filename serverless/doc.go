// Package serverless provides OJS job processing adapters for serverless
// environments including AWS Lambda, Cloudflare Workers, and Vercel. It
// bridges platform-specific events into OJS [JobEvent] objects and handles
// ACK/NACK callbacks automatically.
//
// All adapters support configurable timeouts, request body size limits,
// and structured logging via [log/slog].
//
// # AWS Lambda with SQS
//
// The most common pattern is using SQS event source mapping to trigger
// Lambda functions. The [LambdaHandler] wraps your job handlers and
// translates SQS events into OJS job processing:
//
//	handler := serverless.NewLambdaHandler(
//	    serverless.WithOJSURL("https://ojs.example.com"),
//	    serverless.WithTimeout(25*time.Second), // leave margin for Lambda timeout
//	)
//
//	handler.Register("email.send", func(ctx context.Context, job serverless.JobEvent) error {
//	    // Process the job — ctx has deadline from WithTimeout
//	    return nil
//	})
//
//	lambda.Start(handler.HandleSQS)
//
// For direct Lambda invocations (not via SQS), use [LambdaHandler.HandleDirect].
// For HTTP push delivery via Lambda Function URLs, use [LambdaHandler.HandleHTTP].
//
// # Cloudflare Workers
//
// The [CloudflareHandler] implements [net/http.Handler] for HTTP-based
// serverless platforms. Use it with Cloudflare Workers or any platform
// that accepts standard Go HTTP handlers:
//
//	handler := serverless.NewCloudflareHandler(
//	    serverless.WithCloudflareOJSURL("https://ojs.example.com"),
//	    serverless.WithCloudflareTimeout(25*time.Second),
//	)
//
//	handler.Register("image.resize", func(ctx context.Context, job serverless.JobEvent) error {
//	    // Process the job
//	    return nil
//	})
//
//	http.Handle("/ojs/worker", handler)
//
// For raw job events without the push delivery wrapper, use
// [CloudflareHandler.HandleFetchEvent].
//
// # Vercel Serverless Functions
//
// The [VercelHandler] implements [net/http.Handler] for Vercel Go functions.
// It automatically propagates the Vercel request ID for tracing:
//
//	handler := serverless.NewVercelHandler(
//	    serverless.WithVercelOJSURL("https://ojs.example.com"),
//	    serverless.WithVercelTimeout(10*time.Second),
//	)
//
//	handler.Register("report.generate", func(ctx context.Context, job serverless.JobEvent) error {
//	    // Access Vercel request ID for tracing
//	    reqID := serverless.VercelRequestID(ctx)
//	    _ = reqID
//	    return nil
//	})
//
//	// In api/worker.go:
//	func Handler(w http.ResponseWriter, r *http.Request) {
//	    handler.ServeHTTP(w, r)
//	}
//
// # Push Delivery
//
// All handlers support HTTP push delivery where the OJS server POSTs
// job payloads to a function URL. Use [LambdaHandler.HandleHTTP],
// [CloudflareHandler.ServeHTTP], or [VercelHandler.ServeHTTP] for this pattern.
//
// # Timeout Handling
//
// Each adapter supports configurable timeouts via WithTimeout options. The
// timeout creates a derived [context.Context] with a deadline, so handlers
// can check ctx.Done() for cancellation. Set the timeout lower than your
// platform's function timeout to allow graceful error reporting.
//
// # Cold Starts
//
// Handler instances should be created at package init time (outside the
// request handler) so registration happens once. The [LambdaHandler.Initialized]
// method returns the creation timestamp for cold start latency tracking.
package serverless
