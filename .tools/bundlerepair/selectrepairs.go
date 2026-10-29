// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/ghaction"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const (
	hourlyLimit = 10
	// requestWindow is the window of the hourly limit. The lookback covers
	// the window plus a scan that starts within one hour and lasts at most
	// ten minutes.
	requestWindow   = time.Hour
	requestLookback = 3 * time.Hour
	maxRunPages     = 1000
	openPRLimit     = 200

	autofixWorkflow  = "autofix-bundle-conflicts.yaml"
	selectionJob     = "Select bundle repairs"
	boundedStep      = "Select bounded bundle repairs"
	selectionPrefix  = "Bundle repair selection: "
	budgetFlag       = "--budget"
	isoLayout        = "2006-01-02T15:04:05Z"
	conflictingState = "CONFLICTING"
)

// repairRequest is one selected pull request. The field order is the order of
// the JSON that the workflow matrix reads.
type repairRequest struct {
	PRNumber  int    `json:"pr_number"`
	HeadSHA   string `json:"head_sha"`
	BaseSHA   string `json:"base_sha"`
	ExpiresAt int64  `json:"expires_at"`
}

// selectionRecord is the JSON in the name of the selection step, as the scan
// writes it.
type selectionRecord struct {
	At  int64 `json:"at"`
	PRs []int `json:"prs"`
}

// parsedRecord is a checked selection record. The pull request numbers keep
// the text of the record, so the budget output prints them as the record does.
type parsedRecord struct {
	At  int64
	PRs []json.Number
}

// selectRepairs implements the select command. Without arguments it selects
// the pull requests for automatic repair. With --budget it reports the
// automatic requests of the last hour. It reads REPO, GH_TOKEN, RUN_ID,
// GITHUB_OUTPUT, GITHUB_API_URL, DRY_RUN, AUTO_DEADLINE and RECENT_REQUESTS.
func (a app) selectRepairs(ctx context.Context, args []string) error {
	repo, token, runID, outputPath := a.getenv(
		"REPO",
	), a.getenv(
		"GH_TOKEN",
	), a.getenv(
		"RUN_ID",
	), a.getenv(
		"GITHUB_OUTPUT",
	)
	for name, value := range map[string]string{"REPO": repo, "GH_TOKEN": token, "RUN_ID": runID, "GITHUB_OUTPUT": outputPath} {
		if value == "" {
			return errors.New(name + " must be set")
		}
	}
	files := ghaction.FromEnv(a.getenv)
	if err := files.SetOutput("prs", "[]"); err != nil {
		return err
	}
	if !repoPattern.MatchString(repo) || !numberPattern.MatchString(runID) {
		return errors.New("REPO or RUN_ID is not valid")
	}
	budgetOnly := len(args) == 1 && args[0] == budgetFlag
	if len(args) != 0 && !budgetOnly {
		return errors.New("select takes no argument or --budget")
	}
	client, err := github.New(a.getenv("GITHUB_API_URL"), token)
	if err != nil {
		return err
	}
	now := a.now()
	s := selector{app: a, client: client, repo: repo, files: files, dryRun: a.getenv("DRY_RUN") == trueValue}
	s.deadline = a.getenv("AUTO_DEADLINE")
	recentJSON, recentSet := a.lookup("RECENT_REQUESTS")
	if budgetOnly || !recentSet {
		runIDNumber, parseErr := strconv.ParseInt(runID, 10, 64)
		if parseErr != nil {
			return errors.New("RUN_ID is not valid")
		}
		var requests []json.Number
		s.deadline, requests, err = s.loadBudget(ctx, runIDNumber, now)
		if err != nil {
			return err
		}
		data, marshalErr := json.Marshal(requests)
		if marshalErr != nil {
			return errs.Wrap(marshalErr, "encode the requests")
		}
		recentJSON = string(data)
	}
	if !deadlinePattern.MatchString(s.deadline) {
		return errors.New("AUTO_DEADLINE is not valid")
	}
	deadline, err := strconv.ParseInt(s.deadline, 10, 64)
	if err != nil || deadline <= now.Unix() {
		return errors.New("the automatic deadline has expired")
	}
	s.expiresAt = deadline
	requests, err := parseRequests(recentJSON)
	if err != nil {
		return err
	}
	s.requests = requests
	remaining := hourlyLimit - len(requests)
	if budgetOnly {
		return s.writeBudget(recentJSON, max(remaining, 0))
	}
	if remaining <= 0 {
		_, err = a.stdout.Write(
			[]byte("Automatic repair limit reached: " + strconv.Itoa(len(requests)) + " requests in the last hour.\n"),
		)
		return err
	}
	return s.selectPullRequests(ctx, remaining)
}

type selector struct {
	app      app
	client   *github.Client
	repo     string
	files    ghaction.Files
	dryRun   bool
	deadline string
	// expiresAt is the parsed deadline.
	expiresAt int64
	requests  []json.Number
}

func (s selector) writeBudget(requestsJSON string, remaining int) error {
	if err := s.files.SetOutput("requests", requestsJSON); err != nil {
		return err
	}
	if err := s.files.SetOutput("expires_at", s.deadline); err != nil {
		return err
	}
	return s.files.SetOutput("remaining", strconv.Itoa(remaining))
}

// decodeNumbers decodes one JSON value and keeps numbers as text.
func decodeNumbers(text string, out any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

// parseRequests accepts a JSON array of positive integers.
func parseRequests(text string) ([]json.Number, error) {
	var values []any
	if err := decodeNumbers(text, &values); err != nil || values == nil {
		return nil, errors.New("RECENT_REQUESTS is not a JSON array")
	}
	requests := make([]json.Number, 0, len(values))
	for _, value := range values {
		number, ok := value.(json.Number)
		if _, valid := positiveInt(value); !ok || !valid {
			return nil, errors.New("RECENT_REQUESTS holds a value that is not a positive integer")
		}
		requests = append(requests, number)
	}
	return requests, nil
}

// positiveInt reports whether value is a JSON number with a positive integer value.
func positiveInt(value any) (int, bool) {
	text, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	number, err := text.Float64()
	if err != nil || number <= 0 || number != math.Floor(number) || number > math.MaxInt32 {
		return 0, false
	}
	return int(number), true
}

// loadBudget checks the scanner run and returns the deadline of the automatic
// requests and the requests of the last hour.
func (s selector) loadBudget(ctx context.Context, runID int64, now time.Time) (string, []json.Number, error) {
	run, err := s.client.WorkflowRun(ctx, s.repo, runID)
	if err != nil {
		return "", nil, err
	}
	created, parseErr := time.Parse(isoLayout, run.CreatedAt)
	if parseErr != nil || run.RunAttempt != 1 || created.After(now) || !created.Add(requestWindow).After(now) {
		return "", nil, errors.New("start a new scanner run: this run is stale or is a retry")
	}
	deadline := strconv.FormatInt(created.Unix()+int64(requestWindow/time.Second), 10)
	requests, err := s.recentRequests(ctx, runID, now)
	return deadline, requests, err
}

// recentRequests lists the pull requests that earlier scanner runs selected
// during the last hour. It fails when the history is incomplete or unclear.
func (s selector) recentRequests(ctx context.Context, current int64, now time.Time) ([]json.Number, error) {
	query := url.Values{
		"branch":   {"main"},
		"created":  {">=" + now.Add(-requestLookback).UTC().Format(isoLayout)},
		"per_page": {"100"},
	}
	pages, err := s.client.WorkflowRunPages(ctx, s.repo, autofixWorkflow, query)
	if err != nil {
		return nil, err
	}
	ids, err := priorRunIDs(pages, current)
	if err != nil {
		return nil, err
	}
	requests := []json.Number{}
	for _, id := range ids {
		jobPages, jobsErr := s.client.JobPages(ctx, s.repo, id)
		if jobsErr != nil {
			return nil, jobsErr
		}
		prior, historyErr := selectedInWindow(jobPages, now)
		if historyErr != nil {
			return nil, historyErr
		}
		requests = append(requests, prior...)
	}
	return requests, nil
}

// priorRunIDs returns the ids of every run except the current one.
func priorRunIDs(pages []github.RunsPage, current int64) ([]int64, error) {
	if len(pages) == 0 {
		return nil, errors.New("cannot verify the repair budget")
	}
	var total int64
	for _, page := range pages {
		if page.TotalCount == nil || *page.TotalCount > maxRunPages {
			return nil, errors.New("cannot verify the repair budget")
		}
		total = max(total, *page.TotalCount)
	}
	var ids []int64
	count := 0
	for _, page := range pages {
		if page.WorkflowRuns == nil {
			return nil, errors.New("cannot verify the repair budget")
		}
		for _, run := range *page.WorkflowRuns {
			count++
			if run.ID != current {
				ids = append(ids, run.ID)
			}
		}
	}
	if int64(count) != total {
		return nil, errors.New("incomplete workflow run pages")
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("invalid workflow run id")
		}
	}
	return ids, nil
}

// selectedInWindow returns the pull requests that the selection record of one
// scanner run holds, when the record is not older than the request window.
func selectedInWindow(pages []github.JobsPage, now time.Time) ([]json.Number, error) {
	var total int64
	var jobs []github.Job
	for _, page := range pages {
		if page.Jobs == nil || page.TotalCount == nil {
			return nil, errors.New("missing workflow job pages")
		}
		total = max(total, *page.TotalCount)
		jobs = append(jobs, *page.Jobs...)
	}
	if int64(len(jobs)) != total {
		return nil, errors.New("incomplete workflow job pages")
	}
	if len(jobs) > 0 && !slices.ContainsFunc(jobs, func(job github.Job) bool { return job.Conclusion != "skipped" }) {
		return []json.Number{}, nil
	}
	var selection []github.Job
	for _, job := range jobs {
		if job.Name == selectionJob {
			selection = append(selection, job)
		}
	}
	if len(selection) != 1 {
		return nil, errors.New("missing repair selection history")
	}
	steps := selection[0].Steps
	if slices.ContainsFunc(steps, func(step github.Step) bool {
		return step.Name != nil && *step.Name == boundedStep && step.Conclusion == "skipped"
	}) {
		return []json.Number{}, nil
	}
	var records []string
	for _, step := range steps {
		if step.Name == nil {
			return nil, errors.New("a selection step has no name")
		}
		if text, found := strings.CutPrefix(*step.Name, selectionPrefix); found {
			records = append(records, text)
		}
	}
	if len(records) != 1 {
		return nil, errors.New("cannot prove the repair selection was empty")
	}
	record, err := parseSelectionRecord(records[0], now)
	if err != nil {
		return nil, err
	}
	if record.At >= now.Unix()-int64(requestWindow/time.Second) {
		return record.PRs, nil
	}
	return []json.Number{}, nil
}

// parseSelectionRecord decodes and checks the JSON of a selection record.
func parseSelectionRecord(text string, now time.Time) (parsedRecord, error) {
	var raw struct {
		At  any `json:"at"`
		PRs any `json:"prs"`
	}
	invalid := errors.New("invalid repair selection record")
	if err := decodeNumbers(text, &raw); err != nil {
		return parsedRecord{}, errs.Wrap(err, "decode the repair selection record")
	}
	at, ok := positiveInt64(raw.At)
	if !ok || at > now.Unix() {
		return parsedRecord{}, invalid
	}
	list, ok := raw.PRs.([]any)
	if !ok || len(list) > hourlyLimit {
		return parsedRecord{}, invalid
	}
	record := parsedRecord{At: at, PRs: make([]json.Number, 0, len(list))}
	seen := make([]int, 0, len(list))
	for _, value := range list {
		text, isNumber := value.(json.Number)
		number, valid := positiveInt(value)
		if !isNumber || !valid || slices.Contains(seen, number) {
			return parsedRecord{}, invalid
		}
		seen = append(seen, number)
		record.PRs = append(record.PRs, text)
	}
	return record, nil
}

func positiveInt64(value any) (int64, bool) {
	text, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	number, err := text.Float64()
	if err != nil || number <= 0 || number != math.Floor(number) || number >= math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}

// selectPullRequests probes every open conflicting pull request until the
// remaining budget is used. It writes the prs and rate_record outputs.
func (s selector) selectPullRequests(ctx context.Context, remaining int) error {
	git := gitcmd.Runner{Stdout: s.app.stdout}
	if err := git.Run(
		ctx,
		"fetch",
		"--no-prune",
		"--no-tags",
		"origin",
		"refs/heads/main:refs/remotes/origin/main",
	); err != nil {
		return err
	}
	baseOut, err := git.Output(ctx, "rev-parse", "refs/remotes/origin/main")
	if err != nil {
		return err
	}
	base := strings.TrimSpace(baseOut)
	open, err := s.client.OpenPullRequests(ctx, s.repo, openPRLimit)
	if err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "bundlerepair-")
	if err != nil {
		return errs.Wrap(err, "create the work directory")
	}
	checkout := filepath.Join(workDir, "checkout")
	defer func() {
		// Best effort: the worktree may not exist.
		_ = git.Run(context.WithoutCancel(ctx), "worktree", "remove", "--force", checkout)
		_ = os.RemoveAll(workDir)
	}()

	selected := []repairRequest{}
	for _, pull := range open {
		if pull.IsDraft || pull.Mergeable != conflictingState || pull.BaseRefName != "main" {
			continue
		}
		if s.expiresAt <= s.app.now().Unix() {
			return errors.New("the automatic deadline has expired")
		}
		request, ok, probeErr := s.probe(ctx, git, pull.Number, base, checkout)
		if probeErr != nil {
			return probeErr
		}
		if ok {
			if !s.dryRun {
				selected = append(selected, request)
			}
			remaining--
		}
		if remaining <= 0 {
			break
		}
	}
	return s.writeSelection(selected)
}

func (s selector) writeSelection(selected []repairRequest) error {
	data, err := json.Marshal(selected)
	if err != nil {
		return errs.Wrap(err, "encode the selection")
	}
	numbers := make([]int, 0, len(selected))
	for _, request := range selected {
		numbers = append(numbers, request.PRNumber)
	}
	record, err := json.Marshal(selectionRecord{At: s.app.now().Unix(), PRs: numbers})
	if err != nil {
		return errs.Wrap(err, "encode the selection record")
	}
	if err = s.files.SetOutput("prs", string(data)); err != nil {
		return err
	}
	return s.files.SetOutput("rate_record", string(record))
}

// probe reports whether the conflicts of one pull request are limited to the
// bundle. It merges the base into a temporary worktree of the pull request head.
func (s selector) probe(
	ctx context.Context, git gitcmd.Runner, number int, base, checkout string,
) (repairRequest, bool, error) {
	label := "PR #" + strconv.Itoa(number)
	if slices.ContainsFunc(s.requests, func(request json.Number) bool {
		value, _ := positiveInt(request)
		return value == number
	}) {
		return repairRequest{}, false, s.say(label + " has a request in the last hour. Skipping.\n")
	}
	pull, err := s.client.PullRequest(ctx, s.repo, number)
	if err != nil {
		return repairRequest{}, false, err
	}
	if !automaticEligible(pull, s.repo) {
		return repairRequest{}, false, s.say(label + " is not eligible for automatic repair.\n")
	}
	head := pull.Head.SHA
	if !sha1Pattern.MatchString(head) {
		return repairRequest{}, false, errors.New(label + " has an invalid head commit")
	}
	if err = git.Run(
		ctx,
		"fetch",
		"--no-prune",
		"--no-tags",
		"origin",
		"refs/pull/"+strconv.Itoa(number)+"/head",
	); err != nil {
		return repairRequest{}, false, err
	}
	fetched, err := git.Output(ctx, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return repairRequest{}, false, err
	}
	if strings.TrimSpace(fetched) != head {
		return repairRequest{}, false, nil
	}
	if err = git.Run(ctx, "worktree", "add", "--detach", checkout, head); err != nil {
		return repairRequest{}, false, err
	}
	mergeErr := mergeBase(ctx, checkout, base, s.app.stdout)
	request, ok := repairRequest{}, false
	if mergeErr == nil {
		ok = true
		request = repairRequest{PRNumber: number, HeadSHA: head, BaseSHA: base, ExpiresAt: s.expiresAt}
		if s.dryRun {
			err = s.say("Dry run: would request bundle repair for " + label + ".\n")
		} else {
			err = s.say("Selected bundle repair for " + label + ".\n")
		}
	} else {
		err = s.say(label + " has conflicts outside the bundle. Skipping.\n")
	}
	if err != nil {
		return repairRequest{}, false, err
	}
	if err = git.Run(ctx, "worktree", "remove", "--force", checkout); err != nil {
		return repairRequest{}, false, err
	}
	return request, ok, nil
}

func (s selector) say(text string) error {
	_, err := s.app.stdout.Write([]byte(text))
	return err
}

// automaticEligible reports whether an automatic repair may touch the pull request.
func automaticEligible(pull github.PullRequest, repo string) bool {
	return pull.State == stateOpen &&
		pull.Draft != nil && !*pull.Draft &&
		pull.Base.Ref == defaultBranch &&
		pull.Head.Repo != nil &&
		(pull.Head.Repo.FullName == repo || pull.MaintainerCanModify)
}
