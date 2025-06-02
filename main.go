// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/internal"
)

const minCompileArgs = 2 // compile command + at least one argument

func runCmd(args ...string) error {
	path := args[0]
	args = args[1:]
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to run command %s: %w", path, err)
	}
	return nil
}

func isCompilePackage(args []string, pkg string) bool {
	if len(args) < minCompileArgs {
		return false
	}
	if !strings.HasSuffix(args[0], "compile") &&
		!strings.HasSuffix(args[0], "compile.exe") {
		return false
	}
	for i, arg := range args[1:] {
		if arg == "-p" {
			if i+1 < len(args) && args[i+2] == pkg {
				return true
			}
		}
	}
	return false
}

func main() {
	args := os.Args[1:]
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if isCompilePackage(args, internal.TargetPkg) {
		// It's the compile command, intercept it and inject hook code
		args = internal.Instrument(logger, args)
	}
	err := runCmd(args...)
	if err != nil {
		panic("failed to run command: " + err.Error())
	}
}
