// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "MONGODB"
)

type mongoEnabler struct{}

func (g mongoEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = mongoEnabler{}

// chainMonitors returns a CommandMonitor that invokes each non-nil callback of
// both user and otel in turn (user first), so a caller's own CommandMonitor
// keeps firing alongside the OTel one instead of being replaced by it.
//
// Hardcodes the three CommandMonitor callbacks (Started/Succeeded/Failed). If a
// future mongo-driver release adds another callback field, it must be added here
// too, or it will be silently dropped from the chain.
func chainMonitors(userMonitor, otel *event.CommandMonitor) *event.CommandMonitor {
	return &event.CommandMonitor{
		Started: func(ctx context.Context, e *event.CommandStartedEvent) {
			if userMonitor.Started != nil {
				userMonitor.Started(ctx, e)
			}
			if otel.Started != nil {
				otel.Started(ctx, e)
			}
		},
		Succeeded: func(ctx context.Context, e *event.CommandSucceededEvent) {
			if userMonitor.Succeeded != nil {
				userMonitor.Succeeded(ctx, e)
			}
			if otel.Succeeded != nil {
				otel.Succeeded(ctx, e)
			}
		},
		Failed: func(ctx context.Context, e *event.CommandFailedEvent) {
			if userMonitor.Failed != nil {
				userMonitor.Failed(ctx, e)
			}
			if otel.Failed != nil {
				otel.Failed(ctx, e)
			}
		},
	}
}

// injectMonitor appends a trailing ClientOptions element carrying the OTel
// CommandMonitor, chained with any monitor the caller already configured so
// both keep firing.
//
// mongo.NewClient resolves opts via options.MergeClientOptions, which folds the
// slice into one struct by taking the last non-nil value of each field, in slice
// order. Injecting by mutating an existing element based on its own (per-struct)
// Monitor field can land the OTel monitor on a struct that isn't last, letting it
// win the merge over a user's CommandMonitor set on an earlier struct — replacing
// it rather than combining with it. Appending a new trailing element instead
// always determines the effective monitor from the merged result, and — being
// last — can never be overridden by, or silently override, anything the caller
// already passed in.
func injectMonitor(opts []*options.ClientOptions) []*options.ClientOptions {
	merged := options.MergeClientOptions(opts...)
	otelMonitor := otelmongo.NewMonitor()
	monitor := otelMonitor
	if merged.Monitor != nil {
		monitor = chainMonitors(merged.Monitor, otelMonitor)
	}
	// Set only Monitor, and carry the caller's effective HTTPClient forward, so the
	// appended element is a no-op for every field except Monitor. Using
	// options.Client() here would inject its default HTTPClient which, being last in
	// the merge, silently replaces a caller's custom one.
	injected := &options.ClientOptions{Monitor: monitor, HTTPClient: merged.HTTPClient}
	// Full slice expression forces a new backing array so this never mutates a
	// caller-owned slice passed in via `opts...`.
	return append(opts[:len(opts):len(opts)], injected)
}

// BeforeConnect intercepts mongo.Connect and injects the OTel command monitor
func BeforeConnect(ictx hook.HookContext, ctx context.Context, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	// Explicitly set parameter to ensure otelc compiles and applies it
	ictx.SetParam(1, injectMonitor(opts))
}

// BeforeNewClient intercepts mongo.NewClient and injects the OTel command monitor
func BeforeNewClient(ictx hook.HookContext, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	// Explicitly set parameter to ensure otelc compiles and applies it
	ictx.SetParam(0, injectMonitor(opts))
}
