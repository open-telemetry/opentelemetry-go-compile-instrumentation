// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

func TestBeforeNewClient(t *testing.T) {
	tests := []struct {
		name                    string
		target                  string
		opts                    []grpc.DialOption
		enabledEnv              bool
		expectHandler           bool
		oltpExporterEndpointKey string
	}{
		{
			name:          "no options",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:   "with existing options",
			target: "localhost:50051",
			opts: []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "instrumentation disabled",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    false,
			expectHandler: false,
		},
		{
			name:          "nil options slice",
			target:        "localhost:50051",
			opts:          nil,
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "empty target with options",
			target:        "",
			opts:          []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:                    "oltp exporter endpoint target",
			target:                  "localhost:4317",
			opts:                    []grpc.DialOption{},
			enabledEnv:              true,
			expectHandler:           false,
			oltpExporterEndpointKey: "OTEL_EXPORTER_OTLP_ENDPOINT",
		},
		{
			name:                    "oltp exporter traces endpoint target",
			target:                  "localhost:4317",
			opts:                    []grpc.DialOption{},
			enabledEnv:              true,
			expectHandler:           false,
			oltpExporterEndpointKey: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			if tt.oltpExporterEndpointKey != "" {
				t.Setenv(tt.oltpExporterEndpointKey, tt.target)
			}

			ictx := insttest.NewMockHookContext(tt.target, tt.opts)

			assert.NotPanics(t, func() {
				BeforeNewClient(ictx, tt.target, tt.opts...)
			})

			newOpts, ok := ictx.GetParam(newClientOptionsParamIndex).([]grpc.DialOption)
			require.True(t, ok)

			if tt.expectHandler {
				assert.Greater(t, len(newOpts), len(tt.opts))
			} else {
				assert.Equal(t, len(tt.opts), len(newOpts))
			}
		})
	}
}

func TestAfterNewClient(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		conn       *grpc.ClientConn
		err        error
	}{
		{
			name:       "successful connection with instrumentation enabled",
			enabledEnv: true,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation enabled",
			enabledEnv: true,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "successful connection with instrumentation disabled",
			enabledEnv: false,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation disabled",
			enabledEnv: false,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "both nil conn and nil error",
			enabledEnv: true,
			conn:       nil,
			err:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := insttest.NewMockHookContext()
			assert.NotPanics(t, func() {
				AfterNewClient(ictx, tt.conn, tt.err)
			})
		})
	}
}

func TestBeforeDialContext(t *testing.T) {
	tests := []struct {
		name                    string
		target                  string
		opts                    []grpc.DialOption
		enabledEnv              bool
		expectHandler           bool
		oltpExporterEndpointKey string
	}{
		{
			name:          "no options",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:   "with existing options",
			target: "localhost:50051",
			opts: []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "instrumentation disabled",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    false,
			expectHandler: false,
		},
		{
			name:                    "oltp exporter endpoint target",
			target:                  "localhost:4317",
			opts:                    []grpc.DialOption{},
			enabledEnv:              true,
			expectHandler:           false,
			oltpExporterEndpointKey: "OTEL_EXPORTER_OTLP_ENDPOINT",
		},
		{
			name:                    "oltp exporter traces endpoint target",
			target:                  "localhost:4317",
			opts:                    []grpc.DialOption{},
			enabledEnv:              true,
			expectHandler:           false,
			oltpExporterEndpointKey: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			if tt.oltpExporterEndpointKey != "" {
				t.Setenv(tt.oltpExporterEndpointKey, tt.target)
			}

			ctx := t.Context()
			ictx := insttest.NewMockHookContext(ctx, tt.target, tt.opts)
			BeforeDialContext(ictx, ctx, tt.target, tt.opts...)

			newOpts, ok := ictx.GetParam(dialOptionsParamIndex).([]grpc.DialOption)
			require.True(t, ok)

			if tt.expectHandler {
				assert.Greater(t, len(newOpts), len(tt.opts))
			} else {
				assert.Equal(t, len(tt.opts), len(newOpts))
			}
		})
	}
}

func TestAfterDialContext(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		conn       *grpc.ClientConn
		err        error
	}{
		{
			name:       "successful connection with instrumentation enabled",
			enabledEnv: true,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation enabled",
			enabledEnv: true,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "successful connection with instrumentation disabled",
			enabledEnv: false,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation disabled",
			enabledEnv: false,
			conn:       nil,
			err:        assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := insttest.NewMockHookContext()
			assert.NotPanics(t, func() {
				AfterDialContext(ictx, tt.conn, tt.err)
			})
		})
	}
}

func TestClientStatsHandler_CreatesSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	handler := newClientStatsHandler()
	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{FullMethodName: "/grpc.testing.TestService/UnaryCall"})
	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-100 * time.Millisecond),
		EndTime:   time.Now(),
	})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "grpc.testing.TestService/UnaryCall", spans[0].Name)
}

func TestRecordRPC(t *testing.T) {
	tests := []struct {
		name           string
		fullMethodName string
		want           bool
	}{
		{
			name:           "regular RPC",
			fullMethodName: "/grpc.testing.TestService/UnaryCall",
			want:           true,
		},
		{
			name:           "OTLP trace exporter",
			fullMethodName: "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
			want:           false,
		},
		{
			name:           "OTLP metrics exporter",
			fullMethodName: "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export",
			want:           false,
		},
		{
			name:           "OTLP logs exporter",
			fullMethodName: "/opentelemetry.proto.collector.logs.v1.LogsService/Export",
			want:           false,
		},
		{
			name: "nil info",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info *stats.RPCTagInfo
			if tt.fullMethodName != "" {
				info = &stats.RPCTagInfo{FullMethodName: tt.fullMethodName}
			}
			assert.Equal(t, tt.want, recordRPC(info))
		})
	}
}
