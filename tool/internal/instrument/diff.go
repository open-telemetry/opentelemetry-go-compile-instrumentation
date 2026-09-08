// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pmezard/go-difflib/difflib"

	"go.opentelemetry.io/otelc/tool/util"
)

// writeDiffForDebug writes a unified diff between oldFile (the original
// source) and newFile (the instrumented output) next to the copies
// keepForDebug already saves, so what otelc wove in is readable without
// diffing those copies by hand. Gated on --debug because, unlike a raw copy,
// the diff has to be computed.
func (ip *instrumentPhase) writeDiffForDebug(oldFile, newFile string) {
	// Parsed rather than tested for emptiness so OTELC_DEBUG=0 means the same
	// here as it does to the --debug flag that reads the same variable.
	if debug, err := strconv.ParseBool(os.Getenv(util.EnvOtelcDebug)); err != nil || !debug {
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
	if bytes.Equal(oldContent, newContent) {
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

	dest := filepath.Join(ip.debugArtifactDir(), filepath.Base(oldFile)+".diff")
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
