// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"

	"github.com/pmezard/go-difflib/difflib"

	"go.opentelemetry.io/otelc/tool/util"
)

// writeDiffForDebug writes a unified diff between oldFile (the original
// source) and newFile (the instrumented output) to .otelc-build/debug, so
// what otelc wove into a file is visible without hand-reconstructing it from
// the raw copies keepForDebug already saves. Only runs under --debug: unlike
// keepForDebug's raw copy, computing a diff isn't free, and the .diff file is
// purely a debugging aid.
func (ip *instrumentPhase) writeDiffForDebug(oldFile, newFile string) {
	if os.Getenv(util.EnvOtelcDebug) == "" {
		return
	}

	oldContent, err := os.ReadFile(oldFile)
	if err != nil {
		ip.Warn("failed to read original file for diff", "path", oldFile, "error", err)
		return
	}
	newContent, err := os.ReadFile(newFile)
	if err != nil {
		ip.Warn("failed to read instrumented file for diff", "path", newFile, "error", err)
		return
	}
	if string(oldContent) == string(newContent) {
		return
	}

	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(oldContent)),
		B:        difflib.SplitLines(string(newContent)),
		FromFile: oldFile,
		ToFile:   newFile,
		Context:  3,
	})
	if err != nil {
		ip.Warn("failed to compute instrumentation diff", "old", oldFile, "new", newFile, "error", err)
		return
	}

	dest := util.GetBuildTemp(filepath.Join(ip.debugArtifactDir(), filepath.Base(oldFile)+".diff"))
	if err = os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		ip.Warn("failed to create directory for instrumentation diff", "dest", dest, "error", err)
		return
	}
	if err = os.WriteFile(dest, []byte(text), 0o644); err != nil {
		ip.Warn("failed to write instrumentation diff", "dest", dest, "error", err)
		return
	}
	ip.Info("Wrote instrumentation diff", "path", dest)
}
