//go:build !windows

package instrument

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

const (
	threeBackDir       = "../../.."
	mainGoFile         = threeBackDir + "/demo/main.go"
	pkgDir             = "/pkg/instrumentation/helloworld"
	otelHelloWorldPath = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/helloworld"
	matchedJSONFile    = "matched.json"
	mainPkgDir         = "b001"
)

func TestCompileCommand(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid compile with instrumentation", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tempDir := setupTestEnvironment(t)

			args := createCompileArgs(tempDir)

			_, err := compileCommand(ctx, args)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func setupTestEnvironment(t *testing.T) string {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelWorkDir, tempDir)

	// Create necessary directories
	buildDir := filepath.Join(tempDir, util.BuildTempDir)
	mainPkgPath := filepath.Join(buildDir, mainPkgDir)
	err := os.MkdirAll(mainPkgPath, 0o755)
	require.NoError(t, err)

	// Copy instrumentation package files
	workdir, err := os.Getwd()
	require.NoError(t, err)
	srcPkgPath := filepath.Join(workdir, threeBackDir, strings.TrimPrefix(pkgDir, "/"))
	dstPkgPath := filepath.Join(buildDir, strings.TrimPrefix(pkgDir, "/"))

	// Create destination directory first
	err = os.MkdirAll(filepath.Dir(dstPkgPath), 0o755)
	require.NoError(t, err)

	// Check if source exists before copying
	if _, err = os.Stat(srcPkgPath); err == nil {
		err = os.CopyFS(dstPkgPath, os.DirFS(srcPkgPath))
		require.NoError(t, err)
	}

	// Create matched.json with test rules
	matchedJSON, err := createTestRuleJSON(otelHelloWorldPath)
	require.NoError(t, err)
	matchedFile := filepath.Join(buildDir, matchedJSONFile)
	err = os.WriteFile(matchedFile, matchedJSON, 0o644)
	require.NoError(t, err)

	return tempDir
}

func createCompileArgs(tempDir string) []string {
	buildDir := filepath.Join(tempDir, util.BuildTempDir)
	outputPath := filepath.Join(buildDir, mainPkgDir, "_pkg_.a")
	trimPath := "/" + mainPkgDir + "=>"
	importCfgPath := "/" + mainPkgDir + "/importcfg"

	return []string{
		"compile",
		"-o", outputPath,
		"-trimpath", trimPath,
		"-p", "main",
		"-lang=go1.23",
		"-complete",
		"-buildid", "LQWltgXJxiWKFGWheOxv/LQWltgXJxiWKFGWheOxv",
		"-goversion", "go1.24.4",
		"-c=4",
		"-shared",
		"-nolocalimports",
		"-importcfg", importCfgPath,
		"-pack",
		mainGoFile,
	}
}

func createTestRuleJSON(path string) ([]byte, error) {
	rules := []rule.InstFuncRule{
		{
			Name:     "hook_helloworld",
			Path:     path,
			Pointcut: "main.Example",
			Advice: []rule.Advice{
				{Before: "MyHookBefore", After: ""},
				{Before: "", After: "MyHookAfter"},
			},
		},
	}
	return json.Marshal(rules)
}
