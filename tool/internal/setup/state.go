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
	"slices"
	"sort"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	stateDir      = "state"
	stateFileName = "state.json"
	goWorkOff     = "off"
	// goWorkSumFileName is the checksum file of a workspace.
	goWorkSumFileName = "go.work.sum"
)

// stateManager tracks the original state of files so they can later be restored.
//
// Files that exist when tracked are snapshotted into the build state directory.
// A symbolic link is snapshotted as a symbolic link with the same target.
// Files that do not exist when tracked are recorded and removed during Revert if
// they are later created.
//
// stateManager is not safe for concurrent use.
type stateManager struct {
	files map[string]bool // true = existed when tracked
}

// newStateManager returns an empty stateManager.
func newStateManager() *stateManager {
	return &stateManager{
		files: make(map[string]bool),
	}
}

// loadStateManager loads a previously committed stateManager from disk.
//
// If no state has been committed, it returns (nil, nil).
//
//nolint:nilnil // nil is returned when the state file does not exist
func loadStateManager() (*stateManager, error) {
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

	// The manifest is a file in the project, so it is not trusted: a path
	// in it must be inside the project, else Revert can remove or change any
	// file of the user.
	scope := newStateScope()
	s := newStateManager()
	for _, entry := range entries {
		path, removed := strings.CutPrefix(entry, "-")
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, ex.Newf("the state file %s has an invalid path %q; "+
				"remove %s if otelc did not make it", f, path, util.GetBuildTempDir())
		}
		if scope.inBuildTemp(path) {
			return nil, ex.Newf("the state file %s has the path %s, "+
				"which is in the otelc build directory: otelc never tracks such a path; "+
				"remove %s", f, path, util.GetBuildTempDir())
		}
		if !scope.allows(path) {
			return nil, ex.Newf("the state file %s has the path %s, "+
				"which is outside the otelc work directory, and is not a go.mod, go.sum, "+
				"tool or vendor file of its module. "+
				"otelc restores such a path only in the build that changed it. "+
				"If a build stopped, restore %s by hand (the original files are in %s), "+
				"then remove %s", f, path, path, util.GetBuildTemp(stateDir), util.GetBuildTempDir())
		}
		s.files[path] = !removed
	}

	return s, nil
}

// stateScope tells which paths a manifest on disk can name:
//   - the paths in the otelc work directory, but not the paths in the build
//     temp directory (.otelc-build);
//   - in the module root (the nearest directory at or above the work
//     directory with a go.mod file), only the paths that otelc tracks there:
//     go.mod, go.sum, the tool files, and the paths in vendor/;
//   - the go.work.sum file of the active workspace.
//
// Each path has the form with and without symbolic links resolved.
type stateScope struct {
	// roots are the work directory: all the paths in it are in the scope.
	roots []string
	// moduleRoots are the module root, when it is not the work directory.
	moduleRoots []string
	// buildTemp is the build temp directory.
	buildTemp []string
	files     map[string]bool
}

// newStateScope returns the stateScope of the current otelc work directory.
//
// rootOf gives the work directory for a path in it, before the module root.
// Thus checkNoLinkedParent does not examine the parent directories of the
// work directory: the user selects them, and they can be symbolic links.
func newStateScope() stateScope {
	scope := stateScope{files: make(map[string]bool)}
	workDir, err := filepath.Abs(util.GetOtelcWorkDir())
	if err != nil {
		return scope
	}
	scope.roots = pathForms(workDir)
	scope.buildTemp = pathForms(filepath.Join(workDir, util.BuildTempDir))
	// otelc can run in a package directory below the module root (for
	// example cmd/app). Setup then changes go.mod, go.sum and vendor/ at the
	// module root, so the manifest names these paths.
	if modDir, found := enclosingModuleDir(workDir); found && modDir != workDir {
		scope.moduleRoots = pathForms(modDir)
	}
	if ws := workspaceDir(workDir); ws != "" {
		for _, dir := range pathForms(ws) {
			scope.files[filepath.Join(dir, goWorkSumFileName)] = true
		}
	}
	return scope
}

// pathForms returns the clean path, and the path with symbolic links
// resolved when it is different.
func pathForms(path string) []string {
	path = filepath.Clean(path)
	forms := []string{path}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		forms = append(forms, resolved)
	}
	return forms
}

// allows reports whether a manifest on disk can name path.
func (sc stateScope) allows(path string) bool {
	if sc.inBuildTemp(path) {
		return false
	}
	_, inRoot := sc.rootOf(path)
	return inRoot || sc.files[path]
}

// inBuildTemp reports whether path is the build temp directory or a path in
// it. otelc never tracks such a path.
func (sc stateScope) inBuildTemp(path string) bool {
	for _, dir := range sc.buildTemp {
		if checkInside(dir, path) == nil {
			return true
		}
	}
	return false
}

// rootOf returns the root that has path inside it: the work directory, or
// the module root for a path that otelc tracks in the module root (see
// isModuleRootPath).
func (sc stateScope) rootOf(path string) (string, bool) {
	for _, root := range sc.roots {
		if checkInside(root, path) == nil {
			return root, true
		}
	}
	for _, root := range sc.moduleRoots {
		if checkInside(root, path) != nil {
			continue
		}
		if rel, err := filepath.Rel(root, path); err == nil && isModuleRootPath(rel) {
			return root, true
		}
	}
	return "", false
}

// isModuleRootPath reports whether rel (relative to the module root) is a
// path that otelc tracks in the module root: go.mod, go.sum, a tool file,
// or vendor/ and the paths in it.
func isModuleRootPath(rel string) bool {
	switch rel {
	case goModFileName, goSumFileName, toolFileCanonical, toolFileAlias, vendorDirName:
		return true
	}
	return strings.HasPrefix(rel, vendorDirName+string(filepath.Separator))
}

// workspaceDir returns the directory of the go.work file for workDir, or "".
// GOWORK in the environment wins. Else the go.work file is the first one in
// workDir or its parent directories, as for the go command.
func workspaceDir(workDir string) string {
	if gowork := os.Getenv("GOWORK"); gowork != "" {
		if gowork == goWorkOff {
			return ""
		}
		abs, err := filepath.Abs(gowork)
		if err != nil {
			return ""
		}
		return filepath.Dir(abs)
	}
	for dir := workDir; ; dir = filepath.Dir(dir) {
		if lexists(filepath.Join(dir, "go.work")) {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

type stateManagerKey struct{}

// contextWithStateManager returns a copy of ctx containing s.
func contextWithStateManager(ctx context.Context, s *stateManager) context.Context {
	return context.WithValue(ctx, stateManagerKey{}, s)
}

// stateManagerFromContext returns the stateManager stored in ctx.
//
// If ctx does not contain a stateManager, a new empty stateManager is returned
// along with false.
func stateManagerFromContext(ctx context.Context) (*stateManager, bool) {
	s, ok := ctx.Value(stateManagerKey{}).(*stateManager)
	if !ok {
		return newStateManager(), false
	}
	return s, true
}

func getBackupFiles(ctx context.Context, moduleDirs []string) ([]string, error) {
	var files []string

	dirs := normalizeModuleDirs(moduleDirs)
	// Find all go.mod, go.sum, and tool files
	for _, moduleDir := range dirs {
		goModFile := filepath.Join(moduleDir, goModFileName)
		goSumFile := filepath.Join(moduleDir, "go.sum")
		canonical := filepath.Join(moduleDir, toolFileCanonical)
		alias := filepath.Join(moduleDir, toolFileAlias)

		if util.PathExists(goModFile) {
			files = append(files, goModFile)
			files = append(files, goSumFile)

			// If otelc.tool.go exists, use it (it may get modified)
			// Otherwise, use the canonical path (it may get generated or modified)
			toolFile := canonical
			if !util.PathExists(canonical) && util.PathExists(alias) {
				toolFile = alias
			}

			files = append(files, toolFile)
		}
	}

	// Find go.work.sum if go.work exists
	goWorkCmd := exec.CommandContext(ctx, "go", "env", "GOWORK")
	goWorkOutput, err := goWorkCmd.Output()
	if err != nil {
		return nil, ex.Wrapf(err, "failed to get GOWORK environment variable")
	}
	goWorkPath := strings.TrimSpace(string(goWorkOutput))
	if goWorkPath != "" && goWorkPath != goWorkOff {
		goWorkSumPath := filepath.Join(filepath.Dir(goWorkPath), goWorkSumFileName)
		files = append(files, goWorkSumPath)
	}

	return files, nil
}

func stateSnapshotPath(path string) string {
	p := filepath.Clean(path)
	sum := sha256.Sum256([]byte(p))
	return filepath.Base(p) + "." + hex.EncodeToString(sum[:])
}

// TrackAll calls Track for each path. It persists the state to disk one time,
// after all the paths are recorded, so it is fast for many paths.
func (s *stateManager) TrackAll(paths ...string) error {
	var err error
	changed := false
	for _, path := range paths {
		added, trackErr := s.track(path)
		err = ex.Join(err, trackErr)
		changed = changed || added
	}
	if changed {
		err = ex.Join(err, s.Commit())
	}
	return err
}

// Track records the current state of path.
//
// If path exists, it is snapshotted and will be restored by Revert.
// If path does not exist, it is recorded so Revert will remove it if it is
// later created.
//
// The updated state is persisted to disk before Track returns, so a build
// killed at any later point (SIGKILL, OOM, CI cancellation) leaves behind a
// manifest from which `otelc cleanup` or the next build can restore the tree.
//
// Duplicate calls for the same path are ignored.
func (s *stateManager) Track(path string) error {
	added, err := s.track(path)
	if err != nil || !added {
		return err
	}
	return s.Commit()
}

// track records the current state of path, but does not persist it. It
// reports whether path was not tracked before.
func (s *stateManager) track(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, ex.Wrapf(err, "failed to get absolute path for %s", path)
	}

	abs = filepath.Clean(abs)
	if _, ok := s.files[abs]; ok {
		return false, nil
	}

	// If the file doesn't exist, mark it for removal. A path that does not
	// exist can also be a directory that is created later: Revert removes it
	// when it is empty.
	// A symbolic link counts as existing, also when its target does not
	// exist: Revert must not remove a link that was there before.
	if !lexists(abs) {
		s.files[abs] = false
		return true, nil
	}

	// If the file exists, snapshot it. The snapshot replaces a snapshot
	// that a previous build left, also when that snapshot is read-only.
	dst := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(abs))
	if err = snapshotPath(abs, dst); err != nil {
		return false, err
	}

	s.files[abs] = true
	return true, nil
}

// snapshotPath copies path to the snapshot dst. When path is a symbolic
// link, the snapshot is a symbolic link with the same target. When otelc
// cannot make a symbolic link (for example on Windows without the necessary
// privilege), the snapshot is a copy of the contents of the target.
func snapshotPath(path, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return ex.Wrapf(err, "creating the directory of the snapshot %s", dst)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ex.Wrapf(err, "reading %s", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if target, linkErr := os.Readlink(path); linkErr == nil {
			if linkErr = replaceWithSymlink(target, dst); linkErr == nil {
				return nil
			}
		}
	}
	return replaceWithCopy(path, dst)
}

// replaceWithCopy replaces dst atomically with a copy of the contents and
// the permissions of src. It reads src through a symbolic link. When dst is
// a symbolic link, the link is replaced, and its target does not change.
// When dst is read-only, it is replaced too.
func replaceWithCopy(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return ex.Wrapf(err, "reading %s", src)
	}
	if !info.Mode().IsRegular() {
		return ex.Newf("%s is not a regular file", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return ex.Wrapf(err, "reading %s", src)
	}
	return util.WriteFileAtomic(dst, data, info.Mode().Perm())
}

// replaceWithSymlink replaces path atomically with a symbolic link to
// target. It makes the link at a temporary name in the directory of path,
// and then renames it to path. When path is a symbolic link, the link is
// replaced, and its target does not change.
func replaceWithSymlink(target, path string) error {
	dir := filepath.Dir(path)
	// os.CreateTemp gives a name that is not used. The file is removed, and
	// the link is made with this name.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".link-*")
	if err != nil {
		return ex.Wrapf(err, "creating a temporary file for %s", path)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	if err = os.Remove(tmpPath); err != nil {
		return ex.Wrapf(err, "removing the temporary file %s", tmpPath)
	}
	if err = os.Symlink(target, tmpPath); err != nil {
		return ex.Wrapf(err, "creating a symbolic link for %s", path)
	}
	if err = util.RenameReplace(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return ex.Wrapf(err, "replacing %s with a symbolic link", path)
	}
	return nil
}

// Commit persists the tracked state to disk so it can be restored by a future
// process. The manifest is written atomically (temp file + rename) so a crash
// mid-write never leaves a truncated manifest behind.
//
// Track calls Commit automatically; calling it directly is only needed to
// re-persist after external changes.
func (s *stateManager) Commit() error {
	if len(s.files) == 0 {
		return nil
	}

	entries := make([]string, 0, len(s.files))

	for path, exists := range s.files {
		if exists {
			entries = append(entries, path)
		} else {
			entries = append(entries, "-"+path)
		}
	}

	// Sort the entries for deterministic behavior
	sort.Strings(entries)

	bs, err := json.Marshal(entries)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal state to JSON")
	}

	f := util.GetBuildTemp(stateFileName)
	if err = os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return ex.Wrapf(err, "failed to create build temp directory")
	}

	return util.WriteFileAtomic(f, bs)
}

// Discard removes the persisted manifest and snapshots. Call it only after a
// successful Revert: the state is consumed, and leaving it behind would let a
// later `otelc cleanup` re-apply snapshots from a finished build.
//
// The manifest is removed first. If this fails, the snapshots stay, so the
// state on disk stays complete. Snapshots without a manifest are not used.
func (s *stateManager) Discard() error {
	if len(s.files) == 0 {
		return nil
	}

	if rmErr := os.Remove(util.GetBuildTemp(stateFileName)); rmErr != nil && !os.IsNotExist(rmErr) {
		return ex.Wrapf(rmErr, "removing the state file")
	}
	if rmErr := os.RemoveAll(util.GetBuildTemp(stateDir)); rmErr != nil {
		return ex.Wrapf(rmErr, "removing the snapshots")
	}
	return nil
}

// Revert restores all tracked files to the state they were in when tracked.
//
// Files that originally existed are restored from their snapshots. Files that
// did not exist when tracked are removed if they exist. A path that was a
// symbolic link when tracked is a symbolic link again, with the same target.
// A restored file gets the permissions of its snapshot, also when the file
// is read-only.
//
// Revert never writes or removes through a symbolic link: a tracked path
// that is a symbolic link now is replaced (restore) or removed itself, and a
// path below a tracked directory that is a symbolic link now is not changed.
func (s *stateManager) Revert() error {
	if len(s.files) == 0 {
		return nil
	}

	var err error

	stateDir := util.GetBuildTemp(stateDir)

	scope := newStateScope()
	// Restore the paths in sorted order: a directory comes before the paths
	// in it. Thus, when a snapshot makes a directory a symbolic link again,
	// the check of a path in it sees the link.
	paths := make([]string, 0, len(s.files))
	for path := range s.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	created := make([]string, 0, len(s.files))
	for _, path := range paths {
		if !s.files[path] {
			created = append(created, path)
			continue
		}
		if linkErr := s.checkNoLinkedParent(scope, path); linkErr != nil {
			err = ex.Join(err, linkErr)
			continue
		}

		src := filepath.Join(stateDir, stateSnapshotPath(path))
		err = ex.Join(err, restoreSnapshot(src, path))
	}

	// Remove the created paths in reverse order: a path inside a directory
	// sorts after the directory, so the files go before their directories.
	// Each path is examined immediately before its removal, after all the
	// restores: a restored symbolic link can be a parent directory of the
	// path now, and otelc must not remove a file at the target of the link.
	slices.Reverse(created)
	for _, path := range created {
		if linkErr := s.checkNoLinkedParent(scope, path); linkErr != nil {
			err = ex.Join(err, linkErr)
			continue
		}
		err = ex.Join(err, removeCreated(path))
	}

	return err
}

// checkNoLinkedParent returns an error when a parent directory of path is a
// symbolic link now: a change below this directory changes files at the
// target of the link. For a path in the otelc work directory or in its
// module root (see stateScope), all the parent directories below this root
// are checked. The go.work.sum file of the workspace is
// in a directory of the user, and only the file itself is replaced (see
// restoreSnapshot). For a different path (only in the build that tracked
// it), the parent directories that are tracked are checked.
func (s *stateManager) checkNoLinkedParent(scope stateScope, path string) error {
	if root, ok := scope.rootOf(path); ok {
		if err := checkNoSymlink(root, filepath.Dir(path)); err != nil {
			return ex.Wrapf(err, "cannot restore %s", path)
		}
		return nil
	}
	if scope.files[path] {
		return nil
	}
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, tracked := s.files[dir]; !tracked {
			continue
		}
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return ex.Newf("cannot restore %s: its directory %s is a symbolic link now", path, dir)
		}
	}
	return nil
}

// restoreSnapshot replaces path atomically with the snapshot src. When the
// snapshot is a symbolic link, path becomes a symbolic link with the same
// target. Else path becomes a regular file with the contents and the
// permissions of the snapshot, also when path is read-only now.
//
// The snapshot is not trusted (see loadStateManager), but the caller makes
// sure that path is in the scope. When path is a symbolic link now, otelc
// does not write through it: the link is replaced, and its target does not
// change.
func restoreSnapshot(src, path string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return ex.Wrapf(err, "reading the snapshot of %s", path)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ex.Wrapf(err, "creating the directory of %s", path)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		var target string
		if target, err = os.Readlink(src); err == nil {
			err = replaceWithSymlink(target, path)
		}
	} else {
		err = replaceWithCopy(src, path)
	}
	if err != nil {
		if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return ex.Wrapf(err, "replacing the symbolic link %s", path)
		}
		return ex.Wrapf(err, "restoring %s", path)
	}
	return nil
}

// removeCreated removes a file or a directory that did not exist when it was
// tracked. A directory that is not empty is kept: it has files that the state
// manager does not know about. A symbolic link is removed, not its target.
func removeCreated(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return ex.Wrap(err)
	}
	if info.IsDir() {
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return ex.Wrap(readErr)
		}
		if len(entries) > 0 {
			return nil
		}
	}
	if rmErr := os.Remove(path); rmErr != nil {
		return ex.Wrap(rmErr)
	}
	return nil
}
