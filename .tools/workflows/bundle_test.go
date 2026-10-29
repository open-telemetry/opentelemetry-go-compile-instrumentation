// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package workflows_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rhysd/actionlint"
)

const fixtureSetup = `
source "$1"
mkdir -p "${WORK_DIR}/bin"
cat >"${WORK_DIR}/bin/gh" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
  *'/collaborators/'*'/permission'*) printf '{"role_name":"maintain"}\n' ;;
  *'/git/ref/heads/'*) cat "${GH_BASE_RESPONSE}" ;;
  *) cat "${GH_RESPONSE}" ;;
esac
MOCK
chmod +x "${WORK_DIR}/bin/gh"
export PATH="${WORK_DIR}/bin:${PATH}" GH_TOKEN=test-token COMMENTER=maintainer
unset CALLER_WORKFLOW_REF RUN_ATTEMPT TRIGGERING_ACTOR AUTO_DEADLINE
`

func runFixture(t *testing.T, script string, env ...string) {
	t.Helper()
	path, err := filepath.Abs("../../.github/scripts/test-bundle-workflows.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-c", fixtureSetup+script, "bundle-review-test", path)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture failed: %v\n%s", err, out)
	}
}

func TestArchiveConversionAttributes(t *testing.T) {
	var archive bytes.Buffer
	writer, err := gzip.NewWriterLevel(&archive, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("bundle\r\n$Id: 0123456789012345678901234567890123456789 $\r\n")
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"text", "ident", "text eol=crlf", "working-tree-encoding=UTF-16"} {
		t.Run(attr, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "input.tgz")
			output := filepath.Join(t.TempDir(), "output.tgz")
			if err := os.WriteFile(input, archive.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			runFixture(t, `
publication_fixture
printf 'tool/data/otelc-bundle.tgz %s\n' "${ATTRIBUTES}" >.gitattributes
cp "${ARCHIVE}" "${GENERATED_BUNDLE}"
bash "${SCRIPT_DIR}/publish-bundle.sh"
git --git-dir="${REMOTE}" show contribution:tool/data/otelc-bundle.tgz >"${PUBLISHED}"
`, "ATTRIBUTES="+attr, "ARCHIVE="+input, "PUBLISHED="+output)
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, archive.Bytes()) {
				t.Error("Git changed the generated archive bytes")
			}
			reader, err := gzip.NewReader(bytes.NewReader(got))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			decoded, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(decoded, payload) {
				t.Fatalf("published gzip is invalid: %v", err)
			}
		})
	}
}

func TestAutomaticPublicationEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, filter, baseFilter, allowed string
	}{
		{"valid", ".", ".", "true"},
		{"older base snapshot", ".base.sha = $prior", ".", "true"},
		{"draft", ".draft = true", ".", "false"},
		{"missing draft", "del(.draft)", ".", "false"},
		{"head", `.head.sha = "0000000000000000000000000000000000000000"`, ".", "false"},
		{"base", ".", `.object.sha = "0000000000000000000000000000000000000000"`, "false"},
		{"missing base reference", ".", "{}", "false"},
		{"retargeted", `.base.ref = "release"`, ".", "false"},
		{"edits disabled", ".maintainer_can_modify = false", ".", "false"},
		{"closed", `.state = "closed"`, ".", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runFixture(t, `
publication_fixture
export CALLER_WORKFLOW_REF="${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
export RUN_ATTEMPT=1 AUTO_DEADLINE
AUTO_DEADLINE="$(jq -nr 'now + 300 | floor')"
prior="$(git merge-base "${EXPECTED_HEAD_SHA}" "${BASE_SHA}")"
jq --arg prior "${prior}" "${PR_FILTER}" "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
jq "${BASE_FILTER}" "${GH_BASE_RESPONSE}" >"${GH_BASE_RESPONSE}.new"
mv "${GH_BASE_RESPONSE}.new" "${GH_BASE_RESPONSE}"
before="$(git --git-dir="${REMOTE}" rev-parse contribution)"
if [[ "${ALLOW_PUBLICATION}" == 'true' ]]; then
  bash "${SCRIPT_DIR}/publish-bundle.sh"
  [[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" != "${before}" ]] || fail 'valid automatic publication did not update the fork'
else
  if bash "${SCRIPT_DIR}/publish-bundle.sh"; then
    fail 'publication accepted an ineligible automatic request'
  fi
  [[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" == "${before}" ]] || fail 'rejected publication changed the fork'
fi
`, "PR_FILTER="+tc.filter, "BASE_FILTER="+tc.baseFilter, "ALLOW_PUBLICATION="+tc.allowed)
		})
	}
}

func TestScannerUsesLiveBase(t *testing.T) {
	runFixture(t, `
scanner_fixture
prior="$(git merge-base "${EXPECTED_HEAD_SHA}" "${BASE_SHA}")"
jq --arg prior "${prior}" '.base.sha = $prior' "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
check_plan '[1,2]'
`)
}

func TestManualPublicationUsesLiveBase(t *testing.T) {
	runFixture(t, `
publication_fixture
prior="$(git merge-base "${EXPECTED_HEAD_SHA}" "${BASE_SHA}")"
jq --arg prior "${prior}" '.base.sha = $prior' "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
bash "${SCRIPT_DIR}/publish-bundle.sh"
[[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" == "$(git rev-parse HEAD)" ]] || fail 'manual publication did not update the fork'
`)
}

func TestPrepareCapturesLiveBase(t *testing.T) {
	workflow := loadWorkflow(t, "regenerate-bundle.yaml")
	var body string
	for _, step := range workflow.Jobs["prepare"].Steps {
		if step.ID != nil && step.ID.Value == "pr" {
			if run, ok := step.Exec.(*actionlint.ExecRun); ok {
				body = run.Run.Value
			}
		}
	}
	if body == "" {
		t.Fatal("missing pull request preparation step")
	}
	for _, mode := range []string{"manual", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			runFixture(t, `
publication_fixture
expected_base="${BASE_SHA}"
export GITHUB_OUTPUT="${WORK_DIR}/prepare-output"
prior="$(git merge-base "${EXPECTED_HEAD_SHA}" "${BASE_SHA}")"
jq --arg prior "${prior}" '.base.sha = $prior' "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
if [[ "${MODE}" == automatic ]]; then
  export CALLER_WORKFLOW_REF="${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
else
  export CALLER_WORKFLOW_REF="${REPO}/.github/workflows/regenerate-bundle.yaml@refs/heads/main"
  export BASE_SHA=''
fi
bash -euo pipefail -c "${PREPARE_SCRIPT}"
grep -qx "base_sha=${expected_base}" "${GITHUB_OUTPUT}" || fail 'preparation used the older pull request base snapshot'
grep -qx 'authorized=true' "${GITHUB_OUTPUT}" || fail 'preparation rejected the current branch ref'
`, "MODE="+mode, "PREPARE_SCRIPT="+body)
		})
	}
}

func TestAutomaticDeadline(t *testing.T) {
	runFixture(t, `
export REPO=test/upstream RUN_ATTEMPT=1
export CALLER_WORKFLOW_REF="${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
for AUTO_DEADLINE in '' invalid "$(jq -nr 'now | floor')" "$(jq -nr 'now - 3600 | floor')" "$(jq -nr 'now + 7200 | floor')"; do
  export AUTO_DEADLINE
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'automatic authorization accepted a missing, invalid, or expired deadline'
  fi
done
AUTO_DEADLINE="$(jq -nr 'now + 300 | floor')"
export AUTO_DEADLINE
bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"
`)
}

func TestSelectionRecordedBeforeRepairJobs(t *testing.T) {
	for _, conclusion := range []string{"success", "failure", "skipped"} {
		t.Run(conclusion, func(t *testing.T) {
			runFixture(t, `
scanner_fixture
jq -n --arg conclusion "${RECORD_CONCLUSION}" '[{total_count:1,jobs:[{name:"Select bundle repairs",completed_at:(now | strftime("%Y-%m-%dT%H:%M:%SZ")),steps:[{name:("Bundle repair selection: " + ({at:(now | floor),prs:[range(1;11)]} | tojson)),conclusion:$conclusion}]}]}]' >"${JOBS_JSON}"
DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
check_plan '[]'
`, "RECORD_CONCLUSION="+conclusion)
		})
	}
}

func TestAmbiguousSelectionHistory(t *testing.T) {
	runFixture(t, `
scanner_fixture
jq -n '[{total_count:1,jobs:[{name:"Select bundle repairs",completed_at:(now | strftime("%Y-%m-%dT%H:%M:%SZ")),steps:[{name:"Select bounded bundle repairs",conclusion:"success"}]}]}]' >"${JOBS_JSON}"
if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"; then
  fail 'ambiguous selection history reset the automatic budget'
fi
check_plan '[]'
`)
}

func TestSkippedSelectionDoesNotReserveBudget(t *testing.T) {
	runFixture(t, `
scanner_fixture
jq -n '[{total_count:1,jobs:[{name:"Select bundle repairs",steps:[{name:"Select bounded bundle repairs",conclusion:"skipped"},{name:"Bundle repair selection: ",conclusion:"skipped"}]}]}]' >"${JOBS_JSON}"
DRY_RUN=true bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
check_plan '[]'
`)
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

func TestPublisherEnvironmentTokenRouting(t *testing.T) {
	workflow := loadWorkflow(t, "regenerate-bundle.yaml")
	job := workflow.Jobs["repair"]
	if job == nil || job.WorkflowCall == nil {
		t.Fatal("missing worker call")
	}
	if job.WorkflowCall.InheritSecrets {
		t.Error("the worker must not inherit caller secrets")
	}
	token := job.WorkflowCall.Secrets["otelc_bundle_push_token"]
	if token == nil || token.Value == nil {
		t.Fatal("the publisher environment token needs a named empty placeholder")
	}
	if token.Value.Value != "" {
		t.Error("the caller must not forward the publisher token value")
	}
}

func TestPublisherCredentialSelection(t *testing.T) {
	workflow := loadWorkflow(t, "regenerate-bundle-worker.yaml")
	job := workflow.Jobs["push_update"]
	if job == nil {
		t.Fatal("missing protected publisher job")
	}
	var selector *actionlint.Step
	for _, step := range job.Steps {
		if step.ID != nil && step.ID.Value == "credentials" {
			selector = step
		}
	}
	if selector == nil {
		t.Fatal("publisher must select complete App credentials before creating a token")
	}
	run, ok := selector.Exec.(*actionlint.ExecRun)
	if !ok {
		t.Fatal("credential selector must be a trusted shell step")
	}
	for _, tc := range []struct {
		name, client, key, fork, want string
	}{
		{"missing both", "", "", "false", "false"},
		{"missing key", "client", "", "false", "false"},
		{"missing client", "", "test-key", "false", "false"},
		{"complete App", "client", "test-key", "false", "true"},
		{"fork", "client", "test-key", "true", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "outputs")
			cmd := exec.CommandContext(t.Context(), "bash", "-euo", "pipefail", "-c", run.Run.Value)
			cmd.Env = append(os.Environ(), "CLIENT_ID="+tc.client, "PRIVATE_KEY="+tc.key, "IS_CROSS_REPOSITORY="+tc.fork, "GITHUB_OUTPUT="+output)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("credential selection: %v\n%s", err, out)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "use_app="+tc.want+"\n" {
				t.Errorf("unexpected App decision: %q", data)
			}
		})
	}
}

func TestStageBundleSources(t *testing.T) {
	for _, name := range []string{"test_stage_sync", "test_stage_rejections", "test_stage_arguments"} {
		t.Run(name, func(t *testing.T) {
			runFixture(t, name+"\n")
		})
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
	return slices.IndexFunc(steps, func(s *actionlint.Step) bool {
		return s.Name != nil && s.Name.Value == name
	})
}

func TestGenerateRunsOnlyTrustedBuild(t *testing.T) {
	steps := generateSteps(t)
	merge := stepIndex(steps, "Merge captured base commit")
	stage := stepIndex(steps, "Stage bundle sources")
	build := stepIndex(steps, "Regenerate bundle")
	if merge < 0 || stage < 0 || build < 0 {
		t.Fatalf("missing steps: merge=%d stage=%d build=%d", merge, stage, build)
	}
	if !(merge < stage && stage < build) {
		t.Errorf("steps must run in the order merge, stage, build: %d %d %d", merge, stage, build)
	}
	if check := stepIndex(steps, "Check request before generation"); check < 0 || check > stage {
		t.Error("the request check must run before staging")
	}

	run, ok := steps[stage].Exec.(*actionlint.ExecRun)
	if !ok || strings.TrimSpace(run.Run.Value) != "bash trusted/.github/scripts/stage-bundle-sources.sh branch trusted" {
		t.Fatal("staging must run the trusted script on the branch and trusted directories")
	}
	if run.WorkingDirectory != nil {
		t.Error("the staging step must run from the workspace root")
	}

	run, ok = steps[build].Exec.(*actionlint.ExecRun)
	if !ok || strings.TrimSpace(run.Run.Value) != "make package" {
		t.Fatal("the build step must run make package")
	}
	if wd := run.WorkingDirectory; wd == nil || wd.Value != "trusted" {
		t.Error("make package must run in the trusted checkout")
	}
	for key, want := range map[string]string{"GOTOOLCHAIN": "local", "GOPROXY": "https://proxy.golang.org"} {
		got := steps[build].Env.Vars[strings.ToLower(key)]
		if got == nil || got.Value == nil || got.Value.Value != want {
			t.Errorf("the build step must pin %s=%s", key, want)
		}
	}

	for _, step := range steps {
		if step.Exec == nil {
			continue
		}
		if upload, ok := step.Exec.(*actionlint.ExecAction); ok && strings.HasPrefix(upload.Uses.Value, "actions/upload-artifact@") {
			if path := upload.Inputs["path"]; path == nil || path.Value.Value != "trusted/tool/data/otelc-bundle.tgz" {
				t.Error("the upload must read the archive from the trusted checkout")
			}
		}
	}
}

func TestGenerateNeverRunsBranchCode(t *testing.T) {
	for _, step := range generateSteps(t) {
		run, ok := step.Exec.(*actionlint.ExecRun)
		if !ok {
			continue
		}
		name := ""
		if step.Name != nil {
			name = step.Name.Value
		}
		inBranch := run.WorkingDirectory != nil && strings.HasPrefix(run.WorkingDirectory.Value, "branch")
		for line := range strings.SplitSeq(run.Run.Value, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			switch {
			case strings.Contains(line, "branch/") && !strings.HasPrefix(line, "bash trusted/.github/scripts/"):
				t.Errorf("step %q runs %q against the branch checkout", name, line)
			case inBranch && !strings.HasPrefix(line, "git ") && !strings.HasPrefix(line, "bash ../trusted/.github/scripts/merge-bundle-branch.sh "):
				t.Errorf("step %q runs %q in the branch checkout", name, line)
			case strings.HasPrefix(line, "make ") && (!strings.Contains(name, "Regenerate") || inBranch):
				t.Errorf("step %q runs make outside the trusted build", name)
			case strings.HasPrefix(line, "go "):
				t.Errorf("step %q runs go directly", name)
			}
		}
	}
}
