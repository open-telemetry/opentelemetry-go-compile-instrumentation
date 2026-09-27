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

// removeIgnore walks a constraint.Expr AST and simplifies any "ignore" TagExpr
// according to Boolean logic where "ignore" is treated as true (since otelc
// intends to compile files that go build ignores). If the simplified expression
// evaluates entirely to true, removeIgnore returns nil (no constraint).
// If it evaluates to false, it returns a TagExpr for "ignore" so the target
// platform filtering correctly rejects the file.
func removeIgnore(expr constraint.Expr) constraint.Expr {
	e, isConst, val := simplifyIgnore(expr)
	if isConst {
		if val {
			return nil
		}
		return &constraint.TagExpr{Tag: "ignore"}
	}
	return e
}

// simplifyIgnore evaluates a constraint.Expr assuming "ignore" is true.
// It returns the simplified expression, a boolean indicating if it evaluates to
// a constant, and the constant value if true.
func simplifyIgnore(expr constraint.Expr) (simplified constraint.Expr, isConst bool, constVal bool) {
	if expr == nil {
		return nil, true, true
	}
	switch e := expr.(type) {
	case *constraint.TagExpr:
		if e.Tag == "ignore" {
			return nil, true, true
		}
		return e, false, false
	case *constraint.NotExpr:
		sub, isConst, val := simplifyIgnore(e.X)
		if isConst {
			return nil, true, !val
		}
		return &constraint.NotExpr{X: sub}, false, false
	case *constraint.AndExpr:
		return simplifyAnd(e)
	case *constraint.OrExpr:
		return simplifyOr(e)
	default:
		return expr, false, false
	}
}

// simplifyAnd applies Boolean short-circuit rules to an AndExpr, treating
// "ignore" as true: true&&Y→Y, false&&Y→false, X&&true→X, X&&false→false.
func simplifyAnd(e *constraint.AndExpr) (simplified constraint.Expr, isConst bool, constVal bool) {
	x, xConst, xVal := simplifyIgnore(e.X)
	y, yConst, yVal := simplifyIgnore(e.Y)

	if xConst && yConst {
		return nil, true, xVal && yVal
	}
	if xConst {
		if xVal {
			return y, false, false
		}
		return nil, true, false
	}
	if yConst {
		if yVal {
			return x, false, false
		}
		return nil, true, false
	}
	return &constraint.AndExpr{X: x, Y: y}, false, false
}

// simplifyOr applies Boolean short-circuit rules to an OrExpr, treating
// "ignore" as true: true||Y→true, false||Y→Y, X||true→true, X||false→X.
func simplifyOr(e *constraint.OrExpr) (simplified constraint.Expr, isConst bool, constVal bool) {
	x, xConst, xVal := simplifyIgnore(e.X)
	y, yConst, yVal := simplifyIgnore(e.Y)

	if xConst && yConst {
		return nil, true, xVal || yVal
	}
	if xConst {
		if xVal {
			return nil, true, true
		}
		return y, false, false
	}
	if yConst {
		if yVal {
			return nil, true, true
		}
		return x, false, false
	}
	return &constraint.OrExpr{X: x, Y: y}, false, false
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
