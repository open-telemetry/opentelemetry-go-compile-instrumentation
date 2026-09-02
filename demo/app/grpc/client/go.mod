module go.opentelemetry.io/otelc/demo/app/grpc/client

go 1.25.0

require (
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otelc/demo/app/grpc/server v0.0.0
	google.golang.org/grpc v1.82.0
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace go.opentelemetry.io/otelc/demo/app/grpc/server => ../server
