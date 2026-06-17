// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
)

const (
	instrumentationKey = "MONGODB"
)

type mongoEnabler struct{}

func (g mongoEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var (
	enabler  = mongoEnabler{}
	initOnce sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		version := "dev"
		if ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}

		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/mongodb", version); err != nil {
			shared.Logger().Error("failed to setup OTel SDK", "error", err)
		}
	})
}

// BeforeConnect intercepts mongo.Connect and injects the OTel command monitor
func BeforeConnect(ictx inst.HookContext, ctx context.Context, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	initInstrumentation()

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
func BeforeNewClient(ictx inst.HookContext, opts ...*options.ClientOptions) {
	if !enabler.Enable() {
		return
	}

	initInstrumentation()

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
