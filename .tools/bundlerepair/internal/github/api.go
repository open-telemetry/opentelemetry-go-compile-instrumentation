// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
)

// Permission is the collaborator permission of a user.
type Permission struct {
	// RoleName is the repository role. GitHub reports the maintain role with
	// the legacy permission "write", so the role name is the reliable field.
	RoleName string `json:"role_name"`
}

// Repository is the part of a repository that the repair checks read.
type Repository struct {
	FullName    string `json:"full_name"`
	Permissions struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

// User is an account. The fields keep the raw JSON, so the caller decides how
// to print a missing or unusual value.
type User struct {
	Login json.RawMessage `json:"login"`
	ID    json.RawMessage `json:"id"`
}

// Text prints a JSON value as "jq -r" does: a string without quotes, a number
// as written, and a missing or null value as "null". Other types fail.
func Text(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "null", nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errs.Wrap(err, "decode value")
	}
	switch typed := value.(type) {
	case nil:
		return "null", nil
	case string:
		return typed, nil
	case float64:
		return string(raw), nil
	default:
		return "", errors.New("value is not a string or a number")
	}
}

// WorkflowRun is the part of a workflow run that the scanner reads.
type WorkflowRun struct {
	RunAttempt int    `json:"run_attempt"`
	CreatedAt  string `json:"created_at"`
}

// RunsPage is one page of a workflow run list. TotalCount is nil when the
// response has no total_count, and WorkflowRuns is nil when it has no list.
type RunsPage struct {
	TotalCount   *int64 `json:"total_count"`
	WorkflowRuns *[]struct {
		ID int64 `json:"id"`
	} `json:"workflow_runs"`
}

// Step is a job step. Name is nil when the response has no name.
type Step struct {
	Name       *string `json:"name"`
	Conclusion string  `json:"conclusion"`
}

// Job is a workflow job.
type Job struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Steps      []Step `json:"steps"`
}

// JobsPage is one page of a job list. TotalCount is nil when the response has
// no total_count, and Jobs is nil when it has no list.
type JobsPage struct {
	TotalCount *int64 `json:"total_count"`
	Jobs       *[]Job `json:"jobs"`
}

// OpenPullRequest is an item of the GraphQL open pull request list.
type OpenPullRequest struct {
	Number      int    `json:"number"`
	IsDraft     bool   `json:"isDraft"`
	Mergeable   string `json:"mergeable"`
	BaseRefName string `json:"baseRefName"`
}

// PullRef is one side of a pull request.
type PullRef struct {
	Ref  string      `json:"ref"`
	SHA  string      `json:"sha"`
	Repo *Repository `json:"repo"`
}

// PullRequest is the state of a pull request that the repair checks read.
type PullRequest struct {
	State string `json:"state"`
	// Draft is nil when the response has no draft field.
	Draft               *bool   `json:"draft"`
	MaintainerCanModify bool    `json:"maintainer_can_modify"`
	Head                PullRef `json:"head"`
	Base                PullRef `json:"base"`
}

// GitObject is the object a git ref points at.
type GitObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Ref is a git reference.
type Ref struct {
	Object GitObject `json:"object"`
}

// CollaboratorPermission returns the permission of user on repo ("owner/name").
func (c *Client) CollaboratorPermission(ctx context.Context, repo, user string) (Permission, error) {
	var permission Permission
	path, err := repoPath(repo, "collaborators", user, "permission")
	if err != nil {
		return permission, err
	}
	err = c.Get(ctx, path, nil, &permission)
	return permission, err
}

// PullRequest returns pull request number of repo ("owner/name").
func (c *Client) PullRequest(ctx context.Context, repo string, number int) (PullRequest, error) {
	var pull PullRequest
	path, err := repoPath(repo, "pulls", strconv.Itoa(number))
	if err != nil {
		return pull, err
	}
	err = c.Get(ctx, path, nil, &pull)
	return pull, err
}

// Ref returns the git ref named ref, such as "heads/main", of repo.
func (c *Client) Ref(ctx context.Context, repo, ref string) (Ref, error) {
	var out Ref
	path, err := repoPath(repo, append([]string{"git", "ref"}, strings.Split(ref, "/")...)...)
	if err != nil {
		return out, err
	}
	err = c.Get(ctx, path, nil, &out)
	return out, err
}

// repoPath builds "repos/owner/name/elem...". The URL encoder escapes each
// element later, so an element must not hold a slash and must not climb out of
// the path with "." or "..".
func repoPath(repo string, elems ...string) (string, error) {
	owner, name, found := strings.Cut(repo, "/")
	if !found {
		return "", errors.New("invalid repository " + strconv.Quote(repo))
	}
	segments := append([]string{"repos", owner, name}, elems...)
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "/") {
			return "", errors.New("invalid request path element " + strconv.Quote(segment))
		}
	}
	return strings.Join(segments, "/"), nil
}

// Repository returns repo ("owner/name") with the permissions of the token.
func (c *Client) Repository(ctx context.Context, repo string) (Repository, error) {
	var out Repository
	path, err := repoPath(repo)
	if err != nil {
		return out, err
	}
	err = c.Get(ctx, path, nil, &out)
	return out, err
}

// AuthenticatedUser returns the account of the token.
func (c *Client) AuthenticatedUser(ctx context.Context) (User, error) {
	var out User
	err := c.Get(ctx, "user", nil, &out)
	return out, err
}

// User returns the account login, such as "my-app[bot]".
func (c *Client) User(ctx context.Context, login string) (User, error) {
	var out User
	if login == "" || strings.Contains(login, "/") {
		return out, errors.New("invalid user " + strconv.Quote(login))
	}
	err := c.Get(ctx, "users/"+login, nil, &out)
	return out, err
}

// WorkflowRun returns workflow run id of repo.
func (c *Client) WorkflowRun(ctx context.Context, repo string, id int64) (WorkflowRun, error) {
	var out WorkflowRun
	path, err := repoPath(repo, "actions", "runs", strconv.FormatInt(id, 10))
	if err != nil {
		return out, err
	}
	err = c.Get(ctx, path, nil, &out)
	return out, err
}

// WorkflowRunPages returns every page of the runs of a workflow file.
func (c *Client) WorkflowRunPages(
	ctx context.Context, repo, workflow string, query url.Values,
) ([]RunsPage, error) {
	path, err := repoPath(repo, "actions", "workflows", workflow, "runs")
	if err != nil {
		return nil, err
	}
	var pages []RunsPage
	err = c.Paginate(ctx, path, query, func(body []byte) error {
		var page RunsPage
		if decodeErr := json.Unmarshal(body, &page); decodeErr != nil {
			return decodeErr
		}
		pages = append(pages, page)
		return nil
	})
	return pages, err
}

// JobPages returns every page of the jobs of the first attempt of a run.
func (c *Client) JobPages(ctx context.Context, repo string, runID int64) ([]JobsPage, error) {
	path, err := repoPath(repo, "actions", "runs", strconv.FormatInt(runID, 10), "attempts", "1", "jobs")
	if err != nil {
		return nil, err
	}
	var pages []JobsPage
	err = c.Paginate(ctx, path, url.Values{"per_page": {"100"}}, func(body []byte) error {
		var page JobsPage
		if decodeErr := json.Unmarshal(body, &page); decodeErr != nil {
			return decodeErr
		}
		pages = append(pages, page)
		return nil
	})
	return pages, err
}

const openPullRequestsQuery = `query($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(states: OPEN, first: 100, after: $after, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes { number isDraft mergeable baseRefName }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

// OpenPullRequests returns up to limit open pull requests, newest first. The
// mergeable field is "CONFLICTING" when the pull request has merge conflicts.
func (c *Client) OpenPullRequests(ctx context.Context, repo string, limit int) ([]OpenPullRequest, error) {
	owner, name, found := strings.Cut(repo, "/")
	if !found || owner == "" || name == "" {
		return nil, errors.New("invalid repository " + strconv.Quote(repo))
	}
	var all []OpenPullRequest
	cursor := (*string)(nil)
	for range maxPages {
		var data struct {
			Repository struct {
				PullRequests struct {
					Nodes    []OpenPullRequest `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": owner, "name": name, "after": cursor}
		if err := c.GraphQL(ctx, openPullRequestsQuery, vars, &data); err != nil {
			return nil, err
		}
		list := data.Repository.PullRequests
		all = append(all, list.Nodes...)
		if len(all) >= limit {
			return all[:limit], nil
		}
		if !list.PageInfo.HasNextPage {
			return all, nil
		}
		end := list.PageInfo.EndCursor
		cursor = &end
	}
	return nil, errors.New("POST graphql: more than " + strconv.Itoa(maxPages) + " pages")
}
