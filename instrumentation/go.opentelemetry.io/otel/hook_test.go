// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otel

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lambdadetector "go.opentelemetry.io/contrib/detectors/aws/lambda"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestAWSLambdaResourceDetector(t *testing.T) {
	// Simulate AWS Lambda environment
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	ctx := context.Background()
	
	detector := lambdadetector.NewResourceDetector()
	res, err := detector.Detect(ctx)
	require.NoError(t, err)

	tp := trace.NewTracerProvider(
		trace.WithResource(res),
	)
	_ = tp
	
	// Verify that the resource has expected attributes
	resource := res
	assert.Contains(t, resource.Attributes(), attribute.String("faas.name", "test-function"))
}
