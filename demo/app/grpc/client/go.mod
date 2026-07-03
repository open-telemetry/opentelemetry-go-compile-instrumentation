module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/grpc/client

go 1.25.0

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/grpc/server v0.0.0
	github.com/stretchr/testify v1.11.1
	google.golang.org/grpc v1.82.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/grpc/server => ../server
