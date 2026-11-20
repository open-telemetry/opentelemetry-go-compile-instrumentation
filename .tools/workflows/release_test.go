// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package workflows_test

import (
	"strings"
	"testing"

	"github.com/rhysd/actionlint"
)

// releaseWorkflows pairs each release workflow with the job that ships it.
var releaseWorkflows = map[string]string{
	"release.yml":         "release",
	"release-prepare.yml": "prepare",
}

// findStep returns the step with the given name or id in the job of the named
// workflow. The name argument matches the step name first and the step id
// second, because release-prepare names its steps while the bundle workflows
// mostly use ids.
func findStep(t *testing.T, workflowName, jobID, nameOrID string) *actionlint.Step {
	t.Helper()
	workflow := loadWorkflow(t, workflowName)
	job := workflow.Jobs[jobID]
	if job == nil {
		t.Fatalf("%s: missing job %s", workflowName, jobID)
	}
	for _, step := range job.Steps {
		if step.Name != nil && step.Name.Value == nameOrID {
			return step
		}
		if step.ID != nil && step.ID.Value == nameOrID {
			return step
		}
	}
	t.Fatalf("%s: missing step %q in job %s", workflowName, nameOrID, jobID)
	return nil
}

func findStepOptional(t *testing.T, workflowName, jobID, nameOrID string) *actionlint.Step {
	t.Helper()
	workflow := loadWorkflow(t, workflowName)
	job := workflow.Jobs[jobID]
	if job == nil {
		t.Fatalf("%s: missing job %s", workflowName, jobID)
	}
	for _, step := range job.Steps {
		if step.Name != nil && step.Name.Value == nameOrID {
			return step
		}
		if step.ID != nil && step.ID.Value == nameOrID {
			return step
		}
	}
	return nil
}

// TestAppTokenFailureSurfacesWarning pins the failure signal on both
// create-github-app-token steps. Without it, an App misconfiguration silently
// falls back to github.token and changes the release identity.
func TestAppTokenFailureSurfacesWarning(t *testing.T) {
	for name, jobID := range releaseWorkflows {
		t.Run(name, func(t *testing.T) {
			step := findStep(t, name, jobID, "Warn on App token failure")
			if step.If == nil || step.If.Value != "steps.app-token.outcome == 'failure'" {
				t.Fatalf("%s: warning step must gate on steps.app-token.outcome == 'failure'", name)
			}
		})
	}
}

// TestReleasePreparePullRequestConfiguration pins the create-pull-request
// step: draft state, token fallback, file-based body, and release branch name.
func TestReleasePreparePullRequestConfiguration(t *testing.T) {
	step := findStep(t, "release-prepare.yml", "prepare", "Create Pull Request")
	uses, ok := usesOf(step)
	if !ok {
		t.Fatal("Create Pull Request is not an action step")
	}
	const wantAction = "peter-evans/create-pull-request@3ffccacd46053766916f087ce41d627c0b8cf2d1"
	if !strings.HasPrefix(uses.Uses.Value, wantAction) {
		t.Fatalf("Create Pull Request uses %q, want prefix %q", uses.Uses.Value, wantAction)
	}
	if got, ok := input(uses, "draft"); !ok || got != "true" {
		t.Fatalf("Create Pull Request draft input = %q (present=%v), want \"true\"", got, ok)
	}
	if got, ok := input(uses, "token"); !ok || got != "${{ steps.app-token.outputs.token || github.token }}" {
		t.Fatalf("Create Pull Request token input = %q (present=%v), want the App token with github.token fallback", got, ok)
	}
	if got, ok := input(uses, "body"); ok {
		t.Fatalf("Create Pull Request must use body-path, got inline body %q", got)
	}
	if got, ok := input(uses, "body-path"); !ok || got != "${{ steps.metadata.outputs.body_file }}" {
		t.Fatalf("Create Pull Request body-path input = %q (present=%v), want the metadata body file", got, ok)
	}
	if got, ok := input(uses, "branch"); !ok || got != "release/${{ inputs.version }}" {
		t.Fatalf("Create Pull Request branch input = %q (present=%v), want release/${{ inputs.version }}", got, ok)
	}
}

// TestReleasePrepareMetadataAvoidsHeredoc pins the metadata step to file-based
// output. The pr_body<<EOF heredoc broke whenever a value could contain a
// literal EOF line.
func TestReleasePrepareMetadataAvoidsHeredoc(t *testing.T) {
	step := findStep(t, "release-prepare.yml", "prepare", "Prepare PR metadata")
	run, ok := runOf(step)
	if !ok {
		t.Fatal("Prepare PR metadata is not a run step")
	}
	if strings.Contains(run.Run.Value, "<<EOF") {
		t.Error("Prepare PR metadata must not write the PR body through a <<EOF heredoc")
	}
	if strings.Contains(run.Run.Value, "echo -e") {
		t.Error("Prepare PR metadata must not interpret escapes with echo -e")
	}
	if !strings.Contains(run.Run.Value, "release-pr-body.md") {
		t.Error("Prepare PR metadata must write the PR body to the shared body file")
	}
}

// TestPreviousTagFiltersPreReleases pins the strict final-release filter on
// PREVIOUS_TAG. A pre-release previous tag dropped a whole development cycle
// from the generated notes.
func TestPreviousTagFiltersPreReleases(t *testing.T) {
	step := findStep(t, "release-prepare.yml", "prepare", "Find previous tag")
	run, ok := runOf(step)
	if !ok {
		t.Fatal("Find previous tag is not a run step")
	}
	if !strings.Contains(run.Run.Value, "grep -E '^v[0-9]+\\.[0-9]+\\.[0-9]+$'") {
		t.Error("Find previous tag must filter tags to strict final releases (vX.Y.Z, no pre-release suffix)")
	}
	if !strings.Contains(run.Run.Value, "--merged HEAD") {
		t.Error("Find previous tag must only consider tags reachable from HEAD")
	}
}

// TestReleaseCheckoutDoesNotPersistCredentials pins the checkout step: the App
// token must not linger in the runner git config for the whole job.
func TestReleaseCheckoutDoesNotPersistCredentials(t *testing.T) {
	step := findStep(t, "release.yml", "release", "Checkout merge commit")
	uses, ok := usesOf(step)
	if !ok {
		t.Fatal("Checkout merge commit is not an action step")
	}
	if !strings.HasPrefix(uses.Uses.Value, "actions/checkout@") {
		t.Fatalf("Checkout uses %q, want actions/checkout", uses.Uses.Value)
	}
	if got, ok := input(uses, "persist-credentials"); !ok || got != "false" {
		t.Fatalf("Checkout persist-credentials input = %q (present=%v), want \"false\"", got, ok)
	}
	if got, ok := input(uses, "token"); !ok || got != "${{ steps.app-token.outputs.token || github.token }}" {
		t.Fatalf("Checkout token input = %q (present=%v), want the App token with github.token fallback", got, ok)
	}
}

// TestReleaseTagPushUsesExplicitToken pins the tag push: it must authenticate
// with an explicit token URL instead of persisted checkout credentials.
func TestReleaseTagPushUsesExplicitToken(t *testing.T) {
	step := findStep(t, "release.yml", "release", "Create and push tags")
	run, ok := runOf(step)
	if !ok {
		t.Fatal("Create and push tags is not a run step")
	}
	if !strings.Contains(run.Run.Value, "x-access-token:") {
		t.Error("Create and push tags must push with an explicit x-access-token URL")
	}
	if !strings.Contains(run.Run.Value, "${APP_TOKEN:-${GITHUB_TOKEN}}") {
		t.Error("Create and push tags must fall back from APP_TOKEN to GITHUB_TOKEN for the push")
	}
}

// TestReleaseCreatesDraftRelease pins the draft gate on the GitHub release.
func TestReleaseCreatesDraftRelease(t *testing.T) {
	step := findStep(t, "release.yml", "release", "Create GitHub release")
	run, ok := runOf(step)
	if !ok {
		t.Fatal("Create GitHub release is not a run step")
	}
	if !strings.Contains(run.Run.Value, "--draft") {
		t.Error("Create GitHub release must create a draft release")
	}
}

// TestReleaseWorkflowsExist guards the releaseWorkflows map against renames.
func TestReleaseWorkflowsExist(t *testing.T) {
	for name, jobID := range releaseWorkflows {
		workflow := loadWorkflow(t, name)
		if workflow.Jobs[jobID] == nil {
			t.Fatalf("%s: missing job %s", name, jobID)
		}
		if findStepOptional(t, name, jobID, "app-token") == nil {
			t.Fatalf("%s: missing app-token step", name)
		}
	}
}
