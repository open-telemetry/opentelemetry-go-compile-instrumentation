module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/httpserver

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => ../../../

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation => ../../../instrumentation

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/client => ../../../instrumentation/net/http/client

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server => ../../../instrumentation/net/http/server

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime => ../../../instrumentation/runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../pkg

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime => ../../../pkg/runtime
