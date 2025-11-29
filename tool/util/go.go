// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// IsCompileCommand checks if the line is a compile command.
func IsCompileCommand(line string) bool {
	check := []string{"-o", "-p", "-buildid"}
	if IsWindows() {
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

// FindFlagValue finds the value of a flag in the command line.
func FindFlagValue(cmd []string, flag string) string {
	for i, v := range cmd {
		if v == flag {
			return cmd[i+1]
		}
	}
	return ""
}

// SplitCompileCmds splits the command line by space, but keep the quoted part
// as a whole. For example, "a b" c will be split into ["a b", "c"].
func SplitCompileCmds(input string) []string {
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
	if IsWindows() {
		for i, arg := range args {
			args[i] = strings.ReplaceAll(arg, `\\`, `\`)
		}
	}
	return args
}

func IsGoFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".go")
}

// flagsWithPathValues contains flags that accept a directory or file path as value.
// From: go help build
//
//nolint:gochecknoglobals // private lookup table
var flagsWithPathValues = map[string]bool{
	"-o":       true,
	"-modfile": true,
	"-overlay": true,
	"-pgo":     true,
	"-pkgdir":  true,
}

// GetBuildPackages loads all packages from the go build command arguments.
// Returns a list of loaded packages. If no package patterns are found in args,
// defaults to loading the current directory package.
// The args parameter should be the go build command arguments (e.g., ["build", "-a", "./cmd"]).
// Returns an error if package loading fails or if invalid patterns are provided.
// For example:
//   - args ["build", "-a", "cmd/"] returns packages for "./cmd/"
//   - args ["build", "-a", "./app/vmctl"] returns packages for "./app/vmctl"
//   - args ["build", "-a", ".", "./cmd"] returns packages for both "." and "./cmd"
//   - args ["build"] returns packages for "."
func GetBuildPackages(args []string) ([]*packages.Package, error) {
	buildPkgs := make([]*packages.Package, 0)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule,
	}
	found := false
	for i := len(args) - 1; i >= 0; i-- {
		arg := args[i]

		// If preceded by a flag that takes a path value, this is a flag value
		// We want to avoid scenarios like "go build -o ./tmp ./app" where tmp also contains Go files,
		// as it would be treated as a package.
		if i > 0 && flagsWithPathValues[args[i-1]] {
			break
		}

		// If we hit a flag, stop. Packages come after all flags
		// go build [-o output] [build flags] [packages]
		if strings.HasPrefix(arg, "-") || arg == "go" || arg == "build" || arg == "install" {
			break
		}

		pkgs, err := packages.Load(cfg, arg)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to load packages for pattern %s", arg)
		}
		for _, pkg := range pkgs {
			if pkg.Errors != nil || pkg.Module == nil {
				continue
			}
			buildPkgs = append(buildPkgs, pkg)
			found = true
		}
	}

	if !found {
		var err error
		buildPkgs, err = packages.Load(cfg, ".")
		if err != nil {
			return nil, ex.Wrapf(err, "failed to load packages for pattern .")
		}
	}
	return buildPkgs, nil
}
