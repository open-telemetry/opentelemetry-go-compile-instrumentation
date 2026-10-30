// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/github"
)

const testToken = "test-token-value"

// fake serves fixed routes and fails the test on any other request.
type fake struct {
	t      *testing.T
	routes map[string]http.HandlerFunc
	mu     sync.Mutex
	seen   []string
	server *httptest.Server
}

func newFake(t *testing.T, routes map[string]http.HandlerFunc) *fake {
	t.Helper()
	f := &fake{t: t, routes: routes}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.RequestURI()
	f.mu.Lock()
	f.seen = append(f.seen, key)
	f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		f.t.Errorf("%s: Authorization = %q", key, got)
	}
	handler, ok := f.routes[key]
	if !ok {
		f.t.Errorf("unexpected request %s", key)
		http.Error(w, "unexpected request", http.StatusTeapot)
		return
	}
	handler(w, r)
}

func (f *fake) client() *github.Client {
	f.t.Helper()
	client, err := github.New(f.server.URL, testToken)
	if err != nil {
		f.t.Fatal(err)
	}
	return client
}

func jsonBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

func TestTypedCalls(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/collaborators/alice/permission": jsonBody(
			`{"permission":"write","role_name":"maintain","extra":1}`,
		),
		"GET /repos/o/r/pulls/7": jsonBody(`{"state":"open","draft":false,"maintainer_can_modify":true,
			"head":{"sha":"aa","ref":"h","repo":{"full_name":"o/fork"}},"base":{"sha":"bb","ref":"main","repo":null}}`),
		"GET /repos/o/r/git/ref/heads/main": jsonBody(`{"object":{"type":"commit","sha":"cc"}}`),
	})
	client := f.client()
	ctx := t.Context()

	permission, err := client.CollaboratorPermission(ctx, "o/r", "alice")
	if err != nil || permission.RoleName != "maintain" {
		t.Errorf("permission = %+v, %v", permission, err)
	}
	pull, err := client.PullRequest(ctx, "o/r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pull.State != "open" || pull.Draft == nil || *pull.Draft || !pull.MaintainerCanModify ||
		pull.Head.SHA != "aa" || pull.Head.Repo == nil || pull.Head.Repo.FullName != "o/fork" ||
		pull.Base.Ref != "main" || pull.Base.Repo != nil {
		t.Errorf("pull = %+v", pull)
	}
	ref, err := client.Ref(ctx, "o/r", "heads/main")
	if err != nil || ref.Object.Type != "commit" || ref.Object.SHA != "cc" {
		t.Errorf("ref = %+v, %v", ref, err)
	}
}

func TestMissingDraftIsNil(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls/1": jsonBody(`{"state":"open"}`)})
	pull, err := f.client().PullRequest(t.Context(), "o/r", 1)
	if err != nil || pull.Draft != nil {
		t.Errorf("pull.Draft = %v, err = %v", pull.Draft, err)
	}
}

func TestStatusErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not found", http.StatusNotFound, `{"message":"Not Found"}`, "404 Not Found: Not Found"},
		{"forbidden without json", http.StatusForbidden, `<html>`, "403 Forbidden"},
		{"redirect", http.StatusMovedPermanently, ``, "301"},
		{"server error", http.StatusInternalServerError, `{"message":"boom"}`, "500 Internal Server Error: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, map[string]http.HandlerFunc{
				"GET /repos/o/r/pulls/1": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					fmt.Fprint(w, tt.body)
				},
			})
			_, err := f.client().PullRequest(t.Context(), "o/r", 1)
			var statusErr *github.StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v, want *StatusError", err)
			}
			if statusErr.StatusCode != tt.status || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want status %d and %q", err, tt.status, tt.want)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Error("the error text holds the token")
			}
		})
	}
}

func TestInvalidJSONAndTypes(t *testing.T) {
	for _, body := range []string{`not json`, `[]`, `{"state":5}`, ``} {
		f := newFake(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls/1": jsonBody(body)})
		if _, err := f.client().PullRequest(t.Context(), "o/r", 1); err == nil {
			t.Errorf("body %q was accepted", body)
		}
	}
}

func TestRequestErrorHidesToken(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()
	client, err := github.New(address, testToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PullRequest(t.Context(), "o/r", 1)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Error("the error text holds the token")
	}
}

func TestCanceledContext(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.client().PullRequest(ctx, "o/r", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestMissingToken(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{})
	client, err := github.New(f.server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.PullRequest(t.Context(), "o/r", 1); err == nil {
		t.Error("a request without a token was sent")
	}
}

func TestInvalidPathElements(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{})
	client := f.client()
	for _, repo := range []string{"", "o", "o/", "/r", "../r", "o/..", "./.", "o/r/x"} {
		if _, err := client.PullRequest(t.Context(), repo, 1); err == nil {
			t.Errorf("repository %q was accepted", repo)
		}
	}
	for _, user := range []string{"", "a/b", "..", "."} {
		if _, err := client.CollaboratorPermission(t.Context(), "o/r", user); err == nil {
			t.Errorf("user %q was accepted", user)
		}
	}
	if _, err := client.Ref(t.Context(), "o/r", "heads/../x"); err == nil {
		t.Error("a climbing ref was accepted")
	}
}

func TestNewValidatesURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		ok   bool
	}{
		{"default", "", true},
		{"https", "https://api.github.com", true},
		{"https with a path", "https://ghe.example.com/api/v3/", true},
		{"http to localhost", "http://localhost:8080", true},
		{"http to 127.0.0.1", "http://127.0.0.1:34567", true},
		{"http to another 127/8 address", "http://127.5.5.5", true},
		{"http to ::1", "http://[::1]:8080", true},
		{"http to GitHub", "http://api.github.com", false},
		{"http to a private address", "http://10.0.0.1", false},
		{"http to an unspecified address", "http://0.0.0.0:80", false},
		{"http to another IPv6 address", "http://[::2]", false},
		{"http to a host that starts with 127.0.0.1", "http://127.0.0.1.evil.com", false},
		{"http to a host that starts with localhost", "http://localhost.evil.com", false},
		{"http with localhost as the user name", "http://localhost@evil.com", false},
		{"ftp", "ftp://example.com", false},
		{"file", "file:///etc/passwd", false},
		{"no scheme", "example.com", false},
		{"no host", "http://", false},
		{"https without a host", "https://", false},
		{"unparsable", "://x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := github.New(tt.base, "t")
			if (err == nil) != tt.ok {
				t.Errorf("New(%q) error = %v, want accepted = %v", tt.base, err, tt.ok)
			}
		})
	}
}

func TestBaseURLWithPath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"state":"open"}`)
	}))
	t.Cleanup(server.Close)
	client, err := github.New(server.URL+"/api/v3/", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.PullRequest(t.Context(), "o/r", 2); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v3/repos/o/r/pulls/2" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestPaginate(t *testing.T) {
	var serverURL string
	f := newFake(t, nil)
	serverURL = f.server.URL
	f.routes = map[string]http.HandlerFunc{
		"GET /items?per_page=2": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().
				Add("Link", fmt.Sprintf(`<%s/items?page=2&per_page=2>; rel="next", <%s/items?page=3&per_page=2>; rel="last"`, serverURL, serverURL))
			fmt.Fprint(w, `[1,2]`)
		},
		"GET /items?page=2&per_page=2": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().
				Add("Link", fmt.Sprintf(`<%s/items?page=1&per_page=2>; rel="prev", <%s/items?page=3&per_page=2>; rel="next"`, serverURL, serverURL))
			fmt.Fprint(w, `[3,4]`)
		},
		"GET /items?page=3&per_page=2": jsonBody(`[5]`),
	}
	var all []int
	err := f.client().Paginate(t.Context(), "items", url.Values{"per_page": {"2"}}, func(page []byte) error {
		var numbers []int
		if err := json.Unmarshal(page, &numbers); err != nil {
			return err
		}
		all = append(all, numbers...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(all) != "[1 2 3 4 5]" {
		t.Errorf("items = %v", all)
	}
}

func TestPaginateStops(t *testing.T) {
	t.Run("handler error", func(t *testing.T) {
		f := newFake(t, map[string]http.HandlerFunc{"GET /items": jsonBody(`[]`)})
		want := errors.New("stop")
		err := f.client().Paginate(t.Context(), "items", nil, func([]byte) error { return want })
		if !errors.Is(err, want) {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("foreign host", func(t *testing.T) {
		other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("the client followed a link to another host")
		}))
		t.Cleanup(other.Close)
		f := newFake(t, map[string]http.HandlerFunc{
			"GET /items": func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Add("Link", "<"+other.URL+"/items>; rel=\"next\"")
				fmt.Fprint(w, `[]`)
			},
		})
		if err := f.client().Paginate(t.Context(), "items", nil, func([]byte) error { return nil }); err == nil {
			t.Error("a link to another host was followed")
		}
	})
	t.Run("endless", func(t *testing.T) {
		f := newFake(t, nil)
		f.routes = map[string]http.HandlerFunc{
			"GET /items": func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Add("Link", "<"+f.server.URL+"/items>; rel=\"next\"")
				fmt.Fprint(w, `[]`)
			},
		}
		pages := 0
		err := f.client().Paginate(t.Context(), "items", nil, func([]byte) error { pages++; return nil })
		if err == nil || pages != 100 {
			t.Errorf("pages = %d, err = %v", pages, err)
		}
	})
	t.Run("relative link", func(t *testing.T) {
		f := newFake(t, map[string]http.HandlerFunc{})
		f.routes["GET /items"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("Link", `</items2>; rel="next"`)
			fmt.Fprint(w, `[]`)
		}
		f.routes["GET /items2"] = jsonBody(`[]`)
		pages := 0
		err := f.client().Paginate(t.Context(), "items", nil, func([]byte) error { pages++; return nil })
		if err != nil || pages != 2 {
			t.Errorf("pages = %d, err = %v", pages, err)
		}
	})
}

func TestRepositoryAndUsers(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{
		"GET /repos/o/fork":       jsonBody(`{"full_name":"o/fork","permissions":{"push":true}}`),
		"GET /repos/o/nopush":     jsonBody(`{"full_name":"o/nopush"}`),
		"GET /user":               jsonBody(`{"login":"bot","id":9007199254740993}`),
		"GET /users/app%5Bbot%5D": jsonBody(`{"login":"app[bot]","id":42}`),
	})
	client := f.client()
	ctx := t.Context()
	if repo, err := client.Repository(ctx, "o/fork"); err != nil || !repo.Permissions.Push {
		t.Errorf("fork = %+v, %v", repo, err)
	}
	if repo, err := client.Repository(ctx, "o/nopush"); err != nil || repo.Permissions.Push {
		t.Errorf("nopush = %+v, %v", repo, err)
	}
	if user, err := client.AuthenticatedUser(ctx); err != nil || string(user.ID) != "9007199254740993" {
		t.Errorf("user = %+v, %v", user, err)
	}
	if user, err := client.User(
		ctx,
		"app[bot]",
	); err != nil || string(user.Login) != `"app[bot]"` ||
		string(user.ID) != "42" {
		t.Errorf("app = %+v, %v", user, err)
	}
	for _, login := range []string{"", "a/b"} {
		if _, err := client.User(ctx, login); err == nil {
			t.Errorf("login %q was accepted", login)
		}
	}
}

func TestWorkflowRunCalls(t *testing.T) {
	var runsQuery url.Values
	f := newFake(t, nil)
	f.routes = map[string]http.HandlerFunc{
		"GET /repos/o/r/actions/runs/9": jsonBody(`{"run_attempt":1,"created_at":"2026-01-02T03:04:05Z"}`),
		"GET /repos/o/r/actions/workflows/w.yaml/runs?branch=main&created=%3E%3D2026&per_page=100": func(w http.ResponseWriter, r *http.Request) {
			runsQuery = r.URL.Query()
			w.Header().
				Add("Link", fmt.Sprintf(`<%s/repos/o/r/actions/workflows/w.yaml/runs?page=2>; rel="next"`, f.server.URL))
			fmt.Fprint(w, `{"total_count":2,"workflow_runs":[{"id":1}]}`)
		},
		"GET /repos/o/r/actions/workflows/w.yaml/runs?page=2": jsonBody(`{"total_count":2,"workflow_runs":[{"id":2}]}`),
		"GET /repos/o/r/actions/runs/1/attempts/1/jobs?per_page=100": jsonBody(
			`{"total_count":1,"jobs":[{"name":"j","conclusion":"success","steps":[{"name":"s","conclusion":"skipped"},{"conclusion":"x"}]}]}`,
		),
	}
	client := f.client()
	ctx := t.Context()
	run, err := client.WorkflowRun(ctx, "o/r", 9)
	if err != nil || run.RunAttempt != 1 || run.CreatedAt != "2026-01-02T03:04:05Z" {
		t.Errorf("run = %+v, %v", run, err)
	}
	pages, err := client.WorkflowRunPages(
		ctx,
		"o/r",
		"w.yaml",
		url.Values{"branch": {"main"}, "created": {">=2026"}, "per_page": {"100"}},
	)
	if err != nil || len(pages) != 2 || pages[0].TotalCount == nil || *pages[0].TotalCount != 2 ||
		(*pages[1].WorkflowRuns)[0].ID != 2 {
		t.Errorf("pages = %+v, %v", pages, err)
	}
	if runsQuery.Get("created") != ">=2026" {
		t.Errorf("created = %q", runsQuery.Get("created"))
	}
	jobs, err := client.JobPages(ctx, "o/r", 1)
	if err != nil || len(jobs) != 1 || jobs[0].Jobs == nil {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	steps := (*jobs[0].Jobs)[0].Steps
	if steps[0].Name == nil || *steps[0].Name != "s" || steps[1].Name != nil {
		t.Errorf("steps = %+v", steps)
	}
}

func TestOpenPullRequests(t *testing.T) {
	var bodies []string
	f := newFake(t, nil)
	f.routes = map[string]http.HandlerFunc{
		"POST /graphql": func(w http.ResponseWriter, r *http.Request) {
			data, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(data))
			if len(bodies) == 1 {
				fmt.Fprint(
					w,
					`{"data":{"repository":{"pullRequests":{"nodes":[{"number":3,"isDraft":false,"mergeable":"CONFLICTING","baseRefName":"main"}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`,
				)
				return
			}
			fmt.Fprint(
				w,
				`{"data":{"repository":{"pullRequests":{"nodes":[{"number":2,"isDraft":true,"mergeable":"UNKNOWN","baseRefName":"dev"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`,
			)
		},
	}
	list, err := f.client().OpenPullRequests(t.Context(), "o/r", 200)
	if err != nil || len(list) != 2 || list[0].Number != 3 || list[0].Mergeable != "CONFLICTING" || !list[1].IsDraft {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[1], `"after":"c1"`) ||
		!strings.Contains(bodies[0], `"owner":"o"`) ||
		!strings.Contains(bodies[0], "CREATED_AT") {
		t.Errorf("requests = %q", bodies)
	}
	limited, err := f.client().OpenPullRequests(t.Context(), "o/r", 1)
	if err != nil || len(limited) != 1 {
		t.Errorf("limited = %+v, %v", limited, err)
	}
}

func TestGraphQLErrors(t *testing.T) {
	for name, body := range map[string]string{
		"errors member": `{"data":null,"errors":[{"message":"bad query"}]}`,
		"not JSON":      `nope`,
		"bad data":      `{"data":[1]}`,
	} {
		f := newFake(t, map[string]http.HandlerFunc{"POST /graphql": jsonBody(body)})
		if _, err := f.client().OpenPullRequests(t.Context(), "o/r", 10); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := newFake(t, nil).client().OpenPullRequests(t.Context(), "bad", 10); err == nil {
		t.Error("an invalid repository was accepted")
	}
}

func TestGraphQLEndpointOfEnterpriseServer(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(
			w,
			`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`,
		)
	}))
	t.Cleanup(server.Close)
	client, err := github.New(server.URL+"/api/v3", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.OpenPullRequests(t.Context(), "o/r", 10); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graphql" {
		t.Errorf("path = %q", gotPath)
	}
}
