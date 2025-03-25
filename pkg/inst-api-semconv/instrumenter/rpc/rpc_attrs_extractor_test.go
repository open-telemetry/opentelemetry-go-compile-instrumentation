// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api/utils"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"log"
	"testing"
)

type testResponse struct {
}

type rpcAttrsGetter struct {
}

func (h rpcAttrsGetter) GetSystem(request testRequest) string {
	return "system"
}

func (h rpcAttrsGetter) GetService(request testRequest) string {
	return "service"
}

func (h rpcAttrsGetter) GetMethod(request testRequest) string {
	return "method"
}

func (h rpcAttrsGetter) GetServerAddress(request testRequest) string {
	return "serverAddress"
}

func TestClientGetSpanKey(t *testing.T) {
	rpcExtractor := &ClientRpcAttrsExtractor[testRequest, any, rpcAttrsGetter]{}
	if rpcExtractor.GetSpanKey() != utils.RPC_CLIENT_KEY {
		t.Fatalf("Should have returned RPC_CLIENT_KEY")
	}
}

func TestServerGetSpanKey(t *testing.T) {
	rpcExtractor := &ServerRpcAttrsExtractor[testRequest, any, rpcAttrsGetter]{}
	if rpcExtractor.GetSpanKey() != utils.RPC_SERVER_KEY {
		t.Fatalf("Should have returned RPC_SERVER_KEY")
	}
}

func TestRpcClientExtractorStart(t *testing.T) {
	rpcExtractor := ClientRpcAttrsExtractor[testRequest, testResponse, rpcAttrsGetter]{}
	attrs := make([]attribute.KeyValue, 0)
	parentContext := context.Background()
	attrs, _ = rpcExtractor.OnStart(attrs, parentContext, testRequest{})
	if attrs[0].Key != semconv.RPCSystemKey || attrs[0].Value.AsString() != "system" {
		t.Fatalf("rpc system should be system")
	}
	if attrs[1].Key != semconv.RPCServiceKey || attrs[1].Value.AsString() != "service" {
		t.Fatalf("rpc service should be service")
	}
	if attrs[2].Key != semconv.RPCMethodKey || attrs[2].Value.AsString() != "method" {
		t.Fatalf("rpc method should be method")
	}
}

func TestRpcClientExtractorEnd(t *testing.T) {
	rpcExtractor := ClientRpcAttrsExtractor[testRequest, testResponse, rpcAttrsGetter]{}
	attrs := make([]attribute.KeyValue, 0)
	parentContext := context.Background()
	attrs, _ = rpcExtractor.OnEnd(attrs, parentContext, testRequest{}, testResponse{}, nil)
	if len(attrs) != 0 {
		log.Fatal("attrs should be empty")
	}
}

func TestRpcServerExtractorStart(t *testing.T) {
	rpcExtractor := ServerRpcAttrsExtractor[testRequest, testResponse, rpcAttrsGetter]{}
	attrs := make([]attribute.KeyValue, 0)
	parentContext := context.Background()
	attrs, _ = rpcExtractor.OnStart(attrs, parentContext, testRequest{})
	if attrs[0].Key != semconv.RPCSystemKey || attrs[0].Value.AsString() != "system" {
		t.Fatalf("rpc system should be system")
	}
	if attrs[1].Key != semconv.RPCServiceKey || attrs[1].Value.AsString() != "service" {
		t.Fatalf("rpc service should be service")
	}
	if attrs[2].Key != semconv.RPCMethodKey || attrs[2].Value.AsString() != "method" {
		t.Fatalf("rpc method should be method")
	}
}

func TestRpcServerExtractorEnd(t *testing.T) {
	rpcExtractor := ServerRpcAttrsExtractor[testRequest, testResponse, rpcAttrsGetter]{}
	attrs := make([]attribute.KeyValue, 0)
	parentContext := context.Background()
	attrs, _ = rpcExtractor.OnEnd(attrs, parentContext, testRequest{}, testResponse{}, nil)
	if len(attrs) != 0 {
		log.Fatal("attrs should be empty")
	}
}
