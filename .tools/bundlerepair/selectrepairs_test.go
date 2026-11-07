// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	runsRoute    = "GET /repos/test/upstream/actions/workflows/autofix-bundle-conflicts.yaml/runs"
	jobsRoute    = "GET /repos/test/upstream/actions/runs/123/attempts/1/jobs"
	currentRoute = "GET /repos/test/upstream/actions/runs/999"
	graphQLRoute = "POST /graphql"
)

// scanFixture mirrors scanner_fixture in the old shell test.
type scanFixture struct {
	api    *apiFake
	dir    string
	remote string
	head   string
	base   string
	output string
	env    map[string]string
}

func isoTime(unix int64) string { return time.Unix(unix, 0).UTC().Format(isoLayout) }

func newScanFixture(t *testing.T) *scanFixture {
	t.Helper()
	merge := newMergeFixture(t)
	head := gitIn(t, merge.dir, "rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "fork.git")
	gitIn(t, merge.dir, "clone", "--quiet", "--bare", ".", remote)
	gitIn(t, merge.dir, "remote", "add", "origin", remote)
	gitIn(t, remote, "fetch", "--no-prune", "--quiet", merge.dir,
		head+":refs/pull/1/head", head+":refs/pull/2/head")
	t.Chdir(merge.dir)

	fx := &scanFixture{
		api: newAPI(t), dir: merge.dir, remote: remote, head: head, base: merge.base,
		output: filepath.Join(t.TempDir(), "outputs"),
	}
	fx.env = map[string]string{
		"REPO": testRepo, "GH_TOKEN": "test-token", "RUN_ID": "999", "DRY_RUN": "false",
		"GITHUB_OUTPUT": fx.output, "GITHUB_API_URL": fx.api.URL,
	}
	fx.setRuns(`[{"total_count":2,"workflow_runs":[{"id":123},{"id":999}]}]`)
	fx.setHistory(1, 1)
	fx.setRun(testNow-60, 1)
	fx.setOpenPullRequests(1, 2)
	for _, number := range []int{1, 2} {
		fx.api.setBody("GET /repos/test/upstream/pulls/"+strconv.Itoa(number), fx.pullBody(t, nil))
	}
	return fx
}

func (fx *scanFixture) pullBody(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	return pullBody(t, func(pull map[string]any) {
		pull["head"] = map[string]any{
			"sha":  fx.head,
			"ref":  "contribution",
			"repo": map[string]any{"full_name": "test/fork"},
		}
		if mutate != nil {
			mutate(pull)
		}
	})
}

func (fx *scanFixture) setRun(created int64, attempt int) {
	fx.api.setBody(currentRoute, fmt.Sprintf(`{"created_at":%q,"run_attempt":%d}`, isoTime(created), attempt))
}

// setRuns serves the pages of the run list. The argument is a JSON array of
// pages. The first page answers the request and the others follow its Link header.
func (fx *scanFixture) setRuns(pagesJSON string) {
	var pages []json.RawMessage
	if err := json.Unmarshal([]byte(pagesJSON), &pages); err != nil {
		fx.api.set(runsRoute, jsonHandler(http.StatusOK, pagesJSON))
		return
	}
	fx.api.set(
		runsRoute,
		pagedHandler(fx.api.URL+"/repos/test/upstream/actions/workflows/autofix-bundle-conflicts.yaml/runs", pages),
	)
}

func (fx *scanFixture) setJobs(pagesJSON string) {
	var pages []json.RawMessage
	if err := json.Unmarshal([]byte(pagesJSON), &pages); err != nil {
		fx.api.set(jobsRoute, jsonHandler(http.StatusOK, pagesJSON))
		return
	}
	fx.api.set(jobsRoute, pagedHandler(fx.api.URL+"/repos/test/upstream/actions/runs/123/attempts/1/jobs", pages))
}

// pagedHandler serves page "?page=N" and links to the next page.
func pagedHandler(base string, pages []json.RawMessage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index := 0
		if page := r.URL.Query().Get("page"); page != "" {
			index, _ = strconv.Atoi(page)
		}
		if index >= len(pages) {
			http.Error(w, `{"message":"no such page"}`, http.StatusNotFound)
			return
		}
		if index+1 < len(pages) {
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=%d&per_page=100>; rel="next"`, base, index+1))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pages[index])
	}
}

// setHistory mirrors scanner_history: one earlier scanner run that selected
// the pull requests first to last-1 and ran a repair job for each.
func (fx *scanFixture) setHistory(first, last int) { fx.setHistoryAt(first, last, testNow, testNow) }

func (fx *scanFixture) setHistoryAt(first, last int, recordAt, completed int64) {
	prs := []int{}
	for pr := first; pr < last; pr++ {
		prs = append(prs, pr)
	}
	record, _ := json.Marshal(map[string]any{"at": recordAt, "prs": prs})
	jobs := make([]map[string]any, 0, 1+3*len(prs))
	jobs = append(jobs, map[string]any{
		"name": "Select bundle repairs", "completed_at": isoTime(completed),
		"steps": []map[string]any{{"name": selectionPrefix + string(record), "conclusion": "success"}},
	})
	for _, pr := range prs {
		for _, step := range []string{"Prepare bundle regeneration", "Generate bundle", "Push regenerated bundle"} {
			jobs = append(
				jobs,
				map[string]any{"name": "Bundle repair #" + strconv.Itoa(pr) + " / " + step, "conclusion": "failure"},
			)
		}
	}
	fx.setJobsList(jobs)
}

func (fx *scanFixture) setJobsList(jobs []map[string]any) {
	page, _ := json.Marshal(map[string]any{"total_count": len(jobs), "jobs": jobs})
	fx.setJobs("[" + string(page) + "]")
}

func (fx *scanFixture) setOpenPullRequests(numbers ...int) {
	nodes := make([]string, 0, len(numbers))
	for _, number := range numbers {
		nodes = append(
			nodes,
			fmt.Sprintf(`{"number":%d,"isDraft":false,"mergeable":"CONFLICTING","baseRefName":"main"}`, number),
		)
	}
	fx.setGraphQL(
		`{"data":{"repository":{"pullRequests":{"nodes":[` + strings.Join(
			nodes,
			",",
		) + `],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`,
	)
}

func (fx *scanFixture) setGraphQL(body string) { fx.api.setBody(graphQLRoute, body) }

func (fx *scanFixture) run(args ...string) (int, output) {
	return runApp(fx.api.t, fx.env, append([]string{"select"}, args...)...)
}

// outputs returns the lines of the output file.
func (fx *scanFixture) outputs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(fx.output)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// last returns the value of the last output line for key.
func (fx *scanFixture) last(t *testing.T, key string) (string, bool) {
	t.Helper()
	lines := fx.outputs(t)
	for _, line := range slices.Backward(lines) {
		if value, found := strings.CutPrefix(line, key+"="); found {
			return value, true
		}
	}
	return "", false
}

// plan mirrors check_plan: it checks the last prs output and returns its numbers.
func (fx *scanFixture) plan(t *testing.T, want string) {
	t.Helper()
	raw, ok := fx.last(t, "prs")
	if !ok {
		t.Fatal("no prs output")
	}
	var requests []repairRequest
	if err := json.Unmarshal([]byte(raw), &requests); err != nil {
		t.Fatalf("prs %q: %v", raw, err)
	}
	numbers := make([]int, 0, len(requests))
	for _, request := range requests {
		if request.HeadSHA != fx.head || request.BaseSHA != fx.base || request.PRNumber <= 0 ||
			request.ExpiresAt <= testNow {
			t.Errorf("the plan lost its captured snapshot: %+v", request)
		}
		numbers = append(numbers, request.PRNumber)
	}
	got, _ := json.Marshal(numbers)
	if string(got) != want {
		t.Errorf("plan = %s, want %s", got, want)
	}
}

// Shell group: test_scanner_limit. Failed requests count toward the limit.
func TestSelectLimitReached(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(1, 11)
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "Automatic repair limit reached: 10 requests in the last hour.") {
		t.Errorf("stdout = %q", out.stdout)
	}
	if _, found := fx.last(t, "rate_record"); found {
		t.Error("a limited scan wrote a rate record")
	}
	for _, request := range fx.api.requests() {
		if strings.Contains(request, "pulls") || request == graphQLRoute {
			t.Errorf("a limited scan called %s", request)
		}
	}
}

// Shell group: test_scanner_selection (select an editable fork within the
// remaining hourly budget).
func TestSelectWithinRemainingBudget(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(10, 19)
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[1]")
	record, _ := fx.last(t, "rate_record")
	if record != fmt.Sprintf(`{"at":%d,"prs":[1]}`, testNow) {
		t.Errorf("rate_record = %s", record)
	}
	if !strings.Contains(out.stdout, "Selected bundle repair for PR #1.") {
		t.Errorf("stdout = %q", out.stdout)
	}
	for _, request := range fx.api.requests() {
		if request == "GET /repos/test/upstream/pulls/2" {
			t.Error("the scan probed a pull request after the budget was used")
		}
	}
}

// Shell group: test_scanner_selection (skip a pull request during its
// one-hour cooldown).
func TestSelectSkipsCooldown(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(1, 2)
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[2]")
	if !strings.Contains(out.stdout, "PR #1 has a request in the last hour. Skipping.") {
		t.Errorf("stdout = %q", out.stdout)
	}
}

// Shell group: test_scanner_selection (dry run probes forks without requesting
// repairs).
func TestSelectDryRun(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(1, 1)
	fx.env["DRY_RUN"] = "true"
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "Dry run: would request bundle repair for PR #1.") ||
		!strings.Contains(out.stdout, "Dry run: would request bundle repair for PR #2.") {
		t.Errorf("stdout = %q", out.stdout)
	}
	record, _ := fx.last(t, "rate_record")
	if record != fmt.Sprintf(`{"at":%d,"prs":[]}`, testNow) {
		t.Errorf("rate_record = %s", record)
	}
}

// Shell group: test_scanner_selection (expired requests do not consume the
// current hourly budget).
func TestSelectIgnoresExpiredRequests(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistoryAt(1, 11, testNow-7200, testNow-7200)
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[1,2]")
}

// Shell group: test_scanner_selection (fail closed when the rate-limit query
// fails) and the missing or truncated run page groups.
func TestSelectFailsClosedOnRunPages(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not JSON", `invalid`},
		{"no pages", `[]`},
		{"missing total", `[{"workflow_runs":[{"id":123}]}]`},
		{"missing run list", `[{"total_count":1}]`},
		{"too many runs", `[{"total_count":1001,"workflow_runs":[]}]`},
		{"truncated", `[{"total_count":2,"workflow_runs":[{"id":123}]}]`},
		{"extra runs", `[{"total_count":1,"workflow_runs":[{"id":123},{"id":124}]}]`},
		{"invalid run id", `[{"total_count":2,"workflow_runs":[{"id":0},{"id":999}]}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setRuns(tt.body)
			if code, _ := fx.run(); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			fx.plan(t, "[]")
		})
	}
}

// Shell group: test_scanner_selection (incomplete selection history cannot
// reset the budget) and the missing or truncated job page groups.
func TestSelectFailsClosedOnJobPages(t *testing.T) {
	selection := `{"name":"Select bundle repairs","steps":[{"name":"Bundle repair selection: {\"at\":1,\"prs\":[]}","conclusion":"success"}]}`
	tests := []struct {
		name string
		body string
	}{
		{"incomplete history", `[{"total_count":4,"jobs":[` + selection + `]}]`},
		{"no pages", `[]`},
		{"no jobs", `[{"total_count":1,"jobs":[]}]`},
		{"missing total", `[{"jobs":[` + selection + `]}]`},
		{"missing job list", `[{"total_count":1}]`},
		{"two selection jobs", `[{"total_count":2,"jobs":[` + selection + `,` + selection + `]}]`},
		{"no selection job", `[{"total_count":1,"jobs":[{"name":"Other","conclusion":"success"}]}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setRuns(`[{"total_count":1,"workflow_runs":[{"id":123}]}]`)
			fx.setJobs(tt.body)
			if code, _ := fx.run(); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			fx.plan(t, "[]")
		})
	}
}

// Shell group: test_scanner_selection (stale scanner runs cannot escape the
// history window).
func TestSelectRejectsStaleOrRetriedRun(t *testing.T) {
	tests := []struct {
		name    string
		created int64
		attempt int
	}{
		{"stale", testNow - 7200, 1},
		{"exactly one hour old", testNow - 3600, 1},
		{"retry", testNow - 60, 2},
		{"created in the future", testNow + 60, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setRun(tt.created, tt.attempt)
			code, out := fx.run()
			if code != exitFailure || !strings.Contains(out.stderr, "start a new scanner run") {
				t.Errorf("exit code = %d, stderr = %q", code, out.stderr)
			}
			fx.plan(t, "[]")
		})
	}
	t.Run("unparsable date", func(t *testing.T) {
		fx := newScanFixture(t)
		fx.api.setBody(currentRoute, `{"created_at":"yesterday","run_attempt":1}`)
		if code, _ := fx.run(); code != exitFailure {
			t.Errorf("exit code = %d", code)
		}
	})
}

// Go test TestScannerUsesLiveBase.
func TestSelectUsesLiveBase(t *testing.T) {
	fx := newScanFixture(t)
	prior := gitIn(t, fx.dir, "merge-base", fx.head, fx.base)
	for _, number := range []string{"1", "2"} {
		fx.api.setBody("GET /repos/test/upstream/pulls/"+number, fx.pullBody(t, func(pull map[string]any) {
			pull["base"] = map[string]any{"sha": prior, "ref": "main"}
		}))
	}
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[1,2]")
}

// Go test TestSelectionRecordedBeforeRepairJobs. The conclusion of the record
// step does not matter.
func TestSelectCountsRecordsOfEveryConclusion(t *testing.T) {
	for _, conclusion := range []string{"success", "failure", "skipped"} {
		t.Run(conclusion, func(t *testing.T) {
			fx := newScanFixture(t)
			record := fmt.Sprintf(`{"at":%d,"prs":[1,2,3,4,5,6,7,8,9,10]}`, testNow)
			fx.setJobsList([]map[string]any{{
				"name": "Select bundle repairs", "completed_at": isoTime(testNow),
				"steps": []map[string]any{{"name": selectionPrefix + record, "conclusion": conclusion}},
			}})
			if code, out := fx.run(); code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			fx.plan(t, "[]")
			if _, found := fx.last(t, "rate_record"); found {
				t.Error("the scan continued after ten recorded requests")
			}
		})
	}
}

// Go test TestAmbiguousSelectionHistory.
func TestSelectRejectsAmbiguousHistory(t *testing.T) {
	fx := newScanFixture(t)
	fx.setJobsList([]map[string]any{{
		"name": "Select bundle repairs", "completed_at": isoTime(testNow),
		"steps": []map[string]any{{"name": boundedStep, "conclusion": "success"}},
	}})
	if code, _ := fx.run(); code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	fx.plan(t, "[]")
}

// Go test TestSkippedSelectionDoesNotReserveBudget.
func TestSelectSkippedSelectionDoesNotReserveBudget(t *testing.T) {
	fx := newScanFixture(t)
	fx.setJobsList([]map[string]any{{
		"name": "Select bundle repairs",
		"steps": []map[string]any{
			{"name": boundedStep, "conclusion": "skipped"},
			{"name": selectionPrefix, "conclusion": "skipped"},
		},
	}})
	fx.env["DRY_RUN"] = "true"
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(strings.Join(fx.outputs(t), "\n"), "rate_record=") {
		t.Error("the skipped selection reserved the budget")
	}
}

func TestSelectAllSkippedRunCountsNothing(t *testing.T) {
	fx := newScanFixture(t)
	fx.setJobsList(
		[]map[string]any{
			{"name": "Select bundle repairs", "conclusion": "skipped"},
			{"name": "Other", "conclusion": "skipped"},
		},
	)
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[1,2]")
}

func TestSelectRecordValidation(t *testing.T) {
	tests := []struct {
		name    string
		record  string
		counts  []int
		wantErr bool
	}{
		{name: "explicit empty record", record: fmt.Sprintf(`{"at":%d,"prs":[]}`, testNow)},
		{name: "one request", record: fmt.Sprintf(`{"at":%d,"prs":[2]}`, testNow), counts: []int{2}},
		{name: "old record is ignored", record: fmt.Sprintf(`{"at":%d,"prs":[2]}`, testNow-3601)},
		{name: "record at the window edge", record: fmt.Sprintf(`{"at":%d,"prs":[2]}`, testNow-3600), counts: []int{2}},
		{name: "extra field", record: fmt.Sprintf(`{"at":%d,"prs":[],"x":1}`, testNow)},
		{name: "malformed", record: `{"at":`, wantErr: true},
		{name: "not an object", record: `[1]`, wantErr: true},
		{name: "null", record: `null`, wantErr: true},
		{name: "missing at", record: `{"prs":[]}`, wantErr: true},
		{name: "string at", record: `{"at":"1","prs":[]}`, wantErr: true},
		{name: "zero at", record: `{"at":0,"prs":[]}`, wantErr: true},
		{name: "fractional at", record: fmt.Sprintf(`{"at":%d.5,"prs":[]}`, testNow-5), wantErr: true},
		{name: "future at", record: fmt.Sprintf(`{"at":%d,"prs":[]}`, testNow+1), wantErr: true},
		{name: "missing prs", record: fmt.Sprintf(`{"at":%d}`, testNow), wantErr: true},
		{name: "prs not an array", record: fmt.Sprintf(`{"at":%d,"prs":"1"}`, testNow), wantErr: true},
		{name: "eleven prs", record: fmt.Sprintf(`{"at":%d,"prs":[1,2,3,4,5,6,7,8,9,10,11]}`, testNow), wantErr: true},
		{name: "duplicate prs", record: fmt.Sprintf(`{"at":%d,"prs":[1,1]}`, testNow), wantErr: true},
		{name: "negative pr", record: fmt.Sprintf(`{"at":%d,"prs":[-1]}`, testNow), wantErr: true},
		{name: "zero pr", record: fmt.Sprintf(`{"at":%d,"prs":[0]}`, testNow), wantErr: true},
		{name: "fractional pr", record: fmt.Sprintf(`{"at":%d,"prs":[1.5]}`, testNow), wantErr: true},
		{name: "string pr", record: fmt.Sprintf(`{"at":%d,"prs":["1"]}`, testNow), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setRuns(`[{"total_count":1,"workflow_runs":[{"id":123}]}]`)
			fx.setJobsList([]map[string]any{{
				"name":  "Select bundle repairs",
				"steps": []map[string]any{{"name": selectionPrefix + tt.record, "conclusion": "success"}},
			}})
			fx.env["DRY_RUN"] = "true"
			code, out := fx.run("--budget")
			if (code != 0) != tt.wantErr {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			if tt.wantErr {
				return
			}
			want, _ := json.Marshal(append([]int{}, tt.counts...))
			if got, _ := fx.last(t, "requests"); got != string(want) {
				t.Errorf("requests = %s, want %s", got, want)
			}
		})
	}
}

func TestSelectRejectsUnnamedAndRepeatedRecordSteps(t *testing.T) {
	record := fmt.Sprintf(`{"at":%d,"prs":[]}`, testNow)
	tests := []struct {
		name  string
		steps []map[string]any
	}{
		{"no record", []map[string]any{{"name": "Other", "conclusion": "success"}}},
		{"no steps", nil},
		{"two records", []map[string]any{{"name": selectionPrefix + record}, {"name": selectionPrefix + record}}},
		{"unnamed step", []map[string]any{{"conclusion": "success"}, {"name": selectionPrefix + record}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setRuns(`[{"total_count":1,"workflow_runs":[{"id":123}]}]`)
			fx.setJobsList([]map[string]any{{"name": "Select bundle repairs", "steps": tt.steps}})
			if code, _ := fx.run("--budget"); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
		})
	}
}

func TestSelectBudgetOutputs(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(3, 6)
	fx.setRuns(`[{"total_count":3,"workflow_runs":[{"id":123},{"id":999},{"id":124}]}]`)
	fx.api.set("GET /repos/test/upstream/actions/runs/124/attempts/1/jobs", fx.api.routes[jobsRoute])
	code, out := fx.run("--budget")
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	want := []string{
		"prs=[]",
		"requests=[3,4,5,3,4,5]",
		fmt.Sprintf("expires_at=%d", testNow-60+3600),
		"remaining=4",
	}
	if got := fx.outputs(t); !slices.Equal(got, want) {
		t.Errorf("outputs = %q, want %q", got, want)
	}
}

func TestSelectBudgetRemainingNeverNegative(t *testing.T) {
	fx := newScanFixture(t)
	fx.setHistory(1, 11)
	fx.setRuns(`[{"total_count":3,"workflow_runs":[{"id":123},{"id":999},{"id":124}]}]`)
	fx.api.set("GET /repos/test/upstream/actions/runs/124/attempts/1/jobs", fx.api.routes[jobsRoute])
	if code, out := fx.run("--budget"); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got, _ := fx.last(t, "remaining"); got != "0" {
		t.Errorf("remaining = %q, want 0", got)
	}
}

func TestSelectBudgetFollowsPages(t *testing.T) {
	fx := newScanFixture(t)
	fx.setRuns(
		`[{"total_count":3,"workflow_runs":[{"id":123}]},{"total_count":3,"workflow_runs":[{"id":999},{"id":124}]}]`,
	)
	fx.setHistory(1, 3)
	fx.api.set("GET /repos/test/upstream/actions/runs/124/attempts/1/jobs", fx.api.routes[jobsRoute])
	if code, out := fx.run("--budget"); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got, _ := fx.last(t, "requests"); got != "[1,2,1,2]" {
		t.Errorf("requests = %s", got)
	}
}

func TestSelectUsesBudgetFromEnvironment(t *testing.T) {
	fx := newScanFixture(t)
	for _, key := range []string{currentRoute, runsRoute, jobsRoute} {
		fx.api.remove(key)
	}
	fx.env["RECENT_REQUESTS"] = "[1]"
	fx.env["AUTO_DEADLINE"] = strconv.Itoa(testNow + 600)
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[2]")
	for _, request := range fx.api.requests() {
		if strings.Contains(request, "actions") {
			t.Errorf("the scan queried the history: %s", request)
		}
	}
}

func TestSelectRejectsInvalidEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"missing repository", map[string]string{"REPO": ""}, nil},
		{"missing token", map[string]string{"GH_TOKEN": ""}, nil},
		{"missing run id", map[string]string{"RUN_ID": ""}, nil},
		{"missing output file", map[string]string{"GITHUB_OUTPUT": ""}, nil},
		{"bad repository", map[string]string{"REPO": "a/b/c"}, nil},
		{"bad run id", map[string]string{"RUN_ID": "0"}, nil},
		{"unknown argument", nil, []string{"--budgets"}},
		{"two arguments", nil, []string{"--budget", "--budget"}},
		{"empty requests", map[string]string{"RECENT_REQUESTS": "", "AUTO_DEADLINE": deadline(60)}, nil},
		{"requests not an array", map[string]string{"RECENT_REQUESTS": "{}", "AUTO_DEADLINE": deadline(60)}, nil},
		{"requests with a string", map[string]string{"RECENT_REQUESTS": `["1"]`, "AUTO_DEADLINE": deadline(60)}, nil},
		{"requests with zero", map[string]string{"RECENT_REQUESTS": "[0]", "AUTO_DEADLINE": deadline(60)}, nil},
		{"requests with a fraction", map[string]string{"RECENT_REQUESTS": "[1.5]", "AUTO_DEADLINE": deadline(60)}, nil},
		{"requests null", map[string]string{"RECENT_REQUESTS": "null", "AUTO_DEADLINE": deadline(60)}, nil},
		{"missing deadline", map[string]string{"RECENT_REQUESTS": "[]"}, nil},
		{"expired deadline", map[string]string{"RECENT_REQUESTS": "[]", "AUTO_DEADLINE": deadline(0)}, nil},
		{"invalid deadline", map[string]string{"RECENT_REQUESTS": "[]", "AUTO_DEADLINE": "soon"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			maps.Copy(fx.env, tt.env)
			if code, _ := fx.run(tt.args...); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			for _, request := range fx.api.requests() {
				if strings.Contains(request, "pulls") || request == graphQLRoute {
					t.Errorf("a rejected scan called %s", request)
				}
			}
			// A rejected scan never leaves a plan other than the empty one.
			if prs, found := fx.last(t, "prs"); found && prs != "[]" {
				t.Errorf("prs = %q", prs)
			}
		})
	}
}

func TestSelectWritesEmptyPlanBeforeFailing(t *testing.T) {
	fx := newScanFixture(t)
	fx.env["REPO"] = "not a repository"
	if code, _ := fx.run(); code != exitFailure {
		t.Errorf("exit code = %d", code)
	}
	if got := fx.outputs(t); !slices.Equal(got, []string{"prs=[]"}) {
		t.Errorf("outputs = %q", got)
	}
}

func TestSelectSkipsIneligiblePullRequests(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"closed", func(p map[string]any) { p["state"] = "closed" }},
		{"draft", func(p map[string]any) { p["draft"] = true }},
		{"missing draft", func(p map[string]any) { delete(p, "draft") }},
		{"retargeted", func(p map[string]any) { p["base"] = map[string]any{"ref": "release"} }},
		{"edits disabled", func(p map[string]any) { p["maintainer_can_modify"] = false }},
		{"deleted fork", func(p map[string]any) { p["head"] = map[string]any{"sha": "x", "repo": nil} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setOpenPullRequests(1)
			fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, tt.mutate))
			code, out := fx.run()
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			fx.plan(t, "[]")
			if !strings.Contains(out.stdout, "PR #1 is not eligible for automatic repair.") {
				t.Errorf("stdout = %q", out.stdout)
			}
		})
	}
}

func TestSelectFiltersOpenPullRequests(t *testing.T) {
	fx := newScanFixture(t)
	fx.setGraphQL(`{"data":{"repository":{"pullRequests":{"nodes":[
		{"number":1,"isDraft":true,"mergeable":"CONFLICTING","baseRefName":"main"},
		{"number":2,"isDraft":false,"mergeable":"MERGEABLE","baseRefName":"main"},
		{"number":3,"isDraft":false,"mergeable":"UNKNOWN","baseRefName":"main"},
		{"number":4,"isDraft":false,"mergeable":"CONFLICTING","baseRefName":"release"}],
		"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`)
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	for _, request := range fx.api.requests() {
		if strings.Contains(request, "pulls/") {
			t.Errorf("the scan probed a filtered pull request: %s", request)
		}
	}
}

func TestSelectFailsWhenPullRequestListFails(t *testing.T) {
	for name, body := range map[string]string{
		"graphql errors": `{"errors":[{"message":"boom"}]}`,
		"not JSON":       `nope`,
	} {
		t.Run(name, func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setGraphQL(body)
			if code, _ := fx.run(); code != exitFailure {
				t.Errorf("exit code = %d", code)
			}
			fx.plan(t, "[]")
		})
	}
}

func TestSelectSkipsStaleFetchedHead(t *testing.T) {
	fx := newScanFixture(t)
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, func(p map[string]any) {
		p["head"] = map[string]any{
			"sha":  fx.base,
			"ref":  "contribution",
			"repo": map[string]any{"full_name": "test/fork"},
		}
	}))
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
}

func TestSelectSkipsConflictsOutsideBundle(t *testing.T) {
	fx := newScanFixture(t)
	// A source conflict between the base and the contribution.
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(fx.dir, "source.txt"), "base source\n")
	gitIn(t, fx.dir, "commit", "--quiet", "-am", "base source")
	base := gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
	write(t, filepath.Join(fx.dir, "source.txt"), "contributor source\n")
	gitIn(t, fx.dir, "commit", "--quiet", "-am", "contributor source")
	head := gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "push", "--quiet", fx.remote, "main:refs/heads/main", head+":refs/pull/1/head")
	fx.head, fx.base = head, base
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "PR #1 has conflicts outside the bundle. Skipping.") {
		t.Errorf("stdout = %q", out.stdout)
	}
	if got := gitIn(t, fx.dir, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Errorf("a temporary worktree remains:\n%s", got)
	}
}

func TestSelectStopsAtExpiredDeadline(t *testing.T) {
	fx := newScanFixture(t)
	// The deadline passes between the budget check and the first probe.
	fx.env["RECENT_REQUESTS"] = "[]"
	fx.env["AUTO_DEADLINE"] = strconv.Itoa(testNow + 1)
	calls := 0
	var stdout bytes.Buffer
	a := app{
		getenv: func(key string) string { return fx.env[key] },
		lookup: func(key string) (string, bool) { value, ok := fx.env[key]; return value, ok },
		stdout: &stdout,
		stderr: &stdout,
		now: func() time.Time {
			calls++
			if calls > 1 {
				return time.Unix(testNow+5, 0)
			}
			return time.Unix(testNow, 0)
		},
	}
	if err := a.selectRepairs(t.Context(), nil); err == nil {
		t.Error("the scan continued after its deadline")
	}
	fx.plan(t, "[]")
}

// jq prints a number as the input wrote it, so the budget output keeps 3.0.
func TestSelectBudgetKeepsNumberText(t *testing.T) {
	fx := newScanFixture(t)
	fx.setRuns(`[{"total_count":1,"workflow_runs":[{"id":123}]}]`)
	fx.setJobsList([]map[string]any{
		{
			"name": "Select bundle repairs",
			"steps": []map[string]any{
				{"name": selectionPrefix + fmt.Sprintf(`{"at":%d.0,"prs":[3.0,4]}`, testNow), "conclusion": "success"},
			},
		},
	})
	if code, out := fx.run("--budget"); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got, _ := fx.last(t, "requests"); got != "[3.0,4]" {
		t.Errorf("requests = %s, want [3.0,4]", got)
	}
}

func TestSelectRejectsTrailingDataInRecord(t *testing.T) {
	fx := newScanFixture(t)
	fx.setRuns(`[{"total_count":1,"workflow_runs":[{"id":123}]}]`)
	fx.setJobsList([]map[string]any{
		{
			"name": "Select bundle repairs",
			"steps": []map[string]any{
				{"name": selectionPrefix + fmt.Sprintf(`{"at":%d,"prs":[]} x`, testNow), "conclusion": "success"},
			},
		},
	})
	if code, _ := fx.run("--budget"); code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
}

// Go test TestScannerSkipsResolvedConflicts: the author resolved every
// conflict after the listing, so a clean merge at the new head is not a
// bundle-only conflict. The worktree probe treated it as one.
func TestSelectSkipsCleanMerge(t *testing.T) {
	fx := newScanFixture(t)
	gitIn(t, fx.dir, "checkout", "--quiet", "--detach", fx.head)
	gitIn(t, fx.dir, "merge", "--quiet", "--no-edit", "-X", "ours", fx.base)
	fx.head = gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "push", "--quiet", fx.remote, fx.head+":refs/pull/1/head")
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if strings.Contains(out.stdout, "Selected bundle repair") {
		t.Errorf("a clean merge was selected: %q", out.stdout)
	}
	if !strings.Contains(out.stdout, "PR #1 no longer conflicts with the base. Skipping.") {
		t.Errorf("stdout = %q", out.stdout)
	}
	if _, found := fx.last(t, "rate_record"); !found {
		t.Error("the scan wrote no rate record")
	}
}

// Go test TestScannerContinuesAfterProbeFailure: a failure that affects one
// pull request writes a warning annotation and skips it. The scan still
// writes its selection record, so a later scan can verify the budget.
func TestSelectContinuesAfterCandidateFailure(t *testing.T) {
	fx := newScanFixture(t)
	gitIn(t, fx.remote, "update-ref", "-d", "refs/pull/1/head")
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if !strings.Contains(out.stdout, "::warning::PR #1: the pull request head cannot be fetched") {
		t.Errorf("stdout = %q", out.stdout)
	}
	fx.plan(t, "[2]")
	record, _ := fx.last(t, "rate_record")
	if record != fmt.Sprintf(`{"at":%d,"prs":[2]}`, testNow) {
		t.Errorf("rate_record = %s", record)
	}
}

// The pull ref moved on between the listing and the probe. The scan warns
// and continues; it does not fail the scan for one pull request.
func TestSelectWarnsOnHeadMismatch(t *testing.T) {
	fx := newScanFixture(t)
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, func(pull map[string]any) {
		pull["head"] = map[string]any{
			"sha":  fx.base,
			"ref":  "contribution",
			"repo": map[string]any{"full_name": "test/fork"},
		}
	}))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "::warning::PR #1: the fetched head does not match the reported head") {
		t.Errorf("stdout = %q", out.stdout)
	}
}

// The fetch of the base branch affects every candidate, so it still fails
// the scan and writes no selection record.
func TestSelectFailsWhenMainFetchFails(t *testing.T) {
	fx := newScanFixture(t)
	gitIn(t, fx.dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	if code, _ := fx.run(); code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if _, found := fx.last(t, "rate_record"); found {
		t.Error("a failed scan wrote a rate record")
	}
}

// A conflict on a file other than the bundle, without a bundle conflict,
// makes the pull request skip.
func TestSelectSkipsConflictOnAnotherFile(t *testing.T) {
	fx := newScanFixture(t)
	// The head restores the original bundle, so source.txt is the only conflict.
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(fx.dir, "source.txt"), "base source\n")
	gitIn(t, fx.dir, "commit", "--quiet", "-am", "base source")
	base := gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
	write(t, filepath.Join(fx.dir, "source.txt"), "contributor source\n")
	write(t, filepath.Join(fx.dir, bundlePath), "original\x00bundle")
	commitAll(t, fx.dir, "contributor source")
	head := gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "push", "--quiet", fx.remote, "main:refs/heads/main", head+":refs/pull/1/head")
	fx.head, fx.base = head, base
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "PR #1 has conflicts outside the bundle. Skipping.") {
		t.Errorf("stdout = %q", out.stdout)
	}
}

// Item 5: git merge-tree reads .gitattributes from the current checkout,
// not from the merged trees. The scanner runs in a trusted checkout of main,
// so a hostile .gitattributes in the pull request head is not consulted and
// cannot hide a conflict from the probe.
func TestSelectIgnoresHeadAttributes(t *testing.T) {
	fx := newScanFixture(t)
	hostileConflictFixture(t, fx)
	// The trusted checkout of main is checked out while the scan probes.
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "PR #1 has conflicts outside the bundle. Skipping.") {
		t.Errorf("hostile head attributes changed the probe result: %q", out.stdout)
	}
}

// Item 5, worst case: the checked-out branch of the scan checkout carries
// hostile union attributes. The trusted info/attributes that the scan writes
// take precedence over them and unset every merge driver, so the probe sees
// the source conflict and skips the pull request. The repair merge would
// also fail on the hidden conflict, so nothing can be pushed either way.
func TestSelectHostileAttributesCannotHideConflicts(t *testing.T) {
	fx := newScanFixture(t)
	hostileConflictFixture(t, fx)
	// The hostile attributes of the contribution branch are checked out.
	gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "PR #1 has conflicts outside the bundle. Skipping.") {
		t.Errorf("hostile checkout attributes changed the probe result: %q", out.stdout)
	}
	// Defense in depth: even a selected request would fail here, because the
	// repair merge writes the same trusted attributes.
	var stdout bytes.Buffer
	mergeErr := mergeBase(t.Context(), fx.dir, fx.base, &stdout)
	if mergeErr == nil {
		t.Error("the repair merge accepted a hidden source conflict")
	}
	unmerged := gitIn(t, fx.dir, "diff", "--name-only", "--diff-filter=U")
	if !strings.Contains(unmerged, "source.txt") {
		t.Errorf("unmerged files = %q, want source.txt", unmerged)
	}
	gitIn(t, fx.dir, "merge", "--abort")
}

func TestParseConflictPaths(t *testing.T) {
	const oid = "1e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f"
	tests := []struct {
		name string
		out  string
		want []string
		ok   bool
	}{
		{"clean merge", oid + "\x00", nil, true},
		{"bundle only", oid + "\x00" + bundlePath + "\x00", []string{bundlePath}, true},
		{"two paths", oid + "\x00source.txt\x00" + bundlePath + "\x00", []string{"source.txt", bundlePath}, true},
		{"empty output", "", nil, false},
		{"only a nul", "\x00", nil, false},
		{"no tree", "not a tree\x00" + bundlePath + "\x00", nil, false},
		{"short tree", "abc\x00" + bundlePath + "\x00", nil, false},
		{"empty path", oid + "\x00\x00", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConflictPaths([]byte(tt.out))
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok = %v", err, tt.ok)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("paths = %q, want %q", got, tt.want)
			}
		})
	}
}

// hostileConflictFixture adds a hostile .gitattributes and a source conflict
// to the contribution branch, and a source conflict to main. It records the
// new head and base in fx and leaves main checked out.
func hostileConflictFixture(t *testing.T, fx *scanFixture) {
	t.Helper()
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(fx.dir, "source.txt"), "base source\n")
	gitIn(t, fx.dir, "commit", "--quiet", "-am", "base source")
	fx.base = gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
	write(t, filepath.Join(fx.dir, "source.txt"), "contributor source\n")
	write(t, filepath.Join(fx.dir, ".gitattributes"), "* merge=union\n")
	commitAll(t, fx.dir, "contributor source and hostile attributes")
	fx.head = gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "push", "--quiet", fx.remote, "main:refs/heads/main", fx.head+":refs/pull/1/head")
}

// J1(a): the scan checkout carries the repository .gitattributes and may have
// the bundle merge driver registered, for example after make setup-git. The
// probe must pin the driver itself, so a registered driver cannot hide the
// bundle conflict.
func TestSelectIgnoresRegisteredBundleDriver(t *testing.T) {
	fx := newScanFixture(t)
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(fx.dir, ".gitattributes"), bundlePath+" -text -diff merge=otelc-bundle\n")
	commitAll(t, fx.dir, "bundle attributes")
	fx.base = gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.dir, "push", "--quiet", fx.remote, "main:refs/heads/main")
	gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
	// The driver is registered as make setup-git would register it.
	gitIn(t, fx.dir, "config", "merge.otelc-bundle.driver", "true")
	// The scan runs in the trusted checkout of main, which carries the
	// repository .gitattributes with the bundle merge driver.
	gitIn(t, fx.dir, "checkout", "--quiet", "main")
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[1]")
	if !strings.Contains(out.stdout, "Selected bundle repair for PR #1.") {
		t.Errorf("a registered driver hid the bundle conflict: %q", out.stdout)
	}
}

// J1(b): a delete/modify conflict on the bundle shows as a bundle-only
// conflict, but the repair merge cannot resolve it, so the pull request
// must be skipped in both directions.
func TestSelectSkipsBundleDeleteModifyConflict(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *scanFixture)
	}{
		{
			"base deletes, head modifies",
			func(t *testing.T, fx *scanFixture) {
				gitIn(t, fx.dir, "checkout", "--quiet", "main")
				gitIn(t, fx.dir, "rm", "-q", bundlePath)
				gitIn(t, fx.dir, "commit", "--quiet", "-m", "base deletes the bundle")
				fx.base = gitIn(t, fx.dir, "rev-parse", "HEAD")
				gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
				gitIn(t, fx.dir, "push", "--quiet", fx.remote, "main:refs/heads/main")
			},
		},
		{
			"head deletes, base modifies",
			func(t *testing.T, fx *scanFixture) {
				gitIn(t, fx.dir, "checkout", "--quiet", "contribution")
				gitIn(t, fx.dir, "rm", "-q", bundlePath)
				gitIn(t, fx.dir, "commit", "--quiet", "-m", "head deletes the bundle")
				fx.head = gitIn(t, fx.dir, "rev-parse", "HEAD")
				gitIn(t, fx.dir, "push", "--quiet", fx.remote, fx.head+":refs/pull/1/head")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newScanFixture(t)
			tt.setup(t, fx)
			fx.setOpenPullRequests(1)
			fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, nil))
			code, out := fx.run()
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			fx.plan(t, "[]")
			if !strings.Contains(out.stdout, "PR #1 has a bundle conflict the repair merge cannot resolve. Skipping.") {
				t.Errorf("stdout = %q", out.stdout)
			}
		})
	}
}

// Review #2: a head commit id that is not a full SHA affects one pull
// request only, so the scan warns and skips instead of failing.
func TestSelectWarnsOnInvalidHead(t *testing.T) {
	fx := newScanFixture(t)
	fx.setOpenPullRequests(1)
	fx.api.setBody("GET /repos/test/upstream/pulls/1", fx.pullBody(t, func(pull map[string]any) {
		pull["head"] = map[string]any{"sha": "abc", "ref": "c", "repo": map[string]any{"full_name": "test/fork"}}
	}))
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	fx.plan(t, "[]")
	if !strings.Contains(out.stdout, "::warning::PR #1: the head commit id is not valid") {
		t.Errorf("stdout = %q", out.stdout)
	}
}

// Review #3: a cancelled or expired context is a global failure. A probe
// failure caused by it must fail the scan, not warn and skip.
func TestSelectFailsWhenContextCancelled(t *testing.T) {
	fx := newScanFixture(t)
	fx.env["RECENT_REQUESTS"] = "[]"
	fx.env["AUTO_DEADLINE"] = strconv.Itoa(testNow + 600)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// The context is cancelled while the scan reads the first pull request,
	// so the next git command of the probe fails with the context error.
	fx.api.set("GET /repos/test/upstream/pulls/1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		jsonHandler(http.StatusOK, fx.pullBody(t, nil))(w, r)
	}))
	var stdout, stderr bytes.Buffer
	a := app{
		getenv: func(key string) string { return fx.env[key] },
		lookup: func(key string) (string, bool) { value, ok := fx.env[key]; return value, ok },
		stdout: &stdout,
		stderr: &stderr,
		now:    func() time.Time { return time.Unix(testNow, 0) },
	}
	err := a.selectRepairs(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the scan continued after its context was cancelled: err = %v", err)
	}
}

// Review #4: a gone pull request (404 or 410) affects one candidate, but a
// denied (403) or broken (500) response affects the scan.
func TestSelectPullRequestReadErrors(t *testing.T) {
	for _, tt := range []struct {
		status int
		fail   bool
	}{
		{http.StatusNotFound, false},
		{http.StatusGone, false},
		{http.StatusForbidden, true},
		{http.StatusInternalServerError, true},
	} {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			fx := newScanFixture(t)
			fx.setOpenPullRequests(1)
			fx.api.set("GET /repos/test/upstream/pulls/1", jsonHandler(tt.status, `{"message":"boom"}`))
			code, out := fx.run()
			if tt.fail {
				if code != exitFailure {
					t.Errorf("exit code = %d, want %d", code, exitFailure)
				}
				if _, found := fx.last(t, "rate_record"); found {
					t.Error("a failed scan wrote a rate record")
				}
				return
			}
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			fx.plan(t, "[]")
			if !strings.Contains(out.stdout, "::warning::PR #1: the pull request cannot be read") {
				t.Errorf("stdout = %q", out.stdout)
			}
			if _, found := fx.last(t, "rate_record"); !found {
				t.Error("the scan wrote no rate record")
			}
		})
	}
}

// C8 end to end: the head commit is present but its tree object is missing,
// so git merge-tree fails (git 2.56 exits 128, other versions exit 1, both
// with empty output). The scan must warn and skip, not select.
func TestSelectWarnsWhenMergeTreeFails(t *testing.T) {
	fx := newScanFixture(t)
	fx.setOpenPullRequests(1)
	tree := gitIn(t, fx.dir, "rev-parse", fx.head+"^{tree}")
	// The fetch does not restore the tree: the scan repository advertises the
	// head commit through its own refs, so the remote sends nothing.
	if err := os.Remove(filepath.Join(fx.dir, ".git", "objects", tree[:2], tree[2:])); err != nil {
		t.Fatal(err)
	}
	code, out := fx.run()
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if !strings.Contains(out.stdout, "::warning::PR #1: the conflict probe failed") {
		t.Errorf("stdout = %q", out.stdout)
	}
	fx.plan(t, "[]")
	if _, found := fx.last(t, "rate_record"); !found {
		t.Error("the scan wrote no rate record")
	}
}
