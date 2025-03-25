// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import "go.opentelemetry.io/otel/attribute"

const DB_CLIENT_KEY = attribute.Key("opentelemetry-traces-span-key-db-client")

const RPC_SERVER_KEY = attribute.Key("opentelemetry-traces-span-key-rpc-server")
const RPC_CLIENT_KEY = attribute.Key("opentelemetry-traces-span-key-rpc-client")
const HTTP_CLIENT_KEY = attribute.Key("opentelemetry-traces-span-key-http-client")
const HTTP_SERVER_KEY = attribute.Key("opentelemetry-traces-span-key-http-server")

const PRODUCER_KEY = attribute.Key("opentelemetry-traces-span-key-producer")
const CONSUMER_RECEIVE_KEY = attribute.Key("opentelemetry-traces-span-key-consumer-receive")
const CONSUMER_PROCESS_KEY = attribute.Key("opentelemetry-traces-span-key-consumer-process")

const KIND_SERVER = attribute.Key("opentelemetry-traces-span-key-kind-server")
const KIND_CLIENT = attribute.Key("opentelemetry-traces-span-key-kind-client")
const KIND_CONSUMER = attribute.Key("opentelemetry-traces-span-key-kind-consumer")
const KIND_PRODUCER = attribute.Key("opentelemetry-traces-span-key-kind-producer")

const OTEL_CONTEXT_KEY = attribute.Key("opentelemetry-http-server-route-key")
