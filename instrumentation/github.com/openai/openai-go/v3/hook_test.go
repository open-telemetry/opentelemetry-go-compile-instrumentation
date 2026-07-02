// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook/hooktest"
)

func TestBeforeNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "OPENAI")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	assert.Equal(t, 0, ictx.GetParamCount())
}

func TestBeforeNewClient_Enabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	ictx := hooktest.NewMockHookContext()
	BeforeNewClient(ictx)
	// Should have set param 0 with middleware options
	assert.Greater(t, ictx.GetParamCount(), 0)
}
