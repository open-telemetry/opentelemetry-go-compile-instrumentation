// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCmd(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		expectErr bool
	}{
		{
			name:      "simple echo command",
			args:      []string{"echo", "hello"},
			expectErr: false,
		},
		{
			name:      "command with multiple arguments",
			args:      []string{"echo", "hello", "world"},
			expectErr: false,
		},
		{
			name:      "nonexistent command",
			args:      []string{"nonexistent-command-xyz"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmd(t.Context(), tt.args...)
			if (err != nil) != tt.expectErr {
				t.Errorf("RunCmd() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestRunCmdWithEnv(t *testing.T) {
	if IsWindows() {
		t.Skip("Skipping test on Windows - env handling differs")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_env.sh")

	// Create a simple script that checks for an environment variable
	scriptContent := `#!/bin/sh
if [ "$TEST_VAR" = "test_value" ]; then
    exit 0
else
    exit 1
fi
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	tests := []struct {
		name      string
		env       []string
		args      []string
		expectErr bool
	}{
		{
			name:      "command with custom environment variable",
			env:       []string{"TEST_VAR=test_value"},
			args:      []string{scriptPath},
			expectErr: false,
		},
		{
			name:      "command with missing environment variable",
			env:       []string{"OTHER_VAR=other_value"},
			args:      []string{scriptPath},
			expectErr: true,
		},
		{
			name:      "command with multiple environment variables",
			env:       []string{"TEST_VAR=test_value", "OTHER_VAR=other_value"},
			args:      []string{scriptPath},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmdWithEnv(t.Context(), tt.env, tt.args...)
			if (err != nil) != tt.expectErr {
				t.Errorf("RunCmdWithEnv() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestRunCmdInDir(t *testing.T) {
	if IsWindows() {
		t.Skip("Skipping test on Windows - pwd command not available")
	}

	tmpDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	tests := []struct {
		name      string
		dir       string
		args      []string
		expectErr bool
	}{
		{
			name:      "run command in valid directory",
			dir:       tmpDir,
			args:      []string{"pwd"},
			expectErr: false,
		},
		{
			name:      "run command in subdirectory",
			dir:       subDir,
			args:      []string{"pwd"},
			expectErr: false,
		},
		{
			name:      "run command in nonexistent directory",
			dir:       filepath.Join(tmpDir, "nonexistent"),
			args:      []string{"echo", "test"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmdInDir(t.Context(), tt.dir, tt.args...)
			if (err != nil) != tt.expectErr {
				t.Errorf("RunCmdInDir() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestRunCmdErrorMessages(t *testing.T) {
	t.Run("error message includes command path", func(t *testing.T) {
		err := RunCmd(t.Context(), "nonexistent-command-xyz", "arg1", "arg2")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "nonexistent-command-xyz") {
			t.Errorf("Error message should contain command name, got: %s", errMsg)
		}
	})

	t.Run("error message includes directory for RunCmdInDir", func(t *testing.T) {
		dir := "/nonexistent/dir"
		err := RunCmdInDir(t.Context(), dir, "echo", "test")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, dir) {
			t.Errorf("Error message should contain directory %q, got: %s", dir, errMsg)
		}
	})
}
