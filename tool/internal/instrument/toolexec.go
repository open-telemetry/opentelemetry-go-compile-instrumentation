// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"log/slog"
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

// keepForDebug keeps the the file to .otel-build directory for debugging
func (ip *InstrumentPhase) keepForDebug(name string) {
	util.Assert(ip.packageName != "", "sanity check")
	escape := func(s string) string {
		dirName := strings.ReplaceAll(s, "/", "_")
		dirName = strings.ReplaceAll(dirName, ".", "_")
		return dirName
	}
	dest := filepath.Join("debug", escape(ip.packageName), filepath.Base(name))
	err := util.CopyFile(name, util.GetBuildTemp(dest))
	if err != nil { // error is tolerable here as this is only for debugging
		ip.Warn("failed to save modified file", "dest", dest, "error", err)
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

func interceptCompile(ctx context.Context, args []string) ([]string, error) {
	// Read compilation output directory
	target := util.FindFlagValue(args, "-o")
	util.Assert(target != "", "missing -o flag value")
	ip := &InstrumentPhase{
		logger:      util.LoggerFromContext(ctx),
		workDir:     filepath.Dir(target),
		compileArgs: args,
		packageName: util.FindFlagValue(args, "-p"),
	}

	util.Assert(util.IsCompileCommand(strings.Join(args, " ")), "sanity check")
	// Load matched hook rules from setup phase
	allSet, err := ip.load()
	if err != nil {
		return nil, err
	}

	// Check if the current compile command matches the rules.
	matched := ip.match(allSet, args)
	if !matched.IsEmpty() {
		ip.Info("Instrument package", "rules", matched, "args", args)
		// Okay, this package should be instrumented.
		err = ip.instrument(matched)
		if err != nil {
			return nil, err
		}

		// Strip -complete flag as we may insert some hook points that are
		// not ready yet, i.e. they don't have function body
		ip.compileArgs = stripCompleteFlag(ip.compileArgs)
	}

	// Run the instrumented compile command
	ip.Info("Run instrumented compile command",
		"args", strings.Join(ip.compileArgs, " "))
	return ip.compileArgs, nil
}

// Toolexec is the entry point of the toolexec command. It intercepts all the
// commands(link, compile, asm, etc) during build process. Our responsibility is
// to find out the compile command we are interested in and run it with the
// instrumented code.
func Toolexec(ctx context.Context, args []string) error {
	// Only interested in compile commands
	if util.IsCompileCommand(strings.Join(args, " ")) {
		var err error
		args, err = interceptCompile(ctx, args)
		if err != nil {
			return err
		}
	}
	// Just run the command as is
	return util.RunCmd(ctx, args...)
}
