module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/basic

go 1.25.0

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation v0.5.0
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/basic/instrumentation v0.0.0
	golang.org/x/time v0.14.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dave/dst v0.27.3 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0-00010101000000-000000000000 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/app/basic/instrumentation => ./instrumentation

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => ../../../

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../pkg

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime => ../../../instrumentation/runtime
