module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/client

go 1.23.0

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server v0.0.0
	google.golang.org/grpc v1.64.1
)

require (
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server => ../server
