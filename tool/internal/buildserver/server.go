// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package buildserver provides a lightweight stdlib-only in-memory HTTP server
// started by `otelc go` so toolexec subprocesses avoid redundant per-compile
// disk I/O and coalesce `go list -export` calls across concurrent compiles.
package buildserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/internal/session"
	"go.opentelemetry.io/otelc/tool/util"
)

// BatchResolverFunc resolves export closures for multiple import paths in one
// invocation.
type BatchResolverFunc func(
	ctx context.Context,
	importPaths []string,
	env []string,
	buildFlags ...string,
) (map[string]map[string]string, map[string]error, error)

// SingleResolverFunc resolves the export closure for a single import path.
type SingleResolverFunc func(
	ctx context.Context,
	importPath string,
	env []string,
	buildFlags ...string,
) (map[string]string, error)

// Config configures a build Server.
type Config struct {
	ToolID         string
	InvocationDir  string
	RuleSets       []*rule.InstRuleSet
	ImportNames    map[string]string
	BaseBuildFlags []string
	PGOProfile     string
	ResolveEnv     []string
	ResolveBatch   BatchResolverFunc
	ResolveSingle  SingleResolverFunc
	UseTCP         bool
	SocketPath     string
	UseSessionMemo bool
}

// ToolIDResponse is the JSON payload for GET /v1/tool-id.
type ToolIDResponse struct {
	ToolID string `json:"tool_id"`
}

// CompileResponse is the JSON payload for GET /v1/compile.
type CompileResponse struct {
	Matched                  bool                `json:"matched"`
	RuleSets                 []*rule.InstRuleSet `json:"rule_sets,omitempty"`
	ImportNames              map[string]string   `json:"import_names,omitempty"`
	BuildUsesIgnoreDirective bool                `json:"build_uses_ignore_directive,omitempty"`
	PGOProfile               string              `json:"pgo_profile,omitempty"`
}

// ResolveExportRequest is the JSON payload for POST /v1/resolve-export.
type ResolveExportRequest struct {
	Package        string   `json:"package,omitempty"`
	Imports        []string `json:"imports"`
	UsePGO         bool     `json:"use_pgo,omitempty"`
	Nested         bool     `json:"nested,omitempty"`
	ResolvingChain string   `json:"resolving_chain,omitempty"`
}

// ResolveExportResponse is the JSON payload for POST /v1/resolve-export.
type ResolveExportResponse struct {
	Closures    map[string]map[string]string `json:"closures,omitempty"`
	Failed      map[string]string            `json:"failed,omitempty"`
	CacheHits   int                          `json:"cache_hits,omitempty"`
	GoListCalls int                          `json:"go_list_calls,omitempty"`
}

type exportKey struct {
	flagsKey   string
	importPath string
}

type cachedExport struct {
	closure map[string]string
	errMsg  string
	failed  bool
}

type batchResolveResult struct {
	closures map[string]map[string]string
	failed   map[string]error
	calls    int
	err      error
}

// Server serves immutable build metadata lock-free and coalesces export
// resolutions in memory for one `otelc go` invocation.
type Server struct {
	addr          string
	token         string
	invocationDir string
	sockPath      string
	sockDir       string

	listener net.Listener
	httpSrv  *http.Server
	closeErr error
	closeOne sync.Once
	serveWG  sync.WaitGroup

	// Immutable pre-marshaled responses for lock-free /v1/tool-id and /v1/compile.
	toolIDBody           []byte
	unmatchedCompileBody []byte
	compileByPkg         map[string][]byte

	knownRuleImportsByPkg map[string][]string
	knownRuleImports      []string
	knownRuleImportSet    map[string]struct{}
	baseBuildFlags        []string
	pgoProfile            string

	resolveBatch   BatchResolverFunc
	resolveSingle  SingleResolverFunc
	useSessionMemo bool

	resolveEnvMu sync.RWMutex
	resolveEnv   []string

	// exportMu protects only the export cache and inflight coordination maps.
	// It is NEVER acquired by /v1/tool-id or /v1/compile, and NEVER held across I/O.
	exportMu       sync.Mutex
	exportCache    map[string]map[string]cachedExport
	batchInflight  map[string]chan struct{}
	batchDone      map[string]bool
	singleInflight map[exportKey]chan struct{}
}

// LoadConfigFromDisk loads setup artifacts from the current invocation
// directory into a Server Config.
func LoadConfigFromDisk(toolID string) (Config, error) {
	matchedBytes, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil {
		return Config{}, ex.Wrapf(err, "reading matched rules for build server")
	}
	var sets []*rule.InstRuleSet
	if err = json.Unmarshal(matchedBytes, &sets); err != nil {
		return Config{}, ex.Wrapf(err, "unmarshaling matched rules for build server")
	}

	importNames := make(map[string]string)
	if namesBytes, readErr := os.ReadFile(util.GetImportNamesFile()); readErr == nil {
		if unmarshalErr := json.Unmarshal(namesBytes, &importNames); unmarshalErr != nil {
			return Config{}, ex.Wrapf(unmarshalErr, "unmarshaling import names for build server")
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return Config{}, ex.Wrapf(readErr, "reading import names for build server")
	}

	var pgoProfile string
	if pgoBytes, readErr := os.ReadFile(util.GetPGOProfileFile()); readErr == nil {
		pgoProfile = string(pgoBytes)
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return Config{}, ex.Wrapf(readErr, "reading PGO profile for build server")
	}

	return Config{
		ToolID:         toolID,
		RuleSets:       sets,
		ImportNames:    importNames,
		PGOProfile:     pgoProfile,
		UseSessionMemo: true,
	}, nil
}

const (
	readHeaderTimeout      = 5 * time.Second
	shutdownTimeout        = 2 * time.Second
	authTokenBytes         = 16
	maxUnixSocketPath      = 100
	privateDirMode         = 0o700
	privateSocketMode      = 0o600
	maxResolveRequestBytes = 1 << 20
)

type resolveCallParams struct {
	flagsKey   string
	reqEnv     []string
	buildFlags []string
	memo       *session.Memo
}

// Start creates and starts an in-memory build server.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	s, err := newServer(cfg)
	if err != nil {
		return nil, err
	}

	info, err := listen(ctx, cfg.UseTCP, cfg.SocketPath)
	if err != nil {
		return nil, err
	}
	s.addr = info.addr
	s.token = info.token
	s.sockPath = info.sockPath
	s.sockDir = info.sockDir
	s.listener = info.ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/tool-id", s.handleToolID)
	mux.HandleFunc("GET /v1/compile", s.handleCompile)
	mux.HandleFunc("POST /v1/resolve-export", s.handleResolveExport)

	var handler http.Handler = mux
	if info.token != "" {
		handler = s.requireBearerToken(mux)
	}

	s.httpSrv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	s.serveWG.Go(func() {
		_ = s.httpSrv.Serve(info.ln)
	})

	return s, nil
}

func newServer(cfg Config) (*Server, error) {
	toolIDBody, err := json.Marshal(ToolIDResponse{ToolID: cfg.ToolID})
	if err != nil {
		return nil, ex.Wrapf(err, "marshaling tool ID response")
	}

	buildUsesIgnore := false
	grouped := make(map[string][]*rule.InstRuleSet)
	for _, set := range cfg.RuleSets {
		if set == nil {
			continue
		}
		if len(set.IgnoredCallFiles) > 0 {
			buildUsesIgnore = true
		}
		grouped[set.ModulePath] = append(grouped[set.ModulePath], set)
	}

	compileByPkg := make(map[string][]byte, len(grouped))
	for pkg, sets := range grouped {
		body, marshalErr := json.Marshal(CompileResponse{
			Matched:                  true,
			RuleSets:                 sets,
			ImportNames:              cfg.ImportNames,
			BuildUsesIgnoreDirective: buildUsesIgnore,
			PGOProfile:               cfg.PGOProfile,
		})
		if marshalErr != nil {
			return nil, ex.Wrapf(marshalErr, "marshaling compile response for %q", pkg)
		}
		compileByPkg[pkg] = body
	}

	knownByPkg, knownImports := collectRuleImportsByPkg(cfg.RuleSets)
	knownSet := make(map[string]struct{}, len(knownImports))
	for _, imp := range knownImports {
		knownSet[imp] = struct{}{}
	}

	resolveBatch := cfg.ResolveBatch
	if resolveBatch == nil {
		resolveBatch = pkgload.ResolveExportClosuresWithEnv
	}
	resolveSingle := cfg.ResolveSingle
	if resolveSingle == nil {
		resolveSingle = defaultSingleResolver
	}

	var invocationDir string
	if cfg.InvocationDir != "" {
		invocationDir = filepath.Clean(cfg.InvocationDir)
	}

	return &Server{
		invocationDir:         invocationDir,
		toolIDBody:            toolIDBody,
		unmatchedCompileBody:  []byte(`{"matched":false}`),
		compileByPkg:          compileByPkg,
		knownRuleImportsByPkg: knownByPkg,
		knownRuleImports:      knownImports,
		knownRuleImportSet:    knownSet,
		baseBuildFlags:        slices.Clone(cfg.BaseBuildFlags),
		pgoProfile:            cfg.PGOProfile,
		resolveBatch:          resolveBatch,
		resolveSingle:         resolveSingle,
		useSessionMemo:        cfg.UseSessionMemo,
		resolveEnv:            slices.Clone(cfg.ResolveEnv),
		exportCache:           make(map[string]map[string]cachedExport),
		batchInflight:         make(map[string]chan struct{}),
		batchDone:             make(map[string]bool),
		singleInflight:        make(map[exportKey]chan struct{}),
	}, nil
}

func defaultSingleResolver(
	ctx context.Context,
	importPath string,
	env []string,
	buildFlags ...string,
) (map[string]string, error) {
	closures, failed, err := pkgload.ResolveExportClosuresWithEnv(ctx, []string{importPath}, env, buildFlags...)
	if err != nil {
		return nil, err
	}
	if loadErr, ok := failed[importPath]; ok {
		return nil, loadErr
	}
	closure, ok := closures[importPath]
	if !ok {
		return nil, ex.Newf("package %q not found or has no export file", importPath)
	}
	return closure, nil
}

type listenerInfo struct {
	ln       net.Listener
	addr     string
	sockPath string
	sockDir  string
	token    string
}

// listen binds a Unix domain socket on Unix (falling back to loopback TCP on
// Windows or when UDS listen fails). TCP listeners always require a random
// bearer token to prevent local cross-user spoofing.
func listen(ctx context.Context, useTCP bool, customSockPath string) (listenerInfo, error) {
	if !useTCP && runtime.GOOS != "windows" {
		if info, err := listenUnix(ctx, customSockPath); err == nil {
			return info, nil
		}
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return listenerInfo{}, ex.Wrapf(err, "listening for build server")
	}
	tok, err := randomHex(authTokenBytes)
	if err != nil {
		_ = ln.Close()
		return listenerInfo{}, ex.Wrapf(err, "generating build server token")
	}
	return listenerInfo{ln: ln, addr: "tcp:" + ln.Addr().String(), token: tok}, nil
}

func listenUnix(ctx context.Context, customSockPath string) (listenerInfo, error) {
	sockPath := customSockPath
	var sockDir string
	if sockPath == "" {
		var err error
		sockDir, err = createPrivateSocketDir()
		if err != nil {
			return listenerInfo{}, err
		}
		sockPath = filepath.Join(sockDir, "s.sock")
	} else {
		_ = os.Remove(sockPath)
	}

	cleanup := func() {
		if sockDir != "" {
			_ = os.RemoveAll(sockDir)
		} else {
			_ = os.Remove(sockPath)
		}
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		cleanup()
		return listenerInfo{}, err
	}
	if err = os.Chmod(sockPath, privateSocketMode); err != nil {
		_ = ln.Close()
		cleanup()
		return listenerInfo{}, err
	}
	return listenerInfo{
		ln:       ln,
		addr:     "unix:" + sockPath,
		sockPath: sockPath,
		sockDir:  sockDir,
	}, nil
}

func createPrivateSocketDir() (string, error) {
	dir, err := os.MkdirTemp("", "otelc-")
	if err != nil {
		return "", err
	}
	sockPath := filepath.Join(dir, "s.sock")
	if len(sockPath) > maxUnixSocketPath {
		_ = os.RemoveAll(dir)
		dir, err = os.MkdirTemp("/tmp", "otelc-")
		if err != nil {
			return "", err
		}
		sockPath = filepath.Join(dir, "s.sock")
		if len(sockPath) > maxUnixSocketPath {
			_ = os.RemoveAll(dir)
			return "", ex.Newf("unix socket path %q exceeds %d bytes", sockPath, maxUnixSocketPath)
		}
	}
	if err = os.Chmod(dir, privateDirMode); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Addr returns the server address in EnvOtelcBuildServer format
// ("unix:<path>" or "tcp:<host:port>").
func (s *Server) Addr() string { return s.addr }

// Token returns the bearer token for TCP listeners, or "" for UDS listeners.
func (s *Server) Token() string { return s.token }

// Env returns the environment variables child processes need to reach this
// server.
func (s *Server) Env() []string {
	env := []string{util.EnvOtelcBuildServer + "=" + s.addr}
	if s.token != "" {
		env = append(env, util.EnvOtelcBuildServerAuth+"="+s.token)
	}
	if s.invocationDir != "" {
		env = append(env, util.EnvOtelcBuildServerDir+"="+s.invocationDir)
	}
	return env
}

// SetResolveEnv sets the process environment used when the server spawns
// `go list -export` subprocesses.
func (s *Server) SetResolveEnv(env []string) {
	s.resolveEnvMu.Lock()
	s.resolveEnv = slices.Clone(env)
	s.resolveEnvMu.Unlock()
}

// Close shuts down the HTTP server, closes its listener, and removes any Unix
// domain socket file.
func (s *Server) Close(ctx context.Context) error {
	s.closeOne.Do(func() {
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		err := s.httpSrv.Shutdown(shutCtx)
		if err != nil {
			err = ex.Join(err, s.httpSrv.Close())
		}
		s.serveWG.Wait()
		switch {
		case s.sockDir != "":
			if rmErr := os.RemoveAll(s.sockDir); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				err = ex.Join(err, rmErr)
			}
		case s.sockPath != "":
			if rmErr := os.Remove(s.sockPath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				err = ex.Join(err, rmErr)
			}
		}
		s.closeErr = err
	})
	return s.closeErr
}

func (s *Server) requireBearerToken(next http.Handler) http.Handler {
	expected := []byte("Bearer " + s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleToolID(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.toolIDBody)
}

func (s *Server) handleCompile(w http.ResponseWriter, r *http.Request) {
	pkg := r.URL.Query().Get("pkg")
	body, ok := s.compileByPkg[pkg]
	if !ok {
		body = s.unmatchedCompileBody
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (s *Server) handleResolveExport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxResolveRequestBytes)
	var req ResolveExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := s.resolveExports(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) effectiveBuildFlags(usePGO bool) []string {
	if !usePGO || s.pgoProfile == "" {
		return slices.Clone(s.baseBuildFlags)
	}
	flags := make([]string, 0, len(s.baseBuildFlags)+1)
	flags = append(flags, s.baseBuildFlags...)
	return append(flags, "-pgo="+s.pgoProfile)
}

func (s *Server) resolveExports(ctx context.Context, req ResolveExportRequest) (*ResolveExportResponse, error) {
	effectiveFlags := s.effectiveBuildFlags(req.UsePGO)
	flagsKey := strings.Join(effectiveFlags, "\x00")
	resp := &ResolveExportResponse{
		Closures: make(map[string]map[string]string, len(req.Imports)),
		Failed:   make(map[string]string),
	}

	if len(req.Imports) == 0 {
		return resp, nil
	}

	misses := s.lookupCache(flagsKey, req.Imports, resp, true)
	if len(misses) == 0 {
		return resp, nil
	}

	var memo *session.Memo
	if s.useSessionMemo {
		memo = session.Open(effectiveFlags)
		misses = s.lookupSessionMemo(flagsKey, misses, memo, resp)
		if len(misses) == 0 {
			return resp, nil
		}
	}

	params := resolveCallParams{
		flagsKey:   flagsKey,
		reqEnv:     s.resolveEnvWithChain(req.ResolvingChain),
		buildFlags: effectiveFlags,
		memo:       memo,
	}

	// For non-nested requests touching any known rule-added import, coalesce
	// concurrent callers into ONE batch `go list -export` scoped to req.Package
	// (when provided) so resolving one package's rule imports never triggers
	// import cycles against another package still being compiled.
	if !req.Nested && s.hasKnownImport(misses) {
		calls, err := s.ensureBatchResolved(ctx, params, req.Package, misses)
		if err != nil {
			return nil, err
		}
		resp.GoListCalls += calls
		misses = s.lookupCache(flagsKey, misses, resp, false)
		if len(misses) == 0 {
			return resp, nil
		}
	}

	// Fallback / nested path: resolve each remaining miss per-import.
	// Nested compiles resolve directly without waiting on inflight calls so a
	// nested `go list` spawned by an outer resolution can never deadlock.
	for _, imp := range misses {
		if req.Nested {
			if err := s.resolveNestedImport(ctx, params, imp, resp); err != nil {
				return nil, err
			}
			continue
		}

		entry, called, err := s.resolveSingleflight(ctx, params, imp)
		if err != nil {
			return nil, err
		}
		if called {
			resp.GoListCalls++
		}
		if entry.failed {
			resp.Failed[imp] = entry.errMsg
		} else {
			resp.Closures[imp] = entry.closure
		}
	}

	return resp, nil
}

func (s *Server) resolveNestedImport(
	ctx context.Context,
	params resolveCallParams,
	imp string,
	resp *ResolveExportResponse,
) error {
	closure, err := s.resolveSingle(ctx, imp, params.reqEnv, params.buildFlags...)
	resp.GoListCalls++
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if pkgload.IsDeterministicLoadFailure(err) {
			s.storeCacheEntry(params.flagsKey, imp, cachedExport{failed: true, errMsg: err.Error()})
			if params.memo != nil {
				params.memo.Store(imp, nil, err)
			}
		}
		resp.Failed[imp] = err.Error()
		return nil
	}
	s.storeCacheEntry(params.flagsKey, imp, cachedExport{closure: closure})
	if params.memo != nil {
		params.memo.Store(imp, closure, nil)
	}
	resp.Closures[imp] = closure
	return nil
}

func (s *Server) lookupCache(
	flagsKey string,
	imports []string,
	resp *ResolveExportResponse,
	countHits bool,
) []string {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()

	byImp := s.exportCache[flagsKey]
	var misses []string
	for _, imp := range imports {
		entry, ok := byImp[imp]
		if !ok {
			misses = append(misses, imp)
			continue
		}
		if entry.failed {
			resp.Failed[imp] = entry.errMsg
		} else {
			resp.Closures[imp] = entry.closure
		}
		if countHits {
			resp.CacheHits++
		}
	}
	return misses
}

func (s *Server) lookupSessionMemo(
	flagsKey string,
	misses []string,
	memo *session.Memo,
	resp *ResolveExportResponse,
) []string {
	if memo == nil {
		return misses
	}
	var remaining []string
	for _, imp := range misses {
		closure, failure, hit := memo.Lookup(imp, nil)
		if !hit {
			remaining = append(remaining, imp)
			continue
		}
		if failure != "" {
			s.storeCacheEntry(flagsKey, imp, cachedExport{failed: true, errMsg: failure})
			resp.Failed[imp] = failure
		} else {
			s.storeCacheEntry(flagsKey, imp, cachedExport{closure: closure})
			resp.Closures[imp] = closure
		}
		resp.CacheHits++
	}
	return remaining
}

func (s *Server) storeCacheEntry(flagsKey, imp string, entry cachedExport) {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	if s.exportCache[flagsKey] == nil {
		s.exportCache[flagsKey] = make(map[string]cachedExport)
	}
	s.exportCache[flagsKey][imp] = entry
}

func (s *Server) hasKnownImport(imports []string) bool {
	if len(s.knownRuleImportSet) == 0 {
		return false
	}
	for _, imp := range imports {
		if _, ok := s.knownRuleImportSet[imp]; ok {
			return true
		}
	}
	return false
}

// ensureBatchResolved runs at most one batch `go list -export` per
// (flagsKey, pkg) covering that package's known rule-added imports (or all
// known rule-added imports when pkg is empty). Concurrent callers wait on the
// same inflight call.
func (s *Server) ensureBatchResolved(
	ctx context.Context,
	params resolveCallParams,
	pkg string,
	misses []string,
) (int, error) {
	flagsKey := params.flagsKey
	batchKey := flagsKey + "\x01" + pkg
	for {
		s.exportMu.Lock()
		if s.batchDone[batchKey] {
			s.exportMu.Unlock()
			return 0, nil
		}
		if done, running := s.batchInflight[batchKey]; running {
			s.exportMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}

		done := make(chan struct{})
		s.batchInflight[batchKey] = done
		toResolve := s.collectUncachedBatchImports(flagsKey, pkg, misses)
		s.exportMu.Unlock()

		var res batchResolveResult
		completed := false
		func() {
			defer func() {
				s.finishBatchResolve(flagsKey, batchKey, done, res, completed && ctx.Err() == nil)
			}()
			if len(toResolve) > 0 {
				res = s.resolveBatchWithSplit(ctx, toResolve, params)
			}
			completed = true
		}()

		if res.err == nil && params.memo != nil {
			storeBatchMemo(params.memo, res.closures, res.failed)
		}
		if ctx.Err() != nil {
			return res.calls, ctx.Err()
		}
		return res.calls, nil
	}
}

// resolveBatchWithSplit runs resolveBatch on batch and, when a load failure
// (such as one unprovided optional module import) aborts the whole batch,
// splits the batch recursively so all valid imports still resolve in O(log N)
// calls instead of falling back to one subprocess per import.
func (s *Server) resolveBatchWithSplit(
	ctx context.Context,
	batch []string,
	params resolveCallParams,
) batchResolveResult {
	closures, failed, err := s.resolveBatch(ctx, batch, params.reqEnv, params.buildFlags...)
	calls := 1
	if err == nil {
		return batchResolveResult{closures: closures, failed: failed, calls: calls}
	}
	if len(batch) <= 1 || ctx.Err() != nil || !pkgload.IsLoadFailure(err) {
		return batchResolveResult{calls: calls, err: err}
	}

	const halves = 2
	mid := len(batch) / halves
	left := s.resolveBatchWithSplit(ctx, batch[:mid], params)
	right := s.resolveBatchWithSplit(ctx, batch[mid:], params)
	calls += left.calls + right.calls

	mergedClosures := make(map[string]map[string]string, len(left.closures)+len(right.closures))
	maps.Copy(mergedClosures, left.closures)
	maps.Copy(mergedClosures, right.closures)
	mergedFailed := make(map[string]error, len(left.failed)+len(right.failed))
	maps.Copy(mergedFailed, left.failed)
	maps.Copy(mergedFailed, right.failed)

	if len(mergedClosures) == 0 && len(mergedFailed) == 0 {
		if left.err != nil {
			return batchResolveResult{calls: calls, err: left.err}
		}
		return batchResolveResult{calls: calls, err: right.err}
	}
	return batchResolveResult{closures: mergedClosures, failed: mergedFailed, calls: calls}
}

func (s *Server) collectUncachedBatchImports(flagsKey, pkg string, misses []string) []string {
	existing := s.exportCache[flagsKey]
	known := s.knownRuleImports
	if pkg != "" {
		known = s.knownRuleImportsByPkg[pkg]
	}
	toResolve := make([]string, 0, len(known)+len(misses))
	seen := make(map[string]struct{}, len(known)+len(misses))
	for _, list := range [2][]string{known, misses} {
		for _, imp := range list {
			if _, cached := existing[imp]; cached {
				continue
			}
			if _, dup := seen[imp]; dup {
				continue
			}
			seen[imp] = struct{}{}
			toResolve = append(toResolve, imp)
		}
	}
	slices.Sort(toResolve)
	return toResolve
}

func (s *Server) finishBatchResolve(
	flagsKey, batchKey string,
	done chan struct{},
	res batchResolveResult,
	markDone bool,
) {
	s.exportMu.Lock()
	defer func() {
		delete(s.batchInflight, batchKey)
		close(done)
		s.exportMu.Unlock()
	}()
	if !markDone {
		return
	}
	if res.err == nil {
		s.cacheBatchResultsLocked(flagsKey, res)
	}
	s.batchDone[batchKey] = true
}

func (s *Server) cacheBatchResultsLocked(flagsKey string, res batchResolveResult) {
	if s.exportCache[flagsKey] == nil {
		s.exportCache[flagsKey] = make(map[string]cachedExport, len(res.closures)+len(res.failed))
	}
	for imp, closure := range res.closures {
		s.exportCache[flagsKey][imp] = cachedExport{closure: closure}
	}
	for imp, loadErr := range res.failed {
		if pkgload.IsDeterministicLoadFailure(loadErr) {
			s.exportCache[flagsKey][imp] = cachedExport{failed: true, errMsg: loadErr.Error()}
		}
	}
}

func storeBatchMemo(memo *session.Memo, closures map[string]map[string]string, failed map[string]error) {
	for imp, closure := range closures {
		memo.Store(imp, closure, nil)
	}
	for imp, loadErr := range failed {
		if pkgload.IsDeterministicLoadFailure(loadErr) {
			memo.Store(imp, nil, loadErr)
		}
	}
}

func (s *Server) resolveSingleflight(
	ctx context.Context,
	params resolveCallParams,
	imp string,
) (cachedExport, bool, error) {
	flagsKey := params.flagsKey
	key := exportKey{flagsKey: flagsKey, importPath: imp}
	for {
		s.exportMu.Lock()
		if entry, cached := s.exportCache[flagsKey][imp]; cached {
			s.exportMu.Unlock()
			return entry, false, nil
		}
		if done, running := s.singleInflight[key]; running {
			s.exportMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return cachedExport{}, false, ctx.Err()
			}
		}
		done := make(chan struct{})
		s.singleInflight[key] = done
		s.exportMu.Unlock()

		var closure map[string]string
		var resolveErr error
		completed := false
		func() {
			defer func() {
				s.finishSingleResolve(key, done, closure, resolveErr, completed && ctx.Err() == nil)
			}()
			closure, resolveErr = s.resolveSingle(ctx, imp, params.reqEnv, params.buildFlags...)
			completed = true
		}()

		if resolveErr != nil {
			if ctx.Err() != nil {
				return cachedExport{}, true, ctx.Err()
			}
			if params.memo != nil && pkgload.IsDeterministicLoadFailure(resolveErr) {
				params.memo.Store(imp, nil, resolveErr)
			}
			return cachedExport{failed: true, errMsg: resolveErr.Error()}, true, nil
		}
		if params.memo != nil {
			params.memo.Store(imp, closure, nil)
		}
		return cachedExport{closure: closure}, true, nil
	}
}

func (s *Server) finishSingleResolve(
	key exportKey,
	done chan struct{},
	closure map[string]string,
	resolveErr error,
	cacheable bool,
) {
	s.exportMu.Lock()
	if cacheable && (resolveErr == nil || pkgload.IsDeterministicLoadFailure(resolveErr)) {
		if s.exportCache[key.flagsKey] == nil {
			s.exportCache[key.flagsKey] = make(map[string]cachedExport)
		}
		if resolveErr != nil {
			s.exportCache[key.flagsKey][key.importPath] = cachedExport{failed: true, errMsg: resolveErr.Error()}
		} else {
			s.exportCache[key.flagsKey][key.importPath] = cachedExport{closure: closure}
		}
	}
	delete(s.singleInflight, key)
	close(done)
	s.exportMu.Unlock()
}

func (s *Server) resolveEnvWithChain(chain string) []string {
	s.resolveEnvMu.RLock()
	base := s.resolveEnv
	s.resolveEnvMu.RUnlock()

	if len(base) == 0 && chain == "" {
		return nil
	}
	env := slices.Clone(base)
	if len(env) == 0 {
		env = os.Environ()
	}
	if chain == "" {
		return env
	}
	prefix := util.EnvOtelcNestedResolving + "="
	env = slices.DeleteFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, prefix)
	})
	return append(env, prefix+chain)
}

func collectRuleImportsByPkg(sets []*rule.InstRuleSet) (map[string][]string, []string) {
	if len(sets) == 0 {
		return nil, nil
	}
	bctx := build.Default
	bctx.BuildTags = util.ActiveBuildTags()

	byPkgSeen := make(map[string]map[string]struct{})
	allSeen := make(map[string]struct{})
	for _, set := range sets {
		if set == nil {
			continue
		}
		pkgSeen := byPkgSeen[set.ModulePath]
		if pkgSeen == nil {
			pkgSeen = make(map[string]struct{})
			byPkgSeen[set.ModulePath] = pkgSeen
		}
		addRuleMapImports(pkgSeen, set.RawRules)
		addRuleMapImports(pkgSeen, set.FuncRules)
		addRuleMapImports(pkgSeen, set.StructRules)
		addRuleMapImports(pkgSeen, set.CallRules)
		addRuleMapImports(pkgSeen, set.LitRules)
		addRuleMapImports(pkgSeen, set.DirectiveRules)
		addRuleMapImports(pkgSeen, set.DeclRules)
		for _, fr := range set.FileRules {
			for _, imp := range fr.Imports {
				addCandidateImport(pkgSeen, imp)
			}
			for _, imp := range parseFileRuleImports(&bctx, fr) {
				addCandidateImport(pkgSeen, imp)
			}
		}
		maps.Copy(allSeen, pkgSeen)
	}
	if len(allSeen) == 0 {
		return nil, nil
	}
	byPkg := make(map[string][]string, len(byPkgSeen))
	for pkg, seen := range byPkgSeen {
		if len(seen) == 0 {
			continue
		}
		imps := slices.Collect(maps.Keys(seen))
		slices.Sort(imps)
		byPkg[pkg] = imps
	}
	allImports := slices.Collect(maps.Keys(allSeen))
	slices.Sort(allImports)
	return byPkg, allImports
}

func addRuleMapImports[T interface{ Base() *rule.InstBaseRule }](
	dst map[string]struct{},
	rules map[string][]T,
) {
	for _, list := range rules {
		for _, r := range list {
			for _, imp := range r.Base().Imports {
				addCandidateImport(dst, imp)
			}
		}
	}
}

func addCandidateImport(dst map[string]struct{}, imp string) {
	if imp == "" || imp == "unsafe" || imp == "C" {
		return
	}
	dst[imp] = struct{}{}
}

func parseFileRuleImports(bctx *build.Context, fr *rule.InstFileRule) []string {
	if fr == nil || fr.Path == util.InternalPath || fr.ResolvedPath == "" || fr.File == "" {
		return nil
	}
	path := filepath.Join(fr.ResolvedPath, fr.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	match, err := ast.MatchFileConstraints(bctx, fr.ResolvedPath, fr.File, data)
	if err != nil || !match {
		return nil
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(parsed.Imports))
	for _, imp := range parsed.Imports {
		if unquoted, unqErr := strconv.Unquote(imp.Path.Value); unqErr == nil {
			out = append(out, unquoted)
		}
	}
	return out
}
