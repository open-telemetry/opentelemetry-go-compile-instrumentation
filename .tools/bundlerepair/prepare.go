// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/ghaction"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const regenerateCommand = "/regenerate-bundle"

// prepare checks a bundle repair request and captures the pull request
// snapshot. It reads the variables of the authorize command and EVENT_NAME,
// ISSUE_IS_PULL_REQUEST, AUTHOR_ASSOCIATION, COMMENT_BODY and INPUT_PR_NUMBER.
// A comment that is not the command writes authorized=false and succeeds.
func (a app) prepare(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("prepare takes no arguments")
	}
	files := ghaction.FromEnv(a.getenv)
	if !a.requestedByCommandOrInput() {
		return files.SetOutput("authorized", "false")
	}
	req := a.authorizeRequest()
	if err := authorize(ctx, req, a.now()); err != nil {
		return err
	}
	if !numberPattern.MatchString(req.PRNumber) {
		return errors.New("PR_NUMBER is not a pull request number")
	}
	number, err := strconv.Atoi(req.PRNumber)
	if err != nil {
		return errors.New("PR_NUMBER is not a pull request number")
	}
	client, err := github.New(req.APIURL, req.Token)
	if err != nil {
		return err
	}
	pull, err := client.PullRequest(ctx, req.Repo, number)
	if err != nil {
		return err
	}
	automatic := req.CallerWorkflow == req.Repo+autofixCaller
	if !preparable(pull, req, automatic) {
		return errors.New("the pull request is closed or no longer matches the request")
	}
	baseRef, headRef, headSHA, headRepo := pull.Base.Ref, pull.Head.Ref, pull.Head.SHA, pull.Head.Repo.FullName
	git := gitcmd.Runner{}
	for _, ref := range []string{baseRef, headRef} {
		if ref == "" {
			return errors.New("the pull request has an empty branch name")
		}
		if err = git.Run(ctx, "check-ref-format", "refs/heads/"+ref); err != nil {
			return err
		}
	}
	ref, err := client.Ref(ctx, req.Repo, "heads/"+baseRef)
	if err != nil {
		return err
	}
	if ref.Object.Type != objectCommit {
		return errors.New("the base branch does not point at a commit")
	}
	baseSHA := ref.Object.SHA
	if automatic && baseSHA != req.BaseSHA {
		return errors.New("the captured base branch head is no longer current")
	}
	if !repoPattern.MatchString(headRepo) || !sha1Pattern.MatchString(baseSHA) || !sha1Pattern.MatchString(headSHA) {
		return errors.New("the pull request has invalid repository or commit metadata")
	}
	return writeOutputs(files, [][2]string{
		{"authorized", trueValue},
		{"pr_number", req.PRNumber},
		{"base_ref", baseRef},
		{"base_sha", baseSHA},
		{"head_ref", headRef},
		{"head_repo_full", headRepo},
		{"head_sha", headSHA},
		{"is_cross_repository", strconv.FormatBool(headRepo != req.Repo)},
	})
}

// writeOutputs writes every pair or none of them when a value is not one line.
func writeOutputs(files ghaction.Files, pairs [][2]string) error {
	for _, pair := range pairs {
		if strings.ContainsAny(pair[1], "\r\n") {
			return errors.New("output " + pair[0] + " is not one line")
		}
	}
	for _, pair := range pairs {
		if err := files.SetOutput(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

// requestedByCommandOrInput reports whether the event is the regenerate
// command from a trusted commenter on a pull request, or has a pull request
// number input.
func (a app) requestedByCommandOrInput() bool {
	if a.getenv("INPUT_PR_NUMBER") != "" {
		return true
	}
	return a.getenv("EVENT_NAME") == "issue_comment" &&
		a.getenv("ISSUE_IS_PULL_REQUEST") == "true" &&
		slices.Contains([]string{"OWNER", "MEMBER", "COLLABORATOR"}, a.getenv("AUTHOR_ASSOCIATION")) &&
		a.getenv("COMMENT_BODY") == regenerateCommand
}

// preparable reports whether the pull request may be repaired. A manual
// request needs only an open pull request with a head repository; an
// automatic request must also match the scanned head and stay eligible.
func preparable(pull github.PullRequest, req authorizeRequest, automatic bool) bool {
	if pull.State != stateOpen || pull.Head.Repo == nil {
		return false
	}
	if !automatic {
		return true
	}
	return pull.Head.SHA == req.ExpectedHeadSHA && eligiblePullRequest(pull, req.Repo)
}
