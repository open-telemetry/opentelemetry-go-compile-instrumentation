// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"
)

func TestLabelForTitle(t *testing.T) {
	tests := []struct {
		name         string
		title        string
		wantLabel    string
		wantBreaking bool
		wantErr      error
	}{
		{
			name:      "chore",
			title:     "chore: update dependencies",
			wantLabel: "scope:chore",
		},
		{
			name:      "doc",
			title:     "doc: fix typo in readme",
			wantLabel: "scope:chore",
		},
		{
			name:      "docs",
			title:     "docs(cli): document flags",
			wantLabel: "scope:chore",
		},
		{
			name:      "feat",
			title:     "feat: add new option",
			wantLabel: "scope:feat",
		},
		{
			name:      "fix",
			title:     "fix(hook): handle nil pointer",
			wantLabel: "scope:fix",
		},
		{
			name:      "perf",
			title:     "perf: reduce allocations in parser",
			wantLabel: "scope:perf",
		},
		{
			name:      "perf with scope",
			title:     "perf(ast): cache parsed files",
			wantLabel: "scope:perf",
		},
		{
			name:         "perf breaking",
			title:        "perf!: switch to streaming encoder",
			wantLabel:    "scope:perf",
			wantBreaking: true,
		},
		{
			name:         "perf with scope and breaking",
			title:        "perf(cache)!: change cache key format",
			wantLabel:    "scope:perf",
			wantBreaking: true,
		},
		{
			name:      "release",
			title:     "release: v0.1.0",
			wantLabel: "scope:chore",
		},
		{
			name:      "refactor",
			title:     "refactor: simplify loop",
			wantLabel: "scope:refactor",
		},
		{
			name:      "test",
			title:     "test: add unit tests",
			wantLabel: "scope:chore",
		},
		{
			name:    "unsupported type",
			title:   "build: update makefile",
			wantErr: InvalidFormatError{},
		},
		{
			name:    "invalid format",
			title:   "not a conventional commit",
			wantErr: InvalidFormatError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, breaking, err := labelForTitle(tt.title)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("labelForTitle(%q) error = %v, want %v", tt.title, err, tt.wantErr)
			}
			if label != tt.wantLabel {
				t.Errorf("labelForTitle(%q) label = %q, want %q", tt.title, label, tt.wantLabel)
			}
			if breaking != tt.wantBreaking {
				t.Errorf("labelForTitle(%q) breaking = %v, want %v", tt.title, breaking, tt.wantBreaking)
			}
		})
	}
}
