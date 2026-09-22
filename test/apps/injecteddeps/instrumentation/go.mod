module go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation

go 1.25.0

replace (
	go.opentelemetry.io/otelc/pkg => ../../../../pkg
	go.opentelemetry.io/otelc/pkg/runtime => ../../../../pkg/runtime
)

require go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000
