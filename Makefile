# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

.PHONY: all test test-unit test-integration format lint build install package clean \
        build-demo-grpc format/go format/yaml lint/go lint/yaml lint/action \
        actionlint yamlfmt gotestfmt ratchet ratchet/pin ratchet/update ratchet/check \
        golangci-lint embedmd help docs tmp/make-help.txt

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
SHELL := /bin/bash
package: ## Package the instrumentation code into binary
	@echo "Packaging instrumentation code into binary..."
	set -euo pipefail
	rm -rf $(INST_PKG_TMP)
	cp -a pkg $(INST_PKG_TMP)
	(cd $(INST_PKG_TMP) && go mod tidy)
	tar -czf $(INST_PKG_GZIP) --exclude='*.log' $(INST_PKG_TMP)
	mkdir -p tool/data/
	mv $(INST_PKG_GZIP) tool/data/
	rm -rf $(INST_PKG_TMP)

build-demo-grpc: ## Build gRPC demo server and client
	@echo "Building gRPC demo..."
	@cd demo/grpc/server && go generate && go build -o server .
	@cd demo/grpc/client && go build -o client .

.PHONY: build-demo-http
build-demo-http:
	@echo "Building HTTP demo..."
	@cd demo/http/server && go build -o server .
	@cd demo/http/client && go build -o client .

# Format targets

format: ## Format Go code and YAML files
format: format/go format/yaml

format/go: ## Format Go code only
format/go: golangci-lint
	@echo "Formatting Go code..."
	golangci-lint fmt

format/yaml: ## Format YAML files only (excludes testdata)
format/yaml: yamlfmt
	@echo "Formatting YAML files..."
	yamlfmt -dstar '**/*.yml' '**/*.yaml'

# Lint targets

lint: ## Run all linters (Go, YAML, GitHub Actions)
lint: lint/go lint/yaml lint/action

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

# Ratchet targets for GitHub Actions pinning

ratchet/pin: ## Pin GitHub Actions to commit SHAs
ratchet/pin: ratchet
	@echo "Pinning GitHub Actions to commit SHAs..."
	ratchet pin .github/workflows/*.yml .github/workflows/*.yaml

ratchet/update: ## Update pinned GitHub Actions to latest versions
ratchet/update: ratchet
	@echo "Updating pinned GitHub Actions to latest versions..."
	ratchet update .github/workflows/*.yml .github/workflows/*.yaml

ratchet/check: ## Verify all GitHub Actions are pinned
ratchet/check: ratchet
	@echo "Checking GitHub Actions are pinned..."
	ratchet lint .github/workflows/*.yml .github/workflows/*.yaml

# Documentation targets

docs: ## Update embedded documentation in markdown files
docs: embedmd tmp/make-help.txt
	@echo "Updating embedded documentation..."
	embedmd -w CONTRIBUTING.md README.md

tmp/make-help.txt: ## Generate make help output for embedding in documentation
tmp/make-help.txt: $(MAKEFILE_LIST)
	@mkdir -p tmp
	@$(MAKE) --no-print-directory help > tmp/make-help.txt

# Test targets

test: ## Run all tests (unit + integration)
test: test-unit test-integration

.ONESHELL:
SHELL := /bin/bash
test-unit: ## Run unit tests
test-unit: gotestfmt
	@echo "Running unit tests..."
	set -euo pipefail
	go test -json -v -timeout=5m -count=1 ./tool/... 2>&1 | tee ./gotest-unit.log | gotestfmt

.ONESHELL:
SHELL := /bin/bash
test-integration: ## Run integration tests
test-integration: build gotestfmt
	@echo "Running integration tests..."
	set -euo pipefail
	go test -json -v -timeout=10m -count=1 -run TestBasic ./test/... 2>&1 | tee ./gotest-integration.log | gotestfmt

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
	rm -f ./gotest-unit.log ./gotest-integration.log

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
