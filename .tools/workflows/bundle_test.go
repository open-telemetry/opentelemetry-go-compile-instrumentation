// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package workflows_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rhysd/actionlint"
)

const (
	// setupGo is the pinned setup-go action that the repair jobs use.
	setupGo = "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"
	// toolCall starts every line that runs the bundle repair tool.
	toolCall = `"${RUNNER_TEMP}/bundlerepair" `
)

var (
	// buildStep matches the one command that builds the tool.
	buildStep = regexp.MustCompile(
		`^go -C ((?:trusted/)?\.tools/bundlerepair) build -trimpath -o "\$\{RUNNER_TEMP\}/bundlerepair" \.$`)
	// forbiddenInRun are the programs whose decisions now live in the Go tool.
	forbiddenInRun = regexp.MustCompile(`\bjq\b|\bgh api\b|\bcurl\b|\.github/scripts/`)
)

// toolJobs lists, for every repair workflow, the jobs that call the tool and
// the checkout path that holds the trusted sources in that job.
var toolJobs = map[string]map[string]string{
	"regenerate-bundle.yaml":        {"prepare": ""},
	"regenerate-bundle-worker.yaml": {"generate": "trusted", "push_update": "trusted"},
	"autofix-bundle-conflicts.yaml": {"autofix": ""},
}

func loadWorkflow(t *testing.T, name string) *actionlint.Workflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../.github/workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	workflow, errs := actionlint.Parse(data)
	if len(errs) != 0 {
		t.Fatalf("parse %s: %v", name, errs)
	}
	return workflow
}

func runOf(step *actionlint.Step) (*actionlint.ExecRun, bool) {
	run, ok := step.Exec.(*actionlint.ExecRun)
	return run, ok
}

func usesOf(step *actionlint.Step) (*actionlint.ExecAction, bool) {
	action, ok := step.Exec.(*actionlint.ExecAction)
	return action, ok
}

func stepName(step *actionlint.Step) string {
	if step.Name == nil {
		return ""
	}
	return step.Name.Value
}

func envValue(step *actionlint.Step, key string) (string, bool) {
	if step.Env == nil {
		return "", false
	}
	variable := step.Env.Vars[strings.ToLower(key)]
	if variable == nil || variable.Value == nil {
		return "", false
	}
	return variable.Value.Value, true
}

func input(action *actionlint.ExecAction, key string) (string, bool) {
	value := action.Inputs[key]
	if value == nil || value.Value == nil {
		return "", false
	}
	return value.Value.Value, true
}

// nonBlankLines returns the trimmed lines of a script that are not comments.
func nonBlankLines(script string) []string {
	var lines []string
	for line := range strings.SplitSeq(script, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func callsTool(step *actionlint.Step) bool {
	run, ok := runOf(step)
	if !ok {
		return false
	}
	return slices.ContainsFunc(nonBlankLines(run.Run.Value), func(line string) bool {
		return strings.HasPrefix(line, toolCall)
	})
}

func buildDir(step *actionlint.Step) (string, bool) {
	run, ok := runOf(step)
	if !ok {
		return "", false
	}
	match := buildStep.FindStringSubmatch(strings.TrimSpace(run.Run.Value))
	if match == nil {
		return "", false
	}
	return match[1], true
}

// Every job that calls the tool builds it once, from the trusted checkout,
// with the three pins, after setup-go and before the first call.
func TestToolIsBuiltFromTrustedSourcesBeforeUse(t *testing.T) {
	for name, jobs := range toolJobs {
		workflow := loadWorkflow(t, name)
		for id, trustedPath := range jobs {
			t.Run(name+"/"+id, func(t *testing.T) {
				job := workflow.Jobs[id]
				if job == nil {
					t.Fatalf("missing job %s", id)
				}
				first, build := -1, -1
				for i, step := range job.Steps {
					if callsTool(step) && first < 0 {
						first = i
					}
					if _, ok := buildDir(step); ok {
						if build >= 0 {
							t.Error("the job builds the tool more than once")
						}
						build = i
					}
				}
				if first < 0 {
					t.Fatal("the job never calls the tool")
				}
				if build < 0 || build > first {
					t.Fatalf("the tool must be built before its first call: build=%d first call=%d", build, first)
				}
				dir, _ := buildDir(job.Steps[build])
				want := ".tools/bundlerepair"
				if trustedPath != "" {
					want = trustedPath + "/" + want
				}
				if dir != want {
					t.Errorf("the tool is built from %q, want %q", dir, want)
				}
				if run, _ := runOf(job.Steps[build]); run.WorkingDirectory != nil {
					t.Error("the build step must run from the workspace root")
				}
				for key, wantValue := range map[string]string{"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "-mod=readonly"} {
					if got, ok := envValue(job.Steps[build], key); !ok || got != wantValue {
						t.Errorf("the build step must set %s=%s, got %q", key, wantValue, got)
					}
				}
				checkTrustedCheckoutAndGo(t, job.Steps[:build], trustedPath)
			})
		}
	}
}

// checkTrustedCheckoutAndGo checks the steps before the build: one checkout of
// the trusted path at the default ref, and the pinned setup-go.
func checkTrustedCheckoutAndGo(t *testing.T, steps []*actionlint.Step, trustedPath string) {
	t.Helper()
	checkouts, setups := 0, 0
	for _, step := range steps {
		action, ok := usesOf(step)
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(action.Uses.Value, "actions/checkout@"):
			path, _ := input(action, "path")
			if path != trustedPath {
				continue
			}
			checkouts++
			for _, key := range []string{"ref", "repository"} {
				if value, set := input(action, key); set {
					t.Errorf("the trusted checkout sets %s=%q", key, value)
				}
			}
			if value, _ := input(action, "persist-credentials"); value != "false" {
				t.Error("the trusted checkout must not persist credentials")
			}
		case strings.HasPrefix(action.Uses.Value, "actions/setup-go@"):
			setups++
			if action.Uses.Value != setupGo {
				t.Errorf("setup-go is %q, want the pinned %q", action.Uses.Value, setupGo)
			}
			if value, _ := input(action, "cache"); value != "false" {
				t.Error("setup-go must disable the cache")
			}
			wantFile := "go.mod"
			if trustedPath != "" {
				wantFile = trustedPath + "/go.mod"
			}
			if value, _ := input(action, "go-version-file"); value != wantFile {
				t.Errorf("setup-go reads %q, want %q", value, wantFile)
			}
		}
	}
	if checkouts != 1 {
		t.Errorf("found %d trusted checkouts before the build, want 1", checkouts)
	}
	if setups != 1 {
		t.Errorf("found %d setup-go steps before the build, want 1", setups)
	}
}

// The decisions live in the Go tool. No step reads or calls the API by hand.
func TestNoDecisionLogicInRunSteps(t *testing.T) {
	for name := range toolJobs {
		workflow := loadWorkflow(t, name)
		for id, job := range workflow.Jobs {
			for _, step := range job.Steps {
				run, ok := runOf(step)
				if !ok {
					continue
				}
				if found := forbiddenInRun.FindString(run.Run.Value); found != "" {
					t.Errorf("%s job %s step %q runs %q", name, id, stepName(step), found)
				}
			}
		}
	}
}

// Each tool call in a run step is exactly one subcommand call.
func TestRunStepsOnlyCallOneSubcommand(t *testing.T) {
	subcommand := regexp.MustCompile(`^"\$\{RUNNER_TEMP\}/bundlerepair" (authorize|prepare|select|stage|merge|publish|identity|credentials)\b`)
	for name, jobs := range toolJobs {
		workflow := loadWorkflow(t, name)
		for id := range jobs {
			total := 0
			for _, step := range workflow.Jobs[id].Steps {
				if !callsTool(step) {
					continue
				}
				total++
				run, _ := runOf(step)
				calls := 0
				for _, line := range nonBlankLines(run.Run.Value) {
					if strings.HasPrefix(line, toolCall) {
						calls++
						if !subcommand.MatchString(line) {
							t.Errorf("%s job %s: %q is not a known subcommand call", name, id, line)
						}
					}
				}
				if calls != 1 {
					t.Errorf("%s job %s step %q makes %d tool calls, want 1", name, id, stepName(step), calls)
				}
			}
			if total == 0 {
				t.Errorf("%s job %s never calls the tool", name, id)
			}
		}
	}
}

func generateSteps(t *testing.T) []*actionlint.Step {
	t.Helper()
	job := loadWorkflow(t, "regenerate-bundle-worker.yaml").Jobs["generate"]
	if job == nil {
		t.Fatal("missing generate job")
	}
	return job.Steps
}

func stepIndex(steps []*actionlint.Step, name string) int {
	return slices.IndexFunc(steps, func(s *actionlint.Step) bool { return stepName(s) == name })
}

func TestGenerateRunsOnlyTrustedBuild(t *testing.T) {
	steps := generateSteps(t)
	merge := stepIndex(steps, "Merge captured base commit")
	stage := stepIndex(steps, "Stage bundle sources")
	build := stepIndex(steps, "Regenerate bundle")
	if merge < 0 || stage < 0 || build < 0 {
		t.Fatalf("missing steps: merge=%d stage=%d build=%d", merge, stage, build)
	}
	if merge >= stage || stage >= build {
		t.Errorf("steps must run in the order merge, stage, build: %d %d %d", merge, stage, build)
	}
	if check := stepIndex(steps, "Check request before generation"); check < 0 || check > stage {
		t.Error("the request check must run before staging")
	}

	run, ok := runOf(steps[stage])
	if !ok || strings.TrimSpace(run.Run.Value) != toolCall+`stage branch trusted "${BASE_SHA}"` {
		t.Fatal("staging must run the tool on the branch and trusted directories with the captured base SHA")
	}
	if run.WorkingDirectory != nil {
		t.Error("the staging step must run from the workspace root")
	}

	run, ok = runOf(steps[build])
	if !ok || strings.TrimSpace(run.Run.Value) != "make package" {
		t.Fatal("the build step must run make package")
	}
	if wd := run.WorkingDirectory; wd == nil || wd.Value != "trusted" {
		t.Error("make package must run in the trusted checkout")
	}
	for key, want := range map[string]string{"GOTOOLCHAIN": "local", "GOPROXY": "https://proxy.golang.org"} {
		if got, ok := envValue(steps[build], key); !ok || got != want {
			t.Errorf("the build step must pin %s=%s", key, want)
		}
	}
	makeSteps := 0
	for _, step := range steps {
		if run, ok := runOf(step); ok {
			for _, line := range nonBlankLines(run.Run.Value) {
				if strings.HasPrefix(line, "make ") {
					makeSteps++
				}
			}
		}
		if upload, ok := usesOf(step); ok && strings.HasPrefix(upload.Uses.Value, "actions/upload-artifact@") {
			if path, _ := input(upload, "path"); path != "trusted/tool/data/otelc-bundle.tgz" {
				t.Error("the upload must read the archive from the trusted checkout")
			}
		}
	}
	if makeSteps != 1 {
		t.Errorf("the generate job runs make %d times, want once", makeSteps)
	}
}

// Contributor files must never run. Only git may run in the branch checkout,
// and no command may name a path in it.
func TestNothingRunsInBranchCheckout(t *testing.T) {
	branchPath := regexp.MustCompile(`(^|[\s"'=])(\./)?branch/`)
	for name := range toolJobs {
		workflow := loadWorkflow(t, name)
		for id, job := range workflow.Jobs {
			for _, step := range job.Steps {
				where := name + " job " + id + " step " + stepName(step)
				if action, ok := usesOf(step); ok && strings.Contains(action.Uses.Value, "branch") {
					t.Errorf("%s uses a local action from the branch checkout", where)
				}
				run, ok := runOf(step)
				if !ok {
					continue
				}
				inBranch := run.WorkingDirectory != nil && strings.HasPrefix(run.WorkingDirectory.Value, "branch")
				for _, line := range nonBlankLines(run.Run.Value) {
					switch {
					case inBranch && !strings.HasPrefix(line, "git "):
						t.Errorf("%s runs %q in the branch checkout", where, line)
					case branchPath.MatchString(line):
						t.Errorf("%s names a path in the branch checkout in %q", where, line)
					case strings.HasPrefix(line, "make ") && (stepName(step) != "Regenerate bundle" || inBranch):
						t.Errorf("%s runs make outside the trusted build", where)
					case strings.HasPrefix(line, "go ") && !buildStep.MatchString(line):
						t.Errorf("%s runs go directly: %q", where, line)
					case strings.HasPrefix(line, toolCall) && inBranch:
						t.Errorf("%s runs the tool in the branch checkout", where)
					}
				}
			}
		}
	}
}

func TestAuthorizationBeforeConcurrency(t *testing.T) {
	workflow := loadWorkflow(t, "regenerate-bundle.yaml")
	if workflow.Concurrency != nil {
		t.Error("unauthorized comments can enter workflow-level concurrency")
	}
	job := workflow.Jobs["repair"]
	if job == nil || job.WorkflowCall == nil {
		t.Fatal("missing authorized reusable worker call")
	}
	if !slices.ContainsFunc(job.Needs, func(s *actionlint.String) bool { return s.Value == "prepare" }) {
		t.Error("worker does not depend on authorization")
	}
	if job.If == nil || strings.TrimSpace(job.If.Value) != "needs.prepare.outputs.authorized == 'true'" {
		t.Error("worker can run without an authorized request")
	}
	if job.Concurrency == nil || !strings.Contains(job.Concurrency.Group.Value, "needs.prepare.outputs.pr_number") {
		t.Error("the authorized worker call must hold the pull request lock")
	}
}

// The prepare job runs one command to authorize and capture the snapshot.
func TestPrepareStepCapturesSnapshot(t *testing.T) {
	job := loadWorkflow(t, "regenerate-bundle.yaml").Jobs["prepare"]
	if job == nil {
		t.Fatal("missing prepare job")
	}
	idx := slices.IndexFunc(job.Steps, func(s *actionlint.Step) bool { return s.ID != nil && s.ID.Value == "pr" })
	if idx < 0 {
		t.Fatal("missing step with id pr")
	}
	run, ok := runOf(job.Steps[idx])
	if !ok || strings.TrimSpace(run.Run.Value) != toolCall+"prepare" {
		t.Fatal("the pr step must run the prepare command")
	}
	for _, key := range []string{"GH_TOKEN", "EVENT_NAME", "ISSUE_IS_PULL_REQUEST", "AUTHOR_ASSOCIATION", "COMMENT_BODY", "INPUT_PR_NUMBER"} {
		if _, ok := envValue(job.Steps[idx], key); !ok {
			t.Errorf("the pr step does not set %s", key)
		}
	}
	for i, step := range job.Steps {
		if i != idx && callsTool(step) {
			t.Errorf("step %q calls the tool besides the pr step", stepName(step))
		}
	}
}

func TestScannedSnapshotPropagation(t *testing.T) {
	workflow := loadWorkflow(t, "autofix-bundle-conflicts.yaml")
	job := workflow.Jobs["repair"]
	if job == nil || job.WorkflowCall == nil {
		t.Fatal("missing repair call")
	}
	for _, name := range []string{"expected_head_sha", "expected_base_sha", "expires_at"} {
		input := job.WorkflowCall.Inputs[name]
		if input == nil || !strings.Contains(input.Value.Value, "matrix.") {
			t.Errorf("repair does not receive the scanned %s", name)
		}
	}
}

// The scanner records its selection under step names that the history parser reads.
func TestScannerStepNamesStayStable(t *testing.T) {
	workflow := loadWorkflow(t, "autofix-bundle-conflicts.yaml")
	job := workflow.Jobs["autofix"]
	if job == nil || job.Name == nil || job.Name.Value != "Select bundle repairs" {
		t.Fatal("the scanner job must be named Select bundle repairs")
	}
	budget, scan, record := -1, -1, -1
	for i, step := range job.Steps {
		switch {
		case step.ID != nil && step.ID.Value == "budget":
			budget = i
			assertSingleCall(t, step, "select --budget")
		case step.ID != nil && step.ID.Value == "scan":
			scan = i
			assertSingleCall(t, step, "select")
			if stepName(step) != "Select bounded bundle repairs" {
				t.Errorf("the scan step is named %q", stepName(step))
			}
		case strings.HasPrefix(stepName(step), "Bundle repair selection: "):
			record = i
			if stepName(step) != "Bundle repair selection: ${{ steps.scan.outputs.rate_record }}" {
				t.Errorf("the record step is named %q", stepName(step))
			}
		}
	}
	if budget < 0 || scan < 0 || record < 0 || budget >= scan || scan >= record {
		t.Errorf("steps must run in the order budget, scan, record: %d %d %d", budget, scan, record)
	}
}

func assertSingleCall(t *testing.T, step *actionlint.Step, subcommand string) {
	t.Helper()
	run, ok := runOf(step)
	if !ok || strings.TrimSpace(run.Run.Value) != toolCall+subcommand {
		t.Errorf("step %q must run only %q", stepName(step), toolCall+subcommand)
	}
}

// The publisher environment supplies the token. Each caller forwards the
// secret by name: an environment secret that the caller does not pass resolves
// to an empty string in the reusable workflow, so a literal empty mapping is
// not enough (GitHub docs, "Reuse workflows").
func TestPublisherEnvironmentTokenRouting(t *testing.T) {
	for _, caller := range []struct {
		file  string
		jobID string
	}{
		{file: "regenerate-bundle.yaml", jobID: "repair"},
		{file: "autofix-bundle-conflicts.yaml", jobID: "repair"},
	} {
		t.Run(caller.file, func(t *testing.T) {
			job := loadWorkflow(t, caller.file).Jobs[caller.jobID]
			if job == nil || job.WorkflowCall == nil {
				t.Fatalf("missing worker call in job %s", caller.jobID)
			}
			if job.WorkflowCall.InheritSecrets {
				t.Error("the worker must not inherit caller secrets")
			}
			token := job.WorkflowCall.Secrets["otelc_bundle_push_token"]
			if token == nil || token.Value == nil {
				t.Fatal("the caller must forward the publisher token by name")
			}
			if got := strings.TrimSpace(token.Value.Value); got != "${{ secrets.OTELC_BUNDLE_PUSH_TOKEN }}" {
				t.Errorf("the caller forwards %q, want the secret expression", got)
			}
		})
	}

	// The caller contract must declare the secret, or the forwarding has no name.
	workflow := loadWorkflow(t, "regenerate-bundle.yaml")
	var call *actionlint.WorkflowCallEvent
	for _, event := range workflow.On {
		if event, ok := event.(*actionlint.WorkflowCallEvent); ok {
			call = event
			break
		}
	}
	if call == nil || call.Secrets == nil {
		t.Fatal("regenerate-bundle.yaml does not declare workflow_call secrets")
	}
	secret := call.Secrets["otelc_bundle_push_token"]
	if secret == nil {
		t.Fatal("the caller does not declare OTELC_BUNDLE_PUSH_TOKEN under on.workflow_call.secrets")
	}
	if secret.Required != nil && secret.Required.Value {
		t.Error("the caller declares the publisher token as required")
	}

	// The publisher job binds the environment that supplies the token.
	publisher := loadWorkflow(t, "regenerate-bundle-worker.yaml").Jobs["push_update"]
	if publisher == nil || publisher.Environment == nil || publisher.Environment.Name == nil ||
		publisher.Environment.Name.Value != "bundle-publisher" {
		t.Error("the publisher job must bind the bundle-publisher environment")
	}
}

// The App path: the tool decides whether to use the App, and the App token
// step follows that output. The identity step reports the token owner.
func TestPublisherCredentialSteps(t *testing.T) {
	job := loadWorkflow(t, "regenerate-bundle-worker.yaml").Jobs["push_update"]
	if job == nil {
		t.Fatal("missing protected publisher job")
	}
	var credentials, appToken, identity *actionlint.Step
	for _, step := range job.Steps {
		switch {
		case step.ID != nil && step.ID.Value == "credentials":
			credentials = step
		case step.ID != nil && step.ID.Value == "app-token":
			appToken = step
		case step.ID != nil && step.ID.Value == "identity":
			identity = step
		}
	}
	if credentials == nil || appToken == nil || identity == nil {
		t.Fatalf("missing steps: credentials=%v app-token=%v identity=%v", credentials != nil, appToken != nil, identity != nil)
	}
	assertSingleCall(t, credentials, "credentials")
	for _, key := range []string{"CLIENT_ID", "PRIVATE_KEY", "IS_CROSS_REPOSITORY"} {
		if _, ok := envValue(credentials, key); !ok {
			t.Errorf("the credentials step does not set %s", key)
		}
	}
	if appToken.If == nil || strings.TrimSpace(appToken.If.Value) != "steps.credentials.outputs.use_app == 'true'" {
		t.Error("the App token must follow the credentials decision")
	}
	assertSingleCall(t, identity, "identity")
	for _, key := range []string{"APP_SLUG", "GH_TOKEN"} {
		if _, ok := envValue(identity, key); !ok {
			t.Errorf("the identity step does not set %s", key)
		}
	}
	first := slices.IndexFunc(job.Steps, callsTool)
	if first < 0 || job.Steps[first].Name == nil || job.Steps[first].Name.Value != "Check request permission" {
		t.Error("the publisher must authorize the request before it touches credentials")
	}
}

// Regression guard: the generate job gets no secrets. The publisher job alone
// may read them. This holds before and after the rewiring.
func TestSecretsStayOutOfGenerateJob(t *testing.T) {
	workflow := loadWorkflow(t, "regenerate-bundle-worker.yaml")
	if workflow.Jobs["generate"] == nil {
		t.Fatal("missing generate job")
	}
	for id, job := range workflow.Jobs {
		for i, step := range job.Steps {
			for _, value := range stepExpressions(step) {
				if id != "push_update" && strings.Contains(value, "secrets.") {
					t.Errorf("job %s step %d %q reads a secret in %q", id, i, stepName(step), value)
				}
			}
		}
	}
}

// stepExpressions returns every string of a step that can hold an expression:
// the run script, the env values, the condition, and the action inputs.
func stepExpressions(step *actionlint.Step) []string {
	var values []string
	if step.If != nil {
		values = append(values, step.If.Value)
	}
	if step.Env != nil {
		for _, variable := range step.Env.Vars {
			if variable.Value != nil {
				values = append(values, variable.Value.Value)
			}
		}
		if step.Env.Expression != nil {
			values = append(values, step.Env.Expression.Value)
		}
	}
	switch exec := step.Exec.(type) {
	case *actionlint.ExecRun:
		values = append(values, exec.Run.Value)
	case *actionlint.ExecAction:
		values = append(values, exec.Uses.Value)
		for _, in := range exec.Inputs {
			if in.Value != nil {
				values = append(values, in.Value.Value)
			}
		}
	}
	return values
}

// The requester on a rerun is the original actor, not the person who reran the
// workflow. TRIGGERING_ACTOR separately keeps the rerunner.
func TestCommenterUsesOriginalActor(t *testing.T) {
	for _, name := range []string{"regenerate-bundle.yaml", "regenerate-bundle-worker.yaml"} {
		workflow := loadWorkflow(t, name)
		if workflow.Env == nil {
			t.Fatalf("%s: missing workflow env", name)
		}
		want := map[string]string{
			"COMMENTER":        "${{ github.event.comment.user.login || github.actor }}",
			"TRIGGERING_ACTOR": "${{ github.triggering_actor }}",
		}
		for key, value := range want {
			variable := workflow.Env.Vars[strings.ToLower(key)]
			if variable == nil || variable.Value == nil {
				t.Errorf("%s: missing workflow env %s", name, key)
				continue
			}
			if variable.Value.Value != value {
				t.Errorf("%s: %s = %q, want %q", name, key, variable.Value.Value, value)
			}
		}
	}
}

// A rerun repeats every job in the same run. The upload artifact name must
// include the run attempt, or the second upload of the same name fails.
func TestUploadArtifactNameIsUniquePerAttempt(t *testing.T) {
	steps := generateSteps(t)
	upload := stepIndex(steps, "Upload bundle")
	if upload < 0 {
		t.Fatal("missing upload step")
	}
	action, ok := usesOf(steps[upload])
	if !ok || !strings.HasPrefix(action.Uses.Value, "actions/upload-artifact@") {
		t.Fatal("the upload step must use actions/upload-artifact")
	}
	want := "otelc-bundle-pr-${{ inputs.pr_number }}-${{ github.run_attempt }}"
	if name, _ := input(action, "name"); name != want {
		t.Errorf("upload name = %q, want %q", name, want)
	}
}
