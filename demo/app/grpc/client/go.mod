module go.opentelemetry.io/otelc/demo/app/grpc/client

go 1.25.0

require (
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otelc/demo/app/grpc/server v0.0.0
	google.golang.org/grpc v1.84.0
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace go.opentelemetry.io/otelc/demo/app/grpc/server => ../server
