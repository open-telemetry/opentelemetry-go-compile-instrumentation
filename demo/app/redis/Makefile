# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

.PHONY: build generate test clean

build: ## Build Redis client
	@rm -f client/otelc.runtime.go
	(cd client && go build -a -o client .)

generate: ## No code generation needed
	@echo "No generation needed for Redis demo"

test: ## Run unit tests
	(cd client && go test -v -race ./...)

clean: ## Remove build artifacts
	rm -f client/client client/otelc.runtime.go
	rm -rf client/.otelc-build
