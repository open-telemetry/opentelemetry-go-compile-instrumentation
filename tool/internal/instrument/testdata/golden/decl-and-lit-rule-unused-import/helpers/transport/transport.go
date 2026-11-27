// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transport

// Transport stands in for the transport of a third-party library.
type Transport struct{ Traced bool }

// New returns a transport.
func New() *Transport { return &Transport{} }
