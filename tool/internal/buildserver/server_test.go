// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package buildserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func fakeSingleClosure(imp string) map[string]string {
	return map[string]string{imp: "/cache/" + imp + ".a"}
}

func fakeClosures(importPaths []string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(importPaths))
	for _, imp := range importPaths {
		out[imp] = fakeSingleClosure(imp)
	}
	return out
}

func testRuleSets() []*rule.InstRuleSet {
	setA := rule.NewInstRuleSet("example.com/app")
	setA.SetPackageName("app")
	setA.AddFuncRule("/src/app/a.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "hook-a",
			Imports: map[string]string{"h": "example.com/hook/a"},
		},
		Func: "Handle",
	})
	setA.AddIgnoredCallFile("/src/app/ignored.go")

	setB := rule.NewInstRuleSet("example.com/lib")
	setB.SetPackageName("lib")
	setB.AddFuncRule("/src/lib/b.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "hook-b",
			Imports: map[string]string{"hb": "example.com/hook/b", "hc": "example.com/hook/c"},
		},
		Func: "Run",
	})

	return []*rule.InstRuleSet{setA, setB}
}

func TestServerTransportAuthAndFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("uds permissions cleanup and unavailable fallback", func(t *testing.T) {
			srv, err := Start(t.Context(), Config{ToolID: "abc123"})
			require.NoError(t, err)

			addr := srv.Addr()
			require.True(t, strings.HasPrefix(addr, "unix:"), "expected unix addr, got %q", addr)
			sockPath := strings.TrimPrefix(addr, "unix:")
			sockDir := filepath.Dir(sockPath)
			sockInfo, err := os.Stat(sockPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), sockInfo.Mode().Perm())
			dirInfo, err := os.Stat(sockDir)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

			client, err := NewClient(addr, srv.Token())
			require.NoError(t, err)
			id, err := client.ToolID(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "abc123", id)

			require.NoError(t, srv.Close(t.Context()))
			_, err = os.Stat(sockPath)
			assert.True(t, os.IsNotExist(err))
			_, err = os.Stat(sockDir)
			assert.True(t, os.IsNotExist(err))

			_, err = client.ToolID(t.Context())
			require.ErrorIs(t, err, ErrUnavailable)
			_, err = client.CompileLookup(t.Context(), "net/http")
			require.ErrorIs(t, err, ErrUnavailable)
			_, err = client.ResolveExports(t.Context(), ResolveExportRequest{Imports: []string{"fmt"}})
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}

	t.Run("tcp bearer auth and body limit", func(t *testing.T) {
		t.Setenv(util.EnvOtelcBuildServer, "")
		t.Setenv(util.EnvOtelcBuildServerAuth, "")
		assert.Nil(t, ClientFromEnv())

		srv, err := Start(t.Context(), Config{ToolID: "tcp999", UseTCP: true})
		require.NoError(t, err)
		t.Cleanup(func() { _ = srv.Close(context.Background()) })

		addr := srv.Addr()
		require.True(t, strings.HasPrefix(addr, "tcp:"))
		require.NotEmpty(t, srv.Token())

		baseURL := "http://" + strings.TrimPrefix(addr, "tcp:")
		httpClient := &http.Client{Transport: &http.Transport{Proxy: nil}}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/v1/tool-id", nil)
		require.NoError(t, err)
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

		badClient, err := NewClient(addr, "wrong-token")
		require.NoError(t, err)
		_, err = badClient.ToolID(t.Context())
		require.Error(t, err)

		oversized := append([]byte(`{"imports":["`), bytes.Repeat([]byte("a"), maxResolveRequestBytes+1024)...)
		oversized = append(oversized, []byte(`"]}`)...)
		bigReq, err := http.NewRequestWithContext(
			t.Context(), http.MethodPost, baseURL+"/v1/resolve-export", bytes.NewReader(oversized),
		)
		require.NoError(t, err)
		bigReq.Header.Set("Authorization", "Bearer "+srv.Token())
		bigReq.Header.Set("Content-Type", "application/json")
		bigResp, err := httpClient.Do(bigReq)
		require.NoError(t, err)
		_ = bigResp.Body.Close()
		assert.Equal(t, http.StatusRequestEntityTooLarge, bigResp.StatusCode)

		client, err := NewClient(addr, srv.Token())
		require.NoError(t, err)
		id, err := client.ToolID(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "tcp999", id)
	})
}

func TestServerToolIDAndCompileLookup(t *testing.T) {
	srv, err := Start(t.Context(), Config{
		ToolID:      "deadbeef01234567",
		RuleSets:    testRuleSets(),
		ImportNames: map[string]string{"example.com/hook/a": "hooka"},
		PGOProfile:  "/tmp/default.pgo",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	client, err := NewClient(srv.Addr(), srv.Token())
	require.NoError(t, err)

	unmatched, err := client.CompileLookup(t.Context(), "net/http")
	require.NoError(t, err)
	assert.False(t, unmatched.Matched)
	assert.Empty(t, unmatched.RuleSets)

	matched, err := client.CompileLookup(t.Context(), "example.com/app")
	require.NoError(t, err)
	assert.True(t, matched.Matched)
	require.Len(t, matched.RuleSets, 1)
	assert.Equal(t, map[string]string{"example.com/hook/a": "hooka"}, matched.ImportNames)
	assert.True(t, matched.BuildUsesIgnoreDirective)
	assert.Equal(t, "/tmp/default.pgo", matched.PGOProfile)

	matchedLib, err := client.CompileLookup(t.Context(), "example.com/lib")
	require.NoError(t, err)
	assert.True(t, matchedLib.Matched)
	assert.True(t, matchedLib.BuildUsesIgnoreDirective)
}

func TestServerResolveExportNoDeadlockOnNestedCallback(t *testing.T) {
	var client *Client
	var nestedDone atomic.Bool

	srv, err := Start(t.Context(), Config{
		ToolID:   "nodeadlock",
		RuleSets: testRuleSets(),
		ResolveBatch: func(
			ctx context.Context,
			importPaths, _ []string,
			_ ...string,
		) (map[string]map[string]string, map[string]error, error) {
			id, err := client.ToolID(ctx)
			if err != nil || id != "nodeadlock" {
				return nil, nil, fmt.Errorf("tool-id failed: id=%q err=%w", id, err)
			}
			comp, err := client.CompileLookup(ctx, "example.com/app")
			if err != nil || !comp.Matched {
				return nil, nil, fmt.Errorf("compile lookup failed: err=%w", err)
			}
			nestedResp, err := client.ResolveExports(ctx, ResolveExportRequest{
				Imports: []string{"example.com/nested/dep"},
				Nested:  true,
			})
			if err != nil || nestedResp.Closures["example.com/nested/dep"]["example.com/nested/dep"] == "" {
				return nil, nil, fmt.Errorf("nested resolve failed: err=%w", err)
			}
			nestedDone.Store(true)
			return fakeClosures(importPaths), nil, nil
		},
		ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
			return fakeSingleClosure(imp), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	client, err = NewClient(srv.Addr(), srv.Token())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resp, err := client.ResolveExports(ctx, ResolveExportRequest{Imports: []string{"example.com/hook/a"}})
	require.NoError(t, err)
	assert.True(t, nestedDone.Load())
	assert.Equal(t, fakeSingleClosure("example.com/hook/a"), resp.Closures["example.com/hook/a"])
}

func TestResolveExportsDST(t *testing.T) {
	t.Run("batch coalescing and cache hits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var batchCalls atomic.Int32
			var lastBatch []string
			gate := make(chan struct{})

			srv, err := newServer(Config{
				RuleSets: testRuleSets(),
				ResolveBatch: func(
					_ context.Context,
					importPaths, _ []string,
					_ ...string,
				) (map[string]map[string]string, map[string]error, error) {
					batchCalls.Add(1)
					lastBatch = slices.Clone(importPaths)
					<-gate
					return fakeClosures(importPaths), nil, nil
				},
			})
			require.NoError(t, err)

			targets := []string{
				"example.com/hook/a", "example.com/hook/b", "example.com/hook/c",
				"example.com/hook/a", "example.com/hook/b", "example.com/hook/c",
			}
			results := make([]*ResolveExportResponse, len(targets))
			errs := make([]error, len(targets))

			var wg sync.WaitGroup
			for i, imp := range targets {
				wg.Go(func() {
					results[i], errs[i] = srv.resolveExports(t.Context(), ResolveExportRequest{
						Imports:    []string{imp},
						BuildFlags: []string{"-tags=foo"},
					})
				})
			}

			synctest.Wait()
			assert.Equal(t, int32(1), batchCalls.Load())
			close(gate)
			wg.Wait()

			assert.Equal(t, int32(1), batchCalls.Load())
			assert.Equal(t, []string{"example.com/hook/a", "example.com/hook/b", "example.com/hook/c"}, lastBatch)
			for i, imp := range targets {
				require.NoError(t, errs[i])
				assert.Equal(t, fakeSingleClosure(imp), results[i].Closures[imp])
			}

			cached, err := srv.resolveExports(t.Context(), ResolveExportRequest{
				Imports:    []string{"example.com/hook/a"},
				BuildFlags: []string{"-tags=foo"},
			})
			require.NoError(t, err)
			assert.Equal(t, 1, cached.CacheHits)
			assert.Equal(t, 0, cached.GoListCalls)
		})
	})

	t.Run("owner cancel wakes waiter to retry without poisoning cache", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var singleCalls atomic.Int32
			srv, err := newServer(Config{
				ResolveSingle: func(ctx context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
					if singleCalls.Add(1) == 1 {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return fakeSingleClosure(imp), nil
				},
			})
			require.NoError(t, err)

			ownerCtx, cancelOwner := context.WithCancel(t.Context())
			var ownerErr, waiterErr error
			var waiterResp *ResolveExportResponse
			var wg sync.WaitGroup

			wg.Go(func() {
				_, ownerErr = srv.resolveExports(ownerCtx, ResolveExportRequest{
					Imports: []string{"example.com/retry/pkg"},
				})
			})
			synctest.Wait()

			wg.Go(func() {
				waiterResp, waiterErr = srv.resolveExports(t.Context(), ResolveExportRequest{
					Imports: []string{"example.com/retry/pkg"},
				})
			})
			synctest.Wait()

			cancelOwner()
			wg.Wait()

			require.ErrorIs(t, ownerErr, context.Canceled)
			require.NoError(t, waiterErr)
			assert.Equal(t, fakeSingleClosure("example.com/retry/pkg"), waiterResp.Closures["example.com/retry/pkg"])
			assert.Empty(t, waiterResp.Failed)
			assert.Equal(t, 0, waiterResp.CacheHits)
			assert.Equal(t, 1, waiterResp.GoListCalls)
			assert.Equal(t, int32(2), singleCalls.Load())
		})
	})

	t.Run("waiter cancel unblocks immediately while owner finishes and caches", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			gate := make(chan struct{})
			srv, err := newServer(Config{
				ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
					<-gate
					return fakeSingleClosure(imp), nil
				},
			})
			require.NoError(t, err)

			waiterCtx, cancelWaiter := context.WithCancel(t.Context())
			var ownerErr, waiterErr error
			var ownerResp *ResolveExportResponse
			var wg sync.WaitGroup

			wg.Go(func() {
				ownerResp, ownerErr = srv.resolveExports(t.Context(), ResolveExportRequest{
					Imports: []string{"example.com/slow/pkg"},
				})
			})
			synctest.Wait()

			wg.Go(func() {
				_, waiterErr = srv.resolveExports(waiterCtx, ResolveExportRequest{
					Imports: []string{"example.com/slow/pkg"},
				})
			})
			synctest.Wait()

			cancelWaiter()
			synctest.Wait()
			require.ErrorIs(t, waiterErr, context.Canceled)

			close(gate)
			wg.Wait()

			require.NoError(t, ownerErr)
			assert.Equal(t, fakeSingleClosure("example.com/slow/pkg"), ownerResp.Closures["example.com/slow/pkg"])

			cached, err := srv.resolveExports(t.Context(), ResolveExportRequest{
				Imports: []string{"example.com/slow/pkg"},
			})
			require.NoError(t, err)
			assert.Equal(t, 1, cached.CacheHits)
			assert.Equal(t, 0, cached.GoListCalls)
		})
	})

	t.Run("resolver panic cleans up inflight entry and wakes blocked waiter", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			gate := make(chan struct{})
			var calls atomic.Int32
			srv, err := newServer(Config{
				ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
					if calls.Add(1) == 1 {
						<-gate
						panic("simulated resolver panic")
					}
					return fakeSingleClosure(imp), nil
				},
			})
			require.NoError(t, err)

			var panicked atomic.Bool
			var waiterResp *ResolveExportResponse
			var waiterErr error
			var wg sync.WaitGroup

			wg.Go(func() {
				defer func() {
					if recover() != nil {
						panicked.Store(true)
					}
				}()
				_, _ = srv.resolveExports(t.Context(), ResolveExportRequest{
					Imports: []string{"example.com/panic/pkg"},
				})
			})
			synctest.Wait()

			wg.Go(func() {
				waiterResp, waiterErr = srv.resolveExports(t.Context(), ResolveExportRequest{
					Imports: []string{"example.com/panic/pkg"},
				})
			})
			synctest.Wait()

			close(gate)
			wg.Wait()

			assert.True(t, panicked.Load())
			require.NoError(t, waiterErr)
			assert.Equal(t, fakeSingleClosure("example.com/panic/pkg"), waiterResp.Closures["example.com/panic/pkg"])
			assert.Empty(t, srv.singleInflight)
		})
	})
}

func TestResolveExportsProperty_Invariants(t *testing.T) {
	const numSeeds = 32

	for seed := range uint64(numSeeds) {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runPropertyScenario(t, seed)
			})
		})
	}
}

type propertyScenario struct {
	allImports []string
	poisonSet  map[string]bool
	flakyImp   string
	flagSets   [][]string
	srv        *Server
	plans      []callerPlan
}

type callerPlan struct {
	req         ResolveExportRequest
	cancelAfter time.Duration
}

func buildPropertyRuleSets(allImports []string) []*rule.InstRuleSet {
	rsetA := rule.NewInstRuleSet("example.com/pkgA")
	rsetB := rule.NewInstRuleSet("example.com/pkgB")
	impsA := make(map[string]string)
	impsB := make(map[string]string)
	for i, imp := range allImports {
		alias := fmt.Sprintf("h%d", i)
		if i%2 == 0 {
			impsA[alias] = imp
		} else {
			impsB[alias] = imp
		}
	}
	rsetA.AddFuncRule("/src/a.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "ra", Imports: impsA}, Func: "A",
	})
	rsetB.AddFuncRule("/src/b.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "rb", Imports: impsB}, Func: "B",
	})
	return []*rule.InstRuleSet{rsetA, rsetB}
}

func buildCallerPlans(rng *rand.Rand, allImports []string, flagSets [][]string) []callerPlan {
	pkgs := []string{"example.com/pkgA", "example.com/pkgB", ""}
	numCallers := 6 + rng.IntN(7)
	plans := make([]callerPlan, numCallers)
	for i := range numCallers {
		count := 1 + rng.IntN(min(3, len(allImports)))
		picked := make([]string, 0, count)
		for range count {
			imp := allImports[rng.IntN(len(allImports))]
			if !slices.Contains(picked, imp) {
				picked = append(picked, imp)
			}
		}
		var cancelAfter time.Duration
		if rng.IntN(4) == 0 {
			cancelAfter = time.Duration(1+rng.IntN(4)) * time.Millisecond
		}
		plans[i] = callerPlan{
			req: ResolveExportRequest{
				Package:    pkgs[rng.IntN(len(pkgs))],
				Imports:    picked,
				BuildFlags: flagSets[rng.IntN(len(flagSets))],
				Nested:     rng.IntN(5) == 0,
			},
			cancelAfter: cancelAfter,
		}
	}
	return plans
}

func buildPropertyScenario(t *testing.T, seed uint64) *propertyScenario {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed+1, seed*31+7))

	numImports := 3 + rng.IntN(5)
	allImports := make([]string, numImports)
	for i := range numImports {
		allImports[i] = fmt.Sprintf("example.com/hook/p%d", i)
	}

	poisonSet := make(map[string]bool)
	for range rng.IntN(3) {
		poisonSet[allImports[rng.IntN(numImports)]] = true
	}
	var flakyImp string
	if rng.IntN(2) == 1 {
		for _, imp := range allImports {
			if !poisonSet[imp] {
				flakyImp = imp
				break
			}
		}
	}
	var flakyFailedOnce atomic.Bool

	srv, err := newServer(Config{
		RuleSets: buildPropertyRuleSets(allImports),
		ResolveBatch: func(
			ctx context.Context,
			importPaths, _ []string,
			_ ...string,
		) (map[string]map[string]string, map[string]error, error) {
			if sleepErr := sleepVirtual(ctx, time.Duration(1+len(importPaths))*time.Millisecond); sleepErr != nil {
				return nil, nil, sleepErr
			}
			for _, imp := range importPaths {
				if poisonSet[imp] {
					return nil, nil, errors.New("no required module provides package " + imp)
				}
			}
			if flakyImp != "" && slices.Contains(importPaths, flakyImp) && flakyFailedOnce.CompareAndSwap(false, true) {
				return nil, nil, errors.New("transient batch io error")
			}
			return fakeClosures(importPaths), nil, nil
		},
		ResolveSingle: func(ctx context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
			if sleepErr := sleepVirtual(ctx, 2*time.Millisecond); sleepErr != nil {
				return nil, sleepErr
			}
			if poisonSet[imp] {
				return nil, errors.New("no required module provides package " + imp)
			}
			if imp == flakyImp && flakyFailedOnce.CompareAndSwap(false, true) {
				return nil, errors.New("transient single io error")
			}
			return fakeSingleClosure(imp), nil
		},
	})
	require.NoError(t, err)

	flagSets := [][]string{nil, {"-tags=extra"}}
	return &propertyScenario{
		allImports: allImports,
		poisonSet:  poisonSet,
		flakyImp:   flakyImp,
		flagSets:   flagSets,
		srv:        srv,
		plans:      buildCallerPlans(rng, allImports, flagSets),
	}
}

func runPropertyScenario(t *testing.T, seed uint64) {
	t.Helper()
	sc := buildPropertyScenario(t, seed)

	var wg sync.WaitGroup
	for _, plan := range sc.plans {
		wg.Go(func() {
			runPropertyCaller(t, seed, sc, plan)
		})
	}
	wg.Wait()
	synctest.Wait()

	assertPropertyPostConditions(t, seed, sc)
}

func runPropertyCaller(t *testing.T, seed uint64, sc *propertyScenario, plan callerPlan) {
	t.Helper()
	ctx := t.Context()
	if plan.cancelAfter > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, plan.cancelAfter)
		defer cancel()
	}
	resp, resolveErr := sc.srv.resolveExports(ctx, plan.req)
	if plan.cancelAfter > 0 && errors.Is(resolveErr, context.DeadlineExceeded) {
		return
	}
	require.NoError(t, resolveErr, "seed=%d req=%+v", seed, plan.req)
	for _, imp := range plan.req.Imports {
		if sc.poisonSet[imp] {
			assert.Contains(t, resp.Failed[imp], "no required module", "seed=%d imp=%s", seed, imp)
			continue
		}
		if imp == sc.flakyImp && resp.Failed[imp] != "" {
			assert.Contains(t, resp.Failed[imp], "transient", "seed=%d", seed)
			continue
		}
		assert.Equal(t, fakeSingleClosure(imp), resp.Closures[imp], "seed=%d imp=%s", seed, imp)
		assert.Empty(t, resp.Failed[imp], "seed=%d imp=%s", seed, imp)
	}
}

func assertPropertyPostConditions(t *testing.T, seed uint64, sc *propertyScenario) {
	t.Helper()
	sc.srv.exportMu.Lock()
	assert.Empty(t, sc.srv.batchInflight, "seed=%d", seed)
	assert.Empty(t, sc.srv.singleInflight, "seed=%d", seed)
	for _, byImp := range sc.srv.exportCache {
		for imp, entry := range byImp {
			if entry.failed {
				assert.True(t, sc.poisonSet[imp], "seed=%d non-poison %s cached as failed: %s", seed, imp, entry.errMsg)
			}
		}
	}
	sc.srv.exportMu.Unlock()

	for _, flags := range sc.flagSets {
		for _, imp := range sc.allImports {
			resp, resolveErr := sc.srv.resolveExports(t.Context(), ResolveExportRequest{
				Imports:    []string{imp},
				BuildFlags: flags,
			})
			require.NoError(t, resolveErr, "seed=%d imp=%s", seed, imp)
			if sc.poisonSet[imp] {
				assert.Contains(t, resp.Failed[imp], "no required module", "seed=%d imp=%s", seed, imp)
				again, againErr := sc.srv.resolveExports(t.Context(), ResolveExportRequest{
					Imports:    []string{imp},
					BuildFlags: flags,
				})
				require.NoError(t, againErr)
				assert.Equal(t, 1, again.CacheHits, "seed=%d poison %s should be memoized", seed, imp)
				assert.Equal(t, 0, again.GoListCalls, "seed=%d poison %s should not re-run go list", seed, imp)
			} else {
				assert.Equal(t, fakeSingleClosure(imp), resp.Closures[imp], "seed=%d imp=%s", seed, imp)
				assert.Empty(t, resp.Failed[imp], "seed=%d imp=%s", seed, imp)
			}
		}
	}
}

func sleepVirtual(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func BenchmarkBuildServerCompileLookup(b *testing.B) {
	srv, err := Start(b.Context(), Config{
		ToolID:      "bench",
		RuleSets:    testRuleSets(),
		ImportNames: map[string]string{"example.com/hook/a": "hooka"},
	})
	require.NoError(b, err)
	b.Cleanup(func() { _ = srv.Close(context.Background()) })

	client, err := NewClient(srv.Addr(), srv.Token())
	require.NoError(b, err)
	ctx := b.Context()

	b.Run("unmatched", func(b *testing.B) {
		for b.Loop() {
			resp, lookupErr := client.CompileLookup(ctx, "net/http")
			if lookupErr != nil || resp.Matched {
				b.Fatalf("unexpected result: resp=%v err=%v", resp, lookupErr)
			}
		}
	})

	b.Run("matched", func(b *testing.B) {
		for b.Loop() {
			resp, lookupErr := client.CompileLookup(ctx, "example.com/app")
			if lookupErr != nil || !resp.Matched {
				b.Fatalf("unexpected result: resp=%v err=%v", resp, lookupErr)
			}
		}
	})
}
