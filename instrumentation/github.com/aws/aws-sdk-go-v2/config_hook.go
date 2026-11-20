// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package awssdk provides compile-time instrumentation for the AWS SDK for Go v2.
// It hooks config.LoadDefaultConfig and appends the OpenTelemetry middlewares from
// otelaws to the returned aws.Config, so every service client created from that
// config (e.g. via dynamodb.NewFromConfig) is traced automatically.
package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "AWS_SDK_V2"
)

type awsEnabler struct{}

func (a awsEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = awsEnabler{}

// AfterLoadDefaultConfig intercepts config.LoadDefaultConfig and appends the
// OTel middlewares to the returned aws.Config.APIOptions.
func AfterLoadDefaultConfig(ictx hook.HookContext, cfg aws.Config, err error) {
	if err != nil {
		return
	}
	if !runtime.Ready() {
		// otelaws reads the global OpenTelemetry API, which may not have
		// initialized yet when the config is loaded during package
		// initialization. Add the middlewares when an operation builds its
		// stack instead, so clients made from this config are still traced.
		// See runtime.Ready.
		cfg.APIOptions = append(cfg.APIOptions, appendMiddlewaresWhenReady)
		ictx.SetReturnVal(0, cfg)
		return
	}
	if !enabler.Enable() {
		return
	}

	otelaws.AppendMiddlewares(&cfg.APIOptions)

	ictx.SetReturnVal(0, cfg)
}

// appendMiddlewaresWhenReady adds the otelaws middlewares to an operation's
// stack once pkg/runtime has initialized. Operations run before that are not
// traced.
func appendMiddlewaresWhenReady(stack *middleware.Stack) error {
	if !runtime.Ready() || !enabler.Enable() {
		return nil
	}
	var apiOptions []func(*middleware.Stack) error
	otelaws.AppendMiddlewares(&apiOptions)
	for _, apply := range apiOptions {
		if err := apply(stack); err != nil {
			return err
		}
	}
	return nil
}
