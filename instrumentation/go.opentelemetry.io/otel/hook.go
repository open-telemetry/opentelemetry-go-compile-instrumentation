// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otel

import (
	"context"
	"os"
	"sync/atomic"

	lambdadetector "go.opentelemetry.io/contrib/detectors/aws/lambda"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

var tracerProviderSet atomic.Bool

func beforeSetTracerProvider(ictx hook.HookContext, _ oteltrace.TracerProvider) {
	// If already set, skip the call
	if tracerProviderSet.Swap(true) {
		ictx.SetSkipCall(true)
		return
	}

	// Check if running in AWS Lambda
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		ctx := context.Background()
		detector := lambdadetector.NewResourceDetector()
		res, err := detector.Detect(ctx)
		if err == nil {
			// Create a new provider with the detected resource.
			tp := trace.NewTracerProvider(
				trace.WithResource(res),
			)
			otel.SetTracerProvider(tp)
		}
	}
}
