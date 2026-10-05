// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command check-test-names verifies that every unit test file under this
// repository's Go packages pairs 1:1 with a source file of the same name in
// the same directory (foo_test.go <-> foo.go).
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirNames are directory names skipped wherever they occur: generated
// fixtures and vendored code that don't follow (or need) this convention.
var skipDirNames = map[string]bool{ //nolint:gochecknoglobals // private lookup table
	"testdata":     true,
	"vendor":       true,
	"demo":         true,
	"node_modules": true,
}

// exemptScenarioDirs are dedicated scenario/E2E test suites, addressed
// relative to the repository root. They exercise built binaries and fixture
// apps rather than pairing 1:1 with a source file, so the naming rule below
// does not apply to them.
var exemptScenarioDirs = map[string]bool{ //nolint:gochecknoglobals // private lookup table
	"test/integration":    true,
	"test/e2e":            true,
	"test/bench":          true,
	"test/latestlibbuild": true,
	"test/latestlibrun":   true,
	"test/versionmatrix":  true,
}

func main() {
	if !checkAndReport(".", os.Stdout, os.Stderr) {
		os.Exit(1)
	}
}

// checkAndReport runs the check against root and writes its outcome to out
// (on success) or errOut (on failure), returning whether the check passed.
// It holds everything main does apart from the exit code, so tests can cover
// it without depending on os.Exit.
func checkAndReport(root string, out, errOut io.Writer) bool {
	violations, err := run(root)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return false
	}
	if len(violations) > 0 {
		_, _ = fmt.Fprintln(errOut, "Test files without a matching source file "+
			"(foo_test.go must pair with foo.go in the same directory):")
		for _, v := range violations {
			_, _ = fmt.Fprintf(errOut, "  %s (expected %s)\n", v.path, v.expectedSource)
		}
		_, _ = fmt.Fprintln(
			errOut,
			"\nIf this is a legitimate exception (platform-specific build, fuzz target, shared test helper, ...), "+
				"add it to the allowlist in tool/cmd/check-test-names/allowlist.go.",
		)
		return false
	}
	_, _ = fmt.Fprintln(out, "All test files follow the naming convention.")
	return true
}

type violation struct {
	path           string
	expectedSource string
}

// run expects the repository root as its working directory, as guaranteed by
// make.
func run(root string) ([]violation, error) {
	return checkTree(root, allowlist)
}

// checkTree walks root looking for naming-convention violations among
// "*_test.go" files, exempting anything in allow (slash-separated paths
// relative to root). It is separated from run so tests can exercise it
// against a temporary tree with their own allowlist.
func checkTree(root string, allow []string) ([]violation, error) {
	allowed := make(map[string]bool, len(allow))
	for _, a := range allow {
		allowed[a] = true
	}

	var violations []violation
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			if p == root {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || skipDirNames[d.Name()] || exemptScenarioDirs[relSlash] {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if allowed[relSlash] {
			return nil
		}

		source := strings.TrimSuffix(d.Name(), "_test.go") + ".go"
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(p), source)); statErr == nil {
			return nil
		}

		violations = append(violations, violation{
			path:           relSlash,
			expectedSource: path.Join(path.Dir(relSlash), source),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	sort.Slice(violations, func(i, j int) bool { return violations[i].path < violations[j].path })
	return violations, nil
}
