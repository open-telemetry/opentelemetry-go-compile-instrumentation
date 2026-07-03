// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/redis/go-redis/v9/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		logger.Info("Redis v9 client instrumentation initialized")
	})
}

type otelRedisHook struct {
	Addr string
}

func newOtelRedisHook(addr string) *otelRedisHook {
	return &otelRedisHook{
		Addr: addr,
	}
}

func (o *otelRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if !redisEnabler.Enable() {
			logger.Debug("Redis Client instrumentation disabled")
			return next(ctx, cmd)
		}
		initInstrumentation()
		fullName := cmd.FullName()
		request := semconv.RedisRequest{
			Endpoint:  o.Addr,
			FullName:  fullName,
			Statement: getRedisV9Statement(cmd),
		}
		// Get trace attributes from semconv
		attrs := semconv.RedisClientRequestTraceAttrs(request)

		// Start span
		spanName := request.FullName
		ctx, span := tracer.Start(ctx,
			spanName,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		err := next(ctx, cmd)
		if err != nil && !errors.Is(err, redis.Nil) {
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}

func (o *otelRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if !redisEnabler.Enable() {
			logger.Debug("Redis Client instrumentation disabled")
			return next(ctx, cmds)
		}
		initInstrumentation()

		summary := ""
		summaryCmds := cmds
		if len(summaryCmds) > 10 {
			summaryCmds = summaryCmds[:10]
		}
		for i := range summaryCmds {
			summary += summaryCmds[i].FullName() + "/"
		}
		if len(cmds) > 10 {
			summary += "..."
		}
		cmd := redis.NewCmd(ctx, "pipeline", summary)
		fullName := cmd.FullName()
		request := semconv.RedisRequest{
			Endpoint:  o.Addr,
			FullName:  fullName,
			Statement: getRedisV9Statement(cmd),
		}

		// Get trace attributes from semconv
		attrs := semconv.RedisClientRequestTraceAttrs(request)

		// Start span
		spanName := request.FullName
		ctx, span := tracer.Start(ctx,
			spanName,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		err := next(ctx, cmds)
		if err != nil && !errors.Is(err, redis.Nil) {
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}

func (o *otelRedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := next(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return conn, err
	}
}

func getRedisV9Statement(cmd redis.Cmder) string {
	b := make([]byte, 0, 64)

	for i, arg := range cmd.Args() {
		if i > 0 {
			b = append(b, ' ')
		}
		b = redisV9AppendArg(b, arg)
	}

	if err := cmd.Err(); err != nil && !errors.Is(err, redis.Nil) {
		b = append(b, ": "...)
		b = append(b, err.Error()...)
	}

	if cmd, ok := cmd.(*redis.Cmd); ok {
		b = append(b, ": "...)
		b = redisV9AppendArg(b, cmd.Name())
	}

	return string(b)
}

func redisV9AppendArg(b []byte, v interface{}) []byte {
	switch v := v.(type) {
	case nil:
		return append(b, "<nil>"...)
	case string:
		if utf8.ValidString(v) {
			return append(b, v...)
		}
		return append(b, "<string>"...)
	case []byte:
		if utf8.Valid(v) {
			return append(b, v...)
		}
		return append(b, "<byte>"...)
	case int:
		return strconv.AppendInt(b, int64(v), 10)
	case int8:
		return strconv.AppendInt(b, int64(v), 10)
	case int16:
		return strconv.AppendInt(b, int64(v), 10)
	case int32:
		return strconv.AppendInt(b, int64(v), 10)
	case int64:
		return strconv.AppendInt(b, v, 10)
	case uint:
		return strconv.AppendUint(b, uint64(v), 10)
	case uint8:
		return strconv.AppendUint(b, uint64(v), 10)
	case uint16:
		return strconv.AppendUint(b, uint64(v), 10)
	case uint32:
		return strconv.AppendUint(b, uint64(v), 10)
	case uint64:
		return strconv.AppendUint(b, v, 10)
	case float32:
		return strconv.AppendFloat(b, float64(v), 'f', -1, 64)
	case float64:
		return strconv.AppendFloat(b, v, 'f', -1, 64)
	case bool:
		if v {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case time.Time:
		return v.AppendFormat(b, time.RFC3339Nano)
	default:
		return append(b, "not_support_type"...)
	}
}
