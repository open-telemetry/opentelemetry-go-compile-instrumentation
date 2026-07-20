// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func TestBeforeNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "ANTHROPIC")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	assert.Equal(t, 0, ictx.GetParamCount())
}

func TestBeforeNewClient_Enabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "ANTHROPIC")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	// Should have set param 0 with middleware options
	assert.Greater(t, ictx.GetParamCount(), 0)
}
