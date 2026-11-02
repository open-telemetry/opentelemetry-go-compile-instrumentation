// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prepFixture serves one pull request and its base branch.
type prepFixture struct {
	api    *apiFake
	output string
	env    map[string]string
}

func newPrepFixture(t *testing.T, automatic bool) *prepFixture {
	t.Helper()
	fx := &prepFixture{api: newAPI(t), output: filepath.Join(t.TempDir(), "outputs")}
	fx.env = map[string]string{
		"REPO": testRepo, "GH_TOKEN": "test-token", "GITHUB_API_URL": fx.api.URL, "GITHUB_OUTPUT": fx.output,
		"PR_NUMBER": "1", "INPUT_PR_NUMBER": "1", "COMMENTER": "maintainer",
		"CALLER_WORKFLOW_REF": regenerateRef,
	}
	fx.api.setBody(permissionRoute, permissionBody("maintain"))
	fx.api.setBody(baseRefRoute, baseRefBody(baseSHA))
	fx.setPull(nil)
	if automatic {
		fx.env["CALLER_WORKFLOW_REF"] = autofixRef
		fx.env["RUN_ATTEMPT"] = "1"
		fx.env["AUTO_DEADLINE"] = deadline(300)
		fx.env["EXPECTED_HEAD_SHA"] = headSHA
		fx.env["BASE_SHA"] = baseSHA
	}
	return fx
}

func (fx *prepFixture) setPull(mutate func(map[string]any)) {
	fx.api.setBody(pullRoute, pullBody(fx.api.t, mutate))
}

func (fx *prepFixture) run() (int, output) {
	return runApp(fx.api.t, fx.env, "prepare")
}

func (fx *prepFixture) lines(t *testing.T) []string {
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

// Go test TestPrepareCapturesLiveBase (manual and automatic). The older base
// snapshot of the pull request response must not reach the outputs.
func TestPrepareCapturesLiveBase(t *testing.T) {
	for _, mode := range []string{"manual", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			fx := newPrepFixture(t, mode == "automatic")
			fx.setPull(func(p map[string]any) { p["base"] = map[string]any{"sha": oldSHA, "ref": "main"} })
			code, out := fx.run()
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			want := []string{
				"authorized=true", "pr_number=1", "base_ref=main", "base_sha=" + baseSHA,
				"head_ref=contribution", "head_repo_full=test/fork", "head_sha=" + headSHA,
				"is_cross_repository=true",
			}
			if got := fx.lines(t); !slices.Equal(got, want) {
				t.Errorf("outputs = %q, want %q", got, want)
			}
		})
	}
}

func TestPrepareSameRepositoryIsNotCrossRepository(t *testing.T) {
	fx := newPrepFixture(t, false)
	fx.setPull(func(p map[string]any) {
		p["head"] = map[string]any{"sha": headSHA, "ref": "feature/x", "repo": map[string]any{"full_name": testRepo}}
	})
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	got := fx.lines(t)
	if !slices.Contains(got, "is_cross_repository=false") || !slices.Contains(got, "head_ref=feature/x") {
		t.Errorf("outputs = %q", got)
	}
}

// The job condition of the old workflow: a trusted commenter writing exactly
// the command, or a pull request number input.
func TestPrepareCommentFilter(t *testing.T) {
	comment := map[string]string{
		"EVENT_NAME": "issue_comment", "ISSUE_IS_PULL_REQUEST": "true",
		"AUTHOR_ASSOCIATION": "MEMBER", "COMMENT_BODY": regenerateCommand, "INPUT_PR_NUMBER": "",
	}
	tests := []struct {
		name       string
		env        map[string]string
		authorized string
	}{
		{"member command", comment, "true"},
		{"owner command", with(comment, "AUTHOR_ASSOCIATION", "OWNER"), "true"},
		{"collaborator command", with(comment, "AUTHOR_ASSOCIATION", "COLLABORATOR"), "true"},
		{"dispatch input", map[string]string{"EVENT_NAME": "workflow_dispatch", "INPUT_PR_NUMBER": "1"}, "true"},
		{"contributor command", with(comment, "AUTHOR_ASSOCIATION", "CONTRIBUTOR"), "false"},
		{"no association", with(comment, "AUTHOR_ASSOCIATION", "NONE"), "false"},
		{"command with a suffix", with(comment, "COMMENT_BODY", regenerateCommand+" now"), "false"},
		{"command with a newline", with(comment, "COMMENT_BODY", regenerateCommand+"\n"), "false"},
		{"other comment", with(comment, "COMMENT_BODY", "looks good"), "false"},
		{"issue comment", with(comment, "ISSUE_IS_PULL_REQUEST", ""), "false"},
		{"other event", with(comment, "EVENT_NAME", "pull_request_review_comment"), "false"},
		{"no event and no input", map[string]string{"INPUT_PR_NUMBER": ""}, "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPrepFixture(t, false)
			maps.Copy(fx.env, tt.env)
			code, out := fx.run()
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			got := fx.lines(t)
			if len(got) == 0 || got[0] != "authorized="+tt.authorized {
				t.Errorf("outputs = %q, want authorized=%s", got, tt.authorized)
			}
			if tt.authorized == "false" {
				if len(got) != 1 {
					t.Errorf("a filtered request wrote %q", got)
				}
				if requests := fx.api.requests(); len(requests) != 0 {
					t.Errorf("a filtered request called %v", requests)
				}
			}
		})
	}
}

func with(base map[string]string, key, value string) map[string]string {
	out := maps.Clone(base)
	out[key] = value
	return out
}

func TestPrepareRejections(t *testing.T) {
	tests := []struct {
		name      string
		automatic bool
		setup     func(fx *prepFixture)
	}{
		{name: "closed", setup: func(fx *prepFixture) { fx.setPull(func(p map[string]any) { p["state"] = "closed" }) }},
		{name: "deleted fork", setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) { p["head"] = map[string]any{"sha": headSHA, "ref": "c", "repo": nil} })
		}},
		{
			name:  "not a maintainer",
			setup: func(fx *prepFixture) { fx.api.setBody(permissionRoute, permissionBody("write")) },
		},
		{
			name:  "unknown commenter",
			setup: func(fx *prepFixture) { fx.api.set(permissionRoute, jsonHandler(404, `{"message":"Not Found"}`)) },
		},
		{name: "bad pull request number", setup: func(fx *prepFixture) { fx.env["PR_NUMBER"] = "0" }},
		{name: "text pull request number", setup: func(fx *prepFixture) { fx.env["PR_NUMBER"] = "one" }},
		{name: "bad branch name", setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{
					"sha":  headSHA,
					"ref":  "a..b",
					"repo": map[string]any{"full_name": "test/fork"},
				}
			})
		}},
		{name: "empty branch name", setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{"sha": headSHA, "ref": "", "repo": map[string]any{"full_name": "test/fork"}}
			})
		}},
		{name: "base is not a commit", setup: func(fx *prepFixture) {
			fx.api.setBody(baseRefRoute, `{"object":{"type":"tag","sha":"`+baseSHA+`"}}`)
		}},
		{name: "missing base object", setup: func(fx *prepFixture) { fx.api.setBody(baseRefRoute, `{}`) }},
		{name: "bad head sha", setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{"sha": "abc", "ref": "c", "repo": map[string]any{"full_name": "test/fork"}}
			})
		}},
		{name: "bad head repository", setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{"sha": headSHA, "ref": "c", "repo": map[string]any{"full_name": "fork"}}
			})
		}},
		{name: "automatic base moved", automatic: true, setup: func(fx *prepFixture) {
			fx.api.setBody(baseRefRoute, baseRefBody(zeroSHA))
		}},
		{name: "automatic draft", automatic: true, setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) { p["draft"] = true })
		}},
		{name: "automatic missing draft", automatic: true, setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) { delete(p, "draft") })
		}},
		{name: "automatic head moved", automatic: true, setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{"sha": zeroSHA, "ref": "c", "repo": map[string]any{"full_name": "test/fork"}}
			})
		}},
		{name: "automatic retargeted", automatic: true, setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) { p["base"] = map[string]any{"sha": baseSHA, "ref": "release"} })
		}},
		{name: "automatic edits disabled", automatic: true, setup: func(fx *prepFixture) {
			fx.setPull(func(p map[string]any) { p["maintainer_can_modify"] = false })
		}},
		{
			name:      "automatic expired",
			automatic: true,
			setup:     func(fx *prepFixture) { fx.env["AUTO_DEADLINE"] = deadline(-1) },
		},
		{name: "untrusted caller", setup: func(fx *prepFixture) {
			fx.env["CALLER_WORKFLOW_REF"] = testRepo + "/.github/workflows/regenerate-bundle.yaml@refs/heads/contribution"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPrepFixture(t, tt.automatic)
			tt.setup(fx)
			code, _ := fx.run()
			if code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			for _, line := range fx.lines(t) {
				if strings.HasPrefix(line, "authorized=true") {
					t.Errorf("a rejected request wrote %q", line)
				}
			}
		})
	}
}

func TestPrepareAutomaticAcceptsEditsDisabledInSameRepository(t *testing.T) {
	fx := newPrepFixture(t, true)
	fx.setPull(func(p map[string]any) {
		p["maintainer_can_modify"] = false
		p["head"] = map[string]any{"sha": headSHA, "ref": "feature", "repo": map[string]any{"full_name": testRepo}}
	})
	if code, out := fx.run(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
}

// Go test TestPublisherCredentialSelection.
func TestCredentials(t *testing.T) {
	tests := []struct {
		name, client, key, fork, want string
	}{
		{"missing both", "", "", "false", "false"},
		{"missing key", "client", "", "false", "false"},
		{"missing client", "", "test-key", "false", "false"},
		{"complete App", "client", "test-key", "false", "true"},
		{"fork", "client", "test-key", "true", "false"},
		{"unset cross-repository flag", "client", "test-key", "", "false"},
		{"unexpected cross-repository flag", "client", "test-key", "False", "false"},
		{"fork needs no client or key", "", "", "true", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "outputs")
			env := map[string]string{
				"CLIENT_ID": tt.client, "PRIVATE_KEY": tt.key, "IS_CROSS_REPOSITORY": tt.fork, "GITHUB_OUTPUT": output,
			}
			code, out := runApp(t, env, "credentials")
			if code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "use_app="+tt.want+"\n" {
				t.Errorf("unexpected App decision: %q", data)
			}
			if strings.Contains(out.stdout+out.stderr+string(data), "test-key") {
				t.Error("the private key leaked")
			}
		})
	}
}

// The old step ran under set -u, so a variable that the decision reaches must be set.
func TestCredentialsRequiresReachedVariables(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		want    string
	}{
		{"flag unset", map[string]string{"CLIENT_ID": "c", "PRIVATE_KEY": "k"}, true, ""},
		{"client unset", map[string]string{"IS_CROSS_REPOSITORY": "false", "PRIVATE_KEY": "k"}, true, ""},
		{"key unset", map[string]string{"IS_CROSS_REPOSITORY": "false", "CLIENT_ID": "c"}, true, ""},
		{"fork stops before the client", map[string]string{"IS_CROSS_REPOSITORY": "true"}, false, "false"},
		{
			"empty client stops before the key",
			map[string]string{"IS_CROSS_REPOSITORY": "false", "CLIENT_ID": ""},
			false,
			"false",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "outputs")
			env := map[string]string{"GITHUB_OUTPUT": output}
			maps.Copy(env, tt.env)
			code, _ := runApp(t, env, "credentials")
			if (code != 0) != tt.wantErr {
				t.Fatalf("exit code = %d", code)
			}
			data, _ := os.ReadFile(output)
			if tt.wantErr && len(data) != 0 {
				t.Errorf("a failed decision wrote %q", data)
			}
			if !tt.wantErr && string(data) != "use_app="+tt.want+"\n" {
				t.Errorf("outputs = %q", data)
			}
		})
	}
}

func TestCredentialsNeedsOutputFile(t *testing.T) {
	if code, _ := runApp(t, map[string]string{"IS_CROSS_REPOSITORY": "false"}, "credentials"); code != exitFailure {
		t.Errorf("exit code = %d", code)
	}
	if code, _ := runApp(t, nil, "credentials", "extra"); code != exitFailure {
		t.Errorf("exit code = %d", code)
	}
}

// identityEnv returns the environment of the identity command. The output
// and summary files are env["GITHUB_OUTPUT"] and env["GITHUB_STEP_SUMMARY"].
func identityEnv(t *testing.T, api *apiFake, extra map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{
		"GH_TOKEN": "test-token", "APP_SLUG": "", "GITHUB_API_URL": api.URL,
		"GITHUB_OUTPUT": filepath.Join(dir, "outputs"), "GITHUB_STEP_SUMMARY": filepath.Join(dir, "summary"),
	}
	maps.Copy(env, extra)
	return env
}

func TestIdentityMissingToken(t *testing.T) {
	api := newAPI(t)
	env := identityEnv(t, api, map[string]string{"GH_TOKEN": "", "APP_SLUG": "app"})
	outputPath, summaryPath := env["GITHUB_OUTPUT"], env["GITHUB_STEP_SUMMARY"]
	code, out := runApp(t, env, "identity")
	if code != exitFailure {
		t.Fatalf("exit code = %d", code)
	}
	wantOut := "::error::Missing publisher token in bundle-publisher.\n" +
		"Configure OTELC_BUNDLE_PUSH_TOKEN as an environment secret.\n" +
		"The App remains supported for repository branches.\n"
	if out.stdout != wantOut {
		t.Errorf("stdout = %q, want %q", out.stdout, wantOut)
	}
	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := "Publishing requires a configured token.\nThe generated artifact remains available for one day.\n"
	if string(summary) != wantSummary {
		t.Errorf("summary = %q, want %q", summary, wantSummary)
	}
	if _, err = os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("a missing token wrote outputs")
	}
	if got := api.requests(); len(got) != 0 {
		t.Errorf("requests = %v", got)
	}
}

func TestIdentityLookups(t *testing.T) {
	tests := []struct {
		name  string
		slug  string
		route string
		body  string
		want  []string
	}{
		{
			"App",
			"my-app",
			"GET /users/my-app[bot]",
			`{"login":"my-app[bot]","id":4242}`,
			[]string{"login=my-app[bot]", "id=4242"},
		},
		{"token owner", "", "GET /user", `{"login":"otelbot","id":7}`, []string{"login=otelbot", "id=7"}},
		{"missing fields print null as jq does", "", "GET /user", `{}`, []string{"login=null", "id=null"}},
		{"null fields", "", "GET /user", `{"login":null,"id":null}`, []string{"login=null", "id=null"}},
		{"string id", "", "GET /user", `{"login":"a","id":"12"}`, []string{"login=a", "id=12"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newAPI(t)
			api.setBody(tt.route, tt.body)
			env := identityEnv(t, api, map[string]string{"APP_SLUG": tt.slug})
			outputPath := env["GITHUB_OUTPUT"]
			if code, out := runApp(t, env, "identity"); code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			data, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); !slices.Equal(got, tt.want) {
				t.Errorf("outputs = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIdentityRejections(t *testing.T) {
	tests := []struct {
		name string
		slug string
		body string
		code int
	}{
		{"server error", "", "", 404},
		{"object login", "", `{"login":{},"id":7}`, 200},
		{"boolean id", "", `{"login":"otelbot","id":true}`, 200},
		{"not JSON", "", `nope`, 200},
		{"login with a newline", "", `{"login":"a\nb","id":7}`, 200},
		{"id with a newline", "", `{"login":"a","id":"7\n8"}`, 200},
		{"bad slug", "my app", `{"login":"x","id":1}`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newAPI(t)
			if tt.slug == "" {
				api.set("GET /user", jsonHandler(tt.code, tt.body))
			}
			env := identityEnv(t, api, map[string]string{"APP_SLUG": tt.slug})
			outputPath := env["GITHUB_OUTPUT"]
			if code, _ := runApp(t, env, "identity"); code != exitFailure {
				t.Errorf("exit code = %d", code)
			}
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Error("a rejected identity wrote outputs")
			}
		})
	}
}

// The old step ran under set -u, so unset variables fail before any request.
func TestIdentityRequiresSetVariables(t *testing.T) {
	for _, name := range []string{"GH_TOKEN", "APP_SLUG"} {
		t.Run(name, func(t *testing.T) {
			api := newAPI(t)
			env := identityEnv(t, api, nil)
			delete(env, name)
			if code, out := runApp(t, env, "identity"); code != exitFailure || out.stdout != "" {
				t.Errorf("exit code = %d, stdout = %q", code, out.stdout)
			}
			if got := api.requests(); len(got) != 0 {
				t.Errorf("requests = %v", got)
			}
		})
	}
}
