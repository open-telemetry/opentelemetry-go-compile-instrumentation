module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/basic

go 1.25.0

toolchain go1.26.1

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../..

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0-20260306084614-87d3b37a105b
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.42.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.42.0
	go.opentelemetry.io/otel/sdk v1.42.0
	go.opentelemetry.io/otel/sdk/metric v1.42.0
	go.opentelemetry.io/otel/trace v1.42.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)
