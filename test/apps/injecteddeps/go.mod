module go.opentelemetry.io/otelc/test/apps/injecteddeps

go 1.25.0

replace (
	go.opentelemetry.io/otelc/pkg => ../../../pkg
	go.opentelemetry.io/otelc/pkg/runtime => ../../../pkg/runtime
	go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation => ./instrumentation
)

require go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation v0.0.0-00010101000000-000000000000

require go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 // indirect
