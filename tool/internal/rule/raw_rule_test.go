// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstRawRule(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		ruleID  string
		wantErr bool
		check   func(*testing.T, *InstRawRule)
	}{
		{
			name:   "minimal valid rule",
			ruleID: "raw1",
			yaml: `
target: main
func: Bar
raw: println("hi")
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "raw1", r.Name)
				assert.Equal(t, "main", r.Target)
				assert.Equal(t, "Bar", r.Func)
				assert.Equal(t, `println("hi")`, r.Raw)
			},
		},
		{
			name:   "explicit name is preserved",
			ruleID: "fallback",
			yaml: `
name: explicit
target: main
func: Bar
raw: println()
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "explicit", r.Name)
			},
		},
		{
			name:   "valid placement before with pattern",
			ruleID: "raw2",
			yaml: `
target: main
func: Bar
recv: "*Recv"
raw: println()
pattern: "^name := getName\\(\\)$"
placement: before
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "*Recv", r.Recv)
				assert.Equal(t, "before", r.Placement)
				assert.Equal(t, `^name := getName\(\)$`, r.Pattern)
			},
		},
		{
			name:   "valid placement after",
			ruleID: "raw3",
			yaml: `
target: main
func: Bar
raw: println()
placement: after
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "after", r.Placement)
			},
		},
		{
			name:    "empty raw is rejected",
			ruleID:  "raw4",
			yaml:    "target: main\nfunc: Bar\nraw: \"   \"",
			wantErr: true,
		},
		{
			name:    "missing raw is rejected",
			ruleID:  "raw5",
			yaml:    "target: main\nfunc: Bar",
			wantErr: true,
		},
		{
			name:    "invalid regex pattern is rejected",
			ruleID:  "raw6",
			yaml:    "target: main\nfunc: Bar\nraw: println()\npattern: \"(unclosed\"",
			wantErr: true,
		},
		{
			name:    "invalid placement value is rejected",
			ruleID:  "raw7",
			yaml:    "target: main\nfunc: Bar\nraw: println()\nplacement: sideways",
			wantErr: true,
		},
		{
			name:    "malformed yaml is rejected",
			ruleID:  "raw8",
			yaml:    "target: main\n  bad: [unterminated",
			wantErr: true,
		},
		{
			name:   "valid raw with template tag",
			ruleID: "templated-raw",
			yaml: `
target: main
func: Bar
raw: "println({{ .FuncArgument 0 }})"
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, `println({{ .FuncArgument 0 }})`, r.Raw)
			},
		},
		{
			name:    "invalid template syntax in raw is rejected",
			ruleID:  "bad-template-raw",
			yaml:    "target: main\nfunc: Bar\nraw: \"println({{ FuncArgument 0 )\"",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstRawRule([]byte(tt.yaml), tt.ruleID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}

func rawIdentity(t *testing.T, name string, flat map[string]any) string {
	t.Helper()
	data, err := yaml.Marshal(flat)
	require.NoError(t, err)
	r, err := NewInstRawRule(data, name)
	require.NoError(t, err)
	return r.Identity()
}

func TestInstRawRule_Identity(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"target": "main", "func": "Bar", "raw": `println("hi")`}
	}

	// De-duplication: identical content under different names is one identity.
	dupA := base()
	dupB := base()
	assert.Equal(t, rawIdentity(t, "alpha", dupA), rawIdentity(t, "beta", dupB),
		"identical rule content must share an identity regardless of name")

	// Differing target yields a distinct identity.
	diffTarget := base()
	diffTarget["target"] = "github.com/example/lib"
	assert.NotEqual(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffTarget),
		"rules differing only in target must have distinct identities")

	// Differing version yields a distinct identity.
	diffVersion := base()
	diffVersion["version"] = "v1.0.0,v2.0.0"
	assert.NotEqual(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffVersion),
		"rules differing only in version must have distinct identities")

	// Differing func yields a distinct identity.
	diffFunc := base()
	diffFunc["func"] = "Baz"
	assert.NotEqual(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffFunc),
		"rules differing only in func must have distinct identities")

	// Differing recv yields a distinct identity.
	diffRecv := base()
	diffRecv["recv"] = "*Recv"
	assert.NotEqual(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffRecv),
		"rules differing only in recv must have distinct identities")

	// Differing raw yields a distinct identity.
	diffRaw := base()
	diffRaw["raw"] = `println("bye")`
	assert.NotEqual(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffRaw),
		"rules differing only in raw must have distinct identities")

	// The length-prefixed encoding must not let field boundaries shift: moving
	// characters from func into recv (same concatenation, different split)
	// must not collide.
	shiftedA := map[string]any{"target": "main", "func": "Foo", "recv": "Bar", "raw": `println("hi")`}
	shiftedB := map[string]any{"target": "main", "func": "FooB", "recv": "ar", "raw": `println("hi")`}
	assert.NotEqual(t, rawIdentity(t, "r", shiftedA), rawIdentity(t, "r", shiftedB),
		"field encoding must be unambiguous across a func/recv boundary shift")

	// Pattern and placement do not affect identity: they govern where the
	// same rendered code is inserted, not what the rule does or what names it
	// generates.
	diffPositional := base()
	diffPositional["pattern"] = "^x := 1$"
	diffPositional["placement"] = "after"
	assert.Equal(t, rawIdentity(t, "r", base()), rawIdentity(t, "r", diffPositional),
		"rules differing only in pattern/placement must share an identity")
}
