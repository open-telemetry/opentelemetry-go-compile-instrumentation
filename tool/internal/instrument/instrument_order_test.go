// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// TestSortedFiles asserts that sortedFiles — the function instrument() uses to
// pick its processing order — returns file2rules' keys sorted, regardless of
// the map's internal (randomized) iteration order. instrument() returns on
// the first error, so without this, which failure gets reported (and the
// order of per-file log lines) would vary between identical runs of the same
// input. See #1070.
//
// The rule set is built as a struct literal, matching the pattern already
// used by TestGroupRules, rather than via AddDeclRule: that helper asserts
// its file argument is an absolute path, and a hardcoded absolute path would
// not be portable between Unix and Windows CI runners.
func TestSortedFiles(t *testing.T) {
	// Deliberately unsorted insertion order, so this test would fail under
	// raw map iteration even by coincidence.
	unsorted := []string{"f5.go", "f1.go", "f8.go", "f3.go", "f6.go", "f2.go", "f7.go", "f4.go"}

	declRules := make(map[string][]*rule.InstDeclRule, len(unsorted))
	for _, f := range unsorted {
		declRules[f] = []*rule.InstDeclRule{{
			InstBaseRule: rule.InstBaseRule{Name: "r_" + f},
			Kind:         "var",
			Identifier:   "X",
			Replace:      "1",
		}}
	}
	rset := &rule.InstRuleSet{
		ModulePath: "m",
		DeclRules:  declRules,
	}

	file2rules := groupRules("/work", rset)
	require.Len(t, file2rules, len(unsorted))

	want := slices.Clone(unsorted)
	slices.Sort(want)

	// Repeated calls against the same map must keep agreeing with each other
	// and with the pre-sorted expectation, matching what instrument() relies
	// on across every file it processes in one run.
	for i := range 20 {
		got := sortedFiles(file2rules)
		assert.Equal(t, want, got, "call %d produced a different order", i)
	}
}
