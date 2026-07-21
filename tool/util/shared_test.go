// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"
)

func TestValidateVersionRange(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		expectError bool
	}{
		{name: "empty matches all", version: "", expectError: false},
		{name: "minimum version", version: "v1.0.0", expectError: false},
		{name: "valid range", version: "v1.0.0,v2.0.0", expectError: false},
		{name: "trailing comma", version: "v1.0.0,", expectError: true},
		{name: "missing lower bound", version: ",v2.0.0", expectError: true},
		{name: "extra comma", version: "v1.0.0,v2.0.0,v3.0.0", expectError: true},
		{name: "start not less than end", version: "v2.0.0,v1.0.0", expectError: true},
		{name: "invalid semver", version: "not-a-version", expectError: true},
		{name: "invalid start semver", version: "bad,v2.0.0", expectError: true},
		{name: "invalid end semver", version: "v1.0.0,bad", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVersionRange(tt.version)
			if tt.expectError && err == nil {
				t.Fatalf("ValidateVersionRange(%q) = nil, want error", tt.version)
			}
			if !tt.expectError && err != nil {
				t.Fatalf("ValidateVersionRange(%q) = %v, want nil", tt.version, err)
			}
		})
	}
}

func TestVersionInRange(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		versionRange   string
		expectedResult bool
	}{
		{
			name:           "no version range specified - always matches",
			version:        "v1.5.0",
			versionRange:   "",
			expectedResult: true,
		},
		{
			name:           "version exactly at start of range",
			version:        "v1.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version in middle of range",
			version:        "v1.5.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version just before end of range",
			version:        "v1.9.9",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version exactly at end of range - excluded",
			version:        "v2.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "version after end of range",
			version:        "v2.1.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "version before start of range",
			version:        "v0.9.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "pre-release version in range",
			version:        "v1.5.0-alpha",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "patch version in range",
			version:        "v1.5.3",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "major version jump",
			version:        "v3.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "zero major version",
			version:        "v0.5.0",
			versionRange:   "v0.1.0,v1.0.0",
			expectedResult: true,
		},
		{
			name:           "narrow version range",
			version:        "v1.2.3",
			versionRange:   "v1.2.0,v1.3.0",
			expectedResult: true,
		},
		{
			name:           "version with build metadata",
			version:        "v1.5.0+build123",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "minimal version only - good",
			version:        "v1.2.3",
			versionRange:   "v1.2.3",
			expectedResult: true,
		},
		{
			name:           "minimal version only - bad",
			version:        "v1.2.3",
			versionRange:   "v1.2.4",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VersionInRange(tt.version, tt.versionRange)
			if result != tt.expectedResult {
				t.Errorf("VersionInRange() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}
