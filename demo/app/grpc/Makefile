# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

.PHONY: build generate test clean

build: ## Build gRPC server and client
	@rm -f server/otelc.runtime.go client/otelc.runtime.go
	(cd server && go build -a -o server .)
	(cd client && go build -a -o client .)

generate: ## Generate protobuf code
	(cd server && go generate ./...)

test: ## Run unit tests
	(cd server && go test -v -race ./...)
	(cd client && go test -v -race ./...)

clean: ## Remove build artifacts
	rm -f server/server server/otelc.runtime.go
	rm -rf server/.otelc-build
	rm -f client/client client/otelc.runtime.go
	rm -rf client/.otelc-build
