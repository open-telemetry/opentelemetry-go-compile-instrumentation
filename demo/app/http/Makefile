# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

.PHONY: build generate test clean

build: ## Build HTTP server and client
	@rm -f server/otelc.runtime.go client/otelc.runtime.go
	(cd server && go build -a -o server .)
	(cd client && go build -a -o client .)

generate: ## No code generation needed
	@echo "No generation needed for HTTP demo"

test: ## Run unit tests
	(cd server && go test -v -race ./...)
	(cd client && go test -v -race ./...)

clean: ## Remove build artifacts
	rm -f server/server server/otelc.runtime.go
	rm -rf server/.otelc-build
	rm -f client/client client/otelc.runtime.go
	rm -rf client/.otelc-build
