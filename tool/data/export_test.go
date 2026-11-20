// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetManifestJSON(t *testing.T) {
	original := GetManifestJSON()
	require.NotEmpty(t, original)
	assert.True(t, json.Valid(original))

	mutable := GetManifestJSON()
	require.Equal(t, original, mutable)
	mutable[0] ^= 0xff

	assert.NotEqual(t, original, mutable)
	assert.Equal(t, original, GetManifestJSON())
}
