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

const (
	redisAuthCmd         = "auth"
	redisHelloCmd        = "hello"
	redisSetNameOption   = "setname"
	redisHelloAuthArgN   = 2
	redisQueryTextRedact = "?"
	// redisValueArgsStart is the index at which a command's data-value
	// arguments conventionally begin: args[0] is the command name, args[1]
	// is the key. See redisV9RedactRange.
	redisValueArgsStart = 2
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

func getRedisV9Statement(cmd redis.Cmder) string {
	args := cmd.Args()
	redactStart, redactEnd := redisV9RedactRange(cmd.Name(), args)

	b := make([]byte, 0, 64)
	for i, arg := range args {
		if i > 0 {
			b = append(b, ' ')
		}
		if i >= redactStart && i < redactEnd {
			b = append(b, redisQueryTextRedact...)
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

// redisV9RedactRange returns a half-open index range of args that must not
// appear in db.query.text.
//
// AUTH and HELLO get a narrow, credential-shaped rule: AUTH's arguments are
// credentials outright, and HELLO's AUTH username/password sub-arguments are
// too (SETNAME and the protocol version are not).
//
// Every other command redacts everything after args[1]. Unlike SQL, a Redis
// command's arguments are not a query template plus bindable parameters:
// SET's value, HSET's field/value pairs, LPUSH's elements, and so on are the
// application data being written, indistinguishable from the command shape
// without a per-command grammar. args[0] is always the command name and
// args[1] is conventionally the key, so this keeps "what command touched
// what key" visible while never emitting a value. It also over-redacts
// arguments that happen not to be sensitive (e.g. EXPIRE's TTL) and, for
// multi-key commands like MSET, redacts keys past the first as if they were
// values - both accepted trade-offs for never leaking one by default. See
// https://opentelemetry.io/docs/specs/semconv/db/redis/, which calls for
// query text to be sanitized or opt-in rather than captured by default.
func redisV9RedactRange(name string, args []interface{}) (start, end int) {
	switch name {
	case redisAuthCmd:
		if len(args) > 1 {
			return 1, len(args)
		}
	case redisHelloCmd:
		if i := redisV9HelloAuthIndex(args); i >= 0 {
			return i + 1, min(i+1+redisHelloAuthArgN, len(args))
		}
	default:
		if len(args) > redisValueArgsStart {
			return redisValueArgsStart, len(args)
		}
	}
	return 0, 0
}

func redisV9HelloAuthIndex(args []interface{}) int {
	for i := 1; i < len(args); i++ {
		if !redisV9ArgEqualFold(args[i], redisAuthCmd) {
			continue
		}
		// AUTH and SETNAME each take a fixed number of args in the HELLO
		// grammar, so a client literally named "auth" can only appear
		// directly after "setname" - a one-token lookback is enough.
		if i > 1 && redisV9ArgEqualFold(args[i-1], redisSetNameOption) {
			continue
		}
		return i
	}
	return -1
}

func redisV9ArgEqualFold(v interface{}, s string) bool {
	switch a := v.(type) {
	case string:
		return strings.EqualFold(a, s)
	case []byte:
		return strings.EqualFold(string(a), s)
	default:
		return false
	}
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
