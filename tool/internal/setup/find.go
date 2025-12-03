// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const maxBuildPlanBufferSize = 10 * 1024 * 1024 // 10MB

type Dependency struct {
	ImportPath string
	Version    string
	Sources    []string
	CgoFiles   map[string]string
}

func (d *Dependency) String() string {
	if d.Version == "" {
		return fmt.Sprintf("{%s: %v}", d.ImportPath, d.Sources)
	}
	return fmt.Sprintf("{%s@%s: %v}", d.ImportPath, d.Version, d.Sources)
}

// parseCdDir extracts the directory path from a "cd" command line.
func parseCdDir(line string) (string, bool) {
	if !strings.HasPrefix(line, "cd ") {
		return "", false
	}
	const cdCommandSplitLimit = 2 // Split "cd dir" into [dir, rest] to ignore trailing comments
	parts := strings.SplitN(line[3:], " ", cdCommandSplitLimit)
	return strings.TrimSpace(parts[0]), true
}

// findCompileCommands finds the compile commands from the build plan log.
func findCompileCommands(buildPlanLog *os.File) ([]string, error) {
	scanner, err := util.NewFileScanner(buildPlanLog, maxBuildPlanBufferSize)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to seek to beginning of build plan log")
	}

	var compileCmds []string
	for scanner.Scan() {
		line := scanner.Text()
		if util.IsCompileCommand(line) {
			line = strings.Trim(line, " ")
			compileCmds = append(compileCmds, line)
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, ex.Wrapf(err, "failed to parse build plan log")
	}
	return compileCmds, nil
}

// isCgoCommand checks in the build plan log if the line is a cgo tool invocation.
// CGO commands contain the cgo tool path and -importpath flag.
func isCgoCommand(line string) bool {
	if !strings.Contains(line, "/cgo ") && !strings.Contains(line, "/cgo.exe ") {
		return false
	}
	if !strings.Contains(line, "-importpath") {
		return false
	}
	if strings.Contains(line, "-dynimport") {
		return false
	}
	return true
}

// findCgoObjDirs parses the build plan to extract CGO object directory mappings.
// Returns map[objDir]sourceDir
func findCgoObjDirs(buildPlanLog *os.File) (map[string]string, error) {
	scanner, err := util.NewFileScanner(buildPlanLog, maxBuildPlanBufferSize)
	if err != nil {
		return nil, err
	}

	cgoDirsMap := make(map[string]string)
	var currentDir string

	for scanner.Scan() {
		line := filepath.ToSlash(strings.TrimSpace(scanner.Text()))

		if dir, ok := parseCdDir(line); ok {
			currentDir = dir
			continue
		}

		if isCgoCommand(line) && currentDir != "" {
			if objDir := util.FindFlagValue(util.SplitCompileCmds(line), "-objdir"); objDir != "" {
				normalized := strings.TrimSuffix(filepath.ToSlash(objDir), "/")
				cgoDirsMap[normalized] = currentDir
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, ex.Wrapf(err, "failed to parse build plan log for cgo commands")
	}
	return cgoDirsMap, nil
}

// listBuildPlan lists the build plan by running `go build/install -a -x -n`
// and then filtering the compile commands and CGO object directory mappings from the build plan log.
func (sp *SetupPhase) listBuildPlan(ctx context.Context, goBuildCmd []string) ([]string, map[string]string, error) {
	const goBuildMinArgs = 2 // go build
	const buildPlanLogName = "build-plan.log"
	if len(goBuildCmd) < goBuildMinArgs {
		return nil, nil, ex.Newf("at least %d arguments are required", goBuildMinArgs)
	}
	if goBuildCmd[1] != "build" && goBuildCmd[1] != "install" {
		return nil, nil, ex.Newf("must be go build/install, got %s", goBuildCmd[1])
	}

	// Create a build plan log file in the temporary directory
	buildPlanLog, err := os.Create(util.GetBuildTemp(buildPlanLogName))
	if err != nil {
		return nil, nil, ex.Wrapf(err, "failed to create build plan log file")
	}
	defer buildPlanLog.Close()
	// The full build command is: "go build/install -a -x -n  {...}"
	args := []string{}
	args = append(args, goBuildCmd[:goBuildMinArgs]...) // go build/install
	args = append(args, []string{"-a", "-x", "-n"}...)  // -a -x -n
	if len(goBuildCmd) > goBuildMinArgs {               // {...} remaining
		args = append(args, goBuildCmd[goBuildMinArgs:]...)
	}
	sp.Info("New build command", "new", args, "old", goBuildCmd)

	//nolint:gosec // Command arguments are validated with above assertions
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
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
		// Read the build plan log to see what went wrong
		_, _ = buildPlanLog.Seek(0, 0)
		logContent, _ := os.ReadFile(util.GetBuildTemp(buildPlanLogName))
		return nil, nil, ex.Wrapf(err, "failed to run build plan: \n%s", string(logContent))
	}

	cgoObjDirs, err := findCgoObjDirs(buildPlanLog)
	if err != nil {
		return nil, nil, err
	}
	sp.Debug("Found CGO object directories", "mappings", cgoObjDirs)

	// Find compile commands from build plan log
	compileCmds, err := findCompileCommands(buildPlanLog)
	if err != nil {
		return nil, nil, err
	}
	sp.Debug("Found compile commands", "compileCmds", compileCmds)
	return compileCmds, cgoObjDirs, nil
}

var versionRegexp = regexp.MustCompile(`@v\d+\.\d+\.\d+(-.*?)?/`)

func findModVersion(path string) string {
	path = filepath.ToSlash(path) // Unify the path to Unix style
	version := versionRegexp.FindString(path)
	if version == "" {
		return ""
	}
	// Extract version number from the string
	return version[1 : len(version)-1]
}

// findDeps finds the dependencies of the project by listing the build plan.
func (sp *SetupPhase) findDeps(ctx context.Context, goBuildCmd []string) ([]*Dependency, error) {
	buildPlan, cgoObjDirs, err := sp.listBuildPlan(ctx, goBuildCmd)
	if err != nil {
		return nil, err
	}
	// import path -> list of go files
	deps := make([]*Dependency, 0)
	for _, plan := range buildPlan {
		util.Assert(util.IsCompileCommand(plan), "must be compile command")
		dep := &Dependency{
			ImportPath: "",
			Sources:    make([]string, 0),
			CgoFiles:   make(map[string]string),
		}

		// Find the compiling package name as dependency import path
		args := util.SplitCompileCmds(plan)
		importPath := util.FindFlagValue(args, "-p")
		util.Assert(importPath != "", "import path is empty")
		dep.ImportPath = importPath

		// Find the go files belong to the package as dependency sources
		for _, arg := range args {
			// Skip non-go files
			if !util.IsGoFile(arg) {
				continue
			}
			// This is a generated file during compilation
			if !util.PathExists(arg) {
				// Get the object directory from the CGO file path (e.g., $WORK/b001)
				objDir := filepath.ToSlash(filepath.Dir(arg))
				sourceDir, ok := cgoObjDirs[objDir]
				if !ok {
					// Not a CGO file from a known package, skip
					sp.Debug("Skip generated file - unknown objdir", "file", arg, "objDir", objDir)
					continue
				}
				originalAbsFile, err1 := util.ResolveCgoFile(arg, sourceDir)
				if err1 != nil {
					// Skip non-CGO generated files (_cgo_gotypes.go, _cgo_import.go, ...)
					sp.Debug("Skip generated file", "file", arg, "error", err1)
					continue
				}
				dep.CgoFiles[originalAbsFile] = filepath.Base(arg)
				dep.Sources = append(dep.Sources, originalAbsFile)
				sp.Info("Resolved CGO source", "cgo", arg, "original", originalAbsFile)
				continue
			}
			abs, err1 := filepath.Abs(arg)
			if err1 != nil {
				return nil, ex.Wrap(err1)
			}
			dep.Sources = append(dep.Sources, abs)
		}
		// Extract the version from the source file path if available
		if len(dep.Sources) > 0 {
			dep.Version = findModVersion(dep.Sources[0])
		}

		deps = append(deps, dep)
		sp.Info("Found dependency", "dep", dep)
	}
	return deps, nil
}
