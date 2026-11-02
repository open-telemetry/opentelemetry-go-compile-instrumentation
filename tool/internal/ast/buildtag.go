// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/build"
	"go/build/constraint"
	"io"
	"strings"
)

// StripBuildIgnoreTag strips the "ignore" build constraint tag from content,
// line by line, while preserving any remaining constraints (such as OS or
// architecture tags) and formatting them cleanly. If a build constraint line
// contained only "ignore", it is removed entirely. Text inside string literals
// or comment prose is left untouched. See #1069, #1095, and #1357.
func StripBuildIgnoreTag(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = StripDirectiveIgnore(line)
	}
	return strings.Join(lines, "\n")
}

// MatchFileConstraints reports whether a file with the given content in dir satisfies
// the build constraints in bctx. Any "ignore" build constraint tag is treated as true
// (i.e. stripped) because otelc compiles files that ordinary go build ignores.
func MatchFileConstraints(bctx *build.Context, dir, filename string, content []byte) (bool, error) {
	stripped := StripBuildIgnoreTag(string(content))
	if bctx == nil {
		bctx = &build.Default
	}
	ctxCopy := *bctx
	ctxCopy.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(stripped)), nil
	}
	return ctxCopy.MatchFile(dir, filename)
}

// StripDirectiveIgnore parses a single line as a build constraint directive.
// If it contains an "ignore" tag, it returns the directive without "ignore",
// or "" if the directive only contained "ignore". If the line is not a build
// constraint or does not contain "ignore", it returns the line unmodified.
func StripDirectiveIgnore(line string) string {
	trimmed := strings.TrimSpace(line)
	if !constraint.IsGoBuild(trimmed) && !constraint.IsPlusBuild(trimmed) {
		return line
	}
	expr, err := constraint.Parse(trimmed)
	if err != nil {
		return line
	}
	rem := RemoveIgnore(expr)
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

const ignoreTag = "ignore"

type exprEval struct {
	expr    constraint.Expr
	isConst bool
	val     bool
}

func falseExpr() constraint.Expr {
	return &constraint.AndExpr{
		X: &constraint.TagExpr{Tag: ignoreTag},
		Y: &constraint.NotExpr{X: &constraint.TagExpr{Tag: ignoreTag}},
	}
}

// RemoveIgnore walks a constraint.Expr AST and simplifies any "ignore" TagExpr
// according to Boolean logic where "ignore" is treated as true (since otelc
// intends to compile files that go build ignores). If the simplified expression
// evaluates entirely to true, RemoveIgnore returns nil (no constraint).
// If it evaluates to false, it returns an unconditionally false constraint
// (ignore && !ignore) so that target platform filtering correctly rejects the
// file even if the build explicitly passes -tags=ignore.
func RemoveIgnore(expr constraint.Expr) constraint.Expr {
	res := simplifyIgnore(expr)
	if res.isConst {
		if res.val {
			return nil
		}
		return falseExpr()
	}
	return res.expr
}

// simplifyIgnore evaluates a constraint.Expr assuming "ignore" is true.
// It returns an exprEval struct containing the simplified expression,
// whether it evaluates to a constant, and the constant value.
func simplifyIgnore(expr constraint.Expr) exprEval {
	if expr == nil {
		return exprEval{isConst: true, val: true}
	}
	switch e := expr.(type) {
	case *constraint.TagExpr:
		if e.Tag == ignoreTag {
			return exprEval{isConst: true, val: true}
		}
		return exprEval{expr: e}
	case *constraint.NotExpr:
		sub := simplifyIgnore(e.X)
		if sub.isConst {
			return exprEval{isConst: true, val: !sub.val}
		}
		return exprEval{expr: &constraint.NotExpr{X: sub.expr}}
	case *constraint.AndExpr:
		return simplifyAnd(e)
	case *constraint.OrExpr:
		return simplifyOr(e)
	default:
		return exprEval{expr: expr}
	}
}

// simplifyAnd applies Boolean short-circuit rules to an AndExpr, treating
// "ignore" as true: true&&Y→Y, false&&Y→false, X&&true→X, X&&false→false.
func simplifyAnd(e *constraint.AndExpr) exprEval {
	x := simplifyIgnore(e.X)
	y := simplifyIgnore(e.Y)

	if x.isConst && y.isConst {
		return exprEval{isConst: true, val: x.val && y.val}
	}
	if x.isConst {
		if x.val {
			return y
		}
		return exprEval{isConst: true, val: false}
	}
	if y.isConst {
		if y.val {
			return x
		}
		return exprEval{isConst: true, val: false}
	}
	return exprEval{expr: &constraint.AndExpr{X: x.expr, Y: y.expr}}
}

// simplifyOr applies Boolean short-circuit rules to an OrExpr, treating
// "ignore" as true: true||Y→true, false||Y→Y, X||true→true, X||false→X.
func simplifyOr(e *constraint.OrExpr) exprEval {
	x := simplifyIgnore(e.X)
	y := simplifyIgnore(e.Y)

	if x.isConst && y.isConst {
		return exprEval{isConst: true, val: x.val || y.val}
	}
	if x.isConst {
		if x.val {
			return exprEval{isConst: true, val: true}
		}
		return y
	}
	if y.isConst {
		if y.val {
			return exprEval{isConst: true, val: true}
		}
		return x
	}
	return exprEval{expr: &constraint.OrExpr{X: x.expr, Y: y.expr}}
}
