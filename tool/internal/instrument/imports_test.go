// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestHandleRuleImports_AliasMismatch(t *testing.T) {
	tests := []struct {
		name        string
		root        *dst.File
		imports     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "alias mismatch - file uses ctx but rule expects context",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("ctx"),
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"context": "context"},
			expectError: true,
			errorMsg:    "import alias mismatch",
		},
		{
			name: "implicit alias mismatch - file uses context but rule expects ctx",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"context"`}, // implicit alias "context"
							},
						},
					},
				},
			},
			imports:     map[string]string{"ctx": "context"},
			expectError: true,
			errorMsg:    "import alias mismatch",
		},
		{
			name: "gopkg.in style path - no mismatch for implicit alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								// gopkg.in/yaml.v3 declares "package yaml" - resolvePackageName correctly returns "yaml"
								Path: &dst.BasicLit{Value: `"gopkg.in/yaml.v3"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"yaml": "gopkg.in/yaml.v3"},
			expectError: false, // No error - implicit alias matches inferred package name
		},
		{
			name: "no mismatch - aliases match",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("ctx"),
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"ctx": "context"},
			expectError: false,
		},
		{
			name: "no mismatch - default alias matches rule",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"context": "context"},
			expectError: false,
		},
		{
			name: "blank imports are not checked for alias mismatch",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("_"),
								Path: &dst.BasicLit{Value: `"net/http/pprof"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"_": "net/http/pprof"},
			expectError: false,
		},
		{
			name:        "new import - no mismatch possible",
			root:        &dst.File{},
			imports:     map[string]string{"ctx": "context"},
			expectError: false, // Would fail later at importcfg resolution, not alias check
		},
		{
			name: "dot-import conflict - file uses explicit alias but rule requires dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("runtime"),
								Path: &dst.BasicLit{Value: `"runtime"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: true,
			errorMsg:    "dot-import conflict",
		},
		{
			name: "dot-import conflict - file uses implicit alias but rule requires dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"runtime"`}, // implicit alias "runtime"
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: true,
			errorMsg:    "dot-import conflict",
		},
		{
			name: "dot-import no conflict - file already uses dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("."),
								Path: &dst.BasicLit{Value: `"runtime"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: false, // Both file and rule use dot-import
		},
		{
			name: "dot-import no conflict - path not in file yet",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"fmt"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: false, // Path doesn't exist in file, no conflict
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock InstrumentPhase with no importcfg (to avoid actual file operations)
			ip := &InstrumentPhase{}

			err := ip.addRuleImports(t.Context(), tt.root, tt.imports, "test-rule")
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else if err != nil {
				// Note: This may still fail at updateImportConfig step since we don't have
				// a real importcfg setup. We're only testing the alias mismatch detection.
				// If there's an error, it shouldn't be about alias mismatch.
				assert.NotContains(t, err.Error(), "import alias mismatch")
			}
		})
	}
}

func TestUpdateImportConfigForFile(t *testing.T) {
	t.Run("empty file has no imports to update", func(t *testing.T) {
		ip := &InstrumentPhase{}
		root := &dst.File{}

		// Should not error - no imports to process
		err := ip.updateImportConfigForFile(t.Context(), root, "test-rule")
		require.NoError(t, err)
	})

	t.Run("file with imports attempts update", func(t *testing.T) {
		ip := &InstrumentPhase{
			// No importcfg path, so updateImportConfig will return early
			importConfigPath: "",
		}
		root := &dst.File{
			Decls: []dst.Decl{
				&dst.GenDecl{
					Tok: token.IMPORT,
					Specs: []dst.Spec{
						&dst.ImportSpec{Path: &dst.BasicLit{Value: `"log"`}},
						&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
					},
				},
			},
		}

		// Should not error - updateImportConfig returns early when no importcfg path
		err := ip.updateImportConfigForFile(t.Context(), root, "test-rule")
		require.NoError(t, err)
	})
}

func TestAutoDetectHookImports(t *testing.T) {
	t.Run("empty path skips detection", func(t *testing.T) {
		ip := &InstrumentPhase{}
		r := &rule.InstFuncRule{}
		// Should not error - no path means nothing to detect
		err := ip.autoDetectHookImports(t.Context(), r)
		require.NoError(t, err)
	})

	t.Run("valid testdata hook file detects imports", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		ip := &InstrumentPhase{
			importConfigPath: "", // no importcfg path → updateImportConfig is a no-op
		}
		r := &rule.InstFuncRule{}
		r.Name = "test-rule"
		r.Before = "H1Before"
		r.Path = "testdata"
		// Should not error - the hook file exists and can be parsed; with no
		// importcfg path, updateImportConfig returns early without writing
		err := ip.autoDetectHookImports(t.Context(), r)
		require.NoError(t, err)
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		ip := &InstrumentPhase{}
		r := &rule.InstFuncRule{}
		r.Name = "test-rule"
		r.Before = "SomeHook"
		r.Path = "testdata/nonexistent-path"
		err := ip.autoDetectHookImports(t.Context(), r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "finding hook file")
	})
}
