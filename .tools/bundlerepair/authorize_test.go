// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const (
	testNow  = 1_800_000_000
	testRepo = "test/upstream"
	zeroSHA  = "0000000000000000000000000000000000000000"
	headSHA  = "1111111111111111111111111111111111111111"
	baseSHA  = "2222222222222222222222222222222222222222"
	oldSHA   = "3333333333333333333333333333333333333333"

	permissionRoute = "GET /repos/test/upstream/collaborators/maintainer/permission"
	pullRoute       = "GET /repos/test/upstream/pulls/1"
	baseRefRoute    = "GET /repos/test/upstream/git/ref/heads/main"

	autofixRef    = testRepo + "/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
	regenerateRef = testRepo + "/.github/workflows/regenerate-bundle.yaml@refs/heads/main"
)

func permissionBody(role string) string {
	return `{"permission":"write","role_name":"` + role + `"}`
}

func pullBody(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	pull := map[string]any{
		"state":                 "open",
		"draft":                 false,
		"maintainer_can_modify": true,
		"head": map[string]any{
			"sha":  headSHA,
			"ref":  "contribution",
			"repo": map[string]any{"full_name": "test/fork"},
		},
		"base": map[string]any{"sha": oldSHA, "ref": "main"},
	}
	if mutate != nil {
		mutate(pull)
	}
	data, err := json.Marshal(pull)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func baseRefBody(sha string) string {
	return `{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + sha + `"}}`
}

func deadline(offset int64) string { return strconv.FormatInt(testNow+offset, 10) }

func authEnv(url string, extra map[string]string) map[string]string {
	env := map[string]string{
		"REPO":           testRepo,
		"GITHUB_API_URL": url,
		"GH_TOKEN":       "test-token",
	}
	maps.Copy(env, extra)
	return env
}

// Shell group: test_authorization (manual permission checks).
func TestAuthorizeManualRole(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"maintain role passes", permissionBody("maintain"), false},
		{"admin role passes", `{"permission":"admin","role_name":"admin"}`, false},
		{"write role alone is rejected", permissionBody("write"), true},
		{"triage role is rejected", permissionBody("triage"), true},
		{"missing role is rejected", `{"permission":"admin"}`, true},
		{"legacy permission alone is rejected", `{"permission":"write"}`, true},
		{"unknown user is rejected", "", true},
		{"invalid JSON is rejected", `not json`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAPIWithBodies(t, map[string]string{permissionRoute: tt.body})
			env := authEnv(fake.URL, map[string]string{"COMMENTER": "maintainer"})
			code, out := runApp(t, env, "authorize")
			if (code != 0) != tt.wantErr {
				t.Errorf("exit code = %d, stderr = %q", code, out.stderr)
			}
			if got := len(fake.requests()); got != 1 {
				t.Errorf("made %d requests, want 1", got)
			}
		})
	}
}

// Shell group: test_authorization (a bot name alone).
func TestAuthorizeRejectsInvalidActorsWithoutRequests(t *testing.T) {
	for _, actor := range []string{"github-actions[bot]", "a b", "a/b", "..", "a\nb", "ä"} {
		t.Run(actor, func(t *testing.T) {
			fake := newAPI(t)
			env := authEnv(fake.URL, map[string]string{"COMMENTER": actor, "RUN_ATTEMPT": "1"})
			if code, _ := runApp(t, env, "authorize"); code == 0 {
				t.Error("a name outside the user name pattern was authorized")
			}
			if got := fake.requests(); len(got) != 0 {
				t.Errorf("unexpected requests %v", got)
			}
		})
	}
}

func TestAuthorizeCallers(t *testing.T) {
	tests := []struct {
		name    string
		caller  string
		wantErr bool
		calls   int
	}{
		{"direct invocation", "", false, 1},
		{"trusted regenerate caller", regenerateRef, false, 1},
		// Shell group: test_authorization (a maintainer actor hid an untrusted caller).
		{
			"untrusted autofix branch",
			testRepo + "/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/contribution",
			true,
			0,
		},
		{
			"untrusted regenerate branch",
			testRepo + "/.github/workflows/regenerate-bundle.yaml@refs/heads/contribution",
			true,
			0,
		},
		{"other workflow", testRepo + "/.github/workflows/other.yaml@refs/heads/main", true, 0},
		{"other repository", "other/repo/.github/workflows/regenerate-bundle.yaml@refs/heads/main", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]string{permissionRoute: permissionBody("maintain")}
			fake := newAPIWithBodies(t, routes)
			env := authEnv(fake.URL, map[string]string{"COMMENTER": "maintainer", "CALLER_WORKFLOW_REF": tt.caller})
			code, _ := runApp(t, env, "authorize")
			if (code != 0) != tt.wantErr {
				t.Errorf("exit code = %d", code)
			}
			if got := len(fake.requests()); got != tt.calls {
				t.Errorf("made %d requests, want %d", got, tt.calls)
			}
		})
	}
}

func TestAuthorizeRerunChecksBothActors(t *testing.T) {
	const rerunRoute = "GET /repos/test/upstream/collaborators/rerunner/permission"
	tests := []struct {
		name    string
		routes  map[string]string
		env     map[string]string
		wantErr bool
		calls   int
	}{
		{
			"both maintainers",
			map[string]string{permissionRoute: permissionBody("maintain"), rerunRoute: permissionBody("admin")},
			map[string]string{"TRIGGERING_ACTOR": "rerunner"},
			false, 2,
		},
		{
			"rerunner lacks the role",
			map[string]string{permissionRoute: permissionBody("maintain"), rerunRoute: permissionBody("write")},
			map[string]string{"TRIGGERING_ACTOR": "rerunner"},
			true, 2,
		},
		{
			"commenter lacks the role",
			map[string]string{permissionRoute: permissionBody("write")},
			map[string]string{"TRIGGERING_ACTOR": "rerunner"},
			true, 1,
		},
		{
			"missing triggering actor",
			map[string]string{permissionRoute: permissionBody("maintain")},
			nil, true, 0,
		},
		{
			"first attempt ignores the triggering actor",
			map[string]string{permissionRoute: permissionBody("maintain")},
			map[string]string{"TRIGGERING_ACTOR": "rerunner", "RUN_ATTEMPT": "1"},
			false, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAPIWithBodies(t, tt.routes)
			extra := map[string]string{"COMMENTER": "maintainer", "RUN_ATTEMPT": "2"}
			maps.Copy(extra, tt.env)
			code, _ := runApp(t, authEnv(fake.URL, extra), "authorize")
			if (code != 0) != tt.wantErr {
				t.Errorf("exit code = %d", code)
			}
			if got := len(fake.requests()); got != tt.calls {
				t.Errorf("made %d requests (%v), want %d", got, fake.requests(), tt.calls)
			}
		})
	}
}

func TestAuthorizeRequiredInput(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"missing repository", map[string]string{"REPO": "", "COMMENTER": "maintainer"}},
		{"repository without owner", map[string]string{"REPO": "upstream", "COMMENTER": "maintainer"}},
		{"repository with extra part", map[string]string{"REPO": "a/b/c", "COMMENTER": "maintainer"}},
		{"repository with newline", map[string]string{"REPO": "a/b\nc", "COMMENTER": "maintainer"}},
		{"missing commenter", map[string]string{"COMMENTER": ""}},
		{"missing token", map[string]string{"COMMENTER": "maintainer", "GH_TOKEN": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAPI(t)
			if code, _ := runApp(t, authEnv(fake.URL, tt.env), "authorize"); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			if got := fake.requests(); len(got) != 0 {
				t.Errorf("unexpected requests %v", got)
			}
		})
	}
}

// Shell group: test_authorization (automatic request, rerun, and caller) and
// Go test TestAutomaticDeadline.
func TestAuthorizeAutomaticAttemptAndDeadline(t *testing.T) {
	tests := []struct {
		name     string
		attempt  string
		deadline string
		wantErr  bool
	}{
		{"valid", "1", deadline(300), false},
		{"default attempt", "", deadline(300), false},
		{"rerun", "2", deadline(300), true},
		{"rerun of a later attempt", "10", deadline(300), true},
		{"missing deadline", "1", "", true},
		{"invalid deadline", "1", "invalid", true},
		{"deadline is now", "1", deadline(0), true},
		{"expired deadline", "1", deadline(-3600), true},
		{"one second left", "1", deadline(1), false},
		{"deadline at the window edge", "1", deadline(3600), false},
		{"deadline past the window", "1", deadline(3601), true},
		{"far deadline", "1", deadline(7200), true},
		{"leading zero", "1", "0" + deadline(300), true},
		{"zero", "1", "0", true},
		{"negative", "1", "-5", true},
		{"fractional", "1", deadline(300) + ".5", true},
		{"trailing newline", "1", deadline(300) + "\n", true},
		{"twelve digits", "1", "999999999999", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAPI(t)
			env := authEnv(fake.URL, map[string]string{
				"CALLER_WORKFLOW_REF": autofixRef,
				"COMMENTER":           "github-actions[bot]",
				"RUN_ATTEMPT":         tt.attempt,
				"AUTO_DEADLINE":       tt.deadline,
			})
			code, _ := runApp(t, env, "authorize")
			if (code != 0) != tt.wantErr {
				t.Errorf("exit code = %d", code)
			}
			if got := fake.requests(); len(got) != 0 {
				t.Errorf("a request without a pull request made calls %v", got)
			}
		})
	}
}

// Go test TestAutomaticPublicationEligibility covers the same pull request
// checks through the publisher. Phase 2 reuses this decision.
func TestAuthorizeAutomaticPullRequest(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		baseRef string
		prEnv   map[string]string
		wantErr bool
		calls   int
	}{
		{name: "valid", baseRef: baseRefBody(baseSHA), calls: 2},
		{
			name:    "older base snapshot",
			mutate:  func(p map[string]any) { p["base"] = map[string]any{"sha": oldSHA, "ref": "main"} },
			baseRef: baseRefBody(baseSHA),
			calls:   2,
		},
		{name: "same repository without edits", mutate: func(p map[string]any) {
			p["maintainer_can_modify"] = false
			p["head"] = map[string]any{"sha": headSHA, "repo": map[string]any{"full_name": testRepo}}
		}, baseRef: baseRefBody(baseSHA), calls: 2},
		{
			name:    "draft",
			mutate:  func(p map[string]any) { p["draft"] = true },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{
			name:    "missing draft",
			mutate:  func(p map[string]any) { delete(p, "draft") },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{
			name:    "null draft",
			mutate:  func(p map[string]any) { p["draft"] = nil },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{
			name:    "closed",
			mutate:  func(p map[string]any) { p["state"] = "closed" },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{name: "head moved", mutate: func(p map[string]any) {
			p["head"] = map[string]any{"sha": zeroSHA, "repo": map[string]any{"full_name": "test/fork"}}
		}, baseRef: baseRefBody(baseSHA), wantErr: true, calls: 1},
		{
			name:    "retargeted",
			mutate:  func(p map[string]any) { p["base"] = map[string]any{"sha": oldSHA, "ref": "release"} },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{
			name:    "edits disabled on a fork",
			mutate:  func(p map[string]any) { p["maintainer_can_modify"] = false },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{
			name:    "deleted fork",
			mutate:  func(p map[string]any) { p["head"] = map[string]any{"sha": headSHA, "repo": nil} },
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
		},
		{name: "base branch moved", baseRef: baseRefBody(zeroSHA), wantErr: true, calls: 2},
		{name: "missing base reference", baseRef: `{}`, wantErr: true, calls: 2},
		{
			name:    "base reference is a tag object",
			baseRef: `{"object":{"type":"tag","sha":"` + baseSHA + `"}}`,
			wantErr: true,
			calls:   2,
		},
		{
			name:    "missing pull request",
			mutate:  nil,
			baseRef: baseRefBody(baseSHA),
			wantErr: true,
			calls:   1,
			prEnv:   map[string]string{"PR_NUMBER": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]string{pullRoute: pullBody(t, tt.mutate), baseRefRoute: tt.baseRef}
			if tt.name == "missing pull request" {
				routes[pullRoute] = ""
			}
			fake := newAPIWithBodies(t, routes)
			extra := map[string]string{
				"CALLER_WORKFLOW_REF": autofixRef,
				"RUN_ATTEMPT":         "1",
				"AUTO_DEADLINE":       deadline(300),
				"PR_NUMBER":           "1",
				"EXPECTED_HEAD_SHA":   headSHA,
				"BASE_SHA":            baseSHA,
			}
			maps.Copy(extra, tt.prEnv)
			code, _ := runApp(t, authEnv(fake.URL, extra), "authorize")
			if (code != 0) != tt.wantErr {
				t.Errorf("exit code = %d", code)
			}
			if got := len(fake.requests()); got != tt.calls {
				t.Errorf("made %d requests (%v), want %d", got, fake.requests(), tt.calls)
			}
		})
	}
}

func TestAuthorizeAutomaticInvalidMetadata(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"zero pull request", map[string]string{"PR_NUMBER": "0"}},
		{"leading zero pull request", map[string]string{"PR_NUMBER": "01"}},
		{"word pull request", map[string]string{"PR_NUMBER": "one"}},
		{"huge pull request", map[string]string{"PR_NUMBER": strings.Repeat("9", 30)}},
		{"missing head", map[string]string{"EXPECTED_HEAD_SHA": ""}},
		{"short head", map[string]string{"EXPECTED_HEAD_SHA": "abc"}},
		{"upper case head", map[string]string{"EXPECTED_HEAD_SHA": strings.ToUpper(strings.Repeat("a", 40))}},
		{"missing base", map[string]string{"BASE_SHA": ""}},
		{"non-hex base", map[string]string{"BASE_SHA": strings.Repeat("g", 40)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAPI(t)
			extra := map[string]string{
				"CALLER_WORKFLOW_REF": autofixRef,
				"AUTO_DEADLINE":       deadline(300),
				"PR_NUMBER":           "1",
				"EXPECTED_HEAD_SHA":   headSHA,
				"BASE_SHA":            baseSHA,
			}
			maps.Copy(extra, tt.env)
			if code, _ := runApp(t, authEnv(fake.URL, extra), "authorize"); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			if got := fake.requests(); len(got) != 0 {
				t.Errorf("unexpected requests %v", got)
			}
		})
	}
}

func TestAuthorizeUsesInjectedClock(t *testing.T) {
	req := authorizeRequest{
		Repo:           testRepo,
		CallerWorkflow: autofixRef,
		RunAttempt:     "1",
		AutoDeadline:   deadline(300),
	}
	if err := authorize(t.Context(), req, time.Unix(testNow, 0)); err != nil {
		t.Errorf("before the deadline: %v", err)
	}
	if err := authorize(t.Context(), req, time.Unix(testNow+300, 0)); err == nil {
		t.Error("at the deadline the request was accepted")
	}
}

func TestAuthorizeRejectsArguments(t *testing.T) {
	if code, _ := runApp(t, nil, "authorize", "extra"); code != exitFailure {
		t.Errorf("exit code = %d", code)
	}
}

// D1: one shared eligibility rule that selection, authorization, and
// preparation extend. The rule needs an open, non-draft pull request against
// the default branch whose head repository the maintainers may update or own.
func TestEligiblePullRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   bool
	}{
		{"eligible fork", nil, true},
		{"same repository needs no edit rights", func(p map[string]any) {
			p["maintainer_can_modify"] = false
			p["head"] = map[string]any{"sha": headSHA, "ref": "c", "repo": map[string]any{"full_name": testRepo}}
		}, true},
		{"closed", func(p map[string]any) { p["state"] = "closed" }, false},
		{"draft", func(p map[string]any) { p["draft"] = true }, false},
		{"missing draft", func(p map[string]any) { delete(p, "draft") }, false},
		{"null draft", func(p map[string]any) { p["draft"] = nil }, false},
		{"retargeted", func(p map[string]any) { p["base"] = map[string]any{"sha": oldSHA, "ref": "release"} }, false},
		{"deleted fork", func(p map[string]any) {
			p["head"] = map[string]any{"sha": headSHA, "ref": "c", "repo": nil}
		}, false},
		{"edits disabled on a fork", func(p map[string]any) { p["maintainer_can_modify"] = false }, false},
		{"foreign repository without edit rights", func(p map[string]any) {
			p["maintainer_can_modify"] = false
			p["head"] = map[string]any{"sha": headSHA, "ref": "c", "repo": map[string]any{"full_name": "other/fork"}}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := pullBody(t, tt.mutate)
			var pull github.PullRequest
			if err := json.Unmarshal([]byte(data), &pull); err != nil {
				t.Fatal(err)
			}
			if got := eligiblePullRequest(pull, testRepo); got != tt.want {
				t.Errorf("eligiblePullRequest = %v, want %v", got, tt.want)
			}
		})
	}
}
