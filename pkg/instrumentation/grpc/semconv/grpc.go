// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package semconv provides gRPC semantic convention utilities
package semconv

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc/status"
)

const (
	// OTELExporterTracePath is the gRPC method for OTLP trace export
	OTELExporterTracePath = "/opentelemetry.proto.collector.trace.v1.TraceService/Export"
	// OTELExporterMetricPath is the gRPC method for OTLP metric export
	OTELExporterMetricPath = "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export"
	// OTELExporterLogPath is the gRPC method for OTLP log export
	OTELExporterLogPath = "/opentelemetry.proto.collector.logs.v1.LogsService/Export"
)

// ParseFullMethod returns a span name and attributes based on a gRPC's FullMethod.
// Parsing is consistent with grpc-go implementation.
// Format: /package.service/method
func ParseFullMethod(fullMethod string) (string, []attribute.KeyValue) {
	if !strings.HasPrefix(fullMethod, "/") {
		// Invalid format, does not follow `/package.service/method`.
		return fullMethod, []attribute.KeyValue{semconv.RPCSystemGRPC}
	}
	name := fullMethod[1:]
	pos := strings.LastIndex(name, "/")
	if pos < 0 {
		// Invalid format, does not follow `/package.service/method`.
		return name, []attribute.KeyValue{semconv.RPCSystemGRPC}
	}
	service, method := name[:pos], name[pos+1:]

	attrs := []attribute.KeyValue{semconv.RPCSystemGRPC}
	if service != "" {
		attrs = append(attrs, semconv.RPCService(service))
	}
	if method != "" {
		attrs = append(attrs, semconv.RPCMethod(method))
	}
	return name, attrs
}

// GRPCStatusCodeAttr returns the RPC status code attribute
func GRPCStatusCodeAttr(code int) attribute.KeyValue {
	return semconv.RPCGRPCStatusCodeKey.Int(code)
}

// ServerStatus returns the appropriate span status based on gRPC status code
func ServerStatus(s *status.Status) (codes.Code, string) {
	// For servers, only codes.Unknown, codes.DeadlineExceeded,
	// codes.Unimplemented, codes.Internal, codes.Unavailable,
	// and codes.DataLoss are errors.
	switch s.Code() {
	case 0: // codes.OK
		return codes.Unset, ""
	case 1: // codes.Canceled
		return codes.Unset, ""
	case 2: // codes.Unknown
		return codes.Error, s.Message()
	case 3: // codes.InvalidArgument
		return codes.Unset, ""
	case 4: // codes.DeadlineExceeded
		return codes.Error, s.Message()
	case 5: // codes.NotFound
		return codes.Unset, ""
	case 6: // codes.AlreadyExists
		return codes.Unset, ""
	case 7: // codes.PermissionDenied
		return codes.Unset, ""
	case 8: // codes.ResourceExhausted
		return codes.Unset, ""
	case 9: // codes.FailedPrecondition
		return codes.Unset, ""
	case 10: // codes.Aborted
		return codes.Unset, ""
	case 11: // codes.OutOfRange
		return codes.Unset, ""
	case 12: // codes.Unimplemented
		return codes.Error, s.Message()
	case 13: // codes.Internal
		return codes.Error, s.Message()
	case 14: // codes.Unavailable
		return codes.Error, s.Message()
	case 15: // codes.DataLoss
		return codes.Error, s.Message()
	case 16: // codes.Unauthenticated
		return codes.Unset, ""
	default:
		return codes.Error, s.Message()
	}
}

// ClientStatus returns the appropriate span status for client
func ClientStatus(s *status.Status) (codes.Code, string) {
	// For clients, all non-OK codes are errors
	if s.Code() == 0 { // codes.OK
		return codes.Unset, ""
	}
	return codes.Error, s.Message()
}

// ServerAddrAttrs extracts server address and port attributes from peer address
func ServerAddrAttrs(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	var attrs []attribute.KeyValue
	if host != "" {
		attrs = append(attrs, semconv.ServerAddress(host))
	}
	if port > 0 {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	return attrs
}

// ClientAddrAttrs extracts client address and port attributes from peer address
func ClientAddrAttrs(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	var attrs []attribute.KeyValue
	if host != "" {
		attrs = append(attrs, semconv.ClientAddress(host))
	}
	if port > 0 {
		attrs = append(attrs, semconv.ClientPort(port))
	}
	return attrs
}

// IsOTELExporterPath returns true if the method is an OpenTelemetry exporter endpoint.
// These methods should be excluded from instrumentation to prevent infinite recursion.
func IsOTELExporterPath(fullMethod string) bool {
	return fullMethod == OTELExporterTracePath ||
		fullMethod == OTELExporterMetricPath ||
		fullMethod == OTELExporterLogPath
}
