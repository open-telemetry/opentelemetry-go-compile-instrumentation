module go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel

go 1.25.0

replace go.opentelemetry.io/otelc/pkg => ../../../pkg

require (
	go.opentelemetry.io/otel/trace v1.44.0
	go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
)
