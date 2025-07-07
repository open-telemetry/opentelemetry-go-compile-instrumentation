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
