// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package net

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"strings"
)

var noopAddressAndPort = AddressAndPort{}

type AddressAndPort struct {
	Address string
	Port    int
}

type AddressAndPortExtractor[REQUEST any] interface {
	Extract(request REQUEST) AddressAndPort
}

type NoopAddressAndPortExtractor[REQUEST any] struct{}

func (n *NoopAddressAndPortExtractor[REQUEST]) Extract(request REQUEST) AddressAndPort {
	return noopAddressAndPort
}

type ClientAddressAndPortExtractor[REQUEST any] struct {
	getter            ClientAttributesGetter[REQUEST]
	fallbackExtractor AddressAndPortExtractor[REQUEST]
}

func (c *ClientAddressAndPortExtractor[REQUEST]) Extract(request REQUEST) AddressAndPort {
	address := c.getter.GetClientAddress(request)
	port := c.getter.GetClientPort(request)
	if address == "" && port == 0 && c.fallbackExtractor != nil {
		return c.fallbackExtractor.Extract(request)
	} else {
		return AddressAndPort{
			Address: address,
			Port:    port,
		}
	}
}

type ServerAddressAndPortExtractor[REQUEST any] struct {
	getter            ServerAttributesGetter[REQUEST]
	fallbackExtractor AddressAndPortExtractor[REQUEST]
}

func (s *ServerAddressAndPortExtractor[REQUEST]) Extract(request REQUEST) AddressAndPort {
	address := s.getter.GetServerAddress(request)
	port := s.getter.GetServerPort(request)
	if address == "" && port == 0 {
		return s.fallbackExtractor.Extract(request)
	} else {
		return AddressAndPort{
			Address: address,
			Port:    port,
		}
	}
}

type InternalClientAttributesExtractor[REQUEST any] struct {
	addressAndPortExtractor AddressAndPortExtractor[REQUEST]
	capturePort             bool
}

func (i *InternalClientAttributesExtractor[REQUEST]) OnStart(ctx context.Context, attributes []attribute.KeyValue,
	request REQUEST) ([]attribute.KeyValue, context.Context) {
	clientAddressAndPort := i.addressAndPortExtractor.Extract(request)
	if clientAddressAndPort.Address != "" {
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.ClientAddressKey,
			Value: attribute.StringValue(clientAddressAndPort.Address),
		})
		if i.capturePort && clientAddressAndPort.Port != 0 {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.ClientPortKey,
				Value: attribute.IntValue(clientAddressAndPort.Port),
			})
		}
	}
	return attributes, ctx
}

type InternalServerAttributesExtractor[REQUEST any] struct {
	addressAndPortExtractor AddressAndPortExtractor[REQUEST]
}

func (i *InternalServerAttributesExtractor[REQUEST]) OnStart(context context.Context,
	attributes []attribute.KeyValue, request REQUEST) ([]attribute.KeyValue, context.Context) {
	serverAddressAndPort := i.addressAndPortExtractor.Extract(request)
	if serverAddressAndPort.Address != "" {
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.ServerAddressKey,
			Value: attribute.StringValue(serverAddressAndPort.Address),
		})
		if serverAddressAndPort.Port != 0 {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.ServerPortKey,
				Value: attribute.IntValue(serverAddressAndPort.Port),
			})
		}
	}
	return attributes, context
}

type InternalNetworkAttributesExtractor[REQUEST any, RESPONSE any] struct {
	getter                       NetworkAttrsGetter[REQUEST, RESPONSE]
	captureProtocolAttributes    bool
	captureLocalSocketAttributes bool
}

func (i *InternalNetworkAttributesExtractor[REQUEST, RESPONSE]) OnEnd(context context.Context, attributes []attribute.
	KeyValue, request REQUEST, response RESPONSE) ([]attribute.KeyValue, context.Context) {
	if i.captureProtocolAttributes {
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.NetworkTransportKey,
			Value: attribute.StringValue(strings.ToLower(i.getter.GetNetworkTransport(request, response))),
		}, attribute.KeyValue{
			Key:   semconv.NetworkTypeKey,
			Value: attribute.StringValue(strings.ToLower(i.getter.GetNetworkType(request, response))),
		}, attribute.KeyValue{
			Key:   semconv.NetworkProtocolNameKey,
			Value: attribute.StringValue(strings.ToLower(i.getter.GetNetworkProtocolName(request, response))),
		}, attribute.KeyValue{
			Key:   semconv.NetworkProtocolVersionKey,
			Value: attribute.StringValue(strings.ToLower(i.getter.GetNetworkProtocolVersion(request, response))),
		})
	}
	if i.captureLocalSocketAttributes {
		localAddress := i.getter.GetNetworkLocalInetAddress(request, response)
		if localAddress != "" {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.NetworkLocalAddressKey,
				Value: attribute.StringValue(localAddress),
			})
		}
		localPort := i.getter.GetNetworkLocalPort(request, response)
		if localPort != 0 {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.NetworkLocalPortKey,
				Value: attribute.IntValue(localPort),
			})
		}
	}
	peerAddress := i.getter.GetNetworkPeerInetAddress(request, response)
	if peerAddress != "" {
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.NetworkPeerAddressKey,
			Value: attribute.StringValue(peerAddress),
		})
	}
	peerPort := i.getter.GetNetworkPeerPort(request, response)
	if peerPort != 0 {
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.NetworkPeerPortKey,
			Value: attribute.IntValue(peerPort),
		})
	}
	return attributes, context
}
