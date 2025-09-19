// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
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
