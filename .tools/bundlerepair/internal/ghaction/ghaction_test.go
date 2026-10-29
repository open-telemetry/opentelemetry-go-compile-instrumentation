// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ghaction_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/ghaction"
)

func TestSetOutput(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		want    string
		wantErr bool
	}{
		{name: "plain", key: "prs", value: "[1,2]", want: "prs=[1,2]\n"},
		{name: "empty value", key: "authorized", value: "", want: "authorized=\n"},
		{name: "equals in value", key: "k", value: "a=b", want: "k=a=b\n"},
		{name: "newline in value", key: "k", value: "a\nb=c", wantErr: true},
		{name: "carriage return in value", key: "k", value: "a\rb", wantErr: true},
		{name: "dash in key", key: "artifact-id", value: "7", want: "artifact-id=7\n"},
		{name: "digit first in key", key: "1st", value: "v", want: "1st=v\n"},
		{name: "space in key", key: "a b", value: "v", wantErr: true},
		{name: "empty key", key: "", value: "v", wantErr: true},
		{name: "equals in key", key: "a=b", value: "v", wantErr: true},
		{name: "newline in key", key: "a\nb", value: "v", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output")
			files := ghaction.Files{Output: path}
			err := files.SetOutput(tt.key, tt.value)
			got, readErr := os.ReadFile(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if readErr == nil && len(got) != 0 {
					t.Errorf("a rejected value wrote %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if readErr != nil || string(got) != tt.want {
				t.Errorf("output file = %q (%v), want %q", got, readErr, tt.want)
			}
		})
	}
}

func TestSetOutputAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, []byte("old=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := ghaction.Files{Output: path}
	for _, kv := range [][2]string{{"a", "1"}, {"b", "2"}} {
		if err := files.SetOutput(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "old=1\na=1\nb=2\n"; string(got) != want {
		t.Errorf("output file = %q, want %q", got, want)
	}
}

func TestMissingPaths(t *testing.T) {
	var files ghaction.Files
	if err := files.SetOutput("k", "v"); err == nil {
		t.Error("SetOutput accepted a missing GITHUB_OUTPUT")
	}
	if err := files.AppendSummary("line"); err == nil {
		t.Error("AppendSummary accepted a missing GITHUB_STEP_SUMMARY")
	}
}

func TestAppendSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	files := ghaction.Files{Summary: path}
	if err := files.AppendSummary("first"); err != nil {
		t.Fatal(err)
	}
	if err := files.AppendSummary("one\ntwo"); err == nil {
		t.Error("AppendSummary accepted a line break")
	}
	if err := files.AppendSummary("second"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "first\nsecond\n"; string(got) != want {
		t.Errorf("summary file = %q, want %q", got, want)
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{"GITHUB_OUTPUT": "/o", "GITHUB_STEP_SUMMARY": "/s"}
	files := ghaction.FromEnv(func(k string) string { return env[k] })
	if files.Output != "/o" || files.Summary != "/s" {
		t.Errorf("FromEnv = %+v", files)
	}
}
