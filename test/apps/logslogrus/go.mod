module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/logslogrus

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/sirupsen/logrus => ../../../instrumentation/github.com/sirupsen/logrus

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../pkg

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime => ../../../pkg/runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation => ../../../instrumentation

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => ../../../

require (
	github.com/sirupsen/logrus v1.9.3
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime => ../../../instrumentation/runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server => ../../../instrumentation/net/http/server

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/client => ../../../instrumentation/net/http/client

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/log/slog => ../../../instrumentation/log/slog

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/log => ../../../instrumentation/log

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/go.opentelemetry.io/otel/hook => ../../../instrumentation/go.opentelemetry.io/otel/hook
