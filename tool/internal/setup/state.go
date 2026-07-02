// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	stateDir      = "state"
	stateFileName = "state.json"
)

// StateManager tracks the original state of files so they can later be restored.
//
// Files that exist when tracked are snapshotted into the build state directory.
// Files that do not exist when tracked are recorded and removed during Revert if
// they are later created.
//
// StateManager is not safe for concurrent use.
type StateManager struct {
	files map[string]bool // true = existed when tracked
}

// NewStateManager returns an empty StateManager.
func NewStateManager() *StateManager {
	return &StateManager{
		files: make(map[string]bool),
	}
}

// LoadStateManager loads a previously committed StateManager from disk.
//
// If no state has been committed, it returns (nil, nil).
//
//nolint:nilnil // nil is returned when the state file does not exist
func LoadStateManager() (*StateManager, error) {
	f := util.GetBuildTemp(stateFileName)
	if !util.PathExists(f) {
		return nil, nil
	}

	file, err := os.Open(f)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to open state file %s", f)
	}
	defer file.Close()

	var entries []string
	if err = json.NewDecoder(file).Decode(&entries); err != nil {
		return nil, ex.Wrapf(err, "failed to decode state JSON from file %s", f)
	}

	s := NewStateManager()
	for _, entry := range entries {
		if e, ok := strings.CutPrefix(entry, "-"); ok {
			s.files[e] = false
		} else {
			s.files[entry] = true
		}
	}

	return s, nil
}

type stateManagerKey struct{}

// ContextWithStateManager returns a copy of ctx containing s.
func ContextWithStateManager(ctx context.Context, s *StateManager) context.Context {
	return context.WithValue(ctx, stateManagerKey{}, s)
}

// StateManagerFromContext returns the StateManager stored in ctx.
//
// If ctx does not contain a StateManager, a new empty StateManager is returned
// along with false.
func StateManagerFromContext(ctx context.Context) (*StateManager, bool) {
	s, ok := ctx.Value(stateManagerKey{}).(*StateManager)
	if !ok {
		return NewStateManager(), false
	}
	return s, true
}

func getBackupFiles(ctx context.Context, moduleDirs map[string]bool) ([]string, error) {
	var files []string

	// Find all go.mod and go.sum files
	for moduleDir := range moduleDirs {
		goModFile := filepath.Join(moduleDir, "go.mod")
		goSumFile := filepath.Join(moduleDir, "go.sum")
		if util.PathExists(goModFile) {
			files = append(files, goModFile)
			files = append(files, goSumFile)
		}
	}

	// Find go.work.sum if go.work exists
	goWorkCmd := exec.CommandContext(ctx, "go", "env", "GOWORK")
	goWorkOutput, err := goWorkCmd.Output()
	if err != nil {
		return nil, ex.Wrapf(err, "failed to get GOWORK environment variable")
	}
	goWorkPath := strings.TrimSpace(string(goWorkOutput))
	if goWorkPath != "" {
		goWorkSumPath := filepath.Join(filepath.Dir(goWorkPath), "go.work.sum")
		files = append(files, goWorkSumPath)
	}

	return files, nil
}

func stateSnapshotPath(path string) string {
	p := filepath.Clean(path)
	sum := sha256.Sum256([]byte(p))
	return filepath.Base(p) + "." + hex.EncodeToString(sum[:])
}

// TrackAll calls Track for each path.
func (s *StateManager) TrackAll(paths ...string) error {
	var err error
	for _, path := range paths {
		err = ex.Join(err, s.Track(path))
	}
	return err
}

// Track records the current state of path.
//
// If path exists, it is snapshotted and will be restored by Revert.
// If path does not exist, it is recorded so Revert will remove it if it is
// later created.
//
// Duplicate calls for the same path are ignored.
func (s *StateManager) Track(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ex.Wrapf(err, "failed to get absolute path for %s", path)
	}

	abs = filepath.Clean(abs)
	if _, ok := s.files[abs]; ok {
		return nil
	}

	// If the file doesn't exist, mark it for removal
	if !util.PathExists(abs) {
		s.files[abs] = false
		return nil
	}

	// If the file exists, snapshot it
	dst := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(abs))
	if err = util.CopyFile(abs, dst); err != nil {
		return ex.Wrapf(err, "failed to snapshot %s", abs)
	}

	s.files[abs] = true
	return nil
}

// Commit persists the tracked state to disk so it can be restored by a future
// process.
func (s *StateManager) Commit() error {
	entries := make([]string, 0, len(s.files))

	for path, exists := range s.files {
		if exists {
			entries = append(entries, path)
		} else {
			entries = append(entries, "-"+path)
		}
	}

	f := util.GetBuildTemp(stateFileName)
	file, err := os.Create(f)
	if err != nil {
		return ex.Wrapf(err, "failed to create file %s", f)
	}
	defer file.Close()

	bs, err := json.Marshal(entries)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal state to JSON")
	}

	if _, err = file.Write(bs); err != nil {
		return ex.Wrapf(err, "failed to write JSON to file %s", f)
	}

	return nil
}

// Revert restores all tracked files to the state they were in when tracked.
//
// Files that originally existed are restored from their snapshots. Files that
// did not exist when tracked are removed if they exist.
func (s *StateManager) Revert() error {
	var err error

	stateDir := util.GetBuildTemp(stateDir)

	for path, existed := range s.files {
		if !existed {
			if util.PathExists(path) {
				err = ex.Join(err, os.Remove(path))
			}
			continue
		}

		src := filepath.Join(stateDir, stateSnapshotPath(path))
		err = ex.Join(err, util.CopyFile(src, path))
	}

	return err
}
