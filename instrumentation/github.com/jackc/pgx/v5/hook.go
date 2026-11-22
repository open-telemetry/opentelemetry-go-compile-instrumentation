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
	minConnectParams       = 2
	minConnectConfigParams = 2
	minNewPoolConfigParams = 2
)

func isEnabled() bool {
	return runtime.Instrumented(instrumentationKey)
}

// BeforeConnect hooks pgx.connect(ctx, config)
func BeforeConnect(call hook.HookContext) {
	if !isEnabled() {
		return
	}

	if call.GetParamCount() < minConnectParams {
		return
	}
	cfg, ok := call.GetParam(1).(*pgx.ConnConfig)
	if !ok || cfg == nil {
		return
	}

	cfg.Tracer = chainTracer(cfg.Tracer, newTracer())
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
