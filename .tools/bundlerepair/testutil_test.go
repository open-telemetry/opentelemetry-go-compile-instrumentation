// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// output holds what a command wrote.
type output struct {
	stdout string
	stderr string
}

// runApp runs one command with env as its environment and a fixed clock.
func runApp(t *testing.T, env map[string]string, args ...string) (int, output) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := app{
		getenv: func(key string) string { return env[key] },
		lookup: func(key string) (string, bool) { value, ok := env[key]; return value, ok },
		now:    func() time.Time { return time.Unix(testNow, 0) },
		stdout: &stdout,
		stderr: &stderr,
	}
	code := a.run(t.Context(), args)
	return code, output{stdout: stdout.String(), stderr: stderr.String()}
}

// apiFake serves handlers by "METHOD /path" (the query is ignored) and fails
// the test on any other request.
type apiFake struct {
	URL string

	t      *testing.T
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	seen   []string
}

func newAPI(t *testing.T) *apiFake {
	t.Helper()
	fake := &apiFake{t: t, routes: map[string]http.HandlerFunc{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		fake.mu.Lock()
		fake.seen = append(fake.seen, key)
		handler, ok := fake.routes[key]
		fake.mu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("%s: unexpected Authorization header %q", key, got)
		}
		if !ok {
			t.Errorf("unexpected request %s", r.Method+" "+r.URL.RequestURI())
			http.Error(w, `{"message":"unexpected request"}`, http.StatusTeapot)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	fake.URL = server.URL
	return fake
}

// set replaces or adds the handler of key.
func (f *apiFake) set(key string, handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[key] = handler
}

// setBody serves body as JSON for key.
func (f *apiFake) setBody(key, body string) { f.set(key, jsonHandler(http.StatusOK, body)) }

// remove deletes the handler of key, so a request to it fails the test.
func (f *apiFake) remove(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.routes, key)
}

func (f *apiFake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// newAPIWithBodies serves a fixed JSON body for each "METHOD /path". An empty
// body answers 404, like an unknown user.
func newAPIWithBodies(t *testing.T, bodies map[string]string) *apiFake {
	t.Helper()
	fake := newAPI(t)
	for key, body := range bodies {
		if body == "" {
			fake.set(key, jsonHandler(http.StatusNotFound, `{"message":"Not Found"}`))
			continue
		}
		fake.setBody(key, body)
	}
	return fake
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}
