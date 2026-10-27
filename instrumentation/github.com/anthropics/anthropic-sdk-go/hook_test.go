// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func TestBeforeNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "ANTHROPIC")
	t.Setenv(sdkOpenTelemetryEnv, "")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	assert.Equal(t, 0, ictx.GetParamCount())
	assert.Empty(t, os.Getenv(sdkOpenTelemetryEnv))
}

func TestBeforeNewClient_Enabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "ANTHROPIC")
	t.Setenv(sdkOpenTelemetryEnv, "")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	// Should have set param 0 with middleware options and disabled SDK built-in tracing
	assert.Greater(t, ictx.GetParamCount(), 0)
	assert.Equal(t, "false", os.Getenv(sdkOpenTelemetryEnv))

	// Subsequent NewClient calls in the same process must still inject middleware
	ictx2 := hooktest.NewMockHookContext()
	BeforeNewClient(ictx2)
	assert.Greater(t, ictx2.GetParamCount(), 0)
	assert.Equal(t, "false", os.Getenv(sdkOpenTelemetryEnv))
}

func TestBeforeNewClient_SDKOpenTelemetryExplicitlyEnabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "ANTHROPIC")
	t.Setenv(sdkOpenTelemetryEnv, "true")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	assert.Equal(t, 0, ictx.GetParamCount())
	assert.Equal(t, "true", os.Getenv(sdkOpenTelemetryEnv))
}
