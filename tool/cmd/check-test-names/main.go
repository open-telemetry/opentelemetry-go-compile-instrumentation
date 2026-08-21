// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command check-test-names verifies that every unit test file under this
// repository's Go packages pairs 1:1 with a source file of the same name in
// the same directory (foo_test.go <-> foo.go). Prefix-style pairing (e.g.
// foo_extra_test.go treated as belonging to foo.go) is rejected on purpose:
// it is how segmented, hard-to-navigate test files crept into the tree
// before. Legitimate exceptions are registered in allowlist.go together with
// the reason they can't follow the 1:1 rule.
package main

import (
	"fmt"
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
	violations, err := run(".")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Test files without a matching source file "+
			"(foo_test.go must pair with foo.go in the same directory):")
		for _, v := range violations {
			_, _ = fmt.Fprintf(os.Stderr, "  %s (expected %s)\n", v.path, v.expectedSource)
		}
		_, _ = fmt.Fprintln(
			os.Stderr,
			"\nIf this is a legitimate exception (platform-specific build, fuzz target, shared test helper, ...), "+
				"add it to the allowlist in tool/cmd/check-test-names/allowlist.go with a comment explaining why it can't follow the 1:1 rule.",
		)
		os.Exit(1)
	}
	_, _ = fmt.Println("All test files follow the naming convention.")
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
// "*_test.go" files, exempting anything in allow (keyed by slash-separated
// path relative to root). It is separated from run so tests can exercise it
// against a temporary tree with their own allowlist.
func checkTree(root string, allow map[string]string) ([]violation, error) {
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
		if _, ok := allow[relSlash]; ok {
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
