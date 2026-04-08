module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/logs/log

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../..

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared => ../../shared

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared v0.0.0
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/otel v1.40.0
)

require (
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
)
