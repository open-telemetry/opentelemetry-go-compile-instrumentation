// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"path/filepath"
)

const (
	BuildTempDir = ".otel-build"
)

func GetBuildTemp(name string) string {
	return filepath.Join(BuildTempDir, name)
}
