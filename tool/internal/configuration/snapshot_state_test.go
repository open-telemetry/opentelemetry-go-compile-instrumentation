// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationSnapshotState(t *testing.T) {
	snapshot := []byte(`{"version":1,"values":{"go.sample.enabled":true}}`)
	state, err := EncodeApplicationSnapshotState(snapshot)
	require.NoError(t, err)
	present, err := VerifyApplicationSnapshotState(state, snapshot)
	require.NoError(t, err)
	assert.True(t, present)

	emptyState, err := EncodeApplicationSnapshotState(nil)
	require.NoError(t, err)
	present, err = VerifyApplicationSnapshotState(emptyState, nil)
	require.NoError(t, err)
	assert.False(t, present)
	assert.ErrorContains(t, func() error {
		_, verifyErr := VerifyApplicationSnapshotState(emptyState, snapshot)
		return verifyErr
	}(), "unexpected application configuration snapshot")
}

func TestVerifyApplicationSnapshotStateFailsClosed(t *testing.T) {
	snapshot := []byte(`{"version":1,"values":{"go.sample.enabled":true}}`)
	state, err := EncodeApplicationSnapshotState(snapshot)
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		state     []byte
		snapshot  []byte
		wantError string
	}{
		{
			name:      "missing payload",
			state:     state,
			wantError: "required application configuration snapshot is missing",
		},
		{
			name:      "changed payload",
			state:     state,
			snapshot:  []byte(`{"version":1,"values":{"go.sample.enabled":false}}`),
			wantError: "digest mismatch",
		},
		{
			name:      "invalid state JSON",
			state:     []byte("not JSON"),
			snapshot:  snapshot,
			wantError: "decode application configuration snapshot state",
		},
		{
			name: "unknown state version",
			state: func() []byte {
				var document applicationSnapshotState
				require.NoError(t, json.Unmarshal(state, &document))
				document.Version++
				encoded, marshalErr := json.Marshal(document)
				require.NoError(t, marshalErr)
				return encoded
			}(),
			snapshot:  snapshot,
			wantError: "unsupported application configuration snapshot state version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, verifyErr := VerifyApplicationSnapshotState(test.state, test.snapshot)
			require.ErrorContains(t, verifyErr, test.wantError)
		})
	}
}

func TestEncodeApplicationSnapshotStateRejectsInvalidPayload(t *testing.T) {
	_, err := EncodeApplicationSnapshotState([]byte("not JSON"))
	require.ErrorContains(t, err, "decode application configuration snapshot")
}
