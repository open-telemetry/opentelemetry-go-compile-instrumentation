// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"fmt"
	"net/http"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func BeforeServeHTTP(ictx inst.HookContext, _ interface{}, w http.ResponseWriter, r *http.Request) {
	fmt.Println("BeforeServeHTTP")
	// TODO: Implement the real server hook logic here
}
