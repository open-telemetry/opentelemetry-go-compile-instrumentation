module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/client

go 1.23.0

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server v0.0.0
	google.golang.org/grpc v1.64.0
)

require (
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server => ../server
