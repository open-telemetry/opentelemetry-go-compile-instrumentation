// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/test/apps/injecteddeps/instrumentation/target"
)

// afterNewServeMux reports the flag in the target package. The application
// never imports that package; it is in the build only because this hook is
// blank-imported into the application, which is exactly the case the rule
// targeting it has to survive.
func afterNewServeMux(_ hook.HookContext, _ *http.ServeMux) {
	fmt.Printf("injecteddeps: target.Instrumented=%v extra=%s\n", target.Instrumented, extraStatus())
}
