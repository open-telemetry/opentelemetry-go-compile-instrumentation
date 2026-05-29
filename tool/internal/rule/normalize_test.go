// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]any
		want   []map[string]any
	}{
		{
			name: "passthrough no where no do",
			fields: map[string]any{
				"target": "database/sql",
				"func":   "Open",
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
				},
			},
		},
		{
			name: "where hoisting func",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"before": "BeforeOpen",
				},
			},
		},
		{
			name: "where with recv",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
					"recv": "*DB",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"recv":   "*DB",
					"before": "BeforeOpen",
				},
			},
		},
		{
			name: "where hoists func signature selectors",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
					"signature": map[string]any{
						"args": []any{"context.Context"},
					},
					"signature_contains": map[string]any{
						"returns": []any{"error"},
					},
					"result":      "io.Reader",
					"last_result": "error",
					"param":       "context.Context",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
						"path":   "example.com/hooks",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"signature": map[string]any{
						"args": []any{"context.Context"},
					},
					"signature_contains": map[string]any{
						"returns": []any{"error"},
					},
					"result":      "io.Reader",
					"last_result": "error",
					"param":       "context.Context",
					"before":      "BeforeOpen",
					"path":        "example.com/hooks",
				},
			},
		},
		{
			name: "where.file preserved nested",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
					"file": map[string]any{
						"has_func": "init",
					},
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"where": map[string]any{
						"file": map[string]any{
							"has_func": "init",
						},
					},
					"before": "BeforeOpen",
				},
			},
		},
		{
			name: "where.file with has_recv",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
					"file": map[string]any{
						"has_func": "init",
						"has_recv": "*DB",
					},
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"where": map[string]any{
						"file": map[string]any{
							"has_func": "init",
							"has_recv": "*DB",
						},
					},
					"before": "BeforeOpen",
				},
			},
		},
		{
			name: "where all-of preserved nested",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"all-of": []any{
						map[string]any{"func": "Open"},
						map[string]any{"func": "Close"},
					},
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"where": map[string]any{
						"all-of": []any{
							map[string]any{"func": "Open"},
							map[string]any{"func": "Close"},
						},
					},
					"before": "BeforeOpen",
				},
			},
		},
		{
			name: "do single modifier as map",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
						"path":   "github.com/example/sql",
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"before": "BeforeOpen",
					"path":   "github.com/example/sql",
				},
			},
		},
		{
			name: "do sequence with multiple modifiers",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": []any{
					map[string]any{
						"inject_hooks": map[string]any{
							"before": "BeforeOpen",
						},
					},
					map[string]any{
						"wrap_call": map[string]any{
							"call": "OpenWrapper",
						},
					},
				},
			},
			want: []map[string]any{
				{
					"target": "database/sql",
					"func":   "Open",
					"before": "BeforeOpen",
				},
				{
					"target": "database/sql",
					"func":   "Open",
					"call":   "OpenWrapper",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rule.Normalize(tt.fields)
			if err != nil {
				t.Fatalf("Normalize(%v) error = %v, want nil", tt.fields, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Normalize(%v) mismatch (-want +got):\n%s", tt.fields, diff)
			}
		})
	}
}

func TestNormalize_Errors(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]any
	}{
		{
			name: "where not a map",
			fields: map[string]any{
				"target": "database/sql",
				"where":  "not-a-map",
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
		},
		{
			name: "do not map or sequence",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": 42,
			},
		},
		{
			name: "do modifier payload not a map",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": map[string]any{
					"inject_hooks": "not-a-map",
				},
			},
		},
		{
			name: "where missing do",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
			},
		},
		{
			name: "target inside where rejected",
			fields: map[string]any{
				"where": map[string]any{
					"target": "database/sql",
					"func":   "Open",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
		},
		{
			name: "version inside where rejected",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"version": "v1.0.0",
					"func":    "Open",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
		},
		{
			name: "unsupported where key",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"bogus": "value",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
		},
		{
			name: "where.file not a map",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"file": "not-a-map",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeOpen",
					},
				},
			},
		},
		{
			name: "do sequence with empty list",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": []any{},
			},
		},
		{
			name: "do sequence item not a single-key map",
			fields: map[string]any{
				"target": "database/sql",
				"where": map[string]any{
					"func": "Open",
				},
				"do": []any{
					map[string]any{
						"inject_hooks": map[string]any{"before": "X"},
						"wrap_call":    map[string]any{"call": "Y"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := rule.Normalize(tt.fields); err == nil {
				t.Fatalf("Normalize(%v) error = nil, want error", tt.fields)
			}
		})
	}
}
