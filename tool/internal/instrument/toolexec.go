// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstrumentPhase struct {
	logger *slog.Logger
	// The package name of the target file
	packageName string
	// The working directory during compilation
	workDir string
	// The target file to be instrumented
	target *dst.File
	// The parser for the target file
	parser *ast.AstParser
	// The compiling arguments for the target file
	compileArgs []string
	// The target function to be instrumented
	rawFunc *dst.FuncDecl
	// Whether the rule is exact match with target function, or it's a regexp match
	exact bool
	// The enter hook function, it should be inserted into the target source file
	beforeHookFunc *dst.FuncDecl
	// The exit hook function, it should be inserted into the target source file
	afterHookFunc *dst.FuncDecl
	// Variable declarations waiting to be inserted into target source file
	varDecls []dst.Decl
	// The declaration of the hook context, it should be replenished later
	hookCtxDecl *dst.GenDecl
	// The methods of the hook context
	hookCtxMethods []*dst.FuncDecl
}

func (ip *InstrumentPhase) Info(msg string, args ...any)  { ip.logger.Info(msg, args...) }
func (ip *InstrumentPhase) Error(msg string, args ...any) { ip.logger.Error(msg, args...) }
func (ip *InstrumentPhase) Warn(msg string, args ...any)  { ip.logger.Warn(msg, args...) }
func (ip *InstrumentPhase) Debug(msg string, args ...any) { ip.logger.Debug(msg, args...) }

func (ip *InstrumentPhase) keepForDebug(path string) {
	util.Assert(ip.packageName != "", "sanity check")
	escape := func(s string) string {
		dirName := strings.ReplaceAll(s, "/", "_")
		dirName = strings.ReplaceAll(dirName, ".", "_")
		return dirName
	}
	dest := filepath.Join("debug", escape(ip.packageName), filepath.Base(path))
	dest = util.GetBuildTemp(dest)
	err := os.MkdirAll(filepath.Dir(dest), os.ModePerm)
	if err != nil { // error is tolerable here
		ip.Warn("failed to create debug file directory", "dest", dest, "error", err)
		return
	}
	err = util.CopyFile(path, dest)
	if err != nil { // error is tolerable here
		ip.Warn("failed to save debug file", "dest", dest, "error", err)
		return
	}
}

func stripCompleteFlag(args []string) []string {
	for i, arg := range args {
		if arg == "-complete" {
			return append(args[:i], args[i+1:]...)
		}
	}
	return args
}

func Toolexec(ctx context.Context, args []string) error {
	// Check if the command is a compile command.
	if util.IsCompileCommand(strings.Join(args, " ")) {
		// Read compilation output directory
		target := util.FindFlagValue(args, "-o")
		util.Assert(target != "", "why not otherwise")
		ip := &InstrumentPhase{
			logger:      util.LoggerFromContext(ctx),
			workDir:     filepath.Dir(target),
			compileArgs: args,
			packageName: util.FindFlagValue(args, "-p"),
		}

		// Load matched hook rules from setup phase
		rules, err := ip.load()
		if err != nil {
			return err
		}
		// Check if the current package should be instrumented by matching the
		// current command with list of matched rules
		matchedRules := ip.match(args, rules)
		if len(matchedRules) > 0 {
			ip.Info("Instrumenting package", "rules", matchedRules, "args", args)
			// Okay, this package should be instrumented.
			err = ip.instrument(matchedRules, args)
			if err != nil {
				return err
			}

			// Strip -complete flag as we may insert some hook points that are
			// not ready yet, i.e. they don't have function body
			ip.compileArgs = stripCompleteFlag(ip.compileArgs)

			// Good, run final compilation after instrumentation
			err = util.RunCmd(ctx, ip.compileArgs...)
			if err != nil {
				return err
			}
			return nil
		}
	}
	// Otherwise, just run the command as is
	err := util.RunCmd(ctx, args...)
	if err != nil {
		return err
	}
	return nil
}
