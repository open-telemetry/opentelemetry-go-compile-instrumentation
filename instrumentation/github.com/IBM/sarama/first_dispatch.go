//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sarama

import _ "unsafe"

// registerFirstDispatchFunc lives in the IBM/sarama instrumentation package.
// The linkname avoids an import edge from github.com/IBM/sarama to the
// instrumentation module: this file is injected into sarama's package by the
// add_file rule and only needs to report two otherwise-unexported fields.
//
//go:linkname registerFirstDispatchFunc go.opentelemetry.io/otelc/instrumentation/github.com/IBM/sarama.registerFirstDispatchFunc
func registerFirstDispatchFunc(f func(*ProducerMessage) bool)

func init() {
	// The dispatcher re-runs producer interceptors at every retry level of a
	// message (the interceptor loop sits outside the retries==0 bookkeeping)
	// and routes its internal fin control messages through the same loop. A
	// dispatch is the message's first one exactly when it has not been
	// retried yet and carries no control flag; retryMessage increments
	// retries before redispatching and fin messages carry flags != 0.
	registerFirstDispatchFunc(func(msg *ProducerMessage) bool {
		return msg.retries == 0 && msg.flags == 0
	})
}
