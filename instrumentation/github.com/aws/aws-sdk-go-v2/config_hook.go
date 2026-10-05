// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package awssdk provides compile-time instrumentation for the AWS SDK for Go v2.
// It hooks config.LoadDefaultConfig and appends the OpenTelemetry middlewares from
// otelaws to the returned aws.Config, so every service client created from that
// config (e.g. via dynamodb.NewFromConfig) is traced automatically.
package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
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
	if !enabler.Enable() || err != nil {
		return
	}

	otelaws.AppendMiddlewares(&cfg.APIOptions)

	ictx.SetReturnVal(0, cfg)
}
