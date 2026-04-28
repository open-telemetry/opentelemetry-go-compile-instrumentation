module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcclient

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver => ../grpcserver

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver v0.0.0
	google.golang.org/grpc v1.80.0
)

require (
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
