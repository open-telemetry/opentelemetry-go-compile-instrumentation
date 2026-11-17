// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigurationValue(t *testing.T) {
	RegisterConfiguration(`{"go.sample.enabled":true,"go.sample.labels":["one"]}`)

	value, ok := ConfigurationValue("go.sample.enabled")
	require.True(t, ok)
	assert.Equal(t, true, value)

	value, ok = ConfigurationValue("missing")
	assert.False(t, ok)
	assert.Nil(t, value)
}
