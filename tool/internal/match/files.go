// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package match

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// Logger is the optional sink for match diagnostics. SetupPhase implements it.
type Logger interface {
	Info(msg string, args ...any)
}

// Input is the file/AST matching request. Rules must already be filtered by
// package target and version; this step only decides which source files they
// apply to.
type Input struct {
	Set     *rule.InstRuleSet
	Sources []string
	Rules   []rule.InstRule
	Log     Logger
	// Dep is included in match log lines for compatibility with setup-time logs.
	Dep any
}

// Apply attaches file-level matches to in.Set. File rules are recorded
// unconditionally; other rules are matched against in.Sources via AST and
// where.file filters. Keys in the resulting maps are the given source paths.
func Apply(ctx context.Context, in Input) error {
	preciseRules := make([]rule.InstRule, 0, len(in.Rules))
	for _, r := range in.Rules {
		if fr, ok := r.(*rule.InstFileRule); ok {
			in.Set.AddFileRule(fr)
			logInfo(in.Log, "Match file rule", "rule", fr, "dep", in.Dep)
			continue
		}
		preciseRules = append(preciseRules, r)
	}

	if len(preciseRules) == 0 {
		if !in.Set.IsEmpty() && len(in.Sources) > 0 {
			name, err := ast.ParsePackageName(in.Sources[0])
			if err != nil {
				return err
			}
			in.Set.SetPackageName(name)
		}
		return nil
	}

	return applyPrecise(ctx, in, preciseRules)
}

func applyPrecise(ctx context.Context, in Input, rules []rule.InstRule) error {
	if len(in.Sources) == 0 {
		return nil
	}

	ruleFilters := make([]ruleFilter, 0, len(rules))
	for _, r := range rules {
		var f Filter
		if where := r.GetWhere(); where != nil {
			var err error
			f, err = Build(where)
			if err != nil {
				return ex.Wrapf(err, "build where filter for rule %q", r.GetName())
			}
		}
		ruleFilters = append(ruleFilters, ruleFilter{rule: r, where: f})
	}

	isTest := IsTestBuild(in.Sources)

	for _, source := range in.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		tree, err := ast.ParseFileFast(source)
		if err != nil {
			return err
		}
		in.Set.SetPackageName(tree.Name.Name)

		mctx := MatchContext{
			IsTest:     isTest,
			SourceFile: source,
			AST:        tree,
		}

		for _, rf := range ruleFilters {
			if rf.where != nil && !rf.where.Match(&mctx) {
				continue
			}
			if err = matchOneRule(tree, source, rf.rule, in); err != nil {
				return err
			}
		}
	}
	return nil
}

// ruleFilter pairs a rule with its pre-compiled where filter (if any).
type ruleFilter struct {
	rule  rule.InstRule
	where Filter // nil means no where clause — apply unconditionally
}

// IsTestBuild reports whether a compile invocation is part of a `go test` run.
// The Go toolchain only ever feeds these inputs to the compiler while building
// a test binary: a package augmented with its in-package _test.go files, the
// external xxx_test package (whose sources are also _test.go files), and the
// generated _testmain.go runner. None of them appear in a normal `go build`,
// so their presence in the source set is the signal. There is no dedicated
// "is test" compiler flag — verified against the toolchain — so the source set
// is the only thing to key on.
//
// ponytail: known gap, no fix possible at compile granularity. A package whose
// tests live only in an external xxx_test package (no in-package _test.go) is
// compiled once and shared between normal and test builds — the toolchain emits
// no test-only variant of it — so is_test cannot gate that package's production
// code. The external xxx_test package and any in-package _test.go files are
// still detected.
func IsTestBuild(sources []string) bool {
	for _, src := range sources {
		base := filepath.Base(src)
		if base == "_testmain.go" || strings.HasSuffix(base, "_test.go") {
			return true
		}
	}
	return false
}

func matchOneRule(tree *dst.File, source string, r rule.InstRule, in Input) error {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		_, ok, err := ast.FindFuncDecl(tree, rt)
		if err != nil {
			return err
		}
		if ok {
			in.Set.AddFuncRule(source, rt)
			logInfo(in.Log, "Match func rule", "rule", rt, "dep", in.Dep)
		}
	case *rule.InstStructRule:
		structType := ast.FindStructType(tree, rt.Struct)
		if structType != nil {
			in.Set.AddStructRule(source, rt)
			logInfo(in.Log, "Match struct rule", "rule", rt, "dep", in.Dep)
		}
	case *rule.InstRawRule:
		_, ok, err := ast.FindFuncDecl(tree, rt)
		if err != nil {
			return err
		}
		if ok {
			in.Set.AddRawRule(source, rt)
			logInfo(in.Log, "Match raw rule", "rule", rt, "dep", in.Dep)
		}
	case *rule.InstCallRule:
		// Call rules are added unconditionally to all source files in the
		// target package. Unlike func/struct/raw rules, there is no cheap
		// AST predicate to pre-filter files (the matching requires import
		// alias resolution which happens during the instrument phase).
		// Files without matching calls are a no-op in applyCallRule.
		in.Set.AddCallRule(source, rt)
		logInfo(in.Log, "Match call rule", "rule", rt, "dep", in.Dep)
	case *rule.InstDirectiveRule:
		if ast.FileHasDirective(tree, rt.Directive) {
			in.Set.AddDirectiveRule(source, rt)
			logInfo(in.Log, "Match directive rule", "rule", rt, "dep", in.Dep)
		}
	case *rule.InstDeclRule:
		if ast.FindNamedDecl(tree, rt.Identifier, rt.Kind) != nil {
			in.Set.AddDeclRule(source, rt)
			logInfo(in.Log, "Match decl rule", "rule", rt, "dep", in.Dep)
		}
	case *rule.InstFileRule:
		// Skip as it's already processed
	default:
		util.ShouldNotReachHere()
	}
	return nil
}

func logInfo(log Logger, msg string, args ...any) {
	if log != nil {
		log.Info(msg, args...)
	}
}
