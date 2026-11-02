// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no command", nil, exitUsage, "", "Usage: bundlerepair"},
		{"unknown command", []string{"deploy"}, exitUsage, "", `unknown command "deploy"`},
		{"unknown command with a flag", []string{"--bogus"}, exitUsage, "", "Usage: bundlerepair"},
		{"empty command", []string{""}, exitUsage, "", "Usage: bundlerepair"},
		{"command names are case sensitive", []string{"Stage"}, exitUsage, "", "unknown command"},
		{"help", []string{"help"}, 0, "Usage: bundlerepair", ""},
		{"dash help", []string{"-h"}, 0, "Usage: bundlerepair", ""},
		{"stage failure uses code 1", []string{"stage"}, exitFailure, "", "bundlerepair stage:"},
		{"merge failure uses code 1", []string{"merge"}, exitFailure, "", "bundlerepair merge:"},
		{
			"authorize failure uses code 1",
			[]string{"authorize"},
			exitFailure,
			"",
			"bundlerepair authorize: REPO must be set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runApp(t, nil, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(out.stdout, tt.wantStdout) || (tt.wantStdout == "" && out.stdout != "") {
				t.Errorf("stdout = %q, want %q", out.stdout, tt.wantStdout)
			}
			if !strings.Contains(out.stderr, tt.wantStderr) || (tt.wantStderr == "" && out.stderr != "") {
				t.Errorf("stderr = %q, want %q", out.stderr, tt.wantStderr)
			}
		})
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	for _, command := range []string{"authorize", "stage", "merge"} {
		if !strings.Contains(usageText, command) {
			t.Errorf("usage does not list %s", command)
		}
	}
	for _, command := range []string{"select", "prepare", "publish", "identity", "credentials"} {
		if !strings.Contains(usageText, command) {
			t.Errorf("usage does not list %s", command)
		}
	}
}
