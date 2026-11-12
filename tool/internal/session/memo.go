// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/util"
)

// exportsDir is the session subdirectory that holds the export memo.
const exportsDir = "exports"

// maxLockNameLen bounds the length of the scope part of a lock file name.
const maxLockNameLen = 32

// Memo caches, per root import path, the closure of export files a nested
// `go list -export` resolved for it, so concurrent and repeated builds of one
// session stop resolving the same rule-added imports again. Entries are keyed
// by the resolution scope (the build inputs setup recorded) and the build
// flags, and validated against the importcfg before every use. A nil *Memo is
// valid and caches nothing.
type Memo struct {
	dir   string
	scope string
	flags string
}

// entry is one memoized resolution: the root's closure, or the error that
// made the root unresolvable.
type entry struct {
	Closure map[string]string `json:"closure,omitempty"`
	Failed  string            `json:"failed,omitempty"`
}

// Open returns the memo of this process's session for the resolution scope
// setup recorded and the given build flags. It returns nil, which disables
// the memo, when the session is off or no scope was recorded.
func Open(buildFlags []string) *Memo {
	dir := util.GetSessionDir()
	if dir == "" || dir == util.SessionOff {
		return nil
	}
	scope, ok := currentScope()
	if !ok {
		return nil
	}
	return &Memo{
		dir:   filepath.Join(dir, exportsDir),
		scope: scope,
		flags: flagsDigest(buildFlags),
	}
}

// currentScope returns the resolution scope digest when the scope file was
// written by the latest setup. Setup records a fresh generation token before
// it writes matched.json and appends that token to the scope it writes after,
// so a scope whose token differs, or has none while a token exists, is one an
// earlier setup left behind because this setup could neither replace nor
// remove it. No file times are compared, so coarse or skewed timestamps
// cannot make a stale scope look current.
func currentScope() (string, bool) {
	content, err := os.ReadFile(util.GetResolutionScopeFile())
	if err != nil {
		return "", false
	}
	digest, token, _ := strings.Cut(strings.TrimSpace(string(content)), " ")
	if digest == "" {
		return "", false
	}
	generation, err := os.ReadFile(util.GetSetupGenerationFile())
	switch {
	case err == nil:
		if want := strings.TrimSpace(string(generation)); want == "" || token != want {
			return "", false
		}
	case errors.Is(err, fs.ErrNotExist):
		// No setup recorded a generation here; a scope that claims one is
		// stale, and a bare scope is trusted, as tests that record only a
		// scope expect.
		if token != "" {
			return "", false
		}
	default:
		return "", false
	}
	return digest, true
}

// buildEnvVars are the environment variables that change what a nested
// go list resolves. They are hashed when the memo is opened, not only when
// setup records the scope: in the bare -toolexec flow the session outlives
// one build, and setup may be skipped, so a later build for another target
// would otherwise read entries resolved for the first.
func buildEnvVars() []string {
	return []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOTOOLCHAIN", "GOWORK"}
}

// flagsDigest hashes the build environment, the build flags, plus the content of any PGO profile they
// name, so an edited profile never reuses closures resolved with the old one.
func flagsDigest(buildFlags []string) string {
	h := sha256.New()
	for _, name := range buildEnvVars() {
		_, _ = h.Write([]byte(name + "=" + os.Getenv(name) + "\x00"))
	}
	for _, flag := range buildFlags {
		_, _ = h.Write([]byte(flag))
		_, _ = h.Write([]byte{0})
		if profile, ok := strings.CutPrefix(flag, "-pgo="); ok && profile != "off" && profile != "auto" {
			if content, err := os.ReadFile(profile); err == nil {
				sum := sha256.Sum256(content)
				_, _ = h.Write(sum[:])
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (m *Memo) entryPath(root string) string {
	sum := sha256.Sum256([]byte(m.scope + "\x00" + m.flags + "\x00" + root))
	return filepath.Join(m.dir, hex.EncodeToString(sum[:16])+".json")
}

// Lookup returns the memoized resolution of root. On a hit it returns the
// root's closure, or the recorded failure, with ok set. An entry is a miss
// when it cannot be read, when one of its packages that importcfg already
// lists maps to another archive (it was resolved against other deps), or
// when an archive importcfg does not list is gone.
func (m *Memo) Lookup(root string, importcfg map[string]string) (map[string]string, string, bool) {
	if m == nil {
		return nil, "", false
	}
	data, err := os.ReadFile(m.entryPath(root))
	if err != nil {
		return nil, "", false
	}
	var e entry
	if err = json.Unmarshal(data, &e); err != nil {
		return nil, "", false
	}
	if e.Failed != "" {
		return nil, e.Failed, true
	}
	if len(e.Closure) == 0 {
		return nil, "", false
	}
	for pkg, archive := range e.Closure {
		listed, ok := importcfg[pkg]
		if !ok {
			if _, statErr := os.Stat(archive); statErr != nil {
				return nil, "", false
			}
			continue
		}
		if listed != archive {
			return nil, "", false
		}
	}
	return e.Closure, "", true
}

// Store records the resolution of root: its closure, or failure when it was
// unresolvable. Concurrent writers produce the same content, so a failed
// rename, such as Windows refusing to replace an open file, is ignored.
func (m *Memo) Store(root string, closure map[string]string, failure error) {
	if m == nil {
		return
	}
	e := entry{Closure: closure}
	if failure != nil {
		e = entry{Failed: failure.Error()}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err = os.MkdirAll(m.dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(m.dir, ".entry-*")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err = os.Rename(tmp.Name(), m.entryPath(root)); err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// Lock takes the memo's single-flight lock for this scope and build flags, so concurrent
// links resolve a miss once and the others read the result. Callers re-check
// the memo after taking it. The wait has no deadline; ctx cancels it.
func (m *Memo) Lock(ctx context.Context) (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	return lockPath(ctx, m.lockPath(), nil)
}

// lockPath names the single-flight lock after the scope and the flags
// digest, the same key the entries use, so links with other build flags
// never wait on each other.
func (m *Memo) lockPath() string {
	return filepath.Join(m.dir, m.scope[:min(len(m.scope), maxLockNameLen)]+"-"+m.flags+".lock")
}
