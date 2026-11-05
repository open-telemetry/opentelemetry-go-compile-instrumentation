// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-github/v79/github"
)

const (
	CategoryBreakingChanges = "Breaking Changes"
	CategoryAdded           = "Added"
	CategoryFixed           = "Fixed"
	CategoryChanged         = "Changed"
)

var categoryOrder = []string{
	CategoryBreakingChanges,
	CategoryAdded,
	CategoryFixed,
	CategoryChanged,
}
var conventionalTypes = map[string]bool{
	"feat":     true,
	"fix":      true,
	"refactor": true,
	"chore":    true,
	"doc":      true,
	"docs":     true,
	"test":     true,
	"build":    true,
	"ci":       true,
	"perf":     true,
	"style":    true,
	"release":  true,
}

var (
	prLineRegex    = regexp.MustCompile(`^[\*\-]\s+(.+?)(?:\s+by\s+@.+?)?\s+in\s+(?:https?://\S+/pull/(\d+)|#(\d+))\s*$`)
	ccRegexp       = regexp.MustCompile(`^([a-zA-Z]+)(?:\((.+?)\))?(!)?: (.+)$`)
	reWhatsChanged = regexp.MustCompile(`(?m)^## What's Changed\b.*$`)
	reH2Heading    = regexp.MustCompile(`(?m)^## `)
)

type metadataFetcher interface {
	fetchPRLabels(ctx context.Context, prNumber int) ([]string, error)
}

type ghMetadataFetcher struct {
	client *github.Client
	owner  string
	repo   string
}

func newGitHubMetadataFetcher(client *github.Client, owner, repo string) *ghMetadataFetcher {
	return &ghMetadataFetcher{
		client: client,
		owner:  owner,
		repo:   repo,
	}
}

func (f *ghMetadataFetcher) fetchPRLabels(ctx context.Context, prNumber int) ([]string, error) {
	pr, _, err := f.client.PullRequests.Get(ctx, f.owner, f.repo, prNumber)
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		if l != nil && l.Name != nil {
			labels = append(labels, *l.Name)
		}
	}
	return labels, nil
}

type parsedCommit struct {
	Type        string
	Scope       string
	IsBreaking  bool
	Description string
}

func parseCommit(title string) *parsedCommit {
	matches := ccRegexp.FindStringSubmatch(strings.TrimSpace(title))
	if matches == nil {
		return nil
	}

	ctype := strings.ToLower(matches[1])
	if !conventionalTypes[ctype] {
		return nil
	}

	scope := strings.TrimSpace(matches[2])
	isBreaking := matches[3] == "!"
	desc := strings.TrimSpace(matches[4])
	if desc == "" {
		return nil
	}

	return &parsedCommit{
		Type:        ctype,
		Scope:       scope,
		IsBreaking:  isBreaking,
		Description: desc,
	}
}

func categorizePR(parsed parsedCommit, labels []string) (string, bool) {
	// PR with ! or breaking label always goes to breaking changes
	isBreaking := parsed.IsBreaking
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), "breaking") {
			isBreaking = true
			break
		}
	}
	if isBreaking {
		return CategoryBreakingChanges, true
	}

	// filter dependency updates
	isDependency := false
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), "dependencies") {
			isDependency = true
			break
		}
	}
	if isDependency || parsed.Scope == "deps" || parsed.Scope == "dependencies" {
		return "", false
	}

	switch parsed.Type {
	case "chore", "doc", "docs", "test", "ci", "build", "style", "release":
		return "", false
	case "feat":
		return CategoryAdded, true
	case "fix":
		return CategoryFixed, true
	case "refactor":
		return CategoryChanged, true
	default:
		return "", false
	}
}

func capitalizeFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

func curateReleaseNotes(ctx context.Context, input string, fetcher metadataFetcher) (string, error) {
	return curateReleaseNotesWithLogger(ctx, input, fetcher, log.Default())
}

func curateReleaseNotesWithLogger(ctx context.Context, input string, fetcher metadataFetcher, logger *log.Logger) (string, error) {
	// Locte the "## What's Changed" heading
	loc := reWhatsChanged.FindStringIndex(input)
	if loc == nil {
		return "", errors.New(`missing "## What's Changed" section in release notes`)
	}

	verbatimTail := input[loc[0]:]

	// Find the body of "What's Changed" up to the next H2 section (e.g., ## New Contributors)
	bodyStart := loc[1] - loc[0]
	nextLoc := reH2Heading.FindStringIndex(verbatimTail[bodyStart:])
	var whatsChangedBody string
	if nextLoc != nil {
		whatsChangedBody = verbatimTail[bodyStart : bodyStart+nextLoc[0]]
	} else {
		whatsChangedBody = verbatimTail[bodyStart:]
	}

	// Parse pr entries in the body
	lines := strings.Split(whatsChangedBody, "\n")
	totalPRsFound := 0

	categories := make(map[string][]string)
	for _, c := range categoryOrder {
		categories[c] = []string{}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || (!strings.HasPrefix(trimmed, "*") && !strings.HasPrefix(trimmed, "-")) {
			continue
		}

		m := prLineRegex.FindStringSubmatch(trimmed)
		if m == nil {
			if logger != nil {
				logger.Printf("[releasesummary] skipping non-matching bullet line: %s\n", trimmed)
			}
			continue
		}

		rawTitle := strings.TrimSpace(m[1])
		prNumStr := m[2]
		if prNumStr == "" {
			prNumStr = m[3]
		}
		prNum, err := strconv.Atoi(prNumStr)
		if err != nil {
			if logger != nil {
				logger.Printf("[releasesummary] skipping PR line with invalid number %q: %v\n", trimmed, err)
			}
			continue
		}

		totalPRsFound++

		cc := parseCommit(rawTitle)
		if cc == nil {
			if logger != nil {
				logger.Printf("[releasesummary] skipping PR #%d: title %q is not a supported Conventional Commit format\n", prNum, rawTitle)
			}
			continue
		}

		// fetch metadata (labels) if fetcher is available.
		var labels []string
		if fetcher != nil {
			l, fetchErr := fetcher.fetchPRLabels(ctx, prNum)
			if fetchErr != nil {
				if logger != nil {
					logger.Printf("[releasesummary] skipping PR #%d: metadata unavailable: %v\n", prNum, fetchErr)
				}
				continue
			}
			labels = l
		}

		cat, include := categorizePR(*cc, labels)
		if !include {
			continue
		}

		bullet := fmt.Sprintf("- %s (#%d)", capitalizeFirst(cc.Description), prNum)
		categories[cat] = append(categories[cat], bullet)
	}

	if totalPRsFound == 0 {
		return "", errors.New(`"## What's Changed" section is empty: no pull requests found`)
	}

	var summaryBuilder strings.Builder
	hasEntries := false

	for _, cat := range categoryOrder {
		bullets := categories[cat]
		if len(bullets) == 0 {
			continue
		}
		if hasEntries {
			summaryBuilder.WriteString("\n")
		}
		fmt.Fprintf(&summaryBuilder, "### %s\n\n", cat)
		for _, b := range bullets {
			summaryBuilder.WriteString(b)
			summaryBuilder.WriteString("\n")
		}
		hasEntries = true
	}

	if hasEntries {
		return summaryBuilder.String() + "\n" + verbatimTail, nil
	}
	return verbatimTail, nil
}
