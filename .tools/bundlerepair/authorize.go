// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"time"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const (
	// maxAutomaticWindow bounds how far ahead an automatic request deadline may be.
	maxAutomaticWindow = time.Hour

	autofixCaller    = "/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
	regenerateCaller = "/.github/workflows/regenerate-bundle.yaml@refs/heads/main"
	firstAttempt     = "1"
	defaultBranch    = "main"
	stateOpen        = "open"
	objectCommit     = "commit"
	trueValue        = "true"
)

var (
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	actorPattern    = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	deadlinePattern = regexp.MustCompile(`^[1-9][0-9]{0,10}$`)
	numberPattern   = regexp.MustCompile(`^[1-9][0-9]*$`)
	sha1Pattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// authorizeRequest holds the environment input of the authorize command.
type authorizeRequest struct {
	Repo            string
	CallerWorkflow  string
	Commenter       string
	RunAttempt      string
	TriggeringActor string
	AutoDeadline    string
	PRNumber        string
	ExpectedHeadSHA string
	BaseSHA         string
	APIURL          string
	Token           string
}

func (a app) authorizeRequest() authorizeRequest {
	return authorizeRequest{
		Repo:            a.getenv("REPO"),
		CallerWorkflow:  a.getenv("CALLER_WORKFLOW_REF"),
		Commenter:       a.getenv("COMMENTER"),
		RunAttempt:      cmp.Or(a.getenv("RUN_ATTEMPT"), firstAttempt),
		TriggeringActor: a.getenv("TRIGGERING_ACTOR"),
		AutoDeadline:    a.getenv("AUTO_DEADLINE"),
		PRNumber:        a.getenv("PR_NUMBER"),
		ExpectedHeadSHA: a.getenv("EXPECTED_HEAD_SHA"),
		BaseSHA:         a.getenv("BASE_SHA"),
		APIURL:          a.getenv("GITHUB_API_URL"),
		Token:           a.getenv("GH_TOKEN"),
	}
}

// authorize checks that the request comes from a trusted caller and, for a
// manual request, from a maintainer. It reads REPO, CALLER_WORKFLOW_REF,
// COMMENTER, RUN_ATTEMPT, TRIGGERING_ACTOR, AUTO_DEADLINE, PR_NUMBER,
// EXPECTED_HEAD_SHA, BASE_SHA, GITHUB_API_URL and GH_TOKEN.
func (a app) authorize(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("authorize takes no arguments")
	}
	return authorize(ctx, a.authorizeRequest(), a.now())
}

func authorize(ctx context.Context, req authorizeRequest, now time.Time) error {
	if req.Repo == "" {
		return errors.New("REPO must be set")
	}
	if !repoPattern.MatchString(req.Repo) {
		return errors.New("REPO " + strconv.Quote(req.Repo) + " is not an owner/name pair")
	}
	client, err := github.New(req.APIURL, req.Token)
	if err != nil {
		return err
	}
	if req.CallerWorkflow == req.Repo+autofixCaller {
		return checkAutomaticRequest(ctx, client, req, now)
	}
	if req.CallerWorkflow != "" && req.CallerWorkflow != req.Repo+regenerateCaller {
		return errors.New("this workflow is not an authorized bundle repair caller")
	}
	if req.Commenter == "" {
		return errors.New("COMMENTER must be set")
	}
	actors := []string{req.Commenter}
	if req.RunAttempt != firstAttempt {
		if req.TriggeringActor == "" {
			return errors.New("TRIGGERING_ACTOR must be set on reruns")
		}
		actors = append(actors, req.TriggeringActor)
	}
	for _, actor := range actors {
		if err = checkMaintainer(ctx, client, req.Repo, actor); err != nil {
			return err
		}
	}
	return nil
}

func checkMaintainer(ctx context.Context, client *github.Client, repo, actor string) error {
	if !actorPattern.MatchString(actor) {
		return errors.New(strconv.Quote(actor) + " is not a valid user name")
	}
	permission, err := client.CollaboratorPermission(ctx, repo, actor)
	if err != nil {
		return err
	}
	// GitHub reports maintain access as permission=write, so inspect role_name.
	if !slices.Contains([]string{"maintain", "admin"}, permission.RoleName) {
		return errors.New(actor + " must have the maintain or admin role to regenerate bundles")
	}
	return nil
}

func checkAutomaticRequest(ctx context.Context, client *github.Client, req authorizeRequest, now time.Time) error {
	if req.RunAttempt != firstAttempt {
		return errors.New("start a new scanner run instead of retrying automatic repairs")
	}
	if !deadlinePattern.MatchString(req.AutoDeadline) {
		return errors.New("the automatic request has no valid unexpired deadline")
	}
	deadline, err := strconv.ParseInt(req.AutoDeadline, 10, 64)
	if err != nil {
		return errors.New("the automatic request has no valid unexpired deadline")
	}
	current := now.Unix()
	if deadline <= current || deadline > current+int64(maxAutomaticWindow/time.Second) {
		return errors.New("the automatic request has no valid unexpired deadline")
	}
	if req.PRNumber == "" {
		return nil
	}
	if !numberPattern.MatchString(req.PRNumber) || !sha1Pattern.MatchString(req.ExpectedHeadSHA) ||
		!sha1Pattern.MatchString(req.BaseSHA) {
		return errors.New("the automatic request has invalid pull request metadata")
	}
	number, err := strconv.Atoi(req.PRNumber)
	if err != nil {
		return errors.New("the automatic request has invalid pull request metadata")
	}
	pull, err := client.PullRequest(ctx, req.Repo, number)
	if err != nil {
		return err
	}
	if !pullRequestEligible(pull, req) {
		return errors.New("the automatic request changed or is no longer eligible")
	}
	ref, err := client.Ref(ctx, req.Repo, "heads/"+defaultBranch)
	if err != nil {
		return err
	}
	if ref.Object.Type != objectCommit || ref.Object.SHA != req.BaseSHA {
		return errors.New("the captured base branch head is no longer current")
	}
	return nil
}

func pullRequestEligible(pull github.PullRequest, req authorizeRequest) bool {
	return pull.Head.SHA == req.ExpectedHeadSHA && eligiblePullRequest(pull, req.Repo)
}

// eligiblePullRequest reports whether the pull request is open, is not a
// draft, targets the default branch, and lets repository maintainers update
// its head. The selection, authorization, and preparation checks all extend
// this shared rule with their own conditions.
func eligiblePullRequest(pull github.PullRequest, repo string) bool {
	return pull.State == stateOpen &&
		pull.Draft != nil && !*pull.Draft &&
		pull.Base.Ref == defaultBranch &&
		pull.Head.Repo != nil &&
		(pull.Head.Repo.FullName == repo || pull.MaintainerCanModify)
}
