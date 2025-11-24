// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This tool applies GitHub pull request labels based on the pull request's
// title, ensuring that it adheres to conventional commit standards used by this
// repository. It automatically removes any outdated labels that would have been
// assigned by this tool; and leaves any other labels untouched.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-github/v79/github"
)

var (
	conventionalLabels = map[string]string{
		"chore":    "scope:chore",
		"doc":      "scope:chore",
		"docs":     "scope:chore",
		"feat":     "scope:feat",
		"fix":      "scope:fix",
		"release":  "scope:chore",
		"refactor": "scope:refactor",
	}
	titleRegexp = regexp.MustCompile(fmt.Sprintf(`^(%s)(?:\(.+\))?!?: .*$`, strings.Join(slices.Collect(maps.Keys(conventionalLabels)), "|")))
)

func main() {
	var (
		prOwner  string
		prRepo   string
		prNumber int = -1
	)

	// Get default values from environment if running under GHA.
	eventName := os.Getenv("GITHUB_EVENT_NAME")
	if eventName == "pull_request" || eventName == "pull_request_target" {
		path := os.Getenv("GITHUB_EVENT_PATH")
		if path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Fatalln(err)
			}
			var event struct {
				Number     int `json:"number"`
				Repository struct {
					Name  string `json:"name"`
					Owner struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"repository"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				log.Fatalln(err)
			}
			prRepo = event.Repository.Name
			prOwner = event.Repository.Owner.Login
			prNumber = event.Number
		}
	}

	flags := flag.NewFlagSet("conventionalcommit", flag.ExitOnError)
	flags.StringVar(&prOwner, "owner", prOwner, "The owner of the repository on which the pull request is made")
	flags.StringVar(&prRepo, "repo", prRepo, "The repository on which the pull request is made")
	flags.IntVar(&prNumber, "pr", prNumber, "The pull request number to apply labels to")

	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatalln(err)
	}

	if prOwner == "" {
		log.Fatalln("Missing -owner flag")
	}
	if prRepo == "" {
		log.Fatalln("Missing -repo flag")
	}
	if prNumber <= 0 {
		log.Fatalln("Missing -pr flag")
	}

	ctx := context.Background()
	client := github.NewClient(nil)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		client = client.WithAuthToken(token)
	}

	pr, _, err := client.PullRequests.Get(ctx, prOwner, prRepo, prNumber)
	if err != nil {
		log.Fatalln(err)
	}

	defer cleanupOldComments(ctx, client, pr)

	title := pr.GetTitle()
	log.Printf("The title of this pull request is: %q\n", title)

	prLabel, labelError := labelForTitle(title)
	if labelError != nil {
		log.Fatalln(errors.Join(labelError, createReviewComment(ctx, client, pr, labelError)))
	}
	log.Printf("Conventional commit label for this pull request is: %q\n", prLabel)

	if _, _, err := client.Issues.AddLabelsToIssue(ctx, prOwner, prRepo, prNumber, []string{prLabel}); err != nil {
		log.Fatalf("Failed to add the label to the pull request: %v\n", err)
	}

	for _, label := range conventionalLabels {
		if label == prLabel {
			continue
		}
		if resp, err := client.Issues.RemoveLabelForIssue(ctx, prOwner, prRepo, prNumber, label); err != nil {
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				log.Fatalf("Failed to remove label %q from pull request: %v\n", label, err)
			}
		} else {
			log.Printf("Removed outdated label from pull request: %q\n", label)
		}
	}
}

func labelForTitle(title string) (string, ConventionalCommitError) {
	matches := titleRegexp.FindStringSubmatch(title)
	if matches == nil {
		return "", InvalidFormatError{}
	}
	ctype := matches[1]

	label := conventionalLabels[ctype]
	if label == "" {
		return "", InvalidCommitType{Type: ctype}
	}

	return label, nil
}

const commentMarker = "<!-- .github/cools/conventionalcommit -->\n"

func cleanupOldComments(ctx context.Context, client *github.Client, pr *github.PullRequest) func() {
	oldComments, err := findExistingToolComments(ctx, client, pr)
	if err != nil {
		log.Println("Failed to list existing PR comments, cannot clean up outdated ones:", err)
		return func() {}
	}

	return func() {
		repo := pr.GetBase().GetRepo()
		prOwner := repo.GetOwner().GetLocation()
		prRepo := repo.GetName()
		for _, comment := range oldComments {
			client.Issues.DeleteComment(ctx, prOwner, prRepo, comment.GetID())
		}
	}
}

func findExistingToolComments(ctx context.Context, client *github.Client, pr *github.PullRequest) ([]*github.IssueComment, error) {
	opts := &github.IssueListCommentsOptions{}

	repo := pr.GetBase().GetRepo()
	prOwner := repo.GetOwner().GetLocation()
	prRepo := repo.GetName()
	prNumber := pr.GetNumber()

	oldComments := make([]*github.IssueComment, 0, pr.GetComments())
	for {
		comments, resp, err := client.Issues.ListComments(ctx, prOwner, prRepo, prNumber, opts)
		if err != nil {
			return nil, err
		}

		for _, comment := range comments {
			if comment.User == nil || comment.User.Login == nil || *comment.User.Login != "github-actions[bot]" {
				continue
			}
			if comment.Body != nil && strings.HasPrefix(*comment.Body, commentMarker) {
				oldComments = append(oldComments, comment)
			}
		}

		if resp.NextPage == 0 {
			return oldComments, nil
		}
		opts.ListOptions.Page = resp.NextPage
	}
}

func createReviewComment(ctx context.Context, client *github.Client, pr *github.PullRequest, cause ConventionalCommitError) error {
	repo := pr.GetBase().GetRepo()
	_, _, err := client.Issues.CreateComment(ctx, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber(), &github.IssueComment{
		Body: github.Ptr(commentMarker + cause.ReviewComment()),
	})
	return err
}
