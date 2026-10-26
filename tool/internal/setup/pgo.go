// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

const (
	flagPGO    = "-pgo"
	goflagsEnv = "GOFLAGS"
)

// pgoProfile returns the PGO profile a build with the -pgo value applies to
// every package of pkgs, for the nested builds toolexec runs: an explicit -pgo
// file as given, or with -pgo=auto the default.pgo of the main packages. The
// bool reports that main packages have different default.pgo files; cmd/go then
// compiles shared packages once per profile, and a nested build can follow only
// one.
func pgoProfile(value string, pkgs []*packages.Package) (string, bool) {
	switch value {
	case "off":
		return "", false
	case "auto":
	default:
		return value, false
	}

	profile := ""
	for _, pkg := range pkgs {
		if pkg.Name != "main" {
			continue
		}
		dir := pkgload.PackageDir(pkg)
		if dir == "" {
			continue
		}
		path, err := filepath.Abs(filepath.Join(dir, "default.pgo"))
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if profile != "" && profile != path {
			return "", true
		}
		profile = path
	}
	return profile, false
}

// pgoValue returns the effective -pgo value: the last one on the command line,
// else the last one in GOFLAGS, else auto, as in cmd/go.
func pgoValue(subcommand string, args []string) string {
	value := "auto"
	for _, token := range util.SplitGoflags(os.Getenv(goflagsEnv)) {
		name, v, ok := strings.Cut(util.UnquoteGoflagsToken(token), "=")
		if ok && flagName(name) == flagPGO {
			value = v
		}
	}
	for _, arg := range classifyArgs(subcommand, args) {
		if arg.FlagName == flagPGO &&
			((arg.Kind == ArgBuildFlag && arg.HasValue) || arg.Kind == ArgBuildFlagValue) {
			value = arg.Value
		}
	}
	return value
}
