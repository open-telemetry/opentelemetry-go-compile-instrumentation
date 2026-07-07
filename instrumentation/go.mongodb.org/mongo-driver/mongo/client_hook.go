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

// BeforeConnect intercepts mongo.Connect and injects the OTel command monitor
func BeforeConnect(ictx hook.HookContext, ctx context.Context, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	monitor := otelmongo.NewMonitor()

	// If no options were provided, create a default options struct
	if len(opts) == 0 {
		opts = []*options.ClientOptions{
			options.Client(),
		}
	}

	// Inject monitor to all existing options
	for _, opt := range opts {
		if opt.Monitor == nil {
			opt.SetMonitor(monitor)
		}
	}

	// Explicitly set parameter to ensure otelc compiles and applies it
	ictx.SetParam(1, opts)
}

// BeforeNewClient intercepts mongo.NewClient and injects the OTel command monitor
func BeforeNewClient(ictx hook.HookContext, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	monitor := otelmongo.NewMonitor()

	// If no options were provided, create a default options struct
	if len(opts) == 0 {
		opts = []*options.ClientOptions{
			options.Client(),
		}
	}

	// Inject monitor to all existing options
	for _, opt := range opts {
		if opt.Monitor == nil {
			opt.SetMonitor(monitor)
		}
	}

	// Explicitly set parameter to ensure otelc compiles and applies it
	ictx.SetParam(0, opts)
}
