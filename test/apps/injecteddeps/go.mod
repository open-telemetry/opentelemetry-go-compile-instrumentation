module go.opentelemetry.io/otelc/test/apps/injecteddeps

go 1.26.0

replace (
	go.opentelemetry.io/otelc/pkg => ../../../pkg
	go.opentelemetry.io/otelc/pkg/runtime => ../../../pkg/runtime
	go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation => ./instrumentation
)

require go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation v0.0.0-00010101000000-000000000000

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/dave/dst v0.28.0 // indirect
	github.com/gofrs/flock v0.13.1 // indirect
	github.com/urfave/cli/v3 v3.12.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
)

require (
	go.opentelemetry.io/otelc v1.1.0 // indirect
	go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

tool go.opentelemetry.io/otelc/tool/cmd/otelc

replace go.opentelemetry.io/otelc => ../../..
