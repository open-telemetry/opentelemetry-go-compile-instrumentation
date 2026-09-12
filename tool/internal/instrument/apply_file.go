// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"go/build/constraint"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// stripBuildIgnoreTag strips the "ignore" build constraint tag from content,
// line by line, while preserving any remaining constraints (such as OS or
// architecture tags) and formatting them cleanly. If a build constraint line
// contained only "ignore", it is removed entirely. Text inside string literals
// or comment prose is left untouched. See #1069, #1095, and #1357.
func stripBuildIgnoreTag(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = stripDirectiveIgnore(line)
	}
	return strings.Join(lines, "\n")
}

// stripDirectiveIgnore parses a single line as a build constraint directive.
// If it contains an "ignore" tag, it returns the directive without "ignore",
// or "" if the directive only contained "ignore". If the line is not a build
// constraint or does not contain "ignore", it returns the line unmodified.
func stripDirectiveIgnore(line string) string {
	trimmed := strings.TrimSpace(line)
	if !constraint.IsGoBuild(trimmed) && !constraint.IsPlusBuild(trimmed) {
		return line
	}
	expr, err := constraint.Parse(trimmed)
	if err != nil {
		return line
	}
	rem := removeIgnore(expr)
	if rem == nil {
		return ""
	}
	if rem.String() == expr.String() {
		return line
	}
	if constraint.IsGoBuild(trimmed) {
		return "//go:build " + rem.String()
	}
	plusLines, plusErr := constraint.PlusBuildLines(rem)
	if plusErr != nil || len(plusLines) == 0 {
		return line
	}
	return strings.Join(plusLines, "\n")
}

// removeIgnore walks a constraint.Expr AST and prunes any "ignore" TagExpr,
// returning the simplified constraint.Expr. If the expression contains only
// the "ignore" tag (or becomes empty), removeIgnore returns nil.
func removeIgnore(expr constraint.Expr) constraint.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *constraint.TagExpr:
		if e.Tag == "ignore" {
			return nil
		}
		return e
	case *constraint.NotExpr:
		sub := removeIgnore(e.X)
		if sub == nil {
			return nil
		}
		return &constraint.NotExpr{X: sub}
	case *constraint.AndExpr:
		x := removeIgnore(e.X)
		y := removeIgnore(e.Y)
		if x == nil && y == nil {
			return nil
		}
		if x == nil {
			return y
		}
		if y == nil {
			return x
		}
		return &constraint.AndExpr{X: x, Y: y}
	case *constraint.OrExpr:
		x := removeIgnore(e.X)
		y := removeIgnore(e.Y)
		if x == nil && y == nil {
			return nil
		}
		if x == nil {
			return y
		}
		if y == nil {
			return x
		}
		return &constraint.OrExpr{X: x, Y: y}
	default:
		return expr
	}
}

// applyFileRule introduces the new file to the target package at compile time.
func (ip *instrumentPhase) applyFileRule(ctx context.Context, rule *rule.InstFileRule, pkgName string) error {
	file := filepath.Join(rule.ResolvedPath, rule.File)
	if !util.PathExists(file) {
		return ex.Newf("file %s not found in %s", rule.File, rule.ResolvedPath)
	}

	// Parse the new file into AST nodes and modify it as needed.
	// Keep processing in-memory to avoid mutating shared temp rule files.
	data, err := os.ReadFile(file)
	if err != nil {
		return ex.Wrapf(err, "reading rule source file %s", file)
	}

	strippedSource := stripBuildIgnoreTag(string(data))

	// Evaluate build constraints before adding the file to compilation.
	// go tool compile compiles all explicit CLI arguments unconditionally,
	// so we must skip files whose build constraints do not match the target platform.
	bctx := *ip.getBuildContext()
	bctx.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(strippedSource)), nil
	}
	match, err := bctx.MatchFile(rule.ResolvedPath, rule.File)
	if err != nil {
		return ex.Wrapf(err, "matching build constraints for %s", file)
	}
	if !match {
		ip.Debug("File rule build constraints not satisfied; skipping", "rule", rule.Name, "file", rule.File)
		return nil
	}

	root, err := ast.NewAstParser().ParseSource(strippedSource)
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", file)
	}
	// Always rename the package name to the target package name
	root.Name.Name = pkgName

	// The file being added has its own imports that need to be in importcfg.
	// Without this, the compiler will fail with "could not import X" errors.
	if err = ip.updateImportConfigForFile(ctx, root, rule.Name); err != nil {
		return err
	}

	// Write back the modified AST to a new file in the working directory
	base := filepath.Base(rule.File)
	ext := filepath.Ext(base)
	newName := strings.TrimSuffix(base, ext)
	newFile := filepath.Join(ip.workDir, fmt.Sprintf("otelc.%s.go", newName))
	err = ast.WriteFile(newFile, root)
	if err != nil {
		return ex.Wrapf(err, "writing instrumented file %s", newFile)
	}
	ip.Info("Apply file rule", "rule", rule)

	// Add the new file as part of the source files to be compiled
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	return nil
}
