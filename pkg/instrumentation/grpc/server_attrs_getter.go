// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"strings"
)

// grpcServerAttrsGetter implements RpcAttrsGetter for gRPC server operations
type grpcServerAttrsGetter struct{}

// GetSystem returns the RPC system identifier, which is always "grpc"
func (g grpcServerAttrsGetter) GetSystem(request grpcRequest) string {
	return "grpc"
}

// GetService extracts the service name from the full gRPC method name.
// For a method like "/helloworld.Greeter/SayHello", it returns "/helloworld.Greeter"
func (g grpcServerAttrsGetter) GetService(request grpcRequest) string {
	fullMethodName := request.methodName
	slashIndex := strings.LastIndex(fullMethodName, "/")
	if slashIndex == -1 {
		return ""
	}
	return fullMethodName[0:slashIndex]
}

// GetMethod extracts the method name from the full gRPC method name.
// For a method like "/helloworld.Greeter/SayHello", it returns "SayHello"
func (g grpcServerAttrsGetter) GetMethod(request grpcRequest) string {
	fullMethodName := request.methodName
	slashIndex := strings.LastIndex(fullMethodName, "/")
	if slashIndex == -1 {
		return ""
	}
	return fullMethodName[slashIndex+1:]
}

// GetServerAddress returns the server address
func (g grpcServerAttrsGetter) GetServerAddress(request grpcRequest) string {
	return request.serverAddress
}
