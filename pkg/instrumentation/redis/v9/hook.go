package v9

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/redis/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"net"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"
)

var (
	logger   = shared.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/redis/v9", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("Redis v9 client instrumentation initialized")
	})
}

type otRedisHook struct {
	Addr string
}

func newOtRedisHook(addr string) *otRedisHook {
	return &otRedisHook{
		Addr: addr,
	}
}

func (o *otRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if !redisEnabler.Enable() {
			logger.Debug("Redis Client instrumentation disabled")
			return nil
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

		if err := next(ctx, cmd); err != nil && err != redis.Nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		return nil
	}
}

func (o *otRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if !redisEnabler.Enable() {
			logger.Debug("Redis Client instrumentation disabled")
			return nil
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

		if err := next(ctx, cmds); err != nil && err != redis.Nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		return nil
	}
}

func (o *otRedisHook) DialHook(next redis.DialHook) redis.DialHook {
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

	if err := cmd.Err(); err != nil && err != redis.Nil {
		b = append(b, ": "...)
		b = append(b, err.Error()...)
	}

	if cmd, ok := cmd.(*redis.Cmd); ok {
		b = append(b, ": "...)
		b = redisV9AppendArg(b, cmd.Name())
	}

	return redisV9String(b)
}

func redisV9String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func redisV9AppendUTF8String(dst []byte, src []byte) []byte {
	dst = append(dst, src...)
	return dst
}

func redisV9Bytes(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(
		&struct {
			string
			Cap int
		}{s, len(s)},
	))
}

func redisV9AppendArg(b []byte, v interface{}) []byte {
	switch v := v.(type) {
	case nil:
		return append(b, "<nil>"...)
	case string:
		bts := redisV9Bytes(v)
		if utf8.Valid(bts) {
			return redisV9AppendUTF8String(b, bts)
		} else {
			return redisV9AppendUTF8String(b, redisV9Bytes("<string>"))
		}
	case []byte:
		if utf8.Valid(v) {
			return redisV9AppendUTF8String(b, v)
		} else {
			return redisV9AppendUTF8String(b, redisV9Bytes("<byte>"))
		}
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
