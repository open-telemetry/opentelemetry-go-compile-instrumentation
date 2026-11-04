// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os/exec"
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
	// originBase and fetchBaseSpec address the base branch of the scan.
	originBase    = "refs/remotes/origin/" + defaultBranch
	fetchBaseSpec = "refs/heads/" + defaultBranch + ":" + originBase
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
		"branch":   {defaultBranch},
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
		fetchBaseSpec,
	); err != nil {
		return err
	}
	baseOut, err := git.Output(ctx, "rev-parse", originBase)
	if err != nil {
		return err
	}
	base := strings.TrimSpace(baseOut)
	// The probe must merge with the attributes of the repair merge: write the
	// trusted local attributes into the scan repository before any probe.
	if err = writeLocalAttributes(ctx, git); err != nil {
		return err
	}
	open, err := s.client.OpenPullRequests(ctx, s.repo, openPRLimit)
	if err != nil {
		return err
	}
	selected := []repairRequest{}
	for _, pull := range open {
		if pull.IsDraft || pull.Mergeable != conflictingState || pull.BaseRefName != defaultBranch {
			continue
		}
		if s.expiresAt <= s.app.now().Unix() {
			return errors.New("the automatic deadline has expired")
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		request, ok, probeErr := s.probe(ctx, git, pull.Number, base)
		if probeErr != nil {
			return probeErr
		}
		if !ok {
			continue
		}
		if !s.dryRun {
			selected = append(selected, request)
		}
		remaining--
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
// bundle and whether the repair merge can resolve them. git merge-tree
// merges the base into the head without a working tree, an index, or a
// commit, so the probe runs no contributor content and no hooks. A failure
// that affects only this pull request writes a warning annotation and skips
// it, so the scan continues and writes its record.
func (s selector) probe(
	ctx context.Context, git gitcmd.Runner, number int, base string,
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
		if ctxErr := contextError(ctx, err); ctxErr != nil {
			return repairRequest{}, false, ctxErr
		}
		// 404 and 410 mean the pull request went away between the listing and
		// the probe; every other response is a problem of the whole scan.
		var status *github.StatusError
		gone := errors.As(err, &status) &&
			(status.StatusCode == http.StatusNotFound || status.StatusCode == http.StatusGone)
		if gone {
			return repairRequest{}, false, s.warn(number, "the pull request cannot be read")
		}
		return repairRequest{}, false, err
	}
	if !eligiblePullRequest(pull, s.repo) {
		return repairRequest{}, false, s.say(label + " is not eligible for automatic repair.\n")
	}
	head := pull.Head.SHA
	if !sha1Pattern.MatchString(head) {
		return repairRequest{}, false, s.warn(number, "the head commit id is not valid")
	}
	fetched, err := fetchPullHead(ctx, git, number)
	if err != nil {
		if ctxErr := contextError(ctx, err); ctxErr != nil {
			return repairRequest{}, false, ctxErr
		}
		return repairRequest{}, false, s.warn(number, "the pull request head cannot be fetched")
	}
	if strings.TrimSpace(fetched) != head {
		return repairRequest{}, false, s.warn(number, "the fetched head does not match the reported head")
	}
	skipReason, probeErr := probeMerges(ctx, git, base, head)
	if probeErr != nil {
		if ctxErr := contextError(ctx, probeErr); ctxErr != nil {
			return repairRequest{}, false, ctxErr
		}
		return repairRequest{}, false, s.warn(number, "the conflict probe failed")
	}
	if skipReason != "" {
		return repairRequest{}, false, s.say(label + " " + skipReason + ". Skipping.\n")
	}
	message := "Selected bundle repair for " + label + ".\n"
	if s.dryRun {
		message = "Dry run: would request bundle repair for " + label + ".\n"
	}
	if err = s.say(message); err != nil {
		return repairRequest{}, false, err
	}
	return repairRequest{PRNumber: number, HeadSHA: head, BaseSHA: base, ExpiresAt: s.expiresAt}, true, nil
}

func (s selector) say(text string) error {
	_, err := s.app.stdout.Write([]byte(text))
	return err
}

// warn writes a GitHub warning annotation for one pull request. The reason is
// a fixed line without command output, so no token and no repository data
// leaks into the annotation.
func (s selector) warn(number int, reason string) error {
	return s.say("::warning::PR #" + strconv.Itoa(number) + ": " + reason + "\n")
}

// fetchPullHead fetches the head of one pull request and returns the commit
// id that the remote reported.
func fetchPullHead(ctx context.Context, git gitcmd.Runner, number int) (string, error) {
	if err := git.Run(
		ctx,
		"fetch",
		"--no-prune",
		"--no-tags",
		"origin",
		"refs/pull/"+strconv.Itoa(number)+"/head",
	); err != nil {
		return "", err
	}
	return git.Output(ctx, "rev-parse", "FETCH_HEAD")
}

// probeMerges runs the two conflict probes of one pull request and returns
// the reason a pull request is skipped, or an empty reason when the scan
// selects it. Probe A runs the merge with the bundle merge driver off: the
// merge must conflict on the bundle alone, because a driver registered in
// the scan checkout must not hide the conflict, and any conflict outside the
// bundle must stay visible. Probe B runs the merge with the driver of the
// repair merge: the merge must be clean, because a delete/modify conflict
// on the bundle stays a conflict that the repair cannot resolve.
func probeMerges(ctx context.Context, git gitcmd.Runner, base, head string) (string, error) {
	conflicts, err := conflictPaths(ctx, git, false, base, head)
	if err != nil {
		return "", err
	}
	switch {
	case len(conflicts) == 0:
		return "no longer conflicts with the base", nil
	case len(conflicts) > 1 || conflicts[0] != bundleFile:
		return "has conflicts outside the bundle", nil
	}
	repair, err := conflictPaths(ctx, git, true, base, head)
	if err != nil {
		return "", err
	}
	if len(repair) != 0 {
		return "has a bundle conflict the repair merge cannot resolve", nil
	}
	return "", nil
}

// contextError returns the context error of a cancelled or expired scan, so
// the scan fails instead of skipping pull requests. A nil result means the
// error is not a cancellation.
func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// conflictPaths reports the paths that conflict when the base merges into the
// head. A nil result without an error is a clean merge. The merge runs with
// the trusted info/attributes of the scan repository, which pin the bundle
// path to the otelc-bundle driver and unset every other driver, filter, and
// conversion, so the probe mirrors the repair merge. The repairDriver
// setting selects the bundle driver: off for the conflict probe, on for the
// repair probe. Git exits 0 for a clean merge and 1 for conflicts; any other
// exit status is an error. Some git versions, including 2.56, also exit 1
// for failures such as a missing object, so output that does not parse is an
// error too.
func conflictPaths(ctx context.Context, git gitcmd.Runner, repairDriver bool, base, head string) ([]string, error) {
	driver := "false"
	if repairDriver {
		driver = "true"
	}
	var out bytes.Buffer
	probeGit := git
	probeGit.Stdout = &out
	// The -c setting overrides a driver that the checkout registered, for
	// example after make setup-git.
	probeGit.Config = append(slices.Clone(git.Config), "merge.otelc-bundle.driver="+driver)
	err := probeGit.Run(
		ctx,
		"merge-tree",
		"--write-tree",
		"--name-only",
		"--no-messages",
		"-z",
		base,
		head,
	)
	if err == nil {
		return nil, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return nil, errs.Wrap(err, "probe the conflicts with git merge-tree")
	}
	return parseConflictPaths(out.Bytes())
}

// parseConflictPaths splits the NUL-separated output of
// git merge-tree --write-tree --name-only -z: the tree of the merge first,
// then one conflicted path per entry. The tree must be a full object id, and
// every path must hold text.
func parseConflictPaths(out []byte) ([]string, error) {
	entries := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !sha1Pattern.MatchString(entries[0]) {
		return nil, errors.New("the merge-tree output has no tree")
	}
	if slices.Contains(entries[1:], "") {
		return nil, errors.New("the merge-tree output has an empty path")
	}
	return entries[1:], nil
}
