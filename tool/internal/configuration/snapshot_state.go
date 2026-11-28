// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"go.opentelemetry.io/otelc/tool/ex"
)

type applicationSnapshotState struct {
	Version int    `json:"version"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
}

// EncodeApplicationSnapshotState records whether a runtime snapshot exists and
// its digest. Setup publishes this state after the snapshot so toolexec can
// distinguish an intentionally absent configuration from a missing artifact.
func EncodeApplicationSnapshotState(snapshot []byte) ([]byte, error) {
	state := applicationSnapshotState{Version: SnapshotVersion}
	if len(snapshot) > 0 {
		if err := validateApplicationSnapshot(snapshot); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(snapshot)
		state.Present = true
		state.SHA256 = hex.EncodeToString(sum[:])
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, ex.Wrapf(err, "encode application configuration snapshot state")
	}
	return data, nil
}

// VerifyApplicationSnapshotState reports whether a snapshot is expected and
// verifies its payload when present. Missing, corrupt, or stale snapshots fail
// closed rather than silently removing configuration from the binary.
func VerifyApplicationSnapshotState(stateData, snapshot []byte) (bool, error) {
	var state applicationSnapshotState
	if err := json.Unmarshal(stateData, &state); err != nil {
		return false, ex.Wrapf(err, "decode application configuration snapshot state")
	}
	if state.Version != SnapshotVersion {
		return false, ex.Newf("unsupported application configuration snapshot state version %d", state.Version)
	}
	if state.Present != (state.SHA256 != "") {
		return false, ex.New("inconsistent application configuration snapshot state")
	}
	if !state.Present {
		if len(snapshot) != 0 {
			return false, ex.New("unexpected application configuration snapshot without state")
		}
		return false, nil
	}
	if len(snapshot) == 0 {
		return false, ex.New("required application configuration snapshot is missing")
	}
	if err := validateApplicationSnapshot(snapshot); err != nil {
		return false, err
	}
	sum := sha256.Sum256(snapshot)
	if hex.EncodeToString(sum[:]) != state.SHA256 {
		return false, ex.New("application configuration snapshot digest mismatch")
	}
	return true, nil
}

func validateApplicationSnapshot(snapshot []byte) error {
	var document applicationSnapshotDocument
	if err := json.Unmarshal(snapshot, &document); err != nil {
		return ex.Wrapf(err, "decode application configuration snapshot")
	}
	if document.Version != SnapshotVersion {
		return ex.Newf("unsupported application configuration snapshot version %d", document.Version)
	}
	if len(document.Values) == 0 {
		return ex.New("application configuration snapshot contains no resolved values")
	}
	return nil
}
