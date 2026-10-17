// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package grpcpb holds gRPC fixtures shared by the test applications and the
// integration tests that drive them.
package grpcpb

import "fmt"

// RPCCompletedLog renders the line a test gRPC server logs once an RPC has run
// to completion, so an integration test can wait for one particular RPC rather
// than for whichever finishes first.
//
// The method name is delimited because neither of its ends can otherwise be
// matched on safely. One method name can be a prefix of another, so waiting on
// an undelimited "/greeter.Greeter/SayHello" would also accept
// "/greeter.Greeter/SayHelloStream". The line ending is no better a terminator,
// because the log instrumentation appends trace and span ids after the message.
func RPCCompletedLog(fullMethod string) string {
	return fmt.Sprintf("rpc completed [%s]", fullMethod)
}
