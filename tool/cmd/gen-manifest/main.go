// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.opentelemetry.io/otelc/tool/util"
)

const (
	defaultContribModule = "go.opentelemetry.io/otelc-contrib"
	defaultContribRepo   = "https://github.com/open-telemetry/opentelemetry-go-compile-contrib.git"
	defaultVersion       = "main"
	generatedFilePerm    = 0o644
)

//nolint:gochecknoglobals // fetchRepo is swapped during testing
var fetchRepo = fetchContribRepo

func main() {
	if err := run(os.Args[1:]...); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args ...string) error {
	fs := flag.NewFlagSet("gen-manifest", flag.ContinueOnError)
	version := fs.String("version", defaultVersion, "version, branch, tag, or pseudoversion of otelc-contrib")
	output := fs.String("output", "tool/data/instrumentation-manifest.json", "output manifest path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedVer, checkoutDir, cleanup, err := fetchRepo(*version)
	if err != nil {
		return err
	}
	defer cleanup()

	instRoot := filepath.Join(checkoutDir, "instrumentation")
	generated, err := Generate(instRoot, resolvedVer)
	if err != nil {
		return fmt.Errorf("generate instrumentation manifest: %w", err)
	}
	content, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instrumentation manifest: %w", err)
	}
	content = append(content, '\n')
	if err = util.WriteFileAtomic(*output, content, generatedFilePerm); err != nil {
		return fmt.Errorf("write instrumentation manifest: %w", err)
	}
	return nil
}

//nolint:revive // multiple string return types are clear in context
func fetchContribRepo(version string) (string, string, func(), error) {
	//nolint:gosec // gen-manifest is a developer tool and version is user-supplied CLI input
	cmd := exec.CommandContext(context.Background(), "go", "list", "-m", "-json", defaultContribModule+"@"+version)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", nil, fmt.Errorf("resolving %s@%s: %w (%s)", defaultContribModule, version, err, string(out))
	}
	var modInfo struct {
		Version string `json:"Version"`
		Origin  struct {
			Hash string `json:"Hash"`
		} `json:"Origin"`
	}
	if parseErr := json.Unmarshal(out, &modInfo); parseErr != nil {
		return "", "", nil, fmt.Errorf("parsing go list output: %w", parseErr)
	}

	commitHash := modInfo.Origin.Hash
	if commitHash == "" {
		commitHash = version
	}

	tmpDir, tempErr := os.MkdirTemp("", "otelc-contrib-*")
	if tempErr != nil {
		return "", "", nil, fmt.Errorf("creating temp dir: %w", tempErr)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	if gitErr := runGit(tmpDir, "init"); gitErr != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git init: %w", gitErr)
	}
	if gitErr := runGit(tmpDir, "remote", "add", "origin", defaultContribRepo); gitErr != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git remote add: %w", gitErr)
	}
	if gitErr := runGit(tmpDir, "fetch", "--depth", "1", "origin", commitHash); gitErr != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git fetch: %w", gitErr)
	}
	if gitErr := runGit(tmpDir, "checkout", "FETCH_HEAD"); gitErr != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git checkout: %w", gitErr)
	}
	_ = runGit(tmpDir, "fetch", "--tags", "--depth", "1", "origin")

	return modInfo.Version, tmpDir, cleanup, nil
}

func runGit(dir string, args ...string) error {
	//nolint:gosec // gen-manifest is a developer tool running git commands
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v in %s: %w (%s)", args, dir, err, string(out))
	}
	return nil
}
