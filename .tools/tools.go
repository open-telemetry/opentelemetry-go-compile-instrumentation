// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools
// +build tools

package tools // import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tools"

import (
	_ "github.com/campoy/embedmd"
	_ "github.com/checkmake/checkmake/cmd/checkmake"
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	_ "github.com/google/yamlfmt/cmd/yamlfmt"
	_ "github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt"
	_ "github.com/rhysd/actionlint/cmd/actionlint"
	_ "github.com/sethvargo/ratchet"
	_ "go.opentelemetry.io/build-tools/crosslink"
)

