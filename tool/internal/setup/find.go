// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	BuildPlanLog    = "build-plan.log"
	BuildPgoProfile = "-pgoprofile"
)

// isCompileCommand checks if the line is a compile command.
func isCompileCommand(line string) bool {
	check := []string{"-o", "-p", "-buildid"}
	switch util.IsWindows() {
	case true:
		check = append(check, "compile.exe")
	case false:
		check = append(check, "compile")
	default:
		util.ShouldNotReachHere()
	}

	// Check if the line contains all the required fields
	for _, id := range check {
		if !strings.Contains(line, id) {
			return false
		}
	}

	// @@PGO compile command is different from normal compile command, we
	// should skip it, otherwise the same package will be find twice
	// (one for PGO and one for normal)
	if strings.Contains(line, BuildPgoProfile) {
		return false
	}
	return true
}

// getCompileCommands gets the compile commands from the build plan log.
func getCompileCommands() ([]string, error) {
	const buildPlanBufSize = 10 * 1024 * 1024 // 10MB buffer size
	buildPlanLog, err := os.Open(util.GetBuildTemp(BuildPlanLog))
	if err != nil {
		return nil, fmt.Errorf("failed to open build plan log file: %w", err)
	}
	defer buildPlanLog.Close()

	// Filter compile commands from build plan log
	compileCmds := make([]string, 0)
	scanner := bufio.NewScanner(buildPlanLog)
	// 10MB should be enough to accommodate most long line
	buffer := make([]byte, 0, buildPlanBufSize)
	scanner.Buffer(buffer, cap(buffer))
	for scanner.Scan() {
		line := scanner.Text()
		if isCompileCommand(line) {
			line = strings.Trim(line, " ")
			compileCmds = append(compileCmds, line)
		}
	}
	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("failed to parse build plan log: %w", err)
	}
	return compileCmds, nil
}

// splitCompileCmds splits the command line by space, but keep the quoted part
// as a whole. For example, "a b" c will be split into ["a b", "c"].
func splitCompileCmds(input string) []string {
	var args []string
	var inQuotes bool
	var arg strings.Builder

	for i := range len(input) {
		c := input[i]

		if c == '"' {
			inQuotes = !inQuotes
			continue
		}

		if c == ' ' && !inQuotes {
			if arg.Len() > 0 {
				args = append(args, arg.String())
				arg.Reset()
			}
			continue
		}

		arg.WriteByte(c)
	}

	if arg.Len() > 0 {
		args = append(args, arg.String())
	}

	// Fix the escaped backslashes on Windows
	if util.IsWindows() {
		for i, arg := range args {
			args[i] = strings.ReplaceAll(arg, `\\`, `\`)
		}
	}
	return args
}

// listBuildPlan lists the build plan by running `go build/install -a -x -n`
// and then filtering the compile commands from the build plan log.
func (sp *SetupProcessor) listBuildPlan(goBuildCmd []string) ([]string, error) {
	const goBuildCmdMinLen = 2 // go build/install + at least one argument
	util.Assert(len(goBuildCmd) >= goBuildCmdMinLen, "at least two arguments are required")
	util.Assert(strings.Contains(goBuildCmd[0], "go"), "sanity check")
	util.Assert(goBuildCmd[1] == "build" || goBuildCmd[1] == "install", "sanity check")

	// Create a build plan log file in the temporary directory
	buildPlanLog, err := os.Create(util.GetBuildTemp(BuildPlanLog))
	if err != nil {
		return nil, fmt.Errorf("failed to create build plan log file: %w", err)
	}
	// The full build command is: "go build/install -a -x -n  {...}"
	args := []string{}
	args = append(args, goBuildCmd[:2]...)             // go build/install
	args = append(args, []string{"-a", "-x", "-n"}...) // -a -x -n
	args = append(args, goBuildCmd[2:]...)             // {...} remaining

	sp.Info("List build plan", "args", args)
	//nolint:gosec // Command arguments are validated with above assertions
	cmd := exec.Command(args[0], args[1:]...)
	// This is a little anti-intuitive as the error message is not printed to
	// the stderr, instead it is printed to the stdout, only the build tool
	// knows the reason why.
	cmd.Stdout = os.Stdout
	cmd.Stderr = buildPlanLog
	// @@Note that dir should not be set, as the dry build should be run in the
	// same directory as the original build command
	cmd.Dir = ""
	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run build plan: %w", err)
	}

	// Find compile commands from build plan log
	compileCmds, err := getCompileCommands()
	if err != nil {
		return nil, err
	}
	return compileCmds, nil
}

// findFlagValue finds the value of a flag in the command line.
func findFlagValue(cmd []string, flag string) string {
	for i, v := range cmd {
		if v == flag {
			return cmd[i+1]
		}
	}
	return ""
}

// findDeps finds the dependencies of the project by listing the build plan.
func (sp *SetupProcessor) findDeps(goBuildCmd []string) (map[string][]string, error) {
	buildPlan, err := sp.listBuildPlan(goBuildCmd)
	if err != nil {
		return nil, err
	}
	// import path -> list of go files
	deps := make(map[string][]string)
	for _, plan := range buildPlan {
		util.Assert(strings.Contains(plan, "compile"), "must be compile command")
		args := splitCompileCmds(plan)
		importPath := findFlagValue(args, "-p")
		util.Assert(importPath != "", "import path is empty")
		_, exist := deps[importPath]
		util.Assert(!exist, "import path should not be duplicated")

		for _, arg := range args {
			if strings.HasSuffix(arg, ".go") {
				deps[importPath] = append(deps[importPath], arg)
			}
		}
		sp.Info("Found dependency", "importPath", importPath,
			"sources", deps[importPath])
	}
	return deps, nil
}
