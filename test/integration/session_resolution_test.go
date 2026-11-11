//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// writeSessionFixture writes an app whose only route to example.com/session/extra
// is an import a rule adds to dep, so every link must recover it.
func writeSessionFixture(t *testing.T, appDir string) {
	t.Helper()
	writeTestFile(t, appDir, filepath.Join("extra", "go.mod"), "module example.com/session/extra\n\ngo 1.25.0\n")
	writeTestFile(t, appDir, filepath.Join("extra", "extra.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

import "fmt"

// Note reports that the rule-injected code ran.
func Note() string {
	fmt.Println("extra: note called")
	return "note"
}
`)
	writeTestFile(t, appDir, filepath.Join("dep", "dep.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dep

import "strings"

func Work() string {
	return strings.ToUpper("work")
}
`)
	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"example.com/session/dep"
)

func main() {
	fmt.Println("session: " + dep.Work())
}
`)
	writeTestFile(t, appDir, "go.mod", `module example.com/session

go 1.25.0

require example.com/session/extra v0.0.0

replace example.com/session/extra => ./extra
`)
	writeTestFile(t, appDir, "rules.yml", `dep_wrap_call:
  target: example.com/session/dep
  where:
    function_call: strings.ToUpper
  do:
    - wrap_call:
        replace: '(func() string { extra.Note(); return {{ . }} })()'
  imports:
    extra: example.com/session/extra
`)
}

// linkResolutionRe matches the OTELC_STATS line of one link's rule-added
// import resolution.
var linkResolutionRe = regexp.MustCompile(
	`rule-added import resolution.*phase=link.*memo_hits=(\d+) go_list_calls=(\d+)`)

// linkResolutionStats sums memo hits and go list calls over every link
// resolution the debug log of workDir records.
func linkResolutionStats(t *testing.T, workDir string) (int, int) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(workDir, ".otelc-build", "debug.log"))
	require.NoError(t, err)
	matches := linkResolutionRe.FindAllStringSubmatch(string(content), -1)
	require.NotEmpty(t, matches, "the build logged no link resolution:\n%s", content)
	var hits, calls int
	for _, m := range matches {
		h, _ := strconv.Atoi(m[1])
		c, _ := strconv.Atoi(m[2])
		hits += h
		calls += c
	}
	return hits, calls
}

type sessionHarness struct {
	t         *testing.T
	otelcPath string
	appDir    string
	env       []string
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	appDir := t.TempDir()
	writeSessionFixture(t, appDir)
	env := append(preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"OTELC_RULES=rules.yml",
		"OTELC_STATS=1",
	)
	return &sessionHarness{t: t, otelcPath: otelcPath, appDir: appDir, env: env}
}

// build runs one instrumented build with a fresh work dir, checks the binary,
// and returns the work dir.
func (h *sessionHarness) build(t *testing.T, extraEnv ...string) string {
	t.Helper()
	workDir := t.TempDir()
	bin := filepath.Join(workDir, "app")
	cmd := exec.CommandContext(t.Context(), h.otelcPath, "--work-dir", workDir, "go", "build", "-o", bin, ".")
	cmd.Dir = h.appDir
	cmd.Env = append(append([]string{}, h.env...), extraEnv...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "otelc go build failed:\n%s", out)
	run, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "%s", run)
	assert.Contains(t, string(run), "extra: note called")
	return workDir
}

// TestSessionReusesLinkResolution covers repeated warm builds in one session:
// after the first link resolves the rule-added import, later builds read the
// memo and run no go list -export at link time.
func TestSessionReusesLinkResolution(t *testing.T) {
	h := newSessionHarness(t)
	h.build(t) // prime GOCACHE

	session := "OTELC_SESSION_DIR=" + filepath.Join(t.TempDir(), "session")
	h.build(t, session)
	second := h.build(t, session)

	hits, calls := linkResolutionStats(t, second)
	assert.Zero(t, calls, "the second build of the session must not run go list at link")
	assert.Positive(t, hits)
}

// TestSessionConcurrentBuildsResolveOnce covers concurrent warm builds of
// one session: the single-flight lock lets one link resolve the import while
// the others read its result.
func TestSessionConcurrentBuildsResolveOnce(t *testing.T) {
	h := newSessionHarness(t)
	h.build(t) // prime GOCACHE

	session := "OTELC_SESSION_DIR=" + filepath.Join(t.TempDir(), "session")
	const builds = 4
	workDirs := make([]string, builds)
	var wg sync.WaitGroup
	for i := range builds {
		wg.Go(func() { workDirs[i] = h.build(t, session) })
	}
	wg.Wait()

	total := 0
	for _, dir := range workDirs {
		_, calls := linkResolutionStats(t, dir)
		total += calls
	}
	assert.LessOrEqual(t, total, 1, "concurrent links of one session must resolve the import at most once")
}

// TestSessionNotSharedAcrossBuilds checks the scoping: builds that inherit no
// session each own one, so neither reuses the other's resolution.
func TestSessionNotSharedAcrossBuilds(t *testing.T) {
	h := newSessionHarness(t)
	h.build(t) // prime GOCACHE
	for range 2 {
		_, calls := linkResolutionStats(t, h.build(t))
		assert.GreaterOrEqual(t, calls, 1)
	}
}

// TestSessionReachesNestedOtelcBuilds covers the dd-trace-go lane shape: an
// outer otelc go test whose parallel tests each run otelc go build with
// their own work dir. The inner builds inherit the outer session and share
// its memo.
func TestSessionReachesNestedOtelcBuilds(t *testing.T) {
	h := newSessionHarness(t)
	h.build(t) // prime GOCACHE

	writeTestFile(t, h.appDir, "nested_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

var linkStats = regexp.MustCompile(`+"`"+`rule-added import resolution.*phase=link.*go_list_calls=(\d+)`+"`"+`)

func build(t *testing.T) {
	t.Parallel()
	if os.Getenv("OTELC_SESSION_DIR") == "" {
		t.Fatal("the inner build did not inherit the outer session")
	}
	workDir := t.TempDir()
	bin := filepath.Join(workDir, "app")
	cmd := exec.Command(os.Getenv("NESTED_OTELC"), "--work-dir", workDir, "go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("inner otelc build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || !regexp.MustCompile("extra: note called").Match(out) {
		t.Fatalf("inner binary was not instrumented: %v\n%s", err, out)
	}
	log, err := os.ReadFile(filepath.Join(workDir, ".otelc-build", "debug.log"))
	if err != nil || !linkStats.Match(log) {
		t.Fatalf("inner build logged no link resolution: %v", err)
	}
	t.Logf("inner link: %s", linkStats.Find(log))
}

func TestInnerA(t *testing.T) { build(t) }
func TestInnerB(t *testing.T) { build(t) }
`)
	cmd := exec.CommandContext(t.Context(), h.otelcPath, "go", "test", "-count=1", "-v", "-run", "TestInner", ".")
	cmd.Dir = h.appDir
	cmd.Env = append(append([]string{}, h.env...), "NESTED_OTELC="+h.otelcPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "outer otelc go test failed:\n%s", out)

	calls := regexp.MustCompile(`inner link: .*go_list_calls=(\d+)`).FindAllStringSubmatch(string(out), -1)
	require.Len(t, calls, 2, "%s", out)
	total := 0
	for _, m := range calls {
		n, _ := strconv.Atoi(m[1])
		total += n
	}
	assert.LessOrEqual(t, total, 1, "inner builds of one outer session must share the resolution:\n%s", out)
}
