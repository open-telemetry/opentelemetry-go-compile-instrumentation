# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Use bash for all shell commands (required for pipefail and other bash features)
SHELL := /bin/bash

.PHONY: all test test-unit test-integration test-e2e format lint build build-all build/pkg install package clean \
        build-demo build-demo-grpc build-demo-http format/go format/yaml lint/go lint/yaml \
        lint/action lint/makefile lint/license-header lint/license-header/fix lint/dockerfile actionlint yamlfmt gotestfmt ratchet ratchet/pin \
        ratchet/update ratchet/check golangci-lint embedmd checkmake hadolint help docs check-embed check-api-sync check-golden-files \
        test-unit/update-golden test-unit/tool test-unit/pkg test-unit/demo \
        test-unit/coverage test-unit/tool/coverage test-unit/pkg/coverage \
        test-integration/coverage test-e2e/coverage \
        registry-diff registry-check registry-resolve weaver-install tidy/test-apps \
        adr-tools adr-new adr-list \
        benchmark benchmark/run

# Constant variables
BINARY_NAME := otelc
PLATFORMS := darwin/amd64 linux/amd64 windows/amd64 darwin/arm64 linux/arm64
TOOL_DIR := tool/cmd
INST_PKG_GZIP = otelc-pkg.gz
INST_PKG_TMP = pkg_temp
API_SYNC_SOURCE = pkg/inst/context.go
API_SYNC_TARGET = tool/internal/instrument/api.tmpl
TOOLS_DIR = .tools
GO_VERSION = 1.25

##@ Tooling

TOOLS := $(CURDIR)/.bin

# Tools built from .tools module
$(TOOLS):
	@mkdir -p $@

$(TOOLS)/%: $(TOOLS_DIR)/go.mod | $(TOOLS)
	cd $(TOOLS_DIR) && \
	go build -o $@ $(PACKAGE)

CROSSLINK = $(TOOLS)/crosslink
$(CROSSLINK): PACKAGE=go.opentelemetry.io/build-tools/crosslink

# Go tools built from .tools module (pinned versions in .tools/go.mod)
GOTESTFMT = $(TOOLS)/gotestfmt
$(GOTESTFMT): PACKAGE=github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt

GOLANGCI_LINT = $(TOOLS)/golangci-lint
$(GOLANGCI_LINT): PACKAGE=github.com/golangci/golangci-lint/v2/cmd/golangci-lint

ACTIONLINT = $(TOOLS)/actionlint
$(ACTIONLINT): PACKAGE=github.com/rhysd/actionlint/cmd/actionlint

YAMLFMT = $(TOOLS)/yamlfmt
$(YAMLFMT): PACKAGE=github.com/google/yamlfmt/cmd/yamlfmt

RATCHET = $(TOOLS)/ratchet
$(RATCHET): PACKAGE=github.com/sethvargo/ratchet

EMBEDMD = $(TOOLS)/embedmd
$(EMBEDMD): PACKAGE=github.com/campoy/embedmd

CHECKMAKE = $(TOOLS)/checkmake
$(CHECKMAKE): PACKAGE=github.com/checkmake/checkmake/cmd/checkmake

# Phony targets to build tools from .tools module (no go install; binaries in .bin/)
gotestfmt: $(GOTESTFMT) ## Build gotestfmt from .tools
golangci-lint: $(GOLANGCI_LINT) ## Build golangci-lint from .tools
actionlint: $(ACTIONLINT) ## Build actionlint from .tools
yamlfmt: $(YAMLFMT) ## Build yamlfmt from .tools
ratchet: $(RATCHET) ## Build ratchet from .tools
embedmd: $(EMBEDMD) ## Build embedmd from .tools
checkmake: $(CHECKMAKE) ## Build checkmake from .tools

# Dynamic variables
GOOS ?= $(shell go env GOOS)
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d')
LDFLAGS := -X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)
GO_BUILD_CMD := go build -trimpath -a -ldflags "$(LDFLAGS)"
ALL_GO_MOD_DIRS := $(shell find . -type f -name 'go.mod' -exec dirname {} \; | sort)
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
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \; | grep -v "pkg/instrumentation/runtime" | grep -v "pkg/instrumentation/databasesql"); \
	for moddir in $$PKG_MODULES; do \
		echo "Building $$moddir..."; \
		(cd $$moddir && go mod tidy && go build ./...); \
	done
	@echo "All pkg modules built successfully"

build: build/pkg package ## Build the instrumentation tool
	@echo "Building instrumentation tool..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@$(GO_BUILD_CMD) -o $(BINARY_NAME)$(EXT) ./$(TOOL_DIR)
	@./$(BINARY_NAME)$(EXT) version

build-all: build/pkg package ## Build the instrumentation tool for all platforms
	@echo "Building instrumentation tool for all platforms..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		echo "Building for $$GOOS/$$GOARCH..."; \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		env GOOS=$$GOOS GOARCH=$$GOARCH $(GO_BUILD_CMD) -o dist/$(BINARY_NAME)-$$GOOS-$$GOARCH$$EXT ./$(TOOL_DIR); \
	done
	@echo "All builds completed. Artifacts in dist/"

install: package ## Install otelc to $$GOPATH/bin (auto-packages instrumentation)
	@echo "Installing otelc..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	go install -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" ./$(TOOL_DIR)

.ONESHELL:
package: ## Package the instrumentation code into binary
	@echo "Packaging instrumentation code into binary..."
	@set -euo pipefail
	@rm -rf $(INST_PKG_TMP)
	@if [ ! -d pkg ]; then \
		echo "Error: pkg directory does not exist"; \
		exit 1; \
	fi
	@cp -r pkg $(INST_PKG_TMP)
	@(cd $(INST_PKG_TMP) && go mod tidy)
	@tar -czf $(INST_PKG_GZIP) --exclude='*.log' $(INST_PKG_TMP)
	@mkdir -p tool/data/
	@mv $(INST_PKG_GZIP) tool/data/
	@rm -rf $(INST_PKG_TMP)
	@echo "Package created successfully at tool/data/$(INST_PKG_GZIP)"

build-demo: ## Build all demos
build-demo: build-demo-grpc build-demo-http

build-demo-grpc: ## Build gRPC demo server and client
	@$(MAKE) -C demo/app/grpc build

build-demo-http: ## Build HTTP demo server and client
	@$(MAKE) -C demo/app/http build

##@ Code Quality

format: ## Format Go code and YAML files
format: format/go format/yaml lint/license-header/fix

format/go: ## Format Go code only
format/go: $(GOLANGCI_LINT)
	@echo "Formatting Go code..."
	$(GOLANGCI_LINT) fmt --config .tools/golangci.yml

format/yaml: ## Format YAML files only (excludes testdata)
format/yaml: $(YAMLFMT)
	@echo "Formatting YAML files..."
	$(YAMLFMT) -conf .tools/yamlfmt -dstar '**/*.yml' '**/*.yaml'

lint: ## Run all linters (Go, YAML, GitHub Actions, Makefile, Dockerfile)
lint: lint/go lint/yaml lint/action lint/makefile lint/license-header lint/dockerfile

lint/action: ## Lint GitHub Actions workflows
lint/action: $(ACTIONLINT) ratchet/check
	@echo "Linting GitHub Actions workflows..."
	$(ACTIONLINT)

lint/go: ## Run golangci-lint on Go code
lint/go: $(GOLANGCI_LINT)
	@echo "Linting Go code..."
	$(GOLANGCI_LINT) run --config .tools/golangci.yml

lint/go/fix: ## Run golangci-lint on Go code and fix the issues
lint/go/fix: $(GOLANGCI_LINT)
	@echo "Linting Go code..."
	$(GOLANGCI_LINT) run --config .tools/golangci.yml --fix

lint/yaml: ## Lint YAML formatting
lint/yaml: $(YAMLFMT)
	@echo "Linting YAML files..."
	$(YAMLFMT) -conf .tools/yamlfmt -lint -dstar '**/*.yml' '**/*.yaml'

lint/dockerfile: ## Lint Dockerfiles
lint/dockerfile: hadolint
	@echo "Linting Dockerfiles..."
	@HADOLINT_CMD="hadolint"; \
	if command -v hadolint >/dev/null 2>&1 && hadolint --version >/dev/null 2>&1; then \
		HADOLINT_CMD="hadolint"; \
	elif [ -f /opt/homebrew/bin/hadolint ]; then \
		HADOLINT_CMD="/opt/homebrew/bin/hadolint"; \
	fi; \
	$$HADOLINT_CMD -c .tools/hadolint.yaml demo/app/grpc/client/Dockerfile demo/app/grpc/server/Dockerfile demo/app/http/client/Dockerfile demo/app/http/server/Dockerfile

lint/makefile: ## Lint Makefile
lint/makefile: $(CHECKMAKE)
	@echo "Linting Makefile..."
	$(CHECKMAKE) --config .tools/checkmake Makefile

lint/license-header: ## Check license headers in source files
	@.github/scripts/license-check.sh

.PHONY: lint/license-header/fix
lint/license-header/fix: ## Add missing license headers to source files
	@.github/scripts/license-check.sh --fix

##@ Markdown

.PHONY: lint/markdown
lint/markdown: ## Lint Check the markdown files.
	npx markdownlint-cli -c .tools/markdownlint.yaml **/*.md

.PHONY: lint/markdown/fix
lint/markdown/fix: ## Lint Check the markdown files and fix them.
	npx markdownlint-cli -c .tools/markdownlint.yaml --fix **/*.md

# Ratchet targets for GitHub Actions pinning

ratchet/pin: ## Pin GitHub Actions to commit SHAs
ratchet/pin: $(RATCHET)
	@echo "Pinning GitHub Actions to commit SHAs..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs $(RATCHET) pin

ratchet/update: ## Update pinned GitHub Actions to latest versions
ratchet/update: $(RATCHET)
	@echo "Updating pinned GitHub Actions to latest versions..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs $(RATCHET) update

ratchet/check: ## Verify all GitHub Actions are pinned
ratchet/check: $(RATCHET)
	@echo "Checking GitHub Actions are pinned..."
	@find .github/workflows -name '*.yml' -o -name '*.yaml' | xargs $(RATCHET) lint

##@ Documentation

docs: ## Update embedded documentation in markdown files
docs: $(EMBEDMD) tmp/make-help.txt
	@echo "Updating embedded documentation..."
	$(EMBEDMD) -w CONTRIBUTING.md README.md

tmp/make-help.txt: ## Generate make help output for embedding in documentation
tmp/make-help.txt: $(MAKEFILE_LIST)
	@mkdir -p tmp
	@$(MAKE) --no-print-directory help > tmp/make-help.txt

##@ Architecture Decision Records

adr-tools: ## Install adr-tools if not present
	@if command -v adr >/dev/null 2>&1; then \
		echo "adr-tools is already installed at $$(command -v adr)"; \
	else \
		echo "Installing adr-tools..."; \
		if [ "$$(uname -s)" = "Darwin" ]; then \
			if command -v brew >/dev/null 2>&1; then \
				brew install adr-tools; \
			else \
				echo "Error: Homebrew not found. Install Homebrew from https://brew.sh/ and try again."; \
				exit 1; \
			fi; \
		elif [ "$$(uname -s)" = "Linux" ]; then \
			TMPDIR=$$(mktemp -d); \
			git clone --depth 1 https://github.com/npryce/adr-tools.git "$$TMPDIR/adr-tools"; \
			mkdir -p "$$(go env GOPATH)/bin"; \
			cp "$$TMPDIR/adr-tools/src/"adr-* "$$(go env GOPATH)/bin/"; \
			chmod +x "$$(go env GOPATH)/bin/adr-"*; \
			rm -rf "$$TMPDIR"; \
			echo "Installed adr-tools to $$(go env GOPATH)/bin/"; \
		else \
			echo "Error: Unsupported platform $$(uname -s)"; \
			echo "Please install adr-tools manually from https://github.com/npryce/adr-tools"; \
			exit 1; \
		fi; \
	fi

adr-new: ## Create a new ADR: make adr-new TITLE="Short decision title"
	@if ! command -v adr >/dev/null 2>&1; then \
		echo "adr-tools not found. Run 'make adr-tools' to install."; \
		exit 1; \
	fi
	@if [ -z "$(TITLE)" ]; then \
		echo "Usage: make adr-new TITLE=\"Short decision title\""; \
		exit 1; \
	fi
	adr new -c docs/adr "$(TITLE)"

adr-list: ## List all ADRs
	@if ! command -v adr >/dev/null 2>&1; then \
		echo "adr-tools not found. Run 'make adr-tools' to install."; \
		exit 1; \
	fi
	adr list -c docs/adr

##@ Validation

check-embed: ## Verify that embedded files exist (required for tests)
	@echo "Checking embedded files..."
	@if [ ! -f tool/data/$(INST_PKG_GZIP) ]; then \
		echo "Error: tool/data/$(INST_PKG_GZIP) does not exist"; \
		echo "Run 'make package' to generate it"; \
		exit 1; \
	fi
	@echo "All embedded files present"

check-api-sync: ## Verify api.tmpl is in sync with pkg/inst/context.go
	@echo "Checking api.tmpl sync with $(API_SYNC_SOURCE)..."
	@if ! diff -q $(API_SYNC_SOURCE) $(API_SYNC_TARGET) > /dev/null 2>&1; then \
		echo "Error: $(API_SYNC_TARGET) is out of sync with $(API_SYNC_SOURCE)"; \
		echo "Run 'make build' to sync, or: cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)"; \
		diff $(API_SYNC_SOURCE) $(API_SYNC_TARGET) || true; \
		exit 1; \
	fi
	@echo "api.tmpl is in sync with $(API_SYNC_SOURCE)"

.ONESHELL:
check-golden-files: ## Verify golden test files are up to date
check-golden-files: package
	@echo "Checking golden files are up to date..."
	set -euo pipefail
	cd tool/internal/instrument && go test -v -timeout=5m -count=1 ./... -args -update
	cd "$(CURDIR)"
	if ! git diff --exit-code tool/internal/instrument/testdata/golden/; then \
		echo "Error: golden files are stale"; \
		echo "Run 'make test-unit/update-golden' to regenerate"; \
		exit 1; \
	fi
	git status --porcelain -- tool/internal/instrument/testdata/golden/ | grep -q . && (echo "Golden files have untracked changes"; exit 1) || true
	echo "Golden files are up to date"

##@ Benchmarking

BENCH_HARNESS_DIR := test/bench/cmd/bench
BENCH_SCENARIOS_DIR := test/bench/scenarios
BENCH_OUTPUT := bench.json
BENCH_ITERATIONS ?= 5
BENCH_WARMUP ?= 1
BENCH_MAX_OVERHEAD_PCT ?= -1

benchmark: build ## Build benchmark harness and print usage
	@echo "Building benchmark harness..."
	@go build -C $(BENCH_HARNESS_DIR) -o $(TOOLS)/bench .
	@echo ""
	@echo "Run benchmarks with: make benchmark/run"
	@echo "Override iterations: make benchmark/run BENCH_ITERATIONS=10"

.ONESHELL:
benchmark/run: build ## Run compile-time benchmarks and emit bench.json
benchmark/run: benchmark
	@echo "Running compile-time benchmarks (iterations=$(BENCH_ITERATIONS), warmup=$(BENCH_WARMUP))..."
	set -euo pipefail
	nice -n -10 $(TOOLS)/bench \
		-otelc=$(CURDIR)/$(BINARY_NAME) \
		-scenarios=$(CURDIR)/$(BENCH_SCENARIOS_DIR) \
		-iterations=$(BENCH_ITERATIONS) \
		-warmup=$(BENCH_WARMUP) \
		-max-overhead-pct=$(BENCH_MAX_OVERHEAD_PCT) \
		-output=$(CURDIR)/$(BENCH_OUTPUT)
	@echo ""
	@echo "Results written to $(BENCH_OUTPUT)"

##@ Testing
# NOTE: Tests require the 'package' target to run first because tool/data/export.go
# uses //go:embed to embed otelc-pkg.gz at compile time. If the file doesn't exist
# when Go compiles the test packages, the embed will fail.

test: ## Run all tests (unit + integration + e2e)
test: test-unit test-integration test-e2e

test-unit: test-unit/tool test-unit/pkg test-unit/demo ## Run all unit tests (tool + pkg + demo)

.ONESHELL:
test-unit/update-golden: ## Run unit tests and update golden files
test-unit/update-golden: package
	@echo "Running unit tests and updating golden files..."
	set -euo pipefail
	cd tool/internal/instrument && go test -v -timeout=5m -count=1 ./... -args -update

# - Does NOT use gotestfmt because v2.5.0 has a bug that causes panics when go test
#   outputs build errors (JSON lines with ImportPath but no Package field).

.ONESHELL:
test-unit/tool: build package $(GOTESTFMT) ## Run unit tests for tool modules only
	@echo "Running tool unit tests..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... 2>&1 | tee ./gotest-unit-tool.log

# Notes on test-unit/pkg implementation:
# - Uses find -maxdepth 4 to discover modules at pkg/instrumentation/{name}/ and pkg/instrumentation/{name}/{sub} levels only.
#   This naturally excludes client/ and server/ subdirectories (which will have link errors because it requires the parent module to be built).
# - Excludes "runtime" and "databasesql" modules (have build errors because of compile-time field injection) and root "pkg" module (no tests).
# - Skips modules without test files to avoid empty test output.
# - Uses go test -C to run tests without changing directories (cleaner, more reliable).
# - Does NOT use gotestfmt because v2.5.0 has a bug that causes panics when go test
#   outputs build errors (JSON lines with ImportPath but no Package field).
#   Standard go test -v output is readable enough without formatting.
.ONESHELL:
test-unit/pkg: package ## Run unit tests for pkg modules only
	@echo "Running pkg unit tests..."
	set -euo pipefail
	rm -f ./gotest-unit-pkg.log
	PKG_MODULES=$$(find pkg -maxdepth 4 -name "go.mod" -type f -exec dirname {} \; | grep -v "runtime" | grep -v "databasesql" | grep -v "^pkg$$"); \
	for moddir in $$PKG_MODULES; do \
		if ! find "$$moddir" -name "*_test.go" -type f | grep -q .; then \
			echo "Skipping $$moddir (no tests)..."; \
			continue; \
		fi; \
		echo "Testing $$moddir..."; \
		(cd "$$moddir" && go mod tidy); \
		go test -C "$$moddir" -v -shuffle=on -timeout=5m -count=1 ./... 2>&1 | tee -a ./gotest-unit-pkg.log; \
	done

.ONESHELL:
test-unit/demo: ## Run unit tests for demo applications
	@echo "Running demo unit tests..."
	set -euo pipefail
	rm -f ./gotest-unit-demo.log
	DEMO_MODULES=$$(find demo -maxdepth 3 -name "go.mod" -type f -exec dirname {} \;); \
	for moddir in $$DEMO_MODULES; do \
		if ! find "$$moddir" -maxdepth 1 -name "*_test.go" -type f | grep -q .; then \
			echo "Skipping $$moddir (no tests)..."; \
			continue; \
		fi; \
		echo "Testing $$moddir..."; \
		(cd "$$moddir" && go mod tidy); \
		go test -C "$$moddir" -v -shuffle=on -timeout=5m -count=1 ./... 2>&1 | tee -a ./gotest-unit-demo.log; \
	done


test-unit/coverage: test-unit/tool/coverage test-unit/pkg/coverage ## Run all unit tests with coverage

.ONESHELL:
test-unit/tool/coverage: package ## Run unit tests with coverage for tool modules only
	@echo "Running tool unit tests with coverage..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... -coverprofile=coverage-tool.txt -covermode=atomic 2>&1 | tee ./gotest-unit-tool.log

# Same implementation as test-unit/pkg but with coverage flags.
# Coverage files from each module are merged into a single coverage-pkg.txt file.
.ONESHELL:
test-unit/pkg/coverage: package ## Run unit tests with coverage for pkg modules only
	@echo "Running pkg unit tests with coverage..."
	set -euo pipefail
	rm -f ./gotest-unit-pkg.log
	PKG_MODULES=$$(find pkg -maxdepth 4 -name "go.mod" -type f -exec dirname {} \; | grep -v "runtime" | grep -v "databasesql" | grep -v "^pkg$$"); \
	for moddir in $$PKG_MODULES; do \
		if ! find "$$moddir" -name "*_test.go" -type f | grep -q .; then \
			echo "Skipping $$moddir (no tests)..."; \
			continue; \
		fi; \
		echo "Testing $$moddir with coverage..."; \
		(cd "$$moddir" && go mod tidy); \
		go test -C "$$moddir" -v -shuffle=on -timeout=5m -count=1 ./... -coverprofile=coverage.txt -covermode=atomic 2>&1 | tee -a ./gotest-unit-pkg.log; \
	done
	@echo "Merging coverage files into coverage-pkg.txt..."
	@echo "mode: atomic" > coverage-pkg.txt
	@find pkg -name "coverage.txt" -exec grep -h -v "^mode:" {} \; >> coverage-pkg.txt 2>/dev/null || true
	@find pkg -name "coverage.txt" -delete 2>/dev/null || true

.ONESHELL:
test-integration: go-protobuf-plugins ## Run integration tests
test-integration: build build-demo
	@echo "Running integration tests..."
	set -euo pipefail
	go -C "test" test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./integration/... 2>&1 | tee ../gotest-integration.log

.ONESHELL:
test-integration/coverage: ## Run integration tests with coverage report
test-integration/coverage: build build-demo
	@echo "Running integration tests with coverage report..."
	set -euo pipefail
	go -C "test" test -json -v -shuffle=on -timeout=10m -count=1 -tags integration ./integration/... -coverprofile=../coverage-integration.txt -covermode=atomic 2>&1 | tee ../gotest-integration.log

.ONESHELL:
test-e2e: ## Run e2e tests
test-e2e: build build-demo
	@echo "Running e2e tests..."
	set -euo pipefail
	cd test && go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./e2e/... 2>&1 | tee ../gotest-e2e.log

.ONESHELL:
test-e2e/coverage: ## Run e2e tests with coverage report
test-e2e/coverage: build build-demo
	@echo "Running e2e tests with coverage report..."
	set -euo pipefail
	cd test && go test -json -v -shuffle=on -timeout=10m -count=1 -tags e2e ./e2e/... -coverprofile=../coverage-e2e.txt -covermode=atomic 2>&1 | tee ../gotest-e2e.log

.PHONY: crosslink
crosslink: $(CROSSLINK) ## Update intra-repository dependencies in all go modules
	@# Clean .otel-build directories before generating go.work to avoid parsing generated go.mod
	@find . -type d -name ".otel-build" -exec rm -rf {} + 2>/dev/null || true
	@echo "Updating intra-repository dependencies in all go modules" \
		&& $(CROSSLINK) --root=$(CURDIR)

.PHONY: go-work
go-work: $(CROSSLINK) ## Generate go.work file for local development
	@echo "Generating go.work file for local development..."
	@$(CROSSLINK) work --root=$(CURDIR) --go=$(GO_VERSION)
	@# Fix go version to include patch version (crosslink only supports major.minor)
	@sed -i.bak 's/^go $(GO_VERSION)$$/go $(GO_VERSION).0/' go.work && rm -f go.work.bak
	@echo "go.work file generated successfully"

.PHONY: go-mod-tidy
go-mod-tidy: $(ALL_GO_MOD_DIRS:%=go-mod-tidy/%) ## Run go mod tidy in all modules

go-mod-tidy/%: DIR=$*
go-mod-tidy/%: crosslink
	@echo "Running go mod tidy in $(DIR)" \
		&& cd $(DIR) \
		&& go mod tidy

##@ Utilities

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf dist .bin
	rm -f $(BINARY_NAME)$(EXT)
	rm -f demo/app/basic/basic
	rm -f demo/app/grpc/server/server
	rm -rf demo/app/grpc/server/pb
	rm -f demo/app/grpc/client/client
	rm -f demo/app/http/server/server
	rm -f demo/app/http/client/client
	find demo -type d -name ".otelc-build" -exec rm -rf {} +
	find demo -type f -name "otelc.runtime.go" -delete
	find . -type f \( -name gotest-unit-tool.log -o -name gotest-unit-pkg.log -o -name gotest-integration.log -o -name gotest-e2e.log \) -delete

.ONESHELL:
tidy/test-apps: ## Run go mod tidy in all test app modules
	@echo "Running go mod tidy in test app modules..."
	@set -euo pipefail
	@TEST_APP_MODULES=$$(find test/apps -name "go.mod" -type f -exec dirname {} \;); \
	for moddir in $$TEST_APP_MODULES; do \
		echo "Tidying $$moddir..."; \
		(cd "$$moddir" && go mod tidy); \
	done
	@echo "All test app modules tidied successfully"

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
