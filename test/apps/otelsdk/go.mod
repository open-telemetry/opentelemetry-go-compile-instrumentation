module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/otelsdk

go 1.25.0

require (
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/dave/dst v0.27.3 // indirect
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.65.0 // indirect
	github.com/prometheus/otlptranslator v0.0.2 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opentelemetry.io/contrib/bridges/prometheus v0.63.0 // indirect
	go.opentelemetry.io/contrib/exporters/autoexport v0.63.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.64.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.14.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.14.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.60.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.14.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.38.0 // indirect
	go.opentelemetry.io/otel/log v0.14.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.14.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.1 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation v0.0.0-00010101000000-000000000000 // indirect
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/go.opentelemetry.io/otel/hook v0.0.0-00010101000000-000000000000
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/client v0.0.0-00010101000000-000000000000
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server v0.0.0-00010101000000-000000000000
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime v0.0.0-00010101000000-000000000000
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0-00010101000000-000000000000 // indirect
	github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => ../../../

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\pkg

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\pkg\runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\instrumentation

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\instrumentation\runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/go.opentelemetry.io/otel/hook => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\instrumentation\go.opentelemetry.io\otel\hook

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/client => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\instrumentation\net\http\client

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server => D:\opentelemetry-go-compile-instrumentation\test\apps\otelsdk\.otelc-build\instrumentation\net\http\server
