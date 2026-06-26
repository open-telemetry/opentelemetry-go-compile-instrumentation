module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/mongoclient

go 1.25.0

require go.mongodb.org/mongo-driver v1.17.9

require (
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => ../../../

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/go.mongodb.org/mongo-driver/mongo => ../../../instrumentation/go.mongodb.org/mongo-driver/mongo

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../../pkg

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime => ../../../pkg/runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation => ../../../instrumentation

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/runtime => ../../../instrumentation/runtime

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/client => ../../../instrumentation/net/http/client

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server => ../../../instrumentation/net/http/server
