module go.opentelemetry.io/otelc/test/apps/grpcserverkafkaclient

go 1.25.0

replace go.opentelemetry.io/otelc/test/shared/grpcpb => ../../shared/grpcpb

require (
	github.com/segmentio/kafka-go v0.4.51
	go.opentelemetry.io/otelc/test/shared/grpcpb v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.83.2
)

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
