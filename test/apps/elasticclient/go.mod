module go.opentelemetry.io/otelc/test/apps/elasticclient

go 1.25.0

require (
	github.com/olivere/elastic/v7 v7.0.32
	go.opentelemetry.io/otelc/instrumentation/github.com/olivere/elastic/v7 v7.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/init v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/log v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/log/slog v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/net/http/client v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/net/http/server v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otelc/instrumentation/runtime v0.0.0-00010101000000-000000000000
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dave/dst v0.27.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.24.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/otlptranslator v1.0.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/urfave/cli/v3 v3.10.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/bridges/prometheus v0.71.0 // indirect
	go.opentelemetry.io/contrib/exporters/autoexport v0.71.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.71.0 // indirect
	go.opentelemetry.io/contrib/propagators/autoprop v0.71.0 // indirect
	go.opentelemetry.io/contrib/propagators/aws v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/b3 v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/jaeger v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/ot v1.46.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.46.0 // indirect
	go.opentelemetry.io/otel/log v0.22.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.22.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.opentelemetry.io/otelc/instrumentation v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/otelc/pkg/runtime v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260825221802-da73d73af1c5 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260825221802-da73d73af1c5 // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.opentelemetry.io/otelc v1.1.0 // indirect
)

tool go.opentelemetry.io/otelc/tool/cmd/otelc

replace go.opentelemetry.io/otelc/instrumentation => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation

replace go.opentelemetry.io/otelc/instrumentation/database/sql => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/database/sql

replace go.opentelemetry.io/otelc/instrumentation/github.com/anthropics/anthropic-sdk-go => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/anthropics/anthropic-sdk-go

replace go.opentelemetry.io/otelc/instrumentation/github.com/apache/cassandra-gocql-driver/v2 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/apache/cassandra-gocql-driver/v2

replace go.opentelemetry.io/otelc/instrumentation/github.com/aws/aws-sdk-go-v2 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/aws/aws-sdk-go-v2

replace go.opentelemetry.io/otelc/instrumentation/github.com/gin-gonic/gin => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/gin-gonic/gin

replace go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/linode/linodego/v2

replace go.opentelemetry.io/otelc/instrumentation/github.com/olivere/elastic/v7 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/olivere/elastic/v7

replace go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/openai/openai-go

replace go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/internal/streaming => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/openai/openai-go/internal/streaming

replace go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/v2 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/openai/openai-go/v2

replace go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/v3 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/openai/openai-go/v3

replace go.opentelemetry.io/otelc/instrumentation/github.com/rabbitmq/amqp091-go => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/rabbitmq/amqp091-go

replace go.opentelemetry.io/otelc/instrumentation/github.com/redis/go-redis/v9 => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/redis/go-redis/v9

replace go.opentelemetry.io/otelc/instrumentation/github.com/segmentio/kafka-go/consumer => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/segmentio/kafka-go/consumer

replace go.opentelemetry.io/otelc/instrumentation/github.com/segmentio/kafka-go/producer => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/segmentio/kafka-go/producer

replace go.opentelemetry.io/otelc/instrumentation/github.com/sirupsen/logrus => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/github.com/sirupsen/logrus

replace go.opentelemetry.io/otelc/instrumentation/go.mongodb.org/mongo-driver/mongo => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.mongodb.org/mongo-driver/mongo

replace go.opentelemetry.io/otelc/instrumentation/go.mongodb.org/mongo-driver/v2/mongo => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.mongodb.org/mongo-driver/v2/mongo

replace go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.opentelemetry.io/otel

replace go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/init => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.opentelemetry.io/otel/init

replace go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/sdk/trace => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.opentelemetry.io/otel/sdk/trace

replace go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/trace => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.opentelemetry.io/otel/trace

replace go.opentelemetry.io/otelc/instrumentation/go.uber.org/zap => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/go.uber.org/zap

replace go.opentelemetry.io/otelc/instrumentation/google.golang.org/grpc/client => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/google.golang.org/grpc/client

replace go.opentelemetry.io/otelc/instrumentation/google.golang.org/grpc/server => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/google.golang.org/grpc/server

replace go.opentelemetry.io/otelc/instrumentation/k8s.io/client-go => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/k8s.io/client-go

replace go.opentelemetry.io/otelc/instrumentation/log => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/log

replace go.opentelemetry.io/otelc/instrumentation/log/slog => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/log/slog

replace go.opentelemetry.io/otelc/instrumentation/net/http/client => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/net/http/client

replace go.opentelemetry.io/otelc/instrumentation/net/http/server => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/net/http/server

replace go.opentelemetry.io/otelc/instrumentation/runtime => /Users/adityavishe/Developer/otelc-pr812-current/instrumentation/runtime

replace go.opentelemetry.io/otelc/pkg => /Users/adityavishe/Developer/otelc-pr812-current/pkg

replace go.opentelemetry.io/otelc/pkg/runtime => /Users/adityavishe/Developer/otelc-pr812-current/pkg/runtime
