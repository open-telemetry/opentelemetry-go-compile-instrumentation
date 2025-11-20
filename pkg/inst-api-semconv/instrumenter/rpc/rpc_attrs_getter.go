// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

// RpcAttrsGetter defines the interface for extracting RPC attributes from requests.
// This interface follows OpenTelemetry semantic conventions for RPC systems.
type RpcAttrsGetter[REQUEST any] interface {
	// GetSystem returns the RPC system identifier (e.g., "grpc", "rpc", etc.)
	GetSystem(request REQUEST) string

	// GetService returns the full service name being called
	GetService(request REQUEST) string

	// GetMethod returns the method name being called
	GetMethod(request REQUEST) string

	// GetServerAddress returns the server address
	GetServerAddress(request REQUEST) string
}
