// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/pmezard/go-difflib/difflib"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const diffContextLines = 3

// DiffDebugEnabled parses OTELC_DEBUG the same way the --debug flag does
// (urfave/cli's EnvVars source), so OTELC_DEBUG=0 disables the diff output
// the same as an absent variable, rather than only an empty string doing so.
func DiffDebugEnabled() bool {
	debug, err := strconv.ParseBool(os.Getenv(util.EnvOtelcDebug))
	return err == nil && debug
}

// ruleChange is one rule's incremental contribution to a file, captured by
// rendering the AST immediately before and after applying it.
type ruleChange struct {
	name          string
	before, after []byte
}

// applyRulesCapturingDiffs applies rules to root in order, identically to a
// plain loop over applyOneRule, but under --debug also renders the AST
// before and after each application so writeDiffForDebug can later attribute
// each change to the rule that produced it and to its position in the
// application order. Rendering is skipped entirely when debug is off, so the
// default build path pays nothing extra.
func (ip *instrumentPhase) applyRulesCapturingDiffs(
	ctx context.Context,
	rules []rule.InstRule,
	root *dst.File,
) (bool, []ruleChange, error) {
	return ip.applyRulesCapturingDiffsWithRenderer(ctx, rules, root, ast.RenderFile)
}

func (ip *instrumentPhase) applyRulesCapturingDiffsWithRenderer(
	ctx context.Context,
	rules []rule.InstRule,
	root *dst.File,
	render func(*dst.File) ([]byte, error),
) (bool, []ruleChange, error) {
	debug := DiffDebugEnabled()
	var prev []byte
	if debug {
		var err error
		prev, err = render(root)
		if err != nil {
			return false, nil, ex.Wrapf(err, "rendering AST before applying rules")
		}
	}

	var (
		needsGlobals bool
		changes      []ruleChange
	)
	for _, r := range rules {
		ruleNeedsGlobals, err1 := ip.applyOneRule(ctx, r, root)
		if err1 != nil {
			return needsGlobals, changes, ex.Wrapf(err1, "applying rule %s", r.GetName())
		}
		if ruleNeedsGlobals {
			needsGlobals = true
			if debug {
				ip.globalsContributors = append(ip.globalsContributors, r.GetName())
			}
		}
		if !debug {
			continue
		}

		after, err := render(root)
		if err != nil {
			return needsGlobals, changes, ex.Wrapf(err, "rendering AST after applying rule %s", r.GetName())
		}
		if !bytes.Equal(prev, after) {
			changes = append(changes, ruleChange{name: r.GetName(), before: prev, after: after})
		}
		prev = after
	}
	return needsGlobals, changes, nil
}

// writeDiffForDebug writes a report of what otelc wove into oldFile next to
// the copies keepForDebug already saves: a unified diff per rule, in
// application order, so an injected change can be traced back to the rule
// that caused it, followed by the full old-to-new diff. That full diff is
// the authoritative one for what the compiler actually sees, because
// post-processing (optimizeTJumps) can still alter the file after the last
// rule has run, so the per-rule sections need not sum to it.
// RemoveStaleDiff removes a stale diff file at dest if it exists.
// If removal fails with an error other than os.ErrNotExist, a warning is logged.
func RemoveStaleDiff(dest string, logger *slog.Logger) {
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		if logger != nil {
			logger.Warn("failed to remove stale instrumentation diff", "dest", dest, "error", err)
		}
	}
}

func (ip *instrumentPhase) removeStaleDiff(dest string) {
	RemoveStaleDiff(dest, ip.logger)
}

// writeDiffForDebug writes a report of what otelc wove into oldFile next to
// the copies keepForDebug already saves: a unified diff per rule, in
// application order, so an injected change can be traced back to the rule
// that caused it, followed by the full old-to-new diff. That full diff is
// the authoritative one for what the compiler actually sees, because
// post-processing (optimizeTJumps) can still alter the file after the last
// rule has run, so the per-rule sections need not sum to it.
func (ip *instrumentPhase) writeDiffForDebug(oldFile, newFile string, changes []ruleChange) {
	if !DiffDebugEnabled() {
		return
	}

	dest := filepath.Join(ip.debugArtifactDir(), filepath.Base(oldFile)+".diff")

	oldContent, err := os.ReadFile(oldFile)
	if err != nil {
		ip.removeStaleDiff(dest)
		ip.Warn("failed to read original file for diff", "path", oldFile, "error", err)
		return
	}
	newContent, err := os.ReadFile(newFile)
	if err != nil {
		ip.removeStaleDiff(dest)
		ip.Warn("failed to read instrumented file for diff", "path", newFile, "error", err)
		return
	}
	if bytes.Equal(oldContent, newContent) && len(changes) == 0 {
		ip.removeStaleDiff(dest)
		return
	}

	var report strings.Builder
	for i, c := range changes {
		// Both sides are the same file at different points in the rule
		// sequence. The suffix keeps the two header lines distinguishable
		// while leaving the path itself the first whitespace-delimited
		// token, which is what patch(1) reads as the filename.
		text := unifiedDiff(c.before, c.after,
			fmt.Sprintf("%s (before %s)", oldFile, c.name),
			fmt.Sprintf("%s (after %s)", oldFile, c.name))
		if text == "" {
			continue
		}
		_, _ = fmt.Fprintf(&report, "=== rule %d/%d: %s ===\n%s\n", i+1, len(changes), c.name, text)
	}

	fullText := unifiedDiff(oldContent, newContent, oldFile, newFile)
	if report.Len() > 0 {
		_, _ = fmt.Fprintf(&report, "=== full diff: %s -> %s ===\n%s", oldFile, newFile, fullText)
	} else {
		_, _ = report.WriteString(fullText)
	}

	if err = os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		ip.Warn("failed to create directory for instrumentation diff", "dest", dest, "error", err)
		return
	}
	if err = os.WriteFile(dest, []byte(report.String()), 0o600); err != nil {
		ip.Warn("failed to write instrumentation diff", "dest", dest, "error", err)
		return
	}
	ip.Info("Wrote instrumentation diff", "path", dest, "rules", len(changes))
}

// WriteAddedSourceDiff writes a unified diff representing a new file
// introduced entirely by instrumentation, with /dev/null as the original source.
func WriteAddedSourceDiff(dest, newFile, header string, logger *slog.Logger) {
	if !DiffDebugEnabled() {
		return
	}

	newContent, err := os.ReadFile(newFile)
	if err != nil {
		RemoveStaleDiff(dest, logger)
		if logger != nil {
			logger.Warn("failed to read added file for diff", "path", newFile, "error", err)
		}
		return
	}

	diffText := unifiedDiff(nil, newContent, "/dev/null", newFile)

	var report strings.Builder
	if header != "" {
		_, _ = report.WriteString(header)
		if !strings.HasSuffix(header, "\n") {
			_ = report.WriteByte('\n')
		}
	}
	_, _ = report.WriteString(diffText)

	if err = os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		if logger != nil {
			logger.Warn("failed to create directory for instrumentation diff", "dest", dest, "error", err)
		}
		return
	}
	if err = os.WriteFile(dest, []byte(report.String()), 0o600); err != nil {
		if logger != nil {
			logger.Warn("failed to write instrumentation diff", "dest", dest, "error", err)
		}
		return
	}
	if logger != nil {
		logger.Info("Wrote added source instrumentation diff", "path", dest)
	}
}

// writeAddedSourceDiffForDebug writes a unified diff representing a new file
// introduced entirely by instrumentation, with /dev/null as the original source.
func (ip *instrumentPhase) writeAddedSourceDiffForDebug(newFile, header string) {
	dest := filepath.Join(ip.debugArtifactDir(), filepath.Base(newFile)+".diff")
	WriteAddedSourceDiff(dest, newFile, header, ip.logger)
}

// writeFileRuleDiffForDebug records a generated InstFileRule file in the debug
// report, attributed to the rule that introduced it.
func (ip *instrumentPhase) writeFileRuleDiffForDebug(newFile, ruleName string) {
	header := fmt.Sprintf("=== rule: %s ===", ruleName)
	ip.writeAddedSourceDiffForDebug(newFile, header)
}

// FormatGeneratedFileHeader formats the header block for generated instrumentation files,
// listing the contributing rules in order.
func FormatGeneratedFileHeader(filename string, contributors []string) string {
	var header strings.Builder
	_, _ = header.WriteString("=== generated instrumentation file: " + filename + " ===")
	if len(contributors) > 0 {
		_, _ = header.WriteString("\nrules:")
		for _, c := range contributors {
			_, _ = fmt.Fprintf(&header, "\n  - %s", c)
		}
	}
	return header.String()
}

// writeGlobalsDiffForDebug records the generated otelc.globals.go file in the
// debug report, attributing it to the rules that required its generation.
func (ip *instrumentPhase) writeGlobalsDiffForDebug(path string, contributors []string) {
	header := FormatGeneratedFileHeader(otelcGlobalsFile, contributors)
	ip.writeAddedSourceDiffForDebug(path, header)
}

func unifiedDiff(a, b []byte, fromFile, toFile string) string {
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(a)),
		B:        difflib.SplitLines(string(b)),
		FromFile: fromFile,
		ToFile:   toFile,
		Context:  diffContextLines,
	})
	return diff
}
