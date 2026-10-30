// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/ghaction"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const bundleFile = "tool/data/otelc-bundle.tgz"

var (
	botIDPattern    = regexp.MustCompile(`^[0-9]+$`)
	botLoginPattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\[bot\])?$`)
	slugPattern     = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

// publication holds the validated environment input of the publish command.
type publication struct {
	repo          string
	number        int
	headRepo      string
	headRef       string
	expectedHead  string
	baseRef       string
	baseSHA       string
	botLogin      string
	botID         string
	generated     string
	token         string
	authorizeAuto bool
}

func (a app) publicationFromEnv() (publication, error) {
	required := []string{
		"GH_TOKEN", "REPO", "PR_NUMBER", "HEAD_REPO_FULL", "HEAD_REF", "EXPECTED_HEAD_SHA",
		"BASE_REF", "BASE_SHA", "BOT_LOGIN", "BOT_ID", "GENERATED_BUNDLE",
	}
	for _, name := range required {
		if a.getenv(name) == "" {
			return publication{}, errors.New(name + " must be set")
		}
	}
	p := publication{
		repo:         a.getenv("REPO"),
		headRepo:     a.getenv("HEAD_REPO_FULL"),
		headRef:      a.getenv("HEAD_REF"),
		expectedHead: a.getenv("EXPECTED_HEAD_SHA"),
		baseRef:      a.getenv("BASE_REF"),
		baseSHA:      a.getenv("BASE_SHA"),
		botLogin:     a.getenv("BOT_LOGIN"),
		botID:        a.getenv("BOT_ID"),
		generated:    a.getenv("GENERATED_BUNDLE"),
		token:        a.getenv("GH_TOKEN"),
	}
	number := a.getenv("PR_NUMBER")
	valid := repoPattern.MatchString(p.repo) && repoPattern.MatchString(p.headRepo) &&
		numberPattern.MatchString(number) && botIDPattern.MatchString(p.botID) &&
		botLoginPattern.MatchString(p.botLogin) &&
		sha1Pattern.MatchString(p.baseSHA) && sha1Pattern.MatchString(p.expectedHead)
	if !valid {
		return publication{}, errors.New("invalid publication metadata")
	}
	var err error
	if p.number, err = strconv.Atoi(number); err != nil {
		return publication{}, errors.New("invalid publication metadata")
	}
	p.authorizeAuto = a.getenv("CALLER_WORKFLOW_REF") == p.repo+autofixCaller
	return p, nil
}

// publish commits the regenerated bundle on top of the merge in the checkout
// and pushes it to the pull request branch. The checkout is the optional
// argument, or the current directory. The workflow names the checkout, so a
// step never runs with the contributor checkout as its working directory. It
// reads REPO,
// PR_NUMBER, HEAD_REPO_FULL, HEAD_REF, EXPECTED_HEAD_SHA, BASE_REF, BASE_SHA,
// BOT_LOGIN, BOT_ID, GENERATED_BUNDLE, GH_TOKEN and GITHUB_API_URL. For an
// automatic request it also reads the variables of the authorize command.
func (a app) publish(ctx context.Context, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] == "") {
		return errors.New("publish takes at most one argument: the checkout directory")
	}
	p, err := a.publicationFromEnv()
	if err != nil {
		return err
	}
	if len(args) == 1 {
		if err = enterCheckout(&p, args[0]); err != nil {
			return err
		}
	}
	client, err := github.New(a.getenv("GITHUB_API_URL"), p.token)
	if err != nil {
		return err
	}
	git := gitcmd.Runner{Config: []string{"commit.gpgsign=false"}}
	for _, ref := range []string{p.headRef, p.baseRef} {
		if err = git.Run(ctx, "check-ref-format", "refs/heads/"+ref); err != nil {
			return err
		}
	}
	head, err := revParse(ctx, git, "HEAD")
	if err != nil {
		return err
	}
	if head != p.expectedHead {
		return errors.New("HEAD is not the expected pull request head")
	}
	mergeHead, mergeErr := revParse(ctx, git, "-q", "--verify", "MERGE_HEAD")
	if mergeErr != nil {
		mergeHead = ""
	}
	if mergeHead != "" && mergeHead != p.baseSHA {
		return errors.New("the merge in progress does not merge the captured base commit")
	}
	if mergeHead == "" {
		if err = git.Run(ctx, "merge-base", "--is-ancestor", p.baseSHA, "HEAD"); err != nil {
			return errors.New("HEAD does not contain the captured base commit")
		}
	}
	if err = a.checkPullRequest(ctx, client, p); err != nil {
		return err
	}
	if err = copyBundle(p.generated); err != nil {
		return err
	}
	if err = git.Run(ctx, "add", "--", bundleFile); err != nil {
		return err
	}
	if mergeHead == "" && git.Run(ctx, "diff", "--cached", "--quiet") == nil {
		return a.sayLine("The bundle is already up to date.")
	}
	git.Config = append(git.Config,
		"user.name="+p.botLogin, "user.email="+p.botID+"+"+p.botLogin+"@users.noreply.github.com")
	if err = git.Run(ctx, "commit", "-m", "chore: merge "+p.baseRef+" and regenerate otelc-bundle.tgz"); err != nil {
		return err
	}
	if err = git.Run(ctx, "merge-base", "--is-ancestor", p.expectedHead, "HEAD"); err != nil {
		return errors.New("the new commit does not descend from the expected head")
	}
	if err = a.checkPullRequest(ctx, client, p); err != nil {
		return err
	}
	return a.push(ctx, p)
}

// enterCheckout makes dir the working directory. It resolves the archive path
// first, because a relative path is relative to the starting directory.
func enterCheckout(p *publication, dir string) error {
	generated, err := filepath.Abs(p.generated)
	if err != nil {
		return errs.Wrap(err, "resolve the generated bundle path")
	}
	p.generated = generated
	return errs.Wrap(os.Chdir(dir), "enter the checkout")
}

func (a app) sayLine(text string) error {
	_, err := a.stdout.Write([]byte(text + "\n"))
	return err
}

func revParse(ctx context.Context, git gitcmd.Runner, args ...string) (string, error) {
	out, err := git.Output(ctx, append([]string{"rev-parse"}, args...)...)
	return strings.TrimSpace(out), err
}

// push sends HEAD to the pull request branch. The lease rejects concurrent
// updates and the earlier ancestor check forbids history rewrites.
func (a app) push(ctx context.Context, p publication) error {
	authorization := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + p.token))
	git := gitcmd.Runner{
		Env: []string{
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + authorization,
			"GIT_CONFIG_KEY_1=core.hooksPath",
			"GIT_CONFIG_VALUE_1=" + gitcmd.NoHooksPath,
		},
	}
	err := git.Run(ctx, "push", "--force-with-lease=refs/heads/"+p.headRef+":"+p.expectedHead,
		"origin", "HEAD:refs/heads/"+p.headRef)
	if err != nil {
		return errors.New(scrub(err.Error(), p.token, authorization))
	}
	return a.sayLine("Pushed the regenerated bundle to " + p.headRepo + ":" + p.headRef + ".")
}

// scrub removes secrets from text.
func scrub(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

// checkPullRequest checks that the pull request still matches the captured
// request and that the token may push to its branch.
func (a app) checkPullRequest(ctx context.Context, client *github.Client, p publication) error {
	if p.authorizeAuto {
		if err := authorize(ctx, a.authorizeRequest(), a.now()); err != nil {
			return err
		}
	}
	pull, err := client.PullRequest(ctx, p.repo, p.number)
	if err != nil {
		return err
	}
	ref, err := client.Ref(ctx, p.repo, "heads/"+p.baseRef)
	if err != nil {
		return err
	}
	unchanged := pull.State == stateOpen && pull.Head.SHA == p.expectedHead && pull.Head.Ref == p.headRef &&
		pull.Base.Ref == p.baseRef && pull.Head.Repo != nil && pull.Head.Repo.FullName == p.headRepo &&
		ref.Object.Type == objectCommit && ref.Object.SHA == p.baseSHA
	if !unchanged {
		return errors.New("the pull request closed or changed. No update will be pushed")
	}
	if p.headRepo != p.repo && !pull.MaintainerCanModify {
		fork, forkErr := client.Repository(ctx, p.headRepo)
		if forkErr != nil {
			return forkErr
		}
		if !fork.Permissions.Push {
			return errors.New("the fork author must permit maintainer edits before publication")
		}
	}
	return nil
}

// copyBundle replaces the bundle with the generated archive. Both paths must be
// regular files in real directories. It never follows a link.
func copyBundle(generated string) error {
	errNotRegular := errors.New("the bundle and its destination must be regular paths, not symbolic links")
	info, err := os.Lstat(generated)
	if err != nil || !info.Mode().IsRegular() {
		return errNotRegular
	}
	for _, dir := range []string{"tool", filepath.Join("tool", "data")} {
		dirInfo, dirErr := os.Lstat(dir)
		if dirErr != nil || !dirInfo.IsDir() {
			return errNotRegular
		}
	}
	if dest, destErr := os.Lstat(bundleFile); destErr == nil && dest.Mode()&fs.ModeSymlink != 0 {
		return errNotRegular
	}
	data, err := os.ReadFile(generated)
	if err != nil {
		return errs.Wrap(err, "read the generated bundle")
	}
	// The root refuses any path that leaves the current directory.
	root, err := os.OpenRoot(".")
	if err != nil {
		return errs.Wrap(err, "open the checkout")
	}
	defer root.Close()
	return errs.Wrap(root.WriteFile(bundleFile, data, info.Mode().Perm()), "write the bundle")
}

// identity checks the publisher token and writes the account that owns it. It
// reads GH_TOKEN and APP_SLUG, which must be set even if empty, and GITHUB_API_URL, GITHUB_OUTPUT and GITHUB_STEP_SUMMARY.
func (a app) identity(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("identity takes no arguments")
	}
	files := ghaction.FromEnv(a.getenv)
	token, tokenSet := a.lookup("GH_TOKEN")
	if !tokenSet {
		return errors.New("GH_TOKEN must be set, even if empty")
	}
	if token == "" {
		return a.reportMissingToken(files)
	}
	slug, slugSet := a.lookup("APP_SLUG")
	if !slugSet {
		return errors.New("APP_SLUG must be set, even if empty")
	}
	client, err := github.New(a.getenv("GITHUB_API_URL"), token)
	if err != nil {
		return err
	}
	var user github.User
	if slug != "" {
		if !slugPattern.MatchString(slug) {
			return errors.New("APP_SLUG is not valid")
		}
		user, err = client.User(ctx, slug+"[bot]")
	} else {
		user, err = client.AuthenticatedUser(ctx)
	}
	if err != nil {
		return err
	}
	login, err := github.Text(user.Login)
	if err != nil {
		return errs.Wrap(err, "login")
	}
	id, err := github.Text(user.ID)
	if err != nil {
		return errs.Wrap(err, "id")
	}
	return writeOutputs(files, [][2]string{{"login", login}, {"id", id}})
}

func (a app) reportMissingToken(files ghaction.Files) error {
	for _, line := range []string{
		"::error::Missing publisher token in bundle-publisher.",
		"Configure OTELC_BUNDLE_PUSH_TOKEN as an environment secret.",
		"The App remains supported for repository branches.",
	} {
		if err := a.sayLine(line); err != nil {
			return err
		}
	}
	for _, line := range []string{
		"Publishing requires a configured token.",
		"The generated artifact remains available for one day.",
	} {
		if err := files.AppendSummary(line); err != nil {
			return err
		}
	}
	return errors.New("missing publisher token")
}

// credentials chooses between the App token and the environment token. It
// reads CLIENT_ID, PRIVATE_KEY and IS_CROSS_REPOSITORY and never prints the key.
// A variable that the decision needs must be set, even if empty. The variables
// are read in this order and the decision stops at the first one that rules
// out the App: IS_CROSS_REPOSITORY, CLIENT_ID, PRIVATE_KEY.
func (a app) credentials(args []string) error {
	if len(args) != 0 {
		return errors.New("credentials takes no arguments")
	}
	useApp := true
	for _, rule := range []struct {
		name string
		ok   func(string) bool
	}{
		{"IS_CROSS_REPOSITORY", func(v string) bool { return v == "false" }},
		{"CLIENT_ID", func(v string) bool { return v != "" }},
		{"PRIVATE_KEY", func(v string) bool { return v != "" }},
	} {
		value, set := a.lookup(rule.name)
		if !set {
			return errors.New(rule.name + " must be set")
		}
		if !rule.ok(value) {
			useApp = false
			break
		}
	}
	return ghaction.FromEnv(a.getenv).SetOutput("use_app", strconv.FormatBool(useApp))
}
