module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc

go 1.24.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../..

require (
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/otel v1.38.0
	google.golang.org/grpc v1.77.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251022142026-3a174f9686a8 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
