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
	programPath := filepath.Join(t.TempDir(), "check_env.go")
	err := os.WriteFile(programPath, []byte(`package main

import "os"

func main() {
	if os.Getenv("TEST_VAR") == "test_value" {
		os.Exit(0)
	}
	os.Exit(1)
}
`), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test program: %v", err)
	}

	t.Run("passes environment variable to subprocess", func(t *testing.T) {
		env := append(os.Environ(), "TEST_VAR=test_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err != nil {
			t.Errorf("Expected success when TEST_VAR is set, got: %v", err)
		}
	})

	t.Run("fails when required variable is missing", func(t *testing.T) {
		env := append(os.Environ(), "OTHER_VAR=other_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err == nil {
			t.Error("Expected failure when TEST_VAR is not set")
		}
	})

	t.Run("works with multiple environment variables", func(t *testing.T) {
		env := append(os.Environ(), "TEST_VAR=test_value", "OTHER_VAR=other_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err != nil {
			t.Errorf("Expected success with multiple env vars, got: %v", err)
		}
	})
}

func TestRunCmdInDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	tests := []struct {
		name      string
		dir       string
		expectErr bool
	}{
		{
			name:      "run command in valid directory",
			dir:       tmpDir,
			expectErr: false,
		},
		{
			name:      "run command in subdirectory",
			dir:       subDir,
			expectErr: false,
		},
		{
			name:      "run command in nonexistent directory",
			dir:       filepath.Join(tmpDir, "nonexistent"),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmdInDir(t.Context(), tt.dir, "go", "version")
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
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get current working directory: %v", err)
		}
		dir := filepath.Join(cwd, "nonexistent", "dir")
		err = RunCmdInDir(t.Context(), dir, "go", "version")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, dir) {
			t.Errorf("Error message should contain directory %q, got: %s", dir, errMsg)
		}
	})
}
