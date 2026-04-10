// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package openai contains the compile-time hooks that instrument
// github.com/openai/openai-go v1.x. The hooks inject
// option.WithMiddleware(openaishared.OtelMiddleware) into client construction
// so that every HTTP call the SDK makes flows through our OpenTelemetry
// middleware. All span/metric logic lives in the version-independent
// openai/shared package — this file is a thin adapter that bridges v1's
// option.RequestOption type to the shared middleware.
package openai

import (
	"github.com/openai/openai-go/option"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	openaishared "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai/shared"
)

// optsParamIndex is the index of the variadic opts slice in the target
// functions (openai.NewClient and openai.NewChatCompletionService). Both take
// opts as their only parameter.
const optsParamIndex = 0

// withOtelMiddleware prepends the OTel middleware option to the given opts
// slice so it wraps any user-supplied middleware and runs first on the way
// out / last on the way in — the correct position for accurate span timing.
func withOtelMiddleware(opts []option.RequestOption) []option.RequestOption {
	// option.Middleware in v1 is a type alias to
	// func(*http.Request, MiddlewareNext) (*http.Response, error), which is
	// exactly openaishared.OtelMiddleware's signature, so no conversion is
	// needed.
	newOpts := make([]option.RequestOption, 0, len(opts)+1)
	newOpts = append(newOpts, option.WithMiddleware(openaishared.OtelMiddleware))
	newOpts = append(newOpts, opts...)
	return newOpts
}

// BeforeNewClient hooks openai.NewClient to inject the OTel HTTP middleware
// into every client the user constructs. Because Azure users call
// openai.NewClient(azure.WithEndpoint(...)), this hook also covers Azure
// OpenAI deployments.
func BeforeNewClient(ictx inst.HookContext, opts ...option.RequestOption) {
	if !openaishared.Enabled() {
		return
	}
	ictx.SetParam(optsParamIndex, withOtelMiddleware(opts))
}

// BeforeNewChatCompletionService hooks openai.NewChatCompletionService for
// users who construct the chat service directly without going through
// openai.NewClient.
func BeforeNewChatCompletionService(ictx inst.HookContext, opts ...option.RequestOption) {
	if !openaishared.Enabled() {
		return
	}
	ictx.SetParam(optsParamIndex, withOtelMiddleware(opts))
}
