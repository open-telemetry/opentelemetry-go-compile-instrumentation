# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Use bash for all shell commands (required for pipefail and other bash features)
SHELL := /bin/bash

.PHONY: all test test-unit test-integration test-e2e format lint build build/pkg install package clean \
        build-demo build-demo-grpc build-demo-http format/go format/yaml lint/go lint/yaml \
        lint/action lint/makefile lint/license-header lint/license-header/fix lint/dockerfile actionlint yamlfmt gotestfmt ratchet ratchet/pin \
        ratchet/update ratchet/check golangci-lint embedmd checkmake hadolint help docs check-embed \
        test-unit/coverage test-integration/coverage test-e2e/coverage test-unit/update-golden \
        test-unit/tool test-unit/pkg test-unit/coverage test-unit/tool/coverage test-unit/pkg/coverage \
        test-integration/coverage test-e2e/coverage \
        registry-diff registry-check registry-resolve weaver-install

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
	@echo -e "\033[1;3;34mOpenTelemetry Go Compile Instrumentation.\033[0m\n"
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_0-9\/-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

all: build format lint test

##@ Core Build

.ONESHELL:
build/pkg: ## Build all pkg modules to verify compilation
	@echo "Building pkg modules..."
	@set -euo pipefail
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \; | grep -v "pkg/instrumentation/runtime"); \
	for moddir in $$PKG_MODULES; do \
		echo "Building $$moddir..."; \
		(cd $$moddir && go mod tidy && go build ./...); \
	done
	@echo "All pkg modules built successfully"

build: build/pkg package ## Build the instrumentation tool
	@echo "Building instrumentation tool..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@go build -a -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" -o $(BINARY_NAME)$(EXT) ./$(TOOL_DIR)
	@./$(BINARY_NAME)$(EXT) version

install: package ## Install otel to $$GOPATH/bin (auto-packages instrumentation)
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

build-demo-grpc: go-protobuf-plugins ## Build gRPC demo server and client
	@echo "Building gRPC demo..."
	@(cd demo/grpc/server && go generate && go build -o server .)
	@(cd demo/grpc/client && go build -o client .)

build-demo-http: ## Build HTTP demo server and client
	@echo "Building HTTP demo..."
	@(cd demo/http/server && go build -o server .)
	@(cd demo/http/client && go build -o client .)

##@ Code Quality

format: ## Format Go code and YAML files
format: format/go format/yaml lint/license-header/fix

format/go: ## Format Go code only
format/go: golangci-lint
	@echo "Formatting Go code..."
	golangci-lint fmt --config .config/golangci.yml

format/yaml: ## Format YAML files only (excludes testdata)
format/yaml: yamlfmt
	@echo "Formatting YAML files..."
	yamlfmt -conf .config/yamlfmt -dstar '**/*.yml' '**/*.yaml'

lint: ## Run all linters (Go, YAML, GitHub Actions, Makefile, Dockerfile)
lint: lint/go lint/yaml lint/action lint/makefile lint/license-header lint/dockerfile

lint/action: ## Lint GitHub Actions workflows
lint/action: actionlint ratchet/check
	@echo "Linting GitHub Actions workflows..."
	actionlint

lint/go: ## Run golangci-lint on Go code
lint/go: golangci-lint
	@echo "Linting Go code..."
	golangci-lint run --config .config/golangci.yml

lint/go/fix: ## Run golangci-lint on Go code and fix the issues
lint/go/fix: golangci-lint
	@echo "Linting Go code..."
	golangci-lint run --config .config/golangci.yml --fix

lint/yaml: ## Lint YAML formatting
lint/yaml: yamlfmt
	@echo "Linting YAML files..."
	yamlfmt -conf .config/yamlfmt -lint -dstar '**/*.yml' '**/*.yaml'

lint/dockerfile: ## Lint Dockerfiles
lint/dockerfile: hadolint
	@echo "Linting Dockerfiles..."
	@HADOLINT_CMD="hadolint"; \
	if command -v hadolint >/dev/null 2>&1 && hadolint --version >/dev/null 2>&1; then \
		HADOLINT_CMD="hadolint"; \
	elif [ -f /opt/homebrew/bin/hadolint ]; then \
		HADOLINT_CMD="/opt/homebrew/bin/hadolint"; \
	fi; \
	$$HADOLINT_CMD -c .config/hadolint.yaml demo/grpc/client/Dockerfile demo/grpc/server/Dockerfile demo/http/client/Dockerfile demo/http/server/Dockerfile

lint/makefile: ## Lint Makefile
lint/makefile: checkmake
	@echo "Linting Makefile..."
	checkmake --config .config/checkmake Makefile

lint/license-header: ## Check license headers in source files
	@.github/scripts/license-check.sh

.PHONY: lint/license-header/fix
lint/license-header/fix: ## Add missing license headers to source files
	@.github/scripts/license-check.sh --fix

##@ Markdown

.PHONY: lint/markdown
lint/markdown: ## Lint Check the markdown files.
	npx markdownlint-cli -c .config/markdownlint.yaml **/*.md

.PHONY: lint/markdown/fix
lint/markdown/fix: ## Lint Check the markdown files and fix them.
	npx markdownlint-cli -c .config/markdownlint.yaml --fix **/*.md

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

##@ Documentation

docs: ## Update embedded documentation in markdown files
docs: embedmd tmp/make-help.txt
	@echo "Updating embedded documentation..."
	embedmd -w CONTRIBUTING.md README.md

tmp/make-help.txt: ## Generate make help output for embedding in documentation
tmp/make-help.txt: $(MAKEFILE_LIST)
	@mkdir -p tmp
	@$(MAKE) --no-print-directory help > tmp/make-help.txt

##@ Validation

check-embed: ## Verify that embedded files exist (required for tests)
	@echo "Checking embedded files..."
	@if [ ! -f tool/data/$(INST_PKG_GZIP) ]; then \
		echo "Error: tool/data/$(INST_PKG_GZIP) does not exist"; \
		echo "Run 'make package' to generate it"; \
		exit 1; \
	fi
	@echo "All embedded files present"

##@ Testing
# NOTE: Tests require the 'package' target to run first because tool/data/export.go
# uses //go:embed to embed otel-pkg.gz at compile time. If the file doesn't exist
# when Go compiles the test packages, the embed will fail.

test: ## Run all tests (unit + integration + e2e)
test: test-unit test-integration test-e2e

test-unit: test-unit/tool test-unit/pkg ## Run all unit tests (tool + pkg)

.ONESHELL:
test-unit/update-golden: ## Run unit tests and update golden files
test-unit/update-golden: package
	@echo "Running unit tests and updating golden files..."
	set -euo pipefail
	cd tool/internal/instrument && go test -v -timeout=5m -count=1 -update

.ONESHELL:
test-unit/coverage: ## Run unit tests with coverage report
test-unit/coverage: package gotestfmt
	@echo "Running unit tests with coverage report..."
test-unit/tool: build package gotestfmt ## Run unit tests for tool modules only
	@echo "Running tool unit tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... 2>&1 | tee ./gotest-unit-tool.log | gotestfmt

.ONESHELL:
test-unit/pkg: package gotestfmt ## Run unit tests for pkg modules only
	@echo "Running pkg unit tests..."
	set -euo pipefail
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \; | grep -v "pkg/instrumentation/runtime"); \
	for moddir in $$PKG_MODULES; do \
		echo "Testing $$moddir..."; \
		(cd $$moddir && go mod tidy && go test -json -v -shuffle=on -timeout=5m -count=1 ./... 2>&1 | tee -a ../../gotest-unit-pkg.log | gotestfmt); \
	done

test-unit/coverage: test-unit/tool/coverage test-unit/pkg/coverage ## Run all unit tests with coverage

.ONESHELL:
test-unit/tool/coverage: package gotestfmt ## Run unit tests with coverage for tool modules only
	@echo "Running tool unit tests with coverage..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... -coverprofile=coverage-tool.txt -covermode=atomic 2>&1 | tee ./gotest-unit-tool.log | gotestfmt

.ONESHELL:
test-unit/pkg/coverage: package gotestfmt ## Run unit tests with coverage for pkg modules only
	@echo "Running pkg unit tests with coverage..."
	set -euo pipefail
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \; | grep -v "pkg/instrumentation/runtime"); \
	for moddir in $$PKG_MODULES; do \
		echo "Testing $$moddir..."; \
		(cd $$moddir && go mod tidy && go test -json -v -shuffle=on -timeout=5m -count=1 ./... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee -a ../../gotest-unit-pkg.log | gotestfmt); \
	done

.ONESHELL:
test-integration: go-protobuf-plugins ## Run integration tests
test-integration: build build-demo gotestfmt
	@echo "Running integration tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./test/integration/... 2>&1 | tee ./gotest-integration.log | gotestfmt

.ONESHELL:
test-integration/coverage: ## Run integration tests with coverage report
test-integration/coverage: build build-demo gotestfmt
	@echo "Running integration tests with coverage report..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./test/integration/... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee ./gotest-integration.log | gotestfmt

.ONESHELL:
test-e2e: ## Run e2e tests
test-e2e: build build-demo gotestfmt
	@echo "Running e2e tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./test/e2e/... 2>&1 | tee ./gotest-e2e.log | gotestfmt

.ONESHELL:
test-e2e/coverage: ## Run e2e tests with coverage report
test-e2e/coverage: build gotestfmt
	@echo "Running e2e tests with coverage report..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./test/e2e/... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee ./gotest-e2e.log | gotestfmt

##@ Utilities

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)$(EXT)
	rm -f demo/basic/basic
	rm -f demo/grpc/server/server
	rm -rf demo/grpc/server/pb
	rm -f demo/grpc/client/client
	rm -f demo/http/server/server
	rm -f demo/http/client/client
	find demo -type d -name ".otel-build" -exec rm -rf {} +
	find . -type f \( -name gotest-unit-tool.log -o -name gotest-unit-pkg.log -o -name gotest-integration.log -o -name gotest-e2e.log \) -delete

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

go-protobuf-plugins: ## Install Go protobuf plugins if not present
	@if ! command -v protoc-gen-go >/dev/null 2>&1; then \
		echo "Installing Go protobuf plugins..."; \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
	fi
	@if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then \
		echo "Installing Go protobuf gRPC plugins..."; \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi

hadolint: ## Install hadolint if not present
	@HADOLINT_PATH=""; \
	if command -v hadolint >/dev/null 2>&1 && hadolint --version >/dev/null 2>&1; then \
		HADOLINT_PATH=$$(command -v hadolint); \
	elif [ -f /opt/homebrew/bin/hadolint ] && /opt/homebrew/bin/hadolint --version >/dev/null 2>&1; then \
		HADOLINT_PATH="/opt/homebrew/bin/hadolint"; \
	fi; \
	if [ -z "$$HADOLINT_PATH" ]; then \
		echo "Installing hadolint..."; \
		if [ "$$(uname -s)" = "Darwin" ]; then \
			if command -v brew >/dev/null 2>&1; then \
				brew install hadolint; \
			else \
				echo "Error: Homebrew not found. Install Homebrew from https://brew.sh/ and try again."; \
				exit 1; \
			fi; \
		elif [ "$$(uname -s)" = "Linux" ]; then \
			VERSION="v2.14.0"; \
			ARCH=$$(uname -m); \
			if [ "$$ARCH" = "aarch64" ] || [ "$$ARCH" = "arm64" ]; then ARCH="arm64"; else ARCH="x86_64"; fi; \
			curl -sL "https://github.com/hadolint/hadolint/releases/download/$$VERSION/hadolint-Linux-$$ARCH" -o /tmp/hadolint; \
			chmod +x /tmp/hadolint; \
			mkdir -p "$$(go env GOPATH)/bin"; \
			mv /tmp/hadolint "$$(go env GOPATH)/bin/hadolint"; \
			echo "Installed hadolint to $$(go env GOPATH)/bin/hadolint"; \
		else \
			echo "Error: Unsupported platform $$(uname -s)"; \
			echo "Please install hadolint manually from https://github.com/hadolint/hadolint#install"; \
			exit 1; \
		fi; \
	fi

# Semantic Convention Registry targets

weaver-install: ## Install OTel Weaver if not present
	@if ! command -v weaver >/dev/null 2>&1; then \
		echo "Installing OTel Weaver..."; \
		WEAVER_VERSION="v0.19.0"; \
		if [ "$$(uname -s)" = "Darwin" ]; then \
			if [ "$$(uname -m)" = "arm64" ]; then \
				WEAVER_ARCH="aarch64-apple-darwin"; \
			else \
				WEAVER_ARCH="x86_64-apple-darwin"; \
			fi; \
		elif [ "$$(uname -s)" = "Linux" ]; then \
			WEAVER_ARCH="x86_64-unknown-linux-gnu"; \
		else \
			echo "Error: Unsupported platform $$(uname -s)"; \
			exit 1; \
		fi; \
		WEAVER_URL="https://github.com/open-telemetry/weaver/releases/download/$${WEAVER_VERSION}/weaver-$${WEAVER_ARCH}.tar.xz"; \
		echo "Downloading weaver from $${WEAVER_URL}..."; \
		mkdir -p /tmp/weaver-install; \
		curl -fsSL "$${WEAVER_URL}" -o /tmp/weaver-install/weaver.tar.xz; \
		tar -xJf /tmp/weaver-install/weaver.tar.xz -C /tmp/weaver-install; \
		WEAVER_BIN=$$(find /tmp/weaver-install -name weaver -type f); \
		if [ -z "$$WEAVER_BIN" ]; then \
			echo "Error: weaver binary not found in archive"; \
			rm -rf /tmp/weaver-install; \
			exit 1; \
		fi; \
		chmod +x "$$WEAVER_BIN"; \
		mkdir -p "$$(go env GOPATH)/bin"; \
		mv "$$WEAVER_BIN" "$$(go env GOPATH)/bin/weaver"; \
		rm -rf /tmp/weaver-install; \
		echo "Installed weaver to $$(go env GOPATH)/bin/weaver"; \
		weaver --version; \
	else \
		echo "OTel Weaver is already installed at $$(command -v weaver)"; \
		weaver --version; \
	fi

# Semantic Conventions Validation Targets
lint/semantic-conventions: ## Validate semantic convention registry against the project's version
lint/semantic-conventions: weaver-install
	@echo "Validating semantic convention registry..."
	@# Read the semconv version from .semconv-version file (ignore comments and empty lines)
	@if [ ! -f .semconv-version ]; then \
		echo "Error: .semconv-version file not found"; \
		exit 1; \
	fi; \
	CURRENT_VERSION=$$(grep -E '^v[0-9]+\.[0-9]+\.[0-9]+' .semconv-version | head -1 | tr -d '[:space:]'); \
	if [ -z "$$CURRENT_VERSION" ]; then \
		echo "Error: No version found in .semconv-version file"; \
		exit 1; \
	fi; \
	echo "Checking semantic conventions registry at version: $$CURRENT_VERSION"; \
	echo "Cloning semantic-conventions repository..."; \
	rm -rf /tmp/semconv-$$$$; \
	git clone --depth 1 --branch $$CURRENT_VERSION https://github.com/open-telemetry/semantic-conventions.git /tmp/semconv-$$$$ 2>/dev/null || { \
		echo "::error::Failed to clone semantic-conventions repository at version $$CURRENT_VERSION"; \
		rm -rf /tmp/semconv-$$$$; \
		exit 1; \
	}; \
	weaver registry check --registry /tmp/semconv-$$$$/model; \
	EXIT_CODE=$$?; \
	rm -rf /tmp/semconv-$$$$; \
	exit $$EXIT_CODE

semantic-conventions/diff: ## Generate diff between current version and latest (non-blocking informational check)
semantic-conventions/diff: weaver-install
	@echo "Generating semantic convention registry diff (current vs latest)..."
	@mkdir -p tmp
	@# Read the semconv version from .semconv-version file (ignore comments and empty lines)
	@if [ ! -f .semconv-version ]; then \
		echo "Error: .semconv-version file not found"; \
		exit 1; \
	fi; \
	CURRENT_VERSION=$$(grep -E '^v[0-9]+\.[0-9]+\.[0-9]+' .semconv-version | head -1 | tr -d '[:space:]'); \
	if [ -z "$$CURRENT_VERSION" ]; then \
		echo "Error: No version found in .semconv-version file"; \
		exit 1; \
	fi; \
	echo "Current project version: $$CURRENT_VERSION"; \
	echo "Cloning semantic-conventions repositories..."; \
	rm -rf /tmp/semconv-current-$$$$ /tmp/semconv-latest-$$$$ tmp/registry-diff-latest; \
	git clone --depth 1 --branch $$CURRENT_VERSION https://github.com/open-telemetry/semantic-conventions.git /tmp/semconv-current-$$$$ 2>/dev/null && \
	git clone --depth 1 https://github.com/open-telemetry/semantic-conventions.git /tmp/semconv-latest-$$$$ 2>/dev/null || { \
		echo "⚠️  Warning: Failed to clone repositories (this is non-blocking)"; \
		echo "⚠️  Registry diff generation failed." > tmp/registry-diff-latest.md; \
		rm -rf /tmp/semconv-current-$$$$ /tmp/semconv-latest-$$$$; \
		exit 0; \
	}; \
	mkdir -p tmp/registry-diff-latest; \
	weaver registry diff \
		--registry /tmp/semconv-latest-$$$$/model \
		--baseline-registry /tmp/semconv-current-$$$$/model \
		--diff-format markdown \
		--output tmp/registry-diff-latest || { \
			echo "⚠️  Warning: Registry diff generation failed (this is non-blocking)"; \
			rm -rf tmp/registry-diff-latest; \
			echo "⚠️  Registry diff generation failed." > tmp/registry-diff-latest.md; \
		}; \
	rm -rf /tmp/semconv-current-$$$$ /tmp/semconv-latest-$$$$; \
	if [ -f tmp/registry-diff-latest/diff.md ]; then \
		mv tmp/registry-diff-latest/diff.md tmp/registry-diff-latest.md; \
		rm -rf tmp/registry-diff-latest; \
		echo ""; \
		echo "🆕 Available updates (latest vs $$CURRENT_VERSION):"; \
		echo "Saved to: tmp/registry-diff-latest.md"; \
		echo ""; \
		cat tmp/registry-diff-latest.md; \
	elif [ -f tmp/registry-diff-latest.md ]; then \
		echo ""; \
		echo "⚠️  Registry diff generation failed."; \
		cat tmp/registry-diff-latest.md; \
	fi; \
	exit 0

semantic-conventions/resolve: ## Display the current semantic conventions version
semantic-conventions/resolve:
	@echo "Semantic conventions version management"
	@echo "========================================"
	@if [ ! -f .semconv-version ]; then \
		echo "Error: .semconv-version file not found"; \
		exit 1; \
	fi; \
	CURRENT_VERSION=$$(grep -E '^v[0-9]+\.[0-9]+\.[0-9]+' .semconv-version | head -1 | tr -d '[:space:]'); \
	if [ -z "$$CURRENT_VERSION" ]; then \
		echo "Error: No version found in .semconv-version file"; \
		exit 1; \
	fi; \
	echo "Current version: $$CURRENT_VERSION"; \
	echo ""; \
	echo "Checking for latest version..."; \
	LATEST_TAG=$$(git ls-remote --tags --refs https://github.com/open-telemetry/semantic-conventions.git 2>/dev/null | \
		grep -E 'refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$$' | \
		awk -F/ '{print $$NF}' | \
		sort -t. -k1,1n -k2,2n -k3,3n | \
		tail -1); \
	if [ -n "$$LATEST_TAG" ]; then \
		echo "Latest available: $$LATEST_TAG"; \
		if [ "$$CURRENT_VERSION" != "$$LATEST_TAG" ]; then \
			echo ""; \
			echo "🆕 Update available: $$CURRENT_VERSION → $$LATEST_TAG"; \
		else \
			echo "✅ You are using the latest version"; \
		fi; \
	else \
		echo "⚠️  Unable to check latest version"; \
	fi
