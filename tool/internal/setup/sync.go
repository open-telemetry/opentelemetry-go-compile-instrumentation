// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"golang.org/x/mod/modfile"
)

func parseGoMod(gomod string) (*modfile.File, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil, ex.Errorf(err, "failed to read go.mod file")
	}
	modFile, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return nil, ex.Errorf(err, "failed to parse go.mod file")
	}
	return modFile, nil
}

func writeGoMod(gomod string, modfile *modfile.File) error {
	data, err := modfile.Format()
	if err != nil {
		return ex.Errorf(err, "failed to format go.mod file")
	}
	err = os.WriteFile(gomod, data, 0o644) //nolint:gosec // 0644 is ok
	if err != nil {
		return ex.Errorf(err, "failed to write go.mod file")
	}
	return nil
}

func runModTidy() error {
	err := util.RunCmd("go", "mod", "tidy")
	if err != nil {
		return ex.Errorf(err, "failed to run go mod tidy")
	}
	return nil
}

func addReplace(modfile *modfile.File, path, version, rpath, rversion string) (bool, error) {
	hasReplace := false
	for _, r := range modfile.Replace {
		if r.Old.Path == path {
			hasReplace = true
			break
		}
	}
	if !hasReplace {
		err := modfile.AddReplace(path, version, rpath, rversion)
		if err != nil {
			return false, ex.Errorf(err, "failed to add replace directive")
		}
		return true, nil
	}
	return false, nil
}

func (sp *SetupPhase) syncDeps(matched []*rule.InstRule) error {
	const goModFile = "go.mod"
	modfile, err := parseGoMod(goModFile)
	if err != nil {
		return ex.Error(err)
	}
	changed := false
	// Add matched dependencies to go.mod
	for _, m := range matched {
		util.Assert(strings.HasPrefix(m.Path, util.OtelRoot), "sanity check")
		// TODO: Since we haven't published the instrumentation packages yet,
		// we need to add the replace directive to the local path.
		// Once the instrumentation packages are published, we can remove this.
		replacePath := m.Path
		replacePath = strings.TrimPrefix(replacePath, util.OtelRoot)
		replacePath = filepath.Join("..", replacePath)
		added, addErr := addReplace(modfile, m.Path, "", replacePath, "")
		if addErr != nil {
			return ex.Error(addErr)
		}
		changed = changed || added
		if changed {
			sp.Info("Synced dependency", "dep", m.String())
		}
	}
	// TODO: Since we haven't published the pkg packages yet, we need to add the
	// replace directive to the local path. Once the pkg packages are published,
	// we can remove this.
	// Add special pkg module to go.mod
	added, addErr := addReplace(modfile, util.OtelRoot+"/pkg", "", "../pkg", "")
	if addErr != nil {
		return ex.Error(addErr)
	}
	changed = changed || added
	if changed {
		sp.Info("Synced dependency", "dep", "pkg")
	}
	if changed {
		err = writeGoMod(goModFile, modfile)
		if err != nil {
			return ex.Errorf(err, "failed to write go.mod file")
		}
		err = runModTidy()
		if err != nil {
			return ex.Errorf(err, "failed to run go mod tidy")
		}
		sp.recordModified(goModFile)
	}
	return nil
}
