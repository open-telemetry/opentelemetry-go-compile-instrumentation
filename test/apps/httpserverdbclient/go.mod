module go.opentelemetry.io/otelc/test/apps/httpserverdbclient

go 1.25.0

replace go.opentelemetry.io/otelc/test/shared/testdb => ../../shared/testdb

require (
	go.opentelemetry.io/otel/trace v1.45.0
	go.opentelemetry.io/otelc/test/shared/testdb v0.0.0-00010101000000-000000000000
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
)
