// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestGenAISystem(t *testing.T) {
	kv := GenAISystem("anthropic")
	assert.Equal(t, attribute.Key("gen_ai.system"), kv.Key)
	assert.Equal(t, "anthropic", kv.Value.AsString())
}
