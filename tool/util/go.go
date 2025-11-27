// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"path"
	"strings"

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
//nolint:gochecknoglobals // constant lookup table
var flagsWithPathValues = map[string]bool{
	"-o":       true,
	"-modfile": true,
	"-overlay": true,
	"-pgo":     true,
	"-pkgdir":  true,
}

// GetBuildTarget extracts the build target path from the go build command.
// For example:
//   - "go build -a cmd/" returns "cmd"
//   - "go build -a ./app/vmctl" returns "app/vmctl"
//   - "go build -a ." returns ""
//   - "go build" returns ""
//   - "go build -o ./bin/" returns ""
func GetBuildTarget(goBuildCmd []string) string {
	for i := len(goBuildCmd) - 1; i >= 0; i-- {
		arg := goBuildCmd[i]

		// If preceded by a flag that takes a path value, this is a flag value
		if i > 0 && flagsWithPathValues[goBuildCmd[i-1]] {
			break
		}

		// If we hit a flag, stop - packages come after all flags
		// go build [-o output] [build flags] [packages]
		if strings.HasPrefix(arg, "-") {
			break
		}

		if arg == "go" || arg == "build" {
			continue
		}

		if target := path.Clean(arg); target != "." {
			return target
		}
		return ""
	}

	return ""
}
