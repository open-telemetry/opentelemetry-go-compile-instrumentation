module go.opentelemetry.io/otelc/demo/app/basic

go 1.25.0

replace (
	go.opentelemetry.io/otelc/demo/app/basic/instrumentation => ./instrumentation
	go.opentelemetry.io/otelc/instrumentation/runtime => ../../../instrumentation/runtime
	go.opentelemetry.io/otelc/pkg => ../../../pkg
)

require (
	go.opentelemetry.io/otelc/demo/app/basic/instrumentation v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/runtime v0.0.0-00010101000000-000000000000
	golang.org/x/time v0.14.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 // indirect
	golang.org/x/sys v0.45.0 // indirect
)
