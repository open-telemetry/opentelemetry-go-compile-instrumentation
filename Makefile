# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Use bash for all shell commands (required for pipefail and other bash features)
SHELL := /bin/bash

.PHONY: all test test-unit test-integration test-e2e format lint build install package clean \
        build-demo build-demo-grpc build-demo-http format/go format/yaml format/license lint/go lint/yaml \
        lint/action lint/makefile lint/license actionlint yamlfmt gotestfmt ratchet ratchet/pin \
        ratchet/update ratchet/check golangci-lint embedmd checkmake go-license help docs check-embed \
        test-unit/coverage test-integration/coverage test-e2e/coverage

# Constant variables
BINARY_NAME := otel
TOOL_DIR := tool/cmd
INST_PKG_GZIP = otel-pkg.gz
INST_PKG_TMP = pkg_temp
API_SYNC_SOURCE = pkg/inst/context.go
API_SYNC_TARGET = tool/internal/instrument/api.tmpl

# Dynamic variables
GOOS ?= $(shell go env GOOS)
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d')
EXT :=
ifeq ($(GOOS),windows)
	EXT = .exe
endif

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[A-Za-z0-9_.\/-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: build format lint test

# Build targets

build: package ## Build the instrumentation tool
	@echo "Building instrumentation tool..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@go build -a -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" -o $(BINARY_NAME)$(EXT) ./$(TOOL_DIR)
	@./$(BINARY_NAME)$(EXT) version

install: ## Install otel to $$GOPATH/bin
	@echo "Installing otel..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	go install -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" ./$(TOOL_DIR)

.ONESHELL:
package: ## Package the instrumentation code into binary
	@echo "Packaging instrumentation code into binary..."
	@set -euo pipefail
	rm -rf $(INST_PKG_TMP)
	if [ ! -d pkg ]; then \
		echo "Error: pkg directory does not exist"; \
		exit 1; \
	fi
	cp -r pkg $(INST_PKG_TMP)
	(cd $(INST_PKG_TMP) && go mod tidy)
	tar -czf $(INST_PKG_GZIP) --exclude='*.log' $(INST_PKG_TMP)
	mkdir -p tool/data/
	mv $(INST_PKG_GZIP) tool/data/
	rm -rf $(INST_PKG_TMP)
	@echo "Package created successfully at tool/data/$(INST_PKG_GZIP)"

build-demo: ## Build all demos
build-demo: build-demo-grpc build-demo-http

build-demo-grpc: ## Build gRPC demo server and client
	@echo "Building gRPC demo..."
	@(cd demo/grpc/server && go generate && go build -o server .)
	@(cd demo/grpc/client && go build -o client .)

build-demo-http: ## Build HTTP demo server and client
	@echo "Building HTTP demo..."
	@(cd demo/http/server && go build -o server .)
	@(cd demo/http/client && go build -o client .)

# Format targets

format: ## Format Go code and YAML files
format: format/go format/yaml format/license

format/go: ## Format Go code only
format/go: golangci-lint
	@echo "Formatting Go code..."
	golangci-lint fmt

format/yaml: ## Format YAML files only (excludes testdata)
format/yaml: yamlfmt
	@echo "Formatting YAML files..."
	yamlfmt -dstar '**/*.yml' '**/*.yaml'

format/license: ## Apply license headers to Go files
format/license: go-license
	@echo "Applying license headers..."
	go-license --config=license.yml .

# Lint targets

lint: ## Run all linters (Go, YAML, GitHub Actions, Makefile)
lint: lint/go lint/yaml lint/action lint/makefile

lint/action: ## Lint GitHub Actions workflows
lint/action: actionlint ratchet/check
	@echo "Linting GitHub Actions workflows..."
	actionlint

lint/go: ## Run golangci-lint on Go code
lint/go: golangci-lint
	@echo "Linting Go code..."
	golangci-lint run

lint/yaml: ## Lint YAML formatting
lint/yaml: yamlfmt
	@echo "Linting YAML files..."
	yamlfmt -lint -dstar '**/*.yml' '**/*.yaml'

lint/makefile: ## Lint Makefile
lint/makefile: checkmake
	@echo "Linting Makefile..."
	checkmake --config .checkmake Makefile

lint/license: ## Check license headers
lint/license: go-license
	@echo "Checking license headers..."
	go-license --config=license.yml --verify .

# Ratchet targets for GitHub Actions pinning

ratchet/pin: ## Pin GitHub Actions to commit SHAs
ratchet/pin: ratchet
	@echo "Pinning GitHub Actions to commit SHAs..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs ratchet pin

ratchet/update: ## Update pinned GitHub Actions to latest versions
ratchet/update: ratchet
	@echo "Updating pinned GitHub Actions to latest versions..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs ratchet update

ratchet/check: ## Verify all GitHub Actions are pinned
ratchet/check: ratchet
	@echo "Checking GitHub Actions are pinned..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs ratchet lint

# Documentation targets

docs: ## Update embedded documentation in markdown files
docs: embedmd tmp/make-help.txt
	@echo "Updating embedded documentation..."
	embedmd -w CONTRIBUTING.md README.md

tmp/make-help.txt: ## Generate make help output for embedding in documentation
tmp/make-help.txt: $(MAKEFILE_LIST)
	@mkdir -p tmp
	@$(MAKE) --no-print-directory help > tmp/make-help.txt

# Validation targets

check-embed: ## Verify that embedded files exist (required for tests)
	@echo "Checking embedded files..."
	@if [ ! -f tool/data/$(INST_PKG_GZIP) ]; then \
		echo "Error: tool/data/$(INST_PKG_GZIP) does not exist"; \
		echo "Run 'make package' to generate it"; \
		exit 1; \
	fi
	@echo "All embedded files present"

# Test targets
# NOTE: Tests require the 'package' target to run first because tool/data/export.go
# uses //go:embed to embed otel-pkg.gz at compile time. If the file doesn't exist
# when Go compiles the test packages, the embed will fail.

test: ## Run all tests (unit + integration + e2e)
test: test-unit test-integration test-e2e

.ONESHELL:
test-unit: ## Run unit tests
test-unit: package gotestfmt
	@echo "Running unit tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... 2>&1 | tee ./gotest-unit.log | gotestfmt

.ONESHELL:
test-unit/coverage: ## Run unit tests with coverage report
test-unit/coverage: package gotestfmt
	@echo "Running unit tests with coverage report..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee ./gotest-unit.log | gotestfmt

.ONESHELL:
test-integration: ## Run integration tests
test-integration: build gotestfmt
	@echo "Running integration tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./test/integration/... 2>&1 | tee ./gotest-integration.log | gotestfmt

.ONESHELL:
test-integration/coverage: ## Run integration tests with coverage report
test-integration/coverage: build gotestfmt
	@echo "Running integration tests with coverage report..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./test/integration/... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee ./gotest-integration.log | gotestfmt

.ONESHELL:
test-e2e: ## Run e2e tests
test-e2e: build gotestfmt
	@echo "Running e2e tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./test/e2e/... 2>&1 | tee ./gotest-e2e.log | gotestfmt

.ONESHELL:
test-e2e/coverage: ## Run e2e tests with coverage report
test-e2e/coverage: build gotestfmt
	@echo "Running e2e tests with coverage report..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./test/e2e/... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee ./gotest-e2e.log | gotestfmt

# Clean targets

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)$(EXT)
	rm -f demo/basic/basic
	rm -rf demo/basic/.otel-build
	rm -f demo/grpc/server/server
	rm -rf demo/grpc/server/pb
	rm -f demo/grpc/client/client
	rm -rf demo/grpc/server/.otel-build
	rm -rf demo/grpc/client/.otel-build
	rm -f demo/http/server/server
	rm -f demo/http/client/client
	rm -rf demo/http/server/.otel-build
	rm -rf demo/http/client/.otel-build
	rm -f ./gotest-unit.log ./gotest-integration.log ./gotest-e2e.log

# Tool installation targets

gotestfmt: ## Install gotestfmt if not present
	@if ! command -v gotestfmt >/dev/null 2>&1; then \
		echo "Installing gotestfmt..."; \
		go install github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt@latest; \
	fi

golangci-lint: ## Install golangci-lint if not present
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi

actionlint: ## Install actionlint if not present
	@if ! command -v actionlint >/dev/null 2>&1; then \
		echo "Installing actionlint..."; \
		go install github.com/rhysd/actionlint/cmd/actionlint@latest; \
	fi

yamlfmt: ## Install yamlfmt if not present
	@if ! command -v yamlfmt >/dev/null 2>&1; then \
		echo "Installing yamlfmt..."; \
		go install github.com/google/yamlfmt/cmd/yamlfmt@latest; \
	fi

ratchet: ## Install ratchet if not present
	@if ! command -v ratchet >/dev/null 2>&1; then \
		echo "Installing ratchet..."; \
		go install github.com/sethvargo/ratchet@latest; \
	fi

embedmd: ## Install embedmd if not present
	@if ! command -v embedmd >/dev/null 2>&1; then \
		echo "Installing embedmd..."; \
		go install github.com/campoy/embedmd@latest; \
	fi

checkmake: ## Install checkmake if not present
	@if ! command -v checkmake >/dev/null 2>&1; then \
		echo "Installing checkmake..."; \
		go install github.com/checkmake/checkmake/cmd/checkmake@latest; \
	fi

go-license: ## Install go-license if not present
	@if ! command -v go-license >/dev/null 2>&1; then \
		echo "Installing go-license..."; \
		go install github.com/palantir/go-license@latest; \
	fi
