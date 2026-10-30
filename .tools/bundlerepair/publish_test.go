// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	forkRoute       = "GET /repos/test/fork"
	generatedBundle = "regenerated\x00bundle"
)

// pubFixture mirrors publication_fixture in the old shell test. The working
// directory is the checkout with the base merge in progress.
type pubFixture struct {
	api       *apiFake
	dir       string
	remote    string
	head      string
	base      string
	generated string
	marker    string
	env       map[string]string
	pull      map[string]any
}

func newPubFixture(t *testing.T) *pubFixture {
	t.Helper()
	merge := newMergeFixture(t)
	head := gitIn(t, merge.dir, "rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "fork.git")
	gitIn(t, merge.dir, "clone", "--quiet", "--bare", ".", remote)
	gitIn(t, merge.dir, "remote", "add", "origin", remote)
	generated := filepath.Join(t.TempDir(), "bundle")
	if err := os.WriteFile(generated, []byte(generatedBundle), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeBase(t.Context(), merge.dir, merge.base, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Chdir(merge.dir)

	fx := &pubFixture{
		api: newAPI(t), dir: merge.dir, remote: remote, head: head, base: merge.base, generated: generated,
		marker: filepath.Join(t.TempDir(), "hook-ran"),
	}
	fx.env = map[string]string{
		"REPO": testRepo, "HEAD_REPO_FULL": "test/fork", "HEAD_REF": "contribution", "BASE_REF": "main",
		"PR_NUMBER": "1", "BOT_LOGIN": "test-bot", "BOT_ID": "123", "GH_TOKEN": "test-token",
		"EXPECTED_HEAD_SHA": head, "BASE_SHA": merge.base, "GENERATED_BUNDLE": generated,
		"GITHUB_API_URL": fx.api.URL,
	}
	fx.pull = map[string]any{
		"state": "open", "draft": false, "maintainer_can_modify": true,
		"head": map[string]any{"sha": head, "ref": "contribution", "repo": map[string]any{"full_name": "test/fork"}},
		"base": map[string]any{"sha": merge.base, "ref": "main"},
	}
	fx.api.setBody(baseRefRoute, baseRefBody(merge.base))
	fx.setPull(nil)
	return fx
}

// setPull serves the pull request with the mutation applied to the defaults.
func (fx *pubFixture) setPull(mutate func(map[string]any)) {
	t := fx.api.t
	t.Helper()
	fx.api.setBody(pullRoute, pullBody(t, func(pull map[string]any) {
		maps.Copy(pull, fx.pull)
		if mutate != nil {
			mutate(pull)
		}
	}))
}

func (fx *pubFixture) publish() (int, output) {
	return runApp(fx.api.t, fx.env, "publish")
}

func (fx *pubFixture) remoteHead(t *testing.T) string {
	t.Helper()
	return gitIn(t, fx.remote, "rev-parse", "contribution")
}

func (fx *pubFixture) automatic() {
	fx.env["CALLER_WORKFLOW_REF"] = autofixRef
	fx.env["RUN_ATTEMPT"] = "1"
	fx.env["AUTO_DEADLINE"] = deadline(300)
}

// installHook adds a hook that records that it ran.
func (fx *pubFixture) installHook(t *testing.T) {
	t.Helper()
	hook := "#!/bin/sh\ntouch " + fx.marker + "\n"
	for _, name := range []string{"pre-commit", "commit-msg", "pre-merge-commit", "pre-push", "post-commit", "prepare-commit-msg"} {
		if err := os.WriteFile(filepath.Join(fx.dir, ".git", "hooks", name), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// Shell group: test_fork_publication (publish the merged bundle to a fork
// without executing hooks) and the duplicate request group.
func TestPublishToFork(t *testing.T) {
	fx := newPubFixture(t)
	fx.installHook(t)
	if code, out := fx.publish(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got := gitIn(t, fx.dir, "rev-parse", "HEAD^1"); got != fx.head {
		t.Errorf("the contribution parent changed: %s", got)
	}
	if got := gitIn(t, fx.dir, "rev-parse", "HEAD^2"); got != fx.base {
		t.Errorf("the base parent changed: %s", got)
	}
	if got, want := fx.remoteHead(t), gitIn(t, fx.dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("the fork was not updated: %s, want %s", got, want)
	}
	published := gitIn(t, fx.remote, "show", "contribution:"+bundlePath)
	if published != generatedBundle {
		t.Errorf("the published bundle differs: %q", published)
	}
	if _, err := os.Stat(fx.marker); !os.IsNotExist(err) {
		t.Error("a contributor hook ran during publication")
	}
	author := gitIn(t, fx.dir, "log", "-1", "--format=%an <%ae>%n%cn <%ce>%n%s")
	want := "test-bot <123+test-bot@users.noreply.github.com>\ntest-bot <123+test-bot@users.noreply.github.com>\n" +
		"chore: merge main and regenerate otelc-bundle.tgz"
	if author != want {
		t.Errorf("commit = %q, want %q", author, want)
	}

	// An unchanged bundle does not create another commit.
	head := gitIn(t, fx.dir, "rev-parse", "HEAD")
	fx.env["EXPECTED_HEAD_SHA"] = head
	fx.pull["head"] = map[string]any{
		"sha":  head,
		"ref":  "contribution",
		"repo": map[string]any{"full_name": "test/fork"},
	}
	fx.setPull(nil)
	code, out := fx.publish()
	if code != 0 {
		t.Fatalf("second publication: exit code = %d: %s", code, out.stderr)
	}
	if got := gitIn(t, fx.dir, "rev-parse", "HEAD"); got != head {
		t.Error("a duplicate request created a redundant commit")
	}
	if !strings.Contains(out.stdout, "The bundle is already up to date.") {
		t.Errorf("stdout = %q", out.stdout)
	}
}

// Shell group: test_empty_merge (preserve the merge commit when the index
// equals the head).
func TestPublishKeepsMergeWhenIndexEqualsHead(t *testing.T) {
	fx := newPubFixture(t)
	gitIn(t, fx.dir, "merge", "--abort")
	write(t, filepath.Join(fx.dir, "base.txt"), "base change\n")
	gitIn(t, fx.dir, "add", "base.txt")
	gitIn(t, fx.dir, "commit", "--quiet", "-m", "include base change")
	head := gitIn(t, fx.dir, "rev-parse", "HEAD")
	gitIn(t, fx.remote, "fetch", "--no-prune", "--quiet", fx.dir, "refs/heads/contribution:refs/heads/contribution")
	fx.env["EXPECTED_HEAD_SHA"] = head
	fx.pull["head"] = map[string]any{
		"sha":  head,
		"ref":  "contribution",
		"repo": map[string]any{"full_name": "test/fork"},
	}
	fx.setPull(nil)
	current, err := os.ReadFile(filepath.Join(fx.dir, bundlePath))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(fx.generated, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = mergeBase(t.Context(), fx.dir, fx.base, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err = exec0(t, "diff", "--cached", "--quiet"); err != nil {
		t.Fatal("the empty-merge fixture has a staged diff")
	}
	if code, out := fx.publish(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got := gitIn(t, fx.dir, "rev-parse", "HEAD^2"); got != fx.base {
		t.Errorf("an empty staged diff lost the required merge: %s", got)
	}
}

// exec0 runs git and returns its error, so a test can tell exit codes apart.
func exec0(t *testing.T, args ...string) error {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	return cmd.Run()
}

// Shell group: test_repository_and_direct_access.
func TestPublishWithRepositoryAndDirectAccess(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		mutate func(map[string]any)
		fork   string
	}{
		{
			name: "repository",
			env:  map[string]string{"HEAD_REPO_FULL": testRepo},
			mutate: func(p map[string]any) {
				p["head"] = map[string]any{
					"sha":  nil,
					"ref":  "contribution",
					"repo": map[string]any{"full_name": testRepo},
				}
				p["maintainer_can_modify"] = false
			},
		},
		{
			name:   "direct",
			mutate: func(p map[string]any) { p["maintainer_can_modify"] = false },
			fork:   `{"full_name":"test/fork","permissions":{"push":true}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			maps.Copy(fx.env, tt.env)
			fx.setPull(func(p map[string]any) {
				tt.mutate(p)
				if head, ok := p["head"].(map[string]any); ok && head["sha"] == nil {
					head["sha"] = fx.head
				}
			})
			if tt.fork != "" {
				fx.api.setBody(forkRoute, tt.fork)
			}
			if code, out := fx.publish(); code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			if got, want := fx.remoteHead(t), gitIn(t, fx.dir, "rev-parse", "HEAD"); got != want {
				t.Errorf("publication rejected %s access", tt.name)
			}
			if tt.fork == "" {
				for _, request := range fx.api.requests() {
					if request == forkRoute {
						t.Error("a same-repository branch queried fork permissions")
					}
				}
			}
		})
	}
}

// Shell group: test_publication_rejections.
func TestPublishRejections(t *testing.T) {
	zero := map[string]any{"sha": zeroSHA, "ref": "contribution", "repo": map[string]any{"full_name": "test/fork"}}
	tests := []struct {
		name  string
		setup func(t *testing.T, fx *pubFixture)
	}{
		{"closed", func(_ *testing.T, fx *pubFixture) { fx.setPull(func(p map[string]any) { p["state"] = "closed" }) }},
		{"head", func(_ *testing.T, fx *pubFixture) { fx.setPull(func(p map[string]any) { p["head"] = zero }) }},
		{"base", func(_ *testing.T, fx *pubFixture) { fx.api.setBody(baseRefRoute, baseRefBody(zeroSHA)) }},
		{"repository", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{
					"sha":  fx.head,
					"ref":  "contribution",
					"repo": map[string]any{"full_name": "other/fork"},
				}
			})
		}},
		{"edits", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(func(p map[string]any) { p["maintainer_can_modify"] = false })
			fx.api.setBody(forkRoute, `{"full_name":"test/fork","permissions":{"push":false}}`)
		}},
		{"edits and no permissions field", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(func(p map[string]any) { p["maintainer_can_modify"] = false })
			fx.api.setBody(forkRoute, `{"full_name":"test/fork"}`)
		}},
		{"branch moved", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(func(p map[string]any) {
				p["head"] = map[string]any{
					"sha":  fx.head,
					"ref":  "other",
					"repo": map[string]any{"full_name": "test/fork"},
				}
			})
		}},
		{"base branch renamed", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(func(p map[string]any) { p["base"] = map[string]any{"sha": fx.base, "ref": "release"} })
		}},
		{"deleted fork", func(_ *testing.T, fx *pubFixture) {
			fx.setPull(
				func(p map[string]any) { p["head"] = map[string]any{"sha": fx.head, "ref": "contribution", "repo": nil} },
			)
		}},
		{"base reference is not a commit", func(_ *testing.T, fx *pubFixture) {
			fx.api.setBody(baseRefRoute, `{"object":{"type":"tag","sha":"`+fx.base+`"}}`)
		}},
		{"symlink", func(t *testing.T, fx *pubFixture) {
			target := fx.generated + ".target"
			if err := os.Rename(fx.generated, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, fx.generated); err != nil {
				t.Fatal(err)
			}
		}},
		{"advanced", func(t *testing.T, fx *pubFixture) {
			gitIn(t, fx.remote, "update-ref", "refs/heads/contribution", fx.base)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			tt.setup(t, fx)
			before := fx.remoteHead(t)
			code, out := fx.publish()
			if code != exitFailure {
				t.Fatalf("publication accepted %s: exit code %d, stdout %q", tt.name, code, out.stdout)
			}
			if got := fx.remoteHead(t); got != before {
				t.Errorf("publication overwrote %s", tt.name)
			}
		})
	}
}

// Go test TestAutomaticPublicationEligibility.
func TestPublishAutomaticEligibility(t *testing.T) {
	zero := map[string]any{"sha": zeroSHA, "ref": "contribution", "repo": map[string]any{"full_name": "test/fork"}}
	tests := []struct {
		name    string
		mutate  func(fx *pubFixture, pull map[string]any)
		baseRef string
		allowed bool
	}{
		{name: "valid", allowed: true},
		{name: "older base snapshot", allowed: true, mutate: func(_ *pubFixture, p map[string]any) {
			p["base"] = map[string]any{"sha": oldSHA, "ref": "main"}
		}},
		{name: "draft", mutate: func(_ *pubFixture, p map[string]any) { p["draft"] = true }},
		{name: "missing draft", mutate: func(_ *pubFixture, p map[string]any) { delete(p, "draft") }},
		{name: "head", mutate: func(_ *pubFixture, p map[string]any) { p["head"] = zero }},
		{name: "base", baseRef: baseRefBody(zeroSHA)},
		{name: "missing base reference", baseRef: `{}`},
		{name: "retargeted", mutate: func(fx *pubFixture, p map[string]any) {
			p["base"] = map[string]any{"sha": fx.base, "ref": "release"}
		}},
		{name: "edits disabled", mutate: func(_ *pubFixture, p map[string]any) { p["maintainer_can_modify"] = false }},
		{name: "closed", mutate: func(_ *pubFixture, p map[string]any) { p["state"] = "closed" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			fx.automatic()
			fx.api.setBody(forkRoute, `{"full_name":"test/fork","permissions":{"push":false}}`)
			if tt.mutate != nil {
				fx.setPull(func(p map[string]any) { tt.mutate(fx, p) })
			}
			if tt.baseRef != "" {
				fx.api.setBody(baseRefRoute, tt.baseRef)
			}
			before := fx.remoteHead(t)
			code, out := fx.publish()
			if tt.allowed {
				if code != 0 || fx.remoteHead(t) == before {
					t.Errorf("valid automatic publication did not update the fork: exit %d: %s", code, out.stderr)
				}
				return
			}
			if code != exitFailure {
				t.Errorf("publication accepted an ineligible automatic request: exit code %d", code)
			}
			if fx.remoteHead(t) != before {
				t.Error("rejected publication changed the fork")
			}
		})
	}
}

func TestPublishAutomaticRequiresFreshScan(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"rerun", map[string]string{"RUN_ATTEMPT": "2"}},
		{"expired deadline", map[string]string{"AUTO_DEADLINE": deadline(-5)}},
		{"missing deadline", map[string]string{"AUTO_DEADLINE": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			fx.automatic()
			maps.Copy(fx.env, tt.env)
			before := fx.remoteHead(t)
			if code, _ := fx.publish(); code != exitFailure {
				t.Errorf("exit code = %d", code)
			}
			if fx.remoteHead(t) != before {
				t.Error("the fork changed")
			}
		})
	}
}

func TestPublishManualDoesNotAuthorizeInProcess(t *testing.T) {
	fx := newPubFixture(t)
	fx.env["CALLER_WORKFLOW_REF"] = regenerateRef
	fx.env["COMMENTER"] = "maintainer"
	if code, out := fx.publish(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	for _, request := range fx.api.requests() {
		if strings.Contains(request, "collaborators") {
			t.Errorf("a manual publication checked collaborators: %s", request)
		}
	}
}

// Go test TestManualPublicationUsesLiveBase.
func TestPublishManualUsesLiveBase(t *testing.T) {
	fx := newPubFixture(t)
	prior := gitIn(t, fx.dir, "merge-base", fx.head, fx.base)
	fx.setPull(func(p map[string]any) { p["base"] = map[string]any{"sha": prior, "ref": "main"} })
	if code, out := fx.publish(); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got, want := fx.remoteHead(t), gitIn(t, fx.dir, "rev-parse", "HEAD"); got != want {
		t.Error("manual publication did not update the fork")
	}
}

// Go test TestArchiveConversionAttributes (the publish half): contributor
// attributes must not convert the generated archive.
func TestPublishPreservesArchiveBytes(t *testing.T) {
	var archive bytes.Buffer
	writer, err := gzip.NewWriterLevel(&archive, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("bundle\r\n$Id: 0123456789012345678901234567890123456789 $\r\n")
	if _, err = writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"text", "ident", "text eol=crlf", "working-tree-encoding=UTF-16"} {
		t.Run(attr, func(t *testing.T) {
			fx := newPubFixture(t)
			write(t, filepath.Join(fx.dir, ".gitattributes"), bundlePath+" "+attr+"\n")
			if writeErr := os.WriteFile(fx.generated, archive.Bytes(), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}
			if code, out := fx.publish(); code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			published := gitIn(t, fx.remote, "show", "contribution:"+bundlePath)
			if published != archive.String() {
				t.Error("Git changed the generated archive bytes")
			}
			reader, readerErr := gzip.NewReader(strings.NewReader(published))
			if readerErr != nil {
				t.Fatal(readerErr)
			}
			decoded, readErr := io.ReadAll(reader)
			if readErr != nil || !bytes.Equal(decoded, payload) {
				t.Fatalf("published gzip is invalid: %v", readErr)
			}
		})
	}
}

func TestPublishRejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, fx *pubFixture)
	}{
		{"missing archive", func(t *testing.T, fx *pubFixture) { removeFile(t, fx.generated) }},
		{"archive is a directory", func(t *testing.T, fx *pubFixture) {
			removeFile(t, fx.generated)
			if err := os.Mkdir(fx.generated, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"tool is a link", func(t *testing.T, fx *pubFixture) {
			elsewhere := t.TempDir()
			gitIn(t, fx.dir, "reset", "--quiet", "--hard", "HEAD")
			symlink(t, elsewhere, filepath.Join(fx.dir, "tool-link"))
			gitIn(t, fx.dir, "mv", "tool", "tool-real")
			symlink(t, filepath.Join(fx.dir, "tool-real"), filepath.Join(fx.dir, "tool"))
		}},
		{"data is a link", func(t *testing.T, fx *pubFixture) {
			elsewhere := t.TempDir()
			removeFile(t, filepath.Join(fx.dir, bundlePath))
			removeFile(t, filepath.Join(fx.dir, "tool", "data"))
			symlink(t, elsewhere, filepath.Join(fx.dir, "tool", "data"))
		}},
		{"bundle is a link", func(t *testing.T, fx *pubFixture) {
			target := filepath.Join(t.TempDir(), "target")
			write(t, target, "target")
			removeFile(t, filepath.Join(fx.dir, bundlePath))
			symlink(t, target, filepath.Join(fx.dir, bundlePath))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			tt.setup(t, fx)
			before := fx.remoteHead(t)
			if code, _ := fx.publish(); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			if fx.remoteHead(t) != before {
				t.Error("the fork changed")
			}
		})
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		setup func(t *testing.T, fx *pubFixture)
	}{
		{name: "missing token", env: map[string]string{"GH_TOKEN": ""}},
		{name: "missing repository", env: map[string]string{"REPO": ""}},
		{name: "missing pull request", env: map[string]string{"PR_NUMBER": ""}},
		{name: "missing head repository", env: map[string]string{"HEAD_REPO_FULL": ""}},
		{name: "missing head ref", env: map[string]string{"HEAD_REF": ""}},
		{name: "missing head sha", env: map[string]string{"EXPECTED_HEAD_SHA": ""}},
		{name: "missing base ref", env: map[string]string{"BASE_REF": ""}},
		{name: "missing base sha", env: map[string]string{"BASE_SHA": ""}},
		{name: "missing bot login", env: map[string]string{"BOT_LOGIN": ""}},
		{name: "missing bot id", env: map[string]string{"BOT_ID": ""}},
		{name: "missing bundle", env: map[string]string{"GENERATED_BUNDLE": ""}},
		{name: "bad repository", env: map[string]string{"REPO": "a/b/c"}},
		{name: "bad head repository", env: map[string]string{"HEAD_REPO_FULL": "fork"}},
		{name: "zero pull request", env: map[string]string{"PR_NUMBER": "0"}},
		{name: "text bot id", env: map[string]string{"BOT_ID": "abc"}},
		{name: "bad bot login", env: map[string]string{"BOT_LOGIN": "bot\nname"}},
		{name: "short base sha", env: map[string]string{"BASE_SHA": "abc"}},
		{name: "short head sha", env: map[string]string{"EXPECTED_HEAD_SHA": "abc"}},
		{name: "bad head ref", env: map[string]string{"HEAD_REF": "a..b"}},
		{name: "bad base ref", env: map[string]string{"BASE_REF": "x..y"}},
		{name: "head moved", env: map[string]string{"EXPECTED_HEAD_SHA": zeroSHA}},
		{name: "merge of another commit", env: map[string]string{"BASE_SHA": oldSHA}},
		{name: "no merge in progress and base not contained", setup: func(t *testing.T, fx *pubFixture) {
			gitIn(t, fx.dir, "merge", "--abort")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPubFixture(t)
			maps.Copy(fx.env, tt.env)
			if tt.setup != nil {
				tt.setup(t, fx)
			}
			before := fx.remoteHead(t)
			if code, _ := fx.publish(); code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			if fx.remoteHead(t) != before {
				t.Error("the fork changed")
			}
		})
	}
}

func TestScrubRemovesSecrets(t *testing.T) {
	got := scrub("fatal: token abc and b64 YWJj leaked, abc", "abc", "YWJj", "")
	if strings.Contains(got, "abc") || strings.Contains(got, "YWJj") {
		t.Errorf("scrub = %q", got)
	}
}

// The workflow starts the tool in the workspace root and names the checkout,
// so no contributor directory is ever the working directory of a step.
func TestPublishInNamedCheckout(t *testing.T) {
	fx := newPubFixture(t)
	t.Chdir(t.TempDir())
	// A relative archive path is resolved against the starting directory.
	fx.env["GENERATED_BUNDLE"] = relativeTo(t, fx.generated)
	code, out := runApp(t, fx.env, "publish", fx.dir)
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got, want := fx.remoteHead(t), gitIn(t, fx.dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("the fork was not updated: %s, want %s", got, want)
	}
}

func relativeTo(t *testing.T, path string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestPublishRejectsBadCheckoutArguments(t *testing.T) {
	fx := newPubFixture(t)
	before := fx.remoteHead(t)
	for name, args := range map[string][]string{
		"two arguments":     {fx.dir, fx.dir},
		"empty argument":    {""},
		"missing directory": {filepath.Join(t.TempDir(), "missing")},
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := runApp(t, fx.env, append([]string{"publish"}, args...)...)
			if code != exitFailure {
				t.Errorf("exit code = %d, want %d", code, exitFailure)
			}
			if fx.remoteHead(t) != before {
				t.Error("the fork changed")
			}
		})
	}
}
