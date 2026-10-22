// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This tool transforms GitHub-generated release notes by inserting a curated,
// categorized, author-free summary above the verbatim "## What's Changed" list.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/google/go-github/v79/github"
)

func main() {
	var (
		filePath string
		repo     string
	)

	flags := flag.NewFlagSet("releasesummary", flag.ExitOnError)
	flags.StringVar(&filePath, "file", "", "Path to the release notes markdown file to curate in-place")
	flags.StringVar(&repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository in owner/repo format")

	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatalln(err)
	}

	if filePath == "" && flags.NArg() > 0 {
		filePath = flags.Arg(0)
	}

	if filePath == "" {
		log.Fatalln("Missing -file flag: path to release notes markdown file is required")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Failed to read release notes file %q: %v\n", filePath, err)
	}

	ctx := context.Background()

	var fetcher metadataFetcher
	if repo != "" {
		parts := strings.Split(repo, "/")
		if len(parts) == 2 {
			token := os.Getenv("GH_TOKEN")
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			var client *github.Client
			if token != "" {
				client = github.NewClient(nil).WithAuthToken(token)
			} else {
				client = github.NewClient(nil)
			}
			fetcher = newGitHubMetadataFetcher(client, parts[0], parts[1])
		} else {
			log.Printf("Warning: repo %q is not in 'owner/repo' format; proceeding without GitHub metadata API\n", repo)
		}
	}

	curated, err := curateReleaseNotes(ctx, string(content), fetcher)
	if err != nil {
		log.Fatalf("Failed to curate release notes: %v\n", err)
	}

	if err := os.WriteFile(filePath, []byte(curated), 0o600); err != nil {
		log.Fatalf("Failed to write curated release notes to %q: %v\n", filePath, err)
	}

	log.Printf("Successfully curated release notes in %s\n", filePath)
}
