module github.com/openjobspec/ojs-go-sdk/grpc

go 1.24

toolchain go1.24.4

require (
	github.com/openjobspec/ojs-proto v0.0.0
	google.golang.org/grpc v1.79.1
	google.golang.org/protobuf v1.36.11
)

require (
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260209200024-4cfbd4190f57 // indirect
)

replace github.com/openjobspec/ojs-proto => ../../ojs-proto
