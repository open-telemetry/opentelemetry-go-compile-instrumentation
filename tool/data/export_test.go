// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBundleReader(t *testing.T) {
	r := GetBundleReader()
	require.NotNil(t, r)

	// The embedded bundle is a non-empty tgz archive, and a fresh reader is
	// positioned at the start (unread length equals total size).
	assert.Positive(t, r.Len())
	assert.Equal(t, r.Size(), int64(r.Len()), "reader must start at offset 0")

	// A second reader is independent of the first and reports the same size.
	r2 := GetBundleReader()
	require.NotNil(t, r2)
	assert.Equal(t, r.Len(), r2.Len())
}

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
