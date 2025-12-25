module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/helloworld

go 1.24.0

toolchain go1.24.2

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../..

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0-20251208011108-ac0fa4a155e3
	go.opentelemetry.io/otel v1.39.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.38.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
	go.opentelemetry.io/otel/sdk/metric v1.38.0
	go.opentelemetry.io/otel/trace v1.39.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.39.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
)
