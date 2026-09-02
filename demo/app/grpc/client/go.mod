module go.opentelemetry.io/otelc/demo/app/grpc/client

go 1.25.0

require (
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/otelc/demo/app/grpc/server v0.0.0
	google.golang.org/grpc v1.83.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace go.opentelemetry.io/otelc/demo/app/grpc/server => ../server
