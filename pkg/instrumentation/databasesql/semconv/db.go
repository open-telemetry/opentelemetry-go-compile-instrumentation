package semconv

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type DatabaseSqlRequest struct {
	OpType     string
	Sql        string
	Endpoint   string
	DriverName string
	Dsn        string
	Params     []any
	DbName     string
}

func DbClientRequestTraceAttrs(req DatabaseSqlRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameMongoDB,
		semconv.DBOperationName(req.OpType),
		semconv.DBNamespace(req.DbName),
		semconv.NetworkPeerAddress(req.Endpoint),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Sql),
	}

	return attrs
}
