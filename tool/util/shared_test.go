// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"
)

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

func TestValidateVersionRange(t *testing.T) {
	tests := []struct {
		name         string
		versionRange string
		wantErr      bool
	}{
		{name: "empty range", versionRange: "", wantErr: false},
		{name: "single lower bound", versionRange: "v1.2.3", wantErr: false},
		{name: "bounded range", versionRange: "v1.0.0,v2.0.0", wantErr: false},
		{name: "trailing comma", versionRange: "v1.0.0,", wantErr: true},
		{name: "missing lower bound", versionRange: ",v2.0.0", wantErr: true},
		{name: "extra comma", versionRange: "v1.0.0,v2.0.0,v3.0.0", wantErr: true},
		{name: "invalid single version", versionRange: "not-a-version", wantErr: true},
		{name: "invalid start bound", versionRange: "not-a-version,v2.0.0", wantErr: true},
		{name: "invalid end bound", versionRange: "v1.0.0,not-a-version", wantErr: true},
		{name: "reversed range", versionRange: "v2.0.0,v1.0.0", wantErr: true},
		{name: "equal bounds", versionRange: "v1.0.0,v1.0.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVersionRange(tt.versionRange)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateVersionRange(%q) = nil, want error", tt.versionRange)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateVersionRange(%q) = %v, want nil", tt.versionRange, err)
			}
		})
	}
}
