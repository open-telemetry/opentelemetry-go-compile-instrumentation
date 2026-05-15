// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

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
	"google.golang.org/grpc/stats"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

func TestBeforeNewServer(t *testing.T) {
	tests := []struct {
		name          string
		opts          []grpc.ServerOption
		enabledEnv    bool
		expectHandler bool
	}{
		{
			name:          "no options",
			opts:          []grpc.ServerOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name: "with existing options",
			opts: []grpc.ServerOption{
				grpc.MaxRecvMsgSize(1024),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "instrumentation disabled",
			opts:          []grpc.ServerOption{},
			enabledEnv:    false,
			expectHandler: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := insttest.NewMockHookContext(tt.opts)
			BeforeNewServer(ictx, tt.opts...)

			newOpts, ok := ictx.GetParam(optionsParamIndex).([]grpc.ServerOption)
			require.True(t, ok)

			if tt.expectHandler {
				assert.Greater(t, len(newOpts), len(tt.opts))
			} else {
				assert.Equal(t, len(tt.opts), len(newOpts))
			}
		})
	}
}

func TestAfterNewServer(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		server     *grpc.Server
	}{
		{
			name:       "valid server with instrumentation enabled",
			enabledEnv: true,
			server:     grpc.NewServer(),
		},
		{
			name:       "nil server with instrumentation enabled",
			enabledEnv: true,
			server:     nil,
		},
		{
			name:       "valid server with instrumentation disabled",
			enabledEnv: false,
			server:     grpc.NewServer(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			if tt.server != nil {
				t.Cleanup(tt.server.Stop)
			}

			ictx := insttest.NewMockHookContext()
			assert.NotPanics(t, func() {
				AfterNewServer(ictx, tt.server)
			})
		})
	}
}

func TestServerStatsHandler_CreatesSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	handler := newServerStatsHandler()
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
