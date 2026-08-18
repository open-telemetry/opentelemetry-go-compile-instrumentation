// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/redis/go-redis/v9/semconv"
	"go.opentelemetry.io/otelc/pkg/runtime"
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
			span.RecordError(err)
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
			span.RecordError(err)
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

// redactedArg is what replaces a credential in db.query.text.
const redactedArg = "?"

// credentialArgs reports which argument positions carry credentials and must
// not reach db.query.text.
//
// A client configured with a password sends them on the connection handshake,
// before any user command runs, so this is not limited to applications that
// call AUTH themselves:
//
//	AUTH password                              (legacy)
//	AUTH username password                     (ACL, Redis 6+)
//	HELLO 3 AUTH username password [SETNAME c] (RESP3 handshake)
func credentialArgs(args []interface{}) map[int]bool {
	if len(args) == 0 {
		return nil
	}
	name, ok := args[0].(string)
	if !ok {
		return nil
	}

	switch strings.ToLower(name) {
	case "auth":
		// Everything after the command name is a credential, whether the
		// legacy one-argument form or the ACL username/password form.
		redacted := make(map[int]bool, len(args)-1)
		for i := 1; i < len(args); i++ {
			redacted[i] = true
		}
		return redacted

	case "hello":
		// The AUTH section is optional and follows the protocol version, so
		// find it rather than assuming a position.
		for i := 1; i < len(args); i++ {
			s, ok := args[i].(string)
			if !ok || !strings.EqualFold(s, "auth") {
				continue
			}
			// Exactly the username and password that follow, so a trailing
			// SETNAME clientname is still visible.
			redacted := make(map[int]bool, 2)
			for j := i + 1; j < len(args) && j <= i+2; j++ {
				redacted[j] = true
			}
			return redacted
		}
	}

	return nil
}

func getRedisV9Statement(cmd redis.Cmder) string {
	b := make([]byte, 0, 64)

	args := cmd.Args()
	redacted := credentialArgs(args)

	for i, arg := range args {
		if i > 0 {
			b = append(b, ' ')
		}
		if redacted[i] {
			b = append(b, redactedArg...)
			continue
		}
		b = redisV9AppendArg(b, arg)
	}

	if err := cmd.Err(); err != nil && !errors.Is(err, redis.Nil) {
		b = append(b, ": "...)
		b = append(b, err.Error()...)
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
