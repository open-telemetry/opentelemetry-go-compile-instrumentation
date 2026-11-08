// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestPGOProfile(t *testing.T) {
	const explicitProfile = "prof.pgo"
	const explicitFlag = "-pgo=" + explicitProfile
	const goflags = "-pgo=g.pgo"
	const defaultProfile = "default.pgo"
	withProfile := t.TempDir()
	otherProfile := t.TempDir()
	withoutProfile := t.TempDir()
	for _, dir := range []string{withProfile, otherProfile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, defaultProfile), nil, 0o644))
	}
	mainPkg := &packages.Package{Name: "main", GoFiles: []string{filepath.Join(withProfile, "main.go")}}
	otherMain := &packages.Package{Name: "main", GoFiles: []string{filepath.Join(otherProfile, "main.go")}}
	noProfile := &packages.Package{Name: "main", GoFiles: []string{filepath.Join(withoutProfile, "main.go")}}
	library := &packages.Package{Name: "library", GoFiles: mainPkg.GoFiles}
	profilePath := filepath.Join(withProfile, defaultProfile)

	tests := []struct {
		name       string
		subcommand string
		args       []string
		goflags    string
		pkgs       []*packages.Package
		want       string
		ambiguous  bool
	}{
		{name: "auto with profile", pkgs: []*packages.Package{mainPkg}, want: profilePath},
		{name: "auto without profile", pkgs: []*packages.Package{noProfile}},
		{name: "off", args: []string{"-pgo=off"}, pkgs: []*packages.Package{mainPkg}},
		{name: "explicit joined", args: []string{explicitFlag}, want: explicitProfile},
		{name: "explicit separated", args: []string{"-pgo", explicitProfile}, want: explicitProfile},
		{name: "double dash joined", args: []string{"--pgo=" + explicitProfile}, want: explicitProfile},
		{name: "double dash separated", args: []string{"--pgo", explicitProfile}, want: explicitProfile},
		{name: "GOFLAGS", goflags: goflags, want: "g.pgo"},
		{name: "command line beats GOFLAGS", args: []string{explicitFlag}, goflags: goflags, want: explicitProfile},
		{name: "last command line flag", args: []string{"-pgo=off", explicitFlag}, want: explicitProfile},
		{
			name: "last GOFLAGS flag", goflags: goflags + " '-pgo=profile with spaces.pgo'",
			want: "profile with spaces.pgo",
		},
		{name: "non-main package", pkgs: []*packages.Package{library}},
		{name: "multiple profiles", pkgs: []*packages.Package{mainPkg, otherMain}, ambiguous: true},
		{name: "same profile twice", pkgs: []*packages.Package{mainPkg, mainPkg}, want: profilePath},
		{name: "main without files", pkgs: []*packages.Package{{Name: "main"}}},
		{
			name: "test binary flag ignored", subcommand: subcmdTest,
			args: []string{"-args", explicitFlag}, goflags: goflags, want: "g.pgo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOFLAGS", tt.goflags)
			subcommand := tt.subcommand
			if subcommand == "" {
				subcommand = subcmdBuild
			}
			got, ambiguous := pgoProfile(pgoValue(subcommand, tt.args), tt.pkgs)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ambiguous, ambiguous)
		})
	}
}
