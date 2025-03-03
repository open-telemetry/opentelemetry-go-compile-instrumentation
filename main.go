// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/internal"
)

func runCmd(args ...string) error {
	path := args[0]
	args = args[1:]
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func isCompilePackage(args []string, pkg string) bool {
	checks := []string{"compile", "-buildid", "-p " + pkg}
	for _, check := range checks {
		if !strings.Contains(strings.Join(args, " "), check) {
			return false
		}
	}
	return true
}

func main() {
	args := os.Args[1:]
	if isCompilePackage(args, internal.TargetPkg) {
		// It's the compile command, intercept it and inject hook code
		args = internal.Instrument(args)
	}
	err := runCmd(args...)
	if err != nil {
		panic("failed to run command: " + err.Error())
	}
}
