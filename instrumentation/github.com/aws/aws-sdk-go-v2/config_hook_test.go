// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awssdk

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func TestAfterLoadDefaultConfig(t *testing.T) {
	mockCtx := hooktest.NewMockHookContext()

	cfg := aws.Config{}
	AfterLoadDefaultConfig(mockCtx, cfg, nil)

	require.Len(t, mockCtx.ReturnVals, 1)
	updated, ok := mockCtx.ReturnVals[0].(aws.Config)
	require.True(t, ok, "return value 0 should be aws.Config")

	assert.NotEmpty(t, updated.APIOptions, "OTel middlewares should be appended to APIOptions")

	stack := middleware.NewStack("test", nil)
	for _, opt := range updated.APIOptions {
		require.NoError(t, opt(stack))
	}
}

func TestAfterLoadDefaultConfigWithError(t *testing.T) {
	mockCtx := hooktest.NewMockHookContext()

	cfg := aws.Config{}
	AfterLoadDefaultConfig(mockCtx, cfg, errors.New("load failed"))

	assert.Empty(t, mockCtx.ReturnVals)
}

func TestAfterLoadDefaultConfigDisabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "aws_sdk_v2")

	mockCtx := hooktest.NewMockHookContext()

	cfg := aws.Config{}
	AfterLoadDefaultConfig(mockCtx, cfg, nil)

	assert.Empty(t, mockCtx.ReturnVals)
}

// appendMiddlewaresWhenReady is what a config loaded during package
// initialization gets. Once pkg/runtime is ready, it must add the same
// middlewares that AfterLoadDefaultConfig adds directly.
func TestAppendMiddlewaresWhenReady(t *testing.T) {
	mockCtx := hooktest.NewMockHookContext()
	AfterLoadDefaultConfig(mockCtx, aws.Config{}, nil)
	require.Len(t, mockCtx.ReturnVals, 1)
	direct, ok := mockCtx.ReturnVals[0].(aws.Config)
	require.True(t, ok, "return value 0 should be aws.Config")
	want := middleware.NewStack("test", nil)
	for _, opt := range direct.APIOptions {
		require.NoError(t, opt(want))
	}

	got := middleware.NewStack("test", nil)
	require.NoError(t, appendMiddlewaresWhenReady(got))

	assert.NotEqual(t, middleware.NewStack("test", nil).List(), got.List())
	assert.Equal(t, want.List(), got.List())
}

func TestAppendMiddlewaresWhenReadyDisabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "aws_sdk_v2")

	stack := middleware.NewStack("test", nil)
	require.NoError(t, appendMiddlewaresWhenReady(stack))

	assert.Equal(t, middleware.NewStack("test", nil).List(), stack.List())
}
