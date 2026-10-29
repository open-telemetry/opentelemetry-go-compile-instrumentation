// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
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
