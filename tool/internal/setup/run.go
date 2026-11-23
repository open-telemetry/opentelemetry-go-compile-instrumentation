// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
)

// ExitCodeError reports that the go command of `otelc go run` exited with the
// non-zero status Code. The go command has already reported why, a build error
// or the exit status of the program, so otelc exits with Code and adds nothing.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return "exit status " + strconv.Itoa(e.Code)
}

// runBuildArgs returns the go build arguments that compile the program of the
// go run arguments args: the build flags and the package, without -exec and
// without the arguments of the program.
func runBuildArgs(args []string) []string {
	var out []string
	for _, a := range classifyRunArgs(args) {
		if a.Kind != ArgRunFlag && a.Kind != ArgRunFlagValue && a.Kind != ArgProgramArg {
			out = append(out, a.Raw)
		}
	}
	return out
}

// goSubcommand splits the arguments of `otelc go` into the subcommand and its
// arguments (build, install, run or test). For go run it returns the go build
// that compiles the program, as the program's arguments and -exec do not belong
// on that command line.
func goSubcommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if args[0] == subcmdRun {
		return subcmdBuild, runBuildArgs(args[1:])
	}
	return args[0], args[1:]
}

// runProgram runs the instrumented go run command args. On an interrupt, go run
// waits for the program, which gets the signal from the terminal too. otelc
// waits the same way: the command does not inherit the cancellation of ctx, so
// the program can shut down and the go.mod restore still runs after it.
func runProgram(ctx context.Context, env, args []string) error {
	err := runBuildCmd(context.WithoutCancel(ctx), env, args...)
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() > 0 {
		return &ExitCodeError{Code: exitErr.ExitCode()}
	}
	return err
}
