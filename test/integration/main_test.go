// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestMain(m *testing.M) {
	pwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: get working dir:", err)
		os.Exit(1)
	}

	appsRoot := filepath.Join(pwd, "..", "apps")
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: read apps dir:", err)
		os.Exit(1)
	}

	appDirs := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		if e.IsDir() {
			appDirs = append(appDirs, filepath.Join(appsRoot, e.Name()))
		}
	}
	// TestBasic builds and runs the basic demo from a different tree.
	appDirs = append(appDirs, filepath.Join(pwd, "..", "..", "demo", "app", "basic"))

	if err := buildAll(appDirs); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: pre-build failed:", err)
		cleanupAll(appDirs)
		os.Exit(1)
	}

	code := m.Run()
	cleanupAll(appDirs)
	os.Exit(code)
}

// buildAll runs otelc go build for every appDir concurrently. Returns the
// first error if any build fails.
func buildAll(appDirs []string) error {
	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	ctx := context.Background()
	for _, dir := range appDirs {
		wg.Add(1)
		go func(dir string) {
			defer wg.Done()
			if err := testutil.BuildAppAt(ctx, dir); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(dir)
	}
	wg.Wait()
	return firstErr
}

func cleanupAll(appDirs []string) {
	for _, dir := range appDirs {
		testutil.CleanupAppAt(dir)
	}
}
