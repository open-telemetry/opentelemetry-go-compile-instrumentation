// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

// Client stands in for the client of a third-party library.
type Client struct{ Traced bool }

// New returns a client.
func New(name string) *Client { return &Client{} }
