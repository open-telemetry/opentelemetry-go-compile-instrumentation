// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	minConnectConfigParams = 2
	minConnectReturnVals   = 1
	minNewPoolConfigParams = 2
	minNewPoolReturnVals   = 1
)

func isEnabled() bool {
	return runtime.Instrumented(instrumentationKey)
}

// BeforeConnectConfig hooks pgx.ConnectConfig(ctx, config)
func BeforeConnectConfig(call hook.HookContext) {
	if !isEnabled() {
		return
	}

	if call.GetParamCount() < minConnectConfigParams {
		return
	}
	cfg, ok := call.GetParam(1).(*pgx.ConnConfig)
	if !ok || cfg == nil {
		return
	}

	cfg.Tracer = chainTracer(cfg.Tracer, newTracer())
}

// AfterConnect hooks pgx.Connect(ctx, connString)
func AfterConnect(call hook.HookContext) {
	if !isEnabled() {
		return
	}

	if call.GetReturnValCount() < minConnectReturnVals {
		return
	}
	conn, ok := call.GetReturnVal(0).(*pgx.Conn)
	if !ok || conn == nil || conn.Config() == nil {
		return
	}

	conn.Config().Tracer = chainTracer(conn.Config().Tracer, newTracer())
}

// BeforeNewWithConfig hooks pgxpool.NewWithConfig(ctx, config)
func BeforeNewWithConfig(call hook.HookContext) {
	if !isEnabled() {
		return
	}

	if call.GetParamCount() < minNewPoolConfigParams {
		return
	}
	cfg, ok := call.GetParam(1).(*pgxpool.Config)
	if !ok || cfg == nil || cfg.ConnConfig == nil {
		return
	}

	cfg.ConnConfig.Tracer = chainTracer(cfg.ConnConfig.Tracer, newTracer())
}

// AfterNewPool hooks pgxpool.New(ctx, connString)
func AfterNewPool(call hook.HookContext) {
	if !isEnabled() {
		return
	}

	if call.GetReturnValCount() < minNewPoolReturnVals {
		return
	}
	pool, ok := call.GetReturnVal(0).(*pgxpool.Pool)
	if !ok || pool == nil || pool.Config() == nil || pool.Config().ConnConfig == nil {
		return
	}

	pool.Config().ConnConfig.Tracer = chainTracer(pool.Config().ConnConfig.Tracer, newTracer())
}
