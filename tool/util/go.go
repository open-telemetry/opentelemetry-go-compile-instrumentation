// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bufio"
	"os"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// isCompileTool checks if the tool path is the Go compile tool.
// Checks for both Unix (/compile) and Windows (compile.exe) patterns for cross-platform compatibility.
func isCompileTool(toolPath string) bool {
	return strings.HasSuffix(toolPath, "/compile") || strings.HasSuffix(toolPath, "compile.exe")
}

// isLinkTool checks if the tool path is the Go link tool.
// Checks for both Unix (/link) and Windows (link.exe) patterns for cross-platform compatibility.
func isLinkTool(toolPath string) bool {
	return strings.HasSuffix(toolPath, "/link") || strings.HasSuffix(toolPath, "link.exe")
}

// hasFlag checks if the args slice contains the specified flag.
func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

// IsCompileArgs checks if the args slice represents a compile command.
// This is preferred over IsCompileCommand when you have the args as a slice,
// as it correctly handles tool paths with spaces (common on Windows).
func IsCompileArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	// Check if the tool path is the compile tool
	if !isCompileTool(args[0]) {
		return false
	}

	// Verify it has the expected compile command flags
	requiredFlags := []string{"-o", "-p", "-buildid"}
	for _, flag := range requiredFlags {
		if !hasFlag(args, flag) {
			return false
		}
	}

	// PGO compile command is different, skip it
	if hasFlag(args, "-pgoprofile") {
		return false
	}

	return true
}

// IsLinkArgs checks if the args slice represents a link command.
func IsLinkArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	// Check if the tool path is the link tool
	if !isLinkTool(args[0]) {
		return false
	}

	// Verify it has the expected link command flags
	requiredFlags := []string{"-o", "-buildid", "-importcfg"}
	for _, flag := range requiredFlags {
		if !hasFlag(args, flag) {
			return false
		}
	}

	return true
}

// isCgoCommand checks if the line is a cgo tool invocation with -objdir and -importpath flags.
func IsCgoCommand(line string) bool {
	return strings.Contains(line, "cgo") &&
		strings.Contains(line, "-objdir") &&
		strings.Contains(line, "-importpath") &&
		!strings.Contains(line, "-dynimport")
}

// FindFlagValue finds the value of a flag in the command line.
func FindFlagValue(cmd []string, flag string) string {
	flagWithValue := flag + "="
	for i, v := range cmd {
		if v == flag {
			if i+1 < len(cmd) {
				return cmd[i+1]
			}
			return ""
		}
		if strings.HasPrefix(v, flagWithValue) {
			return strings.TrimPrefix(v, flagWithValue)
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

func IsYamlFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".yaml") ||
		strings.HasSuffix(strings.ToLower(path), ".yml")
}

func NewFileScanner(file *os.File, size int) (*bufio.Scanner, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return nil, ex.Wrapf(err, "failed to seek file")
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, size), size)
	return scanner, nil
}
