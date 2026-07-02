# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

BINARY := openai-client

.PHONY: build generate test clean

build: ## Build the OpenAI demo client
	go build -a -o $(BINARY) .

generate: ## No code generation needed
	@echo "No generation needed for OpenAI demo"

test: ## Run unit tests
	go test -v -race ./...

clean: ## Remove build artifacts
	rm -f $(BINARY) otelc.runtime.go
	rm -rf .otelc-build
