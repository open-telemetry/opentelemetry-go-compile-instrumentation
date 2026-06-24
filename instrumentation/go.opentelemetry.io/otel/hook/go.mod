module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/go.opentelemetry.io/otel/hook

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../../pkg

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel/trace v1.42.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
)
