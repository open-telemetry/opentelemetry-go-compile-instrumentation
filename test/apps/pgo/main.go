// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"go.opentelemetry.io/otelc/test/apps/pgo/lib"
)

func runWork() {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = lib.Work()
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "profile" {
		f, err := os.Create(os.Args[2])
		if err != nil {
			panic(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		runWork()
		pprof.StopCPUProfile()
		_ = f.Close()
		return
	}
	fmt.Println(lib.Work())
}
