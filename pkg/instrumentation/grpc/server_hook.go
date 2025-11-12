// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"fmt"
	"net"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func BeforeServe(ictx inst.HookContext, _ interface{}, lis net.Listener) {
	fmt.Println("BeforeServe")
	// TODO: Implement the real server hook logic here
}
