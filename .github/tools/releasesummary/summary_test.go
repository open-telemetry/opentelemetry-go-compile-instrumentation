// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
)

type mockMetadataFetcher struct {
	labels map[int][]string
	errs   map[int]error
}

func (m *mockMetadataFetcher) fetchPRLabels(_ context.Context, prNumber int) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	if err, ok := m.errs[prNumber]; ok {
		return nil, err
	}
	if labels, ok := m.labels[prNumber]; ok {
		return labels, nil
	}
	return nil, nil
}

func TestPrefixStripping(t *testing.T) {
	tests := []struct {
		name         string
		inputTitle   string
		expectedDesc string
		isBreaking   bool
		ctype        string
	}{
		{
			name:         "type only",
			inputTitle:   "feat: add String method for KeyValue type",
			expectedDesc: "add String method for KeyValue type",
			isBreaking:   false,
			ctype:        "feat",
		},
		{
			name:         "type with scope",
			inputTitle:   "feat(attribute): add String method for KeyValue type",
			expectedDesc: "add String method for KeyValue type",
			isBreaking:   false,
			ctype:        "feat",
		},
		{
			name:         "type with exclamation",
			inputTitle:   "feat!: remove old API",
			expectedDesc: "remove old API",
			isBreaking:   true,
			ctype:        "feat",
		},
		{
			name:         "type with scope and exclamation",
			inputTitle:   "feat(attribute)!: add String method for KeyValue type",
			expectedDesc: "add String method for KeyValue type",
			isBreaking:   true,
			ctype:        "feat",
		},
		{
			name:         "fix with scope",
			inputTitle:   "fix(hook): prevent nil pointer dereference",
			expectedDesc: "prevent nil pointer dereference",
			isBreaking:   false,
			ctype:        "fix",
		},
		{
			name:         "refactor with scope and exclamation",
			inputTitle:   "refactor(core)!: rewrite config parser",
			expectedDesc: "rewrite config parser",
			isBreaking:   true,
			ctype:        "refactor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parseCommit(tt.inputTitle)
			if parsed == nil {
				t.Fatalf("expected valid commit for %q, got nil", tt.inputTitle)
			}
			if parsed.Description != tt.expectedDesc {
				t.Errorf("expected description %q, got %q", tt.expectedDesc, parsed.Description)
			}
			if parsed.IsBreaking != tt.isBreaking {
				t.Errorf("expected isBreaking=%v, got %v", tt.isBreaking, parsed.IsBreaking)
			}
			if parsed.Type != tt.ctype {
				t.Errorf("expected type %q, got %q", tt.ctype, parsed.Type)
			}
		})
	}
}

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"add String method", "Add String method"},
		{"Add String method", "Add String method"},
		{"", ""},
		{"   leading whitespace", "Leading whitespace"},
		{"`code` in title", "`code` in title"},
	}

	for _, tt := range tests {
		actual := capitalizeFirst(tt.input)
		if actual != tt.expected {
			t.Errorf("capitalizeFirst(%q): expected %q, got %q", tt.input, tt.expected, actual)
		}
	}
}

func TestPRLineFormatsAndTitleWithBy(t *testing.T) {
	inputNotes := `## What's Changed
* feat: fix sorting by name by @alice in https://github.com/org/repo/pull/101
* feat: support filtering by tags by @dependabot[bot] in https://github.com/org/repo/pull/102
* fix: handle missing parameter in https://github.com/org/repo/pull/103
* feat: add multi-author feature by @alice, @bob in https://github.com/org/repo/pull/104
* feat: add complex author list by @alice, @bob, and @carol in https://github.com/org/repo/pull/105
* fix: sort by date in #106
`

	curated, err := curateReleaseNotes(context.Background(), inputNotes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedAdded := []string{
		"- Fix sorting by name (#101)",
		"- Support filtering by tags (#102)",
		"- Add multi-author feature (#104)",
		"- Add complex author list (#105)",
	}
	for _, ea := range expectedAdded {
		if !strings.Contains(curated, ea) {
			t.Errorf("missing expected bullet in Added section: %s", ea)
		}
	}

	expectedFixed := []string{
		"- Handle missing parameter (#103)",
		"- Sort by date (#106)",
	}
	for _, ef := range expectedFixed {
		if !strings.Contains(curated, ef) {
			t.Errorf("missing expected bullet in Fixed section: %s", ef)
		}
	}

	for i := 101; i <= 106; i++ {
		prStr := fmt.Sprintf("%d", i)
		if !strings.Contains(curated, prStr) {
			t.Errorf("missing PR #%d in What's Changed", i)
		}
	}
}

func TestBreakingPrecedence(t *testing.T) {
	inputNotes := `## What's Changed
* feat(api)!: breaking feature by @alice in https://github.com/org/repo/pull/1
* fix(core)!: breaking fix by @bob in https://github.com/org/repo/pull/2
* refactor(store)!: breaking refactor by @carol in https://github.com/org/repo/pull/3
* feat(log): labeled breaking feature by @dave in https://github.com/org/repo/pull/4
* fix(net): labeled breaking fix by @eve in https://github.com/org/repo/pull/5
* chore!: breaking chore by @frank in https://github.com/org/repo/pull/6
* feat(client): regular feature by @grace in https://github.com/org/repo/pull/7
* fix(server): regular fix by @heidi in https://github.com/org/repo/pull/8
* refactor(db): regular refactor by @ivan in https://github.com/org/repo/pull/9
`

	fetcher := &mockMetadataFetcher{
		labels: map[int][]string{
			4: {"breaking"},
			5: {"breaking"},
		},
	}

	curated, err := curateReleaseNotes(context.Background(), inputNotes, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// all 6 breaking PRs must be under Breaking Changes.
	expectedBreaking := []string{
		"- Breaking feature (#1)",
		"- Breaking fix (#2)",
		"- Breaking refactor (#3)",
		"- Labeled breaking feature (#4)",
		"- Labeled breaking fix (#5)",
		"- Breaking chore (#6)",
	}

	for _, eb := range expectedBreaking {
		if !strings.Contains(curated, eb) {
			t.Errorf("missing expected breaking bullet: %s", eb)
		}
	}

	// verify no duplication
	addedSection := extractSection(curated, "### Added")
	fixedSection := extractSection(curated, "### Fixed")
	changedSection := extractSection(curated, "### Changed")

	for _, eb := range expectedBreaking {
		if strings.Contains(addedSection, eb) {
			t.Errorf("breaking item duplicated in Added section: %s", eb)
		}
		if strings.Contains(fixedSection, eb) {
			t.Errorf("breaking item duplicated in Fixed section: %s", eb)
		}
		if strings.Contains(changedSection, eb) {
			t.Errorf("breaking item duplicated in Changed section: %s", eb)
		}
	}

	// verify non-breaking changes
	if !strings.Contains(addedSection, "- Regular feature (#7)") {
		t.Errorf("expected regular feature (#7) in Added section, got: %s", addedSection)
	}
	if !strings.Contains(fixedSection, "- Regular fix (#8)") {
		t.Errorf("expected regular fix (#8) in Fixed section, got: %s", fixedSection)
	}
	if !strings.Contains(changedSection, "- Regular refactor (#9)") {
		t.Errorf("expected regular refactor (#9) in Changed section, got: %s", changedSection)
	}
}

func TestCategoryMapping(t *testing.T) {
	inputNotes := `## What's Changed
* feat: add tracing support by @alice in https://github.com/org/repo/pull/10
* fix: resolve memory leak by @bob in https://github.com/org/repo/pull/20
* refactor: clean up hook dispatch by @carol in https://github.com/org/repo/pull/30
`

	curated, err := curateReleaseNotes(context.Background(), inputNotes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// headings must appear in exact order
	idxAdded := strings.Index(curated, "### Added\n\n- Add tracing support (#10)")
	idxFixed := strings.Index(curated, "### Fixed\n\n- Resolve memory leak (#20)")
	idxChanged := strings.Index(curated, "### Changed\n\n- Clean up hook dispatch (#30)")

	if idxAdded == -1 {
		t.Errorf("missing or malformed Added section in:\n%s", curated)
	}
	if idxFixed == -1 {
		t.Errorf("missing or malformed Fixed section in:\n%s", curated)
	}
	if idxChanged == -1 {
		t.Errorf("missing or malformed Changed section in:\n%s", curated)
	}

	if !(idxAdded < idxFixed && idxFixed < idxChanged) {
		t.Errorf("sections not in order Added < Fixed < Changed; got indices %d, %d, %d", idxAdded, idxFixed, idxChanged)
	}
}

func TestOmittedPRs(t *testing.T) {
	inputNotes := `## What's Changed
* feat: new metrics exporter by @alice in https://github.com/org/repo/pull/1
* chore(deps): bump golang.org/x/net by @dependabot in https://github.com/org/repo/pull/2
* chore: update lint config by @bob in https://github.com/org/repo/pull/3
* doc: fix typo in README by @carol in https://github.com/org/repo/pull/4
* docs(api): update godoc by @dave in https://github.com/org/repo/pull/5
* test: add edge case tests by @eve in https://github.com/org/repo/pull/6
* ci: pin action shas by @frank in https://github.com/org/repo/pull/7
* feat(deps): auto-update dependencies by @bot in https://github.com/org/repo/pull/8
`

	fetcher := &mockMetadataFetcher{
		labels: map[int][]string{
			8: {"dependencies"},
		},
	}

	curated, err := curateReleaseNotes(context.Background(), inputNotes, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summary := extractSummary(curated)

	omittedBullets := []string{
		"(#2)",
		"(#3)",
		"(#4)",
		"(#5)",
		"(#6)",
		"(#7)",
		"(#8)",
	}

	for _, ob := range omittedBullets {
		if strings.Contains(summary, ob) {
			t.Errorf("omitted PR %s found in summary:\n%s", ob, summary)
		}
	}

	// All of them must still be retained in What's Changed.
	whatsChanged := extractWhatsChanged(curated)
	for i := 1; i <= 8; i++ {
		prMarker := string(rune('0' + i))
		if !strings.Contains(whatsChanged, "pull/"+prMarker) {
			t.Errorf("PR #%d missing from What's Changed section", i)
		}
	}
}

func TestVerbatimTailPreservation(t *testing.T) {
	inputNotes := `Release v1.2.0 Preamble text from GitHub
Some random preamble lines that should not be duplicated.

## What's Changed
* feat(attr): add KeyValue String method by @contributor in https://github.com/org/repo/pull/100
* fix(core): handle context cancellation by @author in https://github.com/org/repo/pull/101

## New Contributors
* @contributor made their first contribution in https://github.com/org/repo/pull/100

**Full Changelog**: https://github.com/org/repo/compare/v1.1.0...v1.2.0
`

	expectedTail := `## What's Changed
* feat(attr): add KeyValue String method by @contributor in https://github.com/org/repo/pull/100
* fix(core): handle context cancellation by @author in https://github.com/org/repo/pull/101

## New Contributors
* @contributor made their first contribution in https://github.com/org/repo/pull/100

**Full Changelog**: https://github.com/org/repo/compare/v1.1.0...v1.2.0
`

	curated, err := curateReleaseNotes(context.Background(), inputNotes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(curated, "Preamble text from GitHub") {
		t.Error("preamble was not stripped from top of release notes")
	}

	if !strings.HasSuffix(curated, expectedTail) {
		t.Errorf("curated notes do not end with verbatim tail byte-for-byte.\nGot:\n%s\nExpected tail:\n%s", curated, expectedTail)
	}

	tailIndex := len(curated) - len(expectedTail)
	actualTail := curated[tailIndex:]
	if actualTail != expectedTail {
		t.Errorf("tail mismatch: expected %q, got %q", expectedTail, actualTail)
	}
}

func TestDeterministicOrder(t *testing.T) {
	inputNotes := `## What's Changed
* feat: third added by @c in https://github.com/org/repo/pull/30
* feat: first added by @a in https://github.com/org/repo/pull/10
* fix: second fixed by @b in https://github.com/org/repo/pull/25
* feat: second added by @b in https://github.com/org/repo/pull/20
* fix: first fixed by @a in https://github.com/org/repo/pull/5
* feat!: breaking change by @z in https://github.com/org/repo/pull/99
`

	curated, err := curateReleaseNotes(context.Background(), inputNotes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addedSection := extractSection(curated, "### Added")
	expectedAdded := "### Added\n\n- Third added (#30)\n- First added (#10)\n- Second added (#20)"
	if !strings.Contains(addedSection, expectedAdded) {
		t.Errorf("Added section order mismatch.\nExpected:\n%s\nGot:\n%s", expectedAdded, addedSection)
	}

	fixedSection := extractSection(curated, "### Fixed")
	expectedFixed := "### Fixed\n\n- Second fixed (#25)\n- First fixed (#5)"
	if !strings.Contains(fixedSection, expectedFixed) {
		t.Errorf("Fixed section order mismatch.\nExpected:\n%s\nGot:\n%s", expectedFixed, fixedSection)
	}

	breakingSection := extractSection(curated, "### Breaking Changes")
	expectedBreaking := "### Breaking Changes\n\n- Breaking change (#99)"
	if !strings.Contains(breakingSection, expectedBreaking) {
		t.Errorf("Breaking section order mismatch.\nExpected:\n%s\nGot:\n%s", expectedBreaking, breakingSection)
	}
}

func TestMalformedTitlesAndMetadata(t *testing.T) {
	inputNotes := `## What's Changed
* Merge pull request #50 from branch by @admin in https://github.com/org/repo/pull/50
* feat: valid feature by @alice in https://github.com/org/repo/pull/10
* Random title without conventional commit by @bob in https://github.com/org/repo/pull/20
* fix: valid fix with failed metadata by @carol in https://github.com/org/repo/pull/30
* fix: valid fix by @dave in https://github.com/org/repo/pull/40
`

	fetcher := &mockMetadataFetcher{
		errs: map[int]error{
			30: errors.New("GitHub API rate limit exceeded"),
		},
	}

	var logBuf bytes.Buffer
	testLogger := log.New(&logBuf, "", 0)

	curated, err := curateReleaseNotesWithLogger(context.Background(), inputNotes, fetcher, testLogger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	diagnostics := logBuf.String()

	if !strings.Contains(diagnostics, "skipping PR #50") || !strings.Contains(diagnostics, "not a supported Conventional Commit format") {
		t.Errorf("missing diagnostic for non-conventional PR #50 in:\n%s", diagnostics)
	}
	if !strings.Contains(diagnostics, "skipping PR #20") || !strings.Contains(diagnostics, "not a supported Conventional Commit format") {
		t.Errorf("missing diagnostic for non-conventional PR #20 in:\n%s", diagnostics)
	}

	if !strings.Contains(diagnostics, "skipping PR #30") || !strings.Contains(diagnostics, "metadata unavailable") {
		t.Errorf("missing diagnostic for metadata-failed PR #30 in:\n%s", diagnostics)
	}

	summary := extractSummary(curated)
	if !strings.Contains(summary, "- Valid feature (#10)") {
		t.Errorf("valid feature #10 missing from summary:\n%s", summary)
	}
	if !strings.Contains(summary, "- Valid fix (#40)") {
		t.Errorf("valid fix #40 missing from summary:\n%s", summary)
	}
	if strings.Contains(summary, "(#50)") || strings.Contains(summary, "(#20)") || strings.Contains(summary, "(#30)") {
		t.Errorf("malformed/failed PRs found in summary:\n%s", summary)
	}

	whatsChanged := extractWhatsChanged(curated)
	for _, prNum := range []string{"50", "10", "20", "30", "40"} {
		if !strings.Contains(whatsChanged, "pull/"+prNum) {
			t.Errorf("What's Changed missing entry for PR #%s", prNum)
		}
	}
}

func TestEmptyOrMissingWhatsChanged(t *testing.T) {
	t.Run("missing section", func(t *testing.T) {
		input := "Just some random release notes without any section heading."
		_, err := curateReleaseNotes(context.Background(), input, nil)
		if err == nil {
			t.Fatal("expected error for missing section, got nil")
		}
		if !strings.Contains(err.Error(), `missing "## What's Changed" section`) {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("empty section", func(t *testing.T) {
		input := "## What's Changed\n\n## New Contributors\n* @user contributed\n"
		_, err := curateReleaseNotes(context.Background(), input, nil)
		if err == nil {
			t.Fatal("expected error for empty section, got nil")
		}
		if !strings.Contains(err.Error(), `empty`) || !strings.Contains(err.Error(), `no pull requests found`) {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// ---- helpers ----

const whatsChangedHeading = "## What's Changed"

func extractSummary(notes string) string {
	before, _, found := strings.Cut(notes, whatsChangedHeading)
	if !found {
		return notes
	}
	return before
}

func extractWhatsChanged(notes string) string {
	_, after, found := strings.Cut(notes, whatsChangedHeading)
	if !found {
		return ""
	}
	return whatsChangedHeading + after
}

func extractSection(notes, heading string) string {
	_, after, found := strings.Cut(notes, heading)
	if !found {
		return ""
	}

	rest := heading + after
	lines := strings.Split(rest, "\n")

	var sectionLines []string
	sectionLines = append(sectionLines, lines[0])

	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") {
			break
		}
		sectionLines = append(sectionLines, line)
	}

	return strings.Join(sectionLines, "\n")
}
