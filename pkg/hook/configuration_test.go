// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import "testing"

func TestConfigurationSnapshotIsEmptyWithoutLinkerValue(t *testing.T) {
	if got := ConfigurationSnapshot(); got != "" {
		t.Fatalf("ConfigurationSnapshot() = %q, want empty", got)
	}
}
