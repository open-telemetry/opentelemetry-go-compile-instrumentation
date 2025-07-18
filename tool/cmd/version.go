// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// These variables are set by the linker. Changes should sync with the Makefile.
//
//nolint:gochecknoglobals // these variables are set by the linker
var (
	Version    = "v0.1.0"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)
