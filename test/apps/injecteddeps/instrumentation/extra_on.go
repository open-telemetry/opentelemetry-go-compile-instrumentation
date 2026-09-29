// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build injectedtag

package hook

import (
	"strconv"

	"go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation/extra"
)

func extraStatus() string { return strconv.FormatBool(extra.Extra) }
