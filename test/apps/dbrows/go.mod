module go.opentelemetry.io/otelc/test/apps/dbrows

go 1.26.0

replace go.opentelemetry.io/otelc/test/shared/testdb => ../../shared/testdb

require (
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otelc/test/shared/testdb v0.0.0-00010101000000-000000000000
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
)
