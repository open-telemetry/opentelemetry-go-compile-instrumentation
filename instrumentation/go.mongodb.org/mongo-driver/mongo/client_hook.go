// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"context"

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

// injectMonitor appends the OTel CommandMonitor as a trailing ClientOptions
// element when no monitor is already present anywhere in opts.
//
// mongo.NewClient resolves opts via options.MergeClientOptions, which folds the
// slice into one struct by taking the last non-nil value of each field, in slice
// order. Injecting by mutating an existing element based on its own (per-struct)
// Monitor field can land the OTel monitor on a struct that isn't last, letting it
// win the merge over a user's CommandMonitor set on an earlier struct. Appending a
// new trailing element instead only ever adds a monitor when the merged, effective
// Monitor is nil, and — being last — can never be overridden by, or override,
// anything the caller already passed in.
func injectMonitor(opts []*options.ClientOptions) []*options.ClientOptions {
	if options.MergeClientOptions(opts...).Monitor != nil {
		return opts
	}
	// Full slice expression forces a new backing array so this never mutates a
	// caller-owned slice passed in via `opts...`.
	return append(opts[:len(opts):len(opts)], options.Client().SetMonitor(otelmongo.NewMonitor()))
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
