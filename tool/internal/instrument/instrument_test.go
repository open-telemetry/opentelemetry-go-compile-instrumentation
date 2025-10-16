//go:build !windows

package instrument

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

const (
	matchedJSONFile = "matched.json"
)

func findGoToolCompile() string {
	cmd := exec.Command("go", "env", "GOTOOLDIR")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error getting GOROOT: %v\n", err)
		return ""
	}

	goroot := strings.TrimSpace(string(output))
	if goroot == "" {
		fmt.Println("GOROOT not set")
		return ""
	}
	return filepath.Join(goroot, "compile")
}

func TestInstrument(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid compile with instrumentation", false},
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	ctx := util.ContextWithLogger(context.Background(), logger)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := setupTestEnvironment(t)

			args := createCompileArgs(tempDir)
			err := Toolexec(ctx, args)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			// TODO: Link the instrumented binary and run it and further check
			// output content
		})
	}
}

func setupTestEnvironment(t *testing.T) string {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelWorkDir, tempDir)

	// Create source code file
	mainGoFile := filepath.Join(tempDir, "main.go")
	err := os.MkdirAll(filepath.Dir(mainGoFile), 0o755)
	require.NoError(t, err)
	err = util.CopyFile(filepath.Join("testdata", "source.go"), mainGoFile)
	require.NoError(t, err)

	// Create matched.json with test rules
	matchedJSON, err := createTestRuleJSON(mainGoFile)
	require.NoError(t, err)
	matchedFile := filepath.Join(tempDir, util.BuildTempDir, matchedJSONFile)
	err = os.MkdirAll(filepath.Dir(matchedFile), 0o755)
	require.NoError(t, err)
	err = util.WriteFile(matchedFile, string(matchedJSON))
	require.NoError(t, err)

	return tempDir
}

func createCompileArgs(tempDir string) []string {
	sourcePath := filepath.Join(tempDir, "main.go")
	outputPath := filepath.Join(tempDir, "_pkg_.a")
	compilePath := findGoToolCompile()

	return []string{
		compilePath,
		"-o", outputPath,
		"-p", "main",
		"-complete",
		"-buildid", "foo/bar",
		"-pack",
		sourcePath,
	}
}

func createTestRuleJSON(mainGoFile string) ([]byte, error) {
	ruleSet := []*rule.InstRuleSet{
		{
			PackageName: "main",
			ModulePath:  "main",
			FuncRules: map[string][]*rule.InstFuncRule{
				mainGoFile: {
					{
						InstBaseRule: rule.InstBaseRule{
							Name:   "instrument_func1",
							Target: "main",
						},
						Path:   filepath.Join(".", "testdata"),
						Func:   "Func1",
						Before: "Func1Before",
						After:  "Func1After",
					},
				},
			},
			RawRules: map[string][]*rule.InstRawRule{
				mainGoFile: {
					{
						InstBaseRule: rule.InstBaseRule{
							Name:   "add_raw_code",
							Target: "main",
						},
						Func: "Func1",
						Raw:  "func2()",
					},
				},
			},
			StructRules: map[string][]*rule.InstStructRule{
				mainGoFile: {
					{
						InstBaseRule: rule.InstBaseRule{
							Name:   "add_new_field",
							Target: "main",
						},
						Struct:    "T",
						FieldName: "NewField",
						FieldType: "string",
					},
				},
			},
			FileRules: []*rule.InstFileRule{
				{
					InstBaseRule: rule.InstBaseRule{
						Name:   "add_new_file",
						Target: "main",
					},
					File: "newfile.go",
					Path: filepath.Join(".", "testdata"),
				},
			},
		},
	}
	return json.Marshal(ruleSet)
}
