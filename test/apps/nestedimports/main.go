// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command nestedimports is an application whose dependency package dep gets a
// rule-added import of late, while the application itself also imports late.
// On a cold build cache, a nested build resolves the added import and the outer
// build compiles late too; both must produce the same instrumented archive or
// the link fails. late imports inner, which gets a rule-added import of leaf,
// so the nested build resolves leaf with a nested build of its own.
package main

import (
	"fmt"

	"go.opentelemetry.io/otelc/test/apps/nestedimports/dep"
	"go.opentelemetry.io/otelc/test/apps/nestedimports/late"
	"go.opentelemetry.io/otelc/test/apps/nestedimports/leaf"
)

func main() {
	// dep.Work is instrumented by a rule that adds an import of late, and
	// late.Instrumented is flipped by a rule on late itself. Both prove the
	// build compiled every package instrumented. inner and leaf repeat this
	// one level down.
	fmt.Println("nestedimports: dep=" + dep.Work())
	fmt.Println("nestedimports: late.Instrumented=", late.Instrumented)
	fmt.Println("nestedimports: inner=" + late.Inner())
	fmt.Println("nestedimports: leaf.Instrumented=", leaf.Instrumented)
}
