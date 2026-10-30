// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/build"
	"go/build/constraint"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchFileConstraints(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		tags     []string
		goos     string
		goarch   string
		expected bool
	}{
		{
			name:     "plain ignore constraint matches",
			content:  "//go:build ignore\n\npackage main\n",
			expected: true,
		},
		{
			name:     "ignore combined with current OS matches",
			content:  "//go:build ignore && linux\n\npackage main\n",
			goos:     "linux",
			expected: true,
		},
		{
			name:     "ignore combined with other OS does not match",
			content:  "//go:build ignore && windows\n\npackage main\n",
			goos:     "linux",
			expected: false,
		},
		{
			name:     "ignore combined with matching custom tag matches",
			content:  "//go:build ignore && mytag\n\npackage main\n",
			tags:     []string{"mytag"},
			expected: true,
		},
		{
			name:     "ignore combined with non-matching custom tag does not match",
			content:  "//go:build ignore && mytag\n\npackage main\n",
			tags:     []string{"othertag"},
			expected: false,
		},
		{
			name:     "legacy plus build ignore matches",
			content:  "// +build ignore\n\npackage main\n",
			expected: true,
		},
		{
			name:     "no build constraints matches",
			content:  "package main\n\nfunc main() {}\n",
			expected: true,
		},
		{
			name:     "negated ignore is always false without tags",
			content:  "//go:build !ignore\n\npackage main\n",
			expected: false,
		},
		{
			name:     "negated ignore remains false even when ignore tag is explicitly set",
			content:  "//go:build !ignore\n\npackage main\n",
			tags:     []string{"ignore"},
			expected: false,
		},
		{
			name:     "contradictory ignore and negated ignore remains false with ignore tag",
			content:  "//go:build ignore && !ignore\n\npackage main\n",
			tags:     []string{"ignore"},
			expected: false,
		},
		{
			name:     "negated ignore with matching OS remains false with ignore tag",
			content:  "//go:build !ignore && linux\n\npackage main\n",
			goos:     "linux",
			tags:     []string{"ignore"},
			expected: false,
		},
		{
			name:     "negated ignore or windows matches on windows with ignore tag",
			content:  "//go:build !ignore || windows\n\npackage main\n",
			goos:     "windows",
			tags:     []string{"ignore"},
			expected: true,
		},
		{
			name:     "negated ignore or windows does not match on linux with ignore tag",
			content:  "//go:build !ignore || windows\n\npackage main\n",
			goos:     "linux",
			tags:     []string{"ignore"},
			expected: false,
		},
		{
			name:     "legacy plus build negated ignore remains false with ignore tag",
			content:  "// +build !ignore\n\npackage main\n",
			tags:     []string{"ignore"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bctx := build.Default
			if tt.goos != "" {
				bctx.GOOS = tt.goos
			}
			if tt.goarch != "" {
				bctx.GOARCH = tt.goarch
			}
			if len(tt.tags) > 0 {
				bctx.BuildTags = append(bctx.BuildTags, tt.tags...)
			}

			matched, err := MatchFileConstraints(&bctx, t.TempDir(), "file.go", []byte(tt.content))
			require.NoError(t, err)
			assert.Equal(t, tt.expected, matched)
		})
	}

	t.Run("nil bctx defaults to build.Default", func(t *testing.T) {
		matched, err := MatchFileConstraints(nil, t.TempDir(), "file.go", []byte("//go:build ignore\n\npackage main\n"))
		require.NoError(t, err)
		assert.True(t, matched)
	})

	t.Run("invalid build constraint returns error", func(t *testing.T) {
		_, err := MatchFileConstraints(nil, t.TempDir(), "file.go", []byte("//go:build (&& foo\n\npackage main\n"))
		require.Error(t, err)
	})
}

func TestStripBuildIgnoreTag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "strips go:build ignore comment",
			input: `//go:build ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "no go:build ignore comment - content unchanged",
			input: `package main

func main() {}
`,
			expected: `package main

func main() {}
`,
		},
		{
			name: "multiple go:build ignore comments",
			input: `//go:build ignore
package main
//go:build ignore
func main() {}
`,
			expected: `
package main

func main() {}
`,
		},
		{
			name:     "empty file",
			input:    "",
			expected: "",
		},
		{
			// Regression test for #1069: a whole-file substring replace
			// deleted this text from the string literal and the comment,
			// since both happen to contain "//go:build ignore" without being
			// a build-constraint line themselves.
			name: "preserves the tag text inside a string literal and comment prose",
			input: `//go:build ignore

package hooks

// Every file rule source must carry //go:build ignore at the top.
const marker = "//go:build ignore"
`,
			expected: `

package hooks

// Every file rule source must carry //go:build ignore at the top.
const marker = "//go:build ignore"
`,
		},
		{
			name: "strips build ignore while preserving platform tag in combined directive",
			input: `//go:build ignore && linux

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "strips build ignore while preserving platform tag in reversed combined directive",
			input: `//go:build linux && ignore

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "preserves standalone platform directive without ignore",
			input: `//go:build linux

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore directive",
			input: `// +build ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore directive while preserving legacy platform directive",
			input: `// +build ignore
// +build darwin linux

package main

func main() {}
`,
			expected: `
// +build darwin linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore while preserving legacy combined directive with comma",
			input: `// +build ignore,linux

package main

func main() {}
`,
			expected: `// +build linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore while preserving legacy combined directive with space",
			input: `// +build ignore linux

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "strips ignore from compound modern build directive containing ignore",
			input: `//go:build ignore && (darwin || linux)

package main

func main() {}
`,
			expected: `//go:build darwin || linux

package main

func main() {}
`,
		},
		{
			name: "preserves compound modern build directive without ignore",
			input: `//go:build darwin || linux

package main

func main() {}
`,
			expected: `//go:build darwin || linux

package main

func main() {}
`,
		},
		{
			name: "malformed build directive is not treated as ignore and left unchanged",
			input: `//go:build (invalid &&
package main
`,
			expected: `//go:build (invalid &&
package main
`,
		},
		{
			name: "strips ignore from compound directive with negated platform tag and ignore",
			input: `//go:build !windows && ignore

package main

func main() {}
`,
			expected: `//go:build !windows

package main

func main() {}
`,
		},
		{
			name: "preserves directive with negated platform tag without ignore",
			input: `//go:build !windows

package main

func main() {}
`,
			expected: `//go:build !windows

package main

func main() {}
`,
		},
		{
			name: "strips directive with negated ignore tag",
			input: `//go:build !ignore

package main

func main() {}
`,
			expected: `//go:build ignore && !ignore

package main

func main() {}
`,
		},
		{
			name: "evaluates !ignore || windows correctly",
			input: `//go:build !ignore || windows

package main

func main() {}
`,
			expected: `//go:build windows

package main

func main() {}
`,
		},
		{
			name: "evaluates !ignore && windows correctly",
			input: `//go:build !ignore && windows

package main

func main() {}
`,
			expected: `//go:build ignore && !ignore

package main

func main() {}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, StripBuildIgnoreTag(tt.input))
		})
	}
}

type dummyConstraintExpr struct {
	constraint.Expr
}

func TestRemoveIgnore_Fallback(t *testing.T) {
	dummy := &dummyConstraintExpr{}
	assert.Equal(t, dummy, RemoveIgnore(dummy))
	assert.Nil(t, RemoveIgnore(nil))
}

func TestStripDirectiveIgnore_BooleanSimplification(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ignore && ignore is always true",
			input:    "//go:build ignore && ignore",
			expected: "",
		},
		{
			name:     "X && !ignore is always false",
			input:    "//go:build linux && !ignore",
			expected: "//go:build ignore && !ignore",
		},
		{
			name:     "keeps remaining AND terms",
			input:    "//go:build ignore && linux && amd64",
			expected: "//go:build linux && amd64",
		},
		{
			name:     "ignore || !ignore is always true",
			input:    "//go:build ignore || !ignore",
			expected: "",
		},
		{
			name:     "X || ignore is always true",
			input:    "//go:build linux || ignore",
			expected: "",
		},
		{
			name:     "X || !ignore keeps X",
			input:    "//go:build linux || !ignore",
			expected: "//go:build linux",
		},
		{
			name:     "legacy plus build negated ignore is always false",
			input:    "// +build !ignore",
			expected: "// +build ignore,!ignore",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, StripDirectiveIgnore(tt.input))
		})
	}
}
