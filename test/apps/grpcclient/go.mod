module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcclient

go 1.24.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver => ../grpcserver

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver v0.0.0
	google.golang.org/grpc v1.78.0
)

require (
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
