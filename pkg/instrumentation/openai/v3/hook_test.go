// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

func TestBeforeNewClient_InjectsMiddleware(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")

	ictx := insttest.NewMockHookContext()
	userOpt := option.WithAPIKey("sk-test")

	BeforeNewClient(ictx, userOpt)

	got, ok := ictx.GetParam(optsParamIndex).([]option.RequestOption)
	require.True(t, ok, "opts must be replaced with a []option.RequestOption slice")
	// Expect our middleware option prepended + the original user option.
	// RequestOption is a function type and cannot be compared with ==, so we
	// just verify the length and rely on the slice ordering contract.
	require.Len(t, got, 2)
	assert.NotNil(t, got[0], "middleware option must be first")
	assert.NotNil(t, got[1], "user option must be preserved")
	_ = userOpt
}

func TestBeforeNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "OPENAI")

	ictx := insttest.NewMockHookContext()
	BeforeNewClient(ictx, option.WithAPIKey("sk-test"))

	// When disabled, opts must not be touched.
	assert.Nil(t, ictx.GetParam(optsParamIndex))
}

func TestBeforeNewChatCompletionService_InjectsMiddleware(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")

	ictx := insttest.NewMockHookContext()
	BeforeNewChatCompletionService(ictx, option.WithAPIKey("sk-test"))

	got, ok := ictx.GetParam(optsParamIndex).([]option.RequestOption)
	require.True(t, ok)
	assert.Len(t, got, 2)
}
