// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"runtime/debug"
)

// These variables are set by the linker. Changes should sync with the Makefile.
//
//nolint:gochecknoglobals // these variables are set by the linker
var (
	Version    = "v0.0.0"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

func init() {
	if Version != "v0.0.0" {
		// Was set at build time
		return
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if version := bi.Main.Version; version != "" {
		Version = version
	}

	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			CommitHash = setting.Value
		case "vcs.time":
			BuildTime = setting.Value
		}
	}
}
