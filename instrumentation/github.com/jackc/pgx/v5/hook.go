// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

type pgxEnabler struct{}

func (pgxEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var (
	enabler    = pgxEnabler{}
	logger     = runtime.Logger()
	otelTracer *tracer
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		otelTracer = newTracer()
		logger.Info("pgx v5 client instrumentation initialized")
	})
}

// BeforeConnectConfig hooks pgx.ConnectConfig(ctx, config)
func BeforeConnectConfig(call hook.HookContext) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	if call.GetParamCount() < 2 {
		return
	}
	cfg, ok := call.GetParam(1).(*pgx.ConnConfig)
	if !ok || cfg == nil {
		return
	}

	cfg.Tracer = chainTracer(cfg.Tracer, otelTracer).(pgx.QueryTracer)
}

// AfterConnect hooks pgx.Connect(ctx, connString)
func AfterConnect(call hook.HookContext) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	if call.GetReturnValCount() < 1 {
		return
	}
	conn, ok := call.GetReturnVal(0).(*pgx.Conn)
	if !ok || conn == nil || conn.Config() == nil {
		return
	}

	conn.Config().Tracer = chainTracer(conn.Config().Tracer, otelTracer).(pgx.QueryTracer)
}

// BeforeNewWithConfig hooks pgxpool.NewWithConfig(ctx, config)
func BeforeNewWithConfig(call hook.HookContext) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	if call.GetParamCount() < 2 {
		return
	}
	cfg, ok := call.GetParam(1).(*pgxpool.Config)
	if !ok || cfg == nil || cfg.ConnConfig == nil {
		return
	}

	cfg.ConnConfig.Tracer = chainTracer(cfg.ConnConfig.Tracer, otelTracer).(pgx.QueryTracer)
}

// AfterNewPool hooks pgxpool.New(ctx, connString)
func AfterNewPool(call hook.HookContext) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	if call.GetReturnValCount() < 1 {
		return
	}
	pool, ok := call.GetReturnVal(0).(*pgxpool.Pool)
	if !ok || pool == nil || pool.Config() == nil || pool.Config().ConnConfig == nil {
		return
	}

	pool.Config().ConnConfig.Tracer = chainTracer(pool.Config().ConnConfig.Tracer, otelTracer).(pgx.QueryTracer)
}

