// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"
)

func TestGetBuildTarget(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "go build with cmd/ target",
			args:     []string{"go", "build", "-a", "cmd/"},
			expected: "cmd",
		},
		{
			name:     "go build with ./cmd target",
			args:     []string{"go", "build", "-a", "./cmd"},
			expected: "cmd",
		},
		{
			name:     "go build with nested path ./app/vmctl",
			args:     []string{"go", "build", "-a", "./app/vmctl"},
			expected: "app/vmctl",
		},
		{
			name:     "go build with nested path app/vmctl/",
			args:     []string{"go", "build", "app/vmctl/"},
			expected: "app/vmctl",
		},
		{
			name:     "go build with dot target",
			args:     []string{"go", "build", "-a", "."},
			expected: "",
		},
		{
			name:     "go build without target",
			args:     []string{"go", "build", "-a"},
			expected: "",
		},
		{
			name:     "go install with cmd/ target",
			args:     []string{"go", "install", "cmd/"},
			expected: "cmd",
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: "",
		},
		{
			name:     "go build with multiple flags before target",
			args:     []string{"go", "build", "-o", "myapp", "-v", "cmd/"},
			expected: "cmd",
		},
		{
			name:     "flag with path value without package",
			args:     []string{"go", "build", "-o", "./bin/"},
			expected: "",
		},
		{
			name:     "flag with path value with package",
			args:     []string{"go", "build", "-o", "./bin/app", "./cmd/app"},
			expected: "cmd/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetBuildTarget(tt.args)
			if result != tt.expected {
				t.Errorf("GetBuildTarget(%v) = %q, want %q", tt.args, result, tt.expected)
			}
		})
	}
}
