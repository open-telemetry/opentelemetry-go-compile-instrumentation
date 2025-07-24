// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	BuildPlanLog = "build-plan.log"
)

type Dependency struct {
	ImportPath string
	Version    string
	Sources    []string
}

func (d *Dependency) String() string {
	if d.Version == "" {
		return fmt.Sprintf("{%s: %v}", d.ImportPath, d.Sources)
	}
	return fmt.Sprintf("{%s@%s: %v}", d.ImportPath, d.Version, d.Sources)
}

// isCompileCommand checks if the line is a compile command.
func isCompileCommand(line string) bool {
	check := []string{"-o", "-p", "-buildid"}
	if util.IsWindows() {
		check = append(check, "compile.exe")
	} else {
		check = append(check, "compile")
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
	if strings.Contains(line, "-pgoprofile") {
		return false
	}
	return true
}

// findCompileCommands finds the compile commands from the build plan log.
func findCompileCommands(buildPlanLog *os.File) ([]string, error) {
	const buildPlanBufSize = 10 * 1024 * 1024 // 10MB buffer size

	// Filter compile commands from build plan log
	compileCmds := make([]string, 0)
	scanner := bufio.NewScanner(buildPlanLog)
	// Seek to the beginning of the file before reading
	_, err := buildPlanLog.Seek(0, 0)
	if err != nil {
		return nil, ex.Errorf(err, "failed to seek to beginning of build plan log")
	}
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
		return nil, ex.Errorf(err, "failed to parse build plan log")
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

		err := arg.WriteByte(c)
		if err != nil {
			ex.Fatal(err)
		}
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
func (sp *SetupPhase) listBuildPlan(goBuildCmd []string) ([]string, error) {
	const goBuildCmdMinLen = 2 // go build/install + at least one argument
	util.Assert(len(goBuildCmd) >= goBuildCmdMinLen, "at least two arguments are required")
	util.Assert(strings.Contains(goBuildCmd[0], "go"), "sanity check")
	util.Assert(goBuildCmd[1] == "build" || goBuildCmd[1] == "install", "sanity check")

	// Create a build plan log file in the temporary directory
	buildPlanLog, err := os.Create(util.GetBuildTemp(BuildPlanLog))
	if err != nil {
		return nil, ex.Errorf(err, "failed to create build plan log file")
	}
	defer buildPlanLog.Close()
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
		return nil, ex.Errorf(err, "failed to run build plan")
	}

	// Find compile commands from build plan log
	compileCmds, err := findCompileCommands(buildPlanLog)
	if err != nil {
		return nil, err
	}
	sp.Debug("Found compile commands", "compileCmds", compileCmds)
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
func (sp *SetupPhase) findDeps(goBuildCmd []string) ([]*Dependency, error) {
	buildPlan, err := sp.listBuildPlan(goBuildCmd)
	if err != nil {
		return nil, err
	}
	// import path -> list of go files
	deps := make([]*Dependency, 0)
	for _, plan := range buildPlan {
		util.Assert(strings.Contains(plan, "compile"), "must be compile command")
		args := splitCompileCmds(plan)
		importPath := findFlagValue(args, "-p")
		util.Assert(importPath != "", "import path is empty")
		exist := false
		dep := &Dependency{
			ImportPath: importPath,
			Sources:    make([]string, 0),
		}
		for _, d := range deps {
			if d.ImportPath == importPath {
				exist = true
				break
			}
		}
		util.Assert(!exist, "import path should not be duplicated")

		for _, arg := range args {
			if strings.HasSuffix(arg, ".go") {
				dep.Sources = append(dep.Sources, arg)
			}
		}
		deps = append(deps, dep)
		sp.Info("Found dependency", "dep", dep)
	}
	return deps, nil
}
