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
	if !strings.HasPrefix(strings.ToLower(line), "cd") {
		return "", false
	}
	const cdCommandSplitLimit = 2 // Split "cd dir" into [dir, rest] to ignore trailing comments
	parts := strings.SplitN(line[3:], " ", cdCommandSplitLimit)
	return strings.TrimSpace(parts[0]), true
}

// findCommands scans the build plan log and returns relevant commands
// (cd, cgo, and compile) for processing by findDeps.
func findCommands(buildPlanLog *os.File) ([]string, error) {
	scanner, err := util.NewFileScanner(buildPlanLog, maxBuildPlanBufferSize)
	if err != nil {
		return nil, err
	}

	var commands []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if util.IsWindows() {
			line = strings.ReplaceAll(line, `\\`, `\`)
		}
		line = filepath.ToSlash(line)

		if _, ok := parseCdDir(line); ok || util.IsCgoCommand(line) || util.IsCompileCommand(line) {
			commands = append(commands, line)
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, ex.Wrapf(err, "failed to parse build plan log")
	}
	return commands, nil
}

// listBuildPlan lists the build plan by running `go build/install -a -x -n`
// and then filtering the commands (cd, cgo, compile) from the build plan log.
func (sp *SetupPhase) listBuildPlan(ctx context.Context, goBuildCmd []string) ([]string, error) {
	const goBuildMinArgs = 2 // go build
	const buildPlanLogName = "build-plan.log"
	if len(goBuildCmd) < goBuildMinArgs {
		return nil, ex.Newf("at least %d arguments are required", goBuildMinArgs)
	}
	if goBuildCmd[1] != "build" && goBuildCmd[1] != "install" {
		return nil, ex.Newf("must be go build/install, got %s", goBuildCmd[1])
	}

	// Create a build plan log file in the temporary directory
	buildPlanLog, err := os.Create(util.GetBuildTemp(buildPlanLogName))
	if err != nil {
		return nil, ex.Wrapf(err, "failed to create build plan log file")
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
		return nil, ex.Wrapf(err, "failed to run build plan: \n%s", string(logContent))
	}

	// Find compile commands from build plan log
	compileCmds, err := findCommands(buildPlanLog)
	if err != nil {
		return nil, err
	}
	sp.Debug("Found compile commands", "compileCmds", compileCmds)
	return compileCmds, nil
}

const (
	cgoSuffix = ".cgo1.go"
	goSuffix  = ".go"
)

// resolveCgoFile maps a CGO-generated file back to its original source.
func resolveCgoFile(cgoFile, sourceDir string) (string, error) {
	if cgoFile == "" || sourceDir == "" {
		return "", ex.Newf("cgoFile and sourceDir cannot be empty, cgoFile: %q, sourceDir: %q", cgoFile, sourceDir)
	}
	baseName := filepath.Base(cgoFile)
	if !strings.HasSuffix(baseName, cgoSuffix) {
		return "", ex.Newf("file %s is not a CGO (%s) generated file", cgoFile, cgoSuffix)
	}
	originalBase := strings.TrimSuffix(baseName, cgoSuffix) + goSuffix
	abs := filepath.Join(sourceDir, originalBase)
	if !util.PathExists(abs) {
		return "", ex.Newf("file %s does not exist", abs)
	}
	return abs, nil
}

var versionRegexp = regexp.MustCompile(`@v\d+\.\d+\.\d+(-.*?)?/`)

func findModVersion(path string) string {
	version := versionRegexp.FindString(filepath.ToSlash(path))
	if version == "" {
		return ""
	}
	return version[1 : len(version)-1]
}

// findGoSources extracts Go source files from compile command arguments,
// resolving CGO files using the provided objDir->sourceDir mapping.
func findGoSources(sp *SetupPhase, args []string, cgoObjDirs map[string]string) (*Dependency, error) {
	dep := &Dependency{
		ImportPath: util.FindFlagValue(args, "-p"),
		Sources:    make([]string, 0),
		CgoFiles:   make(map[string]string),
	}
	util.Assert(dep.ImportPath != "", "import path is empty")

	// Find the go files belong to the package as dependency sources
	for _, arg := range args {
		if !util.IsGoFile(arg) {
			continue
		}
		if !util.PathExists(arg) {
			// Try to resolve as CGO generated file
			objDir := util.NormalizePath(filepath.Dir(arg))
			sourceDir, ok := cgoObjDirs[objDir]
			if !ok {
				sp.Debug("Skip generated file - unknown objdir", "file", arg, "objDir", objDir)
				continue
			}
			originalAbsFile, err := resolveCgoFile(arg, sourceDir)
			if err != nil {
				sp.Debug("Skip generated file", "file", arg, "error", err)
				continue
			}
			dep.CgoFiles[originalAbsFile] = filepath.Base(arg)
			dep.Sources = append(dep.Sources, originalAbsFile)
			sp.Info("Resolved CGO source", "cgo", arg, "original", originalAbsFile)
			continue
		}

		// This is a Go source file, add it to the dependency sources
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to get absolute path of source file %s", arg)
		}
		dep.Sources = append(dep.Sources, abs)
	}
	// We need to skip it as it is not part of the instrumentation target
	if len(dep.Sources) > 0 {
		dep.Version = findModVersion(dep.Sources[0])
	}
	return dep, nil
}

// findDeps finds dependencies by listing the build plan.
func (sp *SetupPhase) findDeps(ctx context.Context, goBuildCmd []string) ([]*Dependency, error) {
	buildPlan, err := sp.listBuildPlan(ctx, goBuildCmd)
	if err != nil {
		return nil, err
	}

	var (
		deps       []*Dependency
		cgoObjDirs = make(map[string]string)
		currentDir string
	)

	for _, cmd := range buildPlan {
		if dir, ok := parseCdDir(cmd); ok {
			currentDir = dir
			continue
		}

		if util.IsCompileCommand(cmd) {
			args := util.SplitCompileCmds(cmd)
			dep, err1 := findGoSources(sp, args, cgoObjDirs)
			if err1 != nil {
				return nil, err1
			}
			deps = append(deps, dep)
			sp.Info("Found dependency", "dep", dep)
		} else if util.IsCgoCommand(cmd) && currentDir != "" {
			args := util.SplitCompileCmds(cmd)
			objDir := util.FindFlagValue(args, "-objdir")
			util.Assert(objDir != "", "sanity check")
			cgoObjDirs[util.NormalizePath(objDir)] = currentDir
			sp.Debug("Found CGO objdir mapping", "objDir", objDir, "sourceDir", currentDir)
		}
	}
	return deps, nil
}
