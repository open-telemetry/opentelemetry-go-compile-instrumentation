# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Use bash for all shell commands (required for pipefail and other bash features)
SHELL := /bin/bash

# Many recipes use .ONESHELL, so that "set -euo pipefail" applies to all the
# lines of the recipe. GNU Make 3.81 (the default on macOS) does not support
# .ONESHELL: it runs each line in a different shell, and failures can be
# hidden. Thus GNU Make 3.82 or later is required.
ifeq ($(filter oneshell,$(.FEATURES)),)
$(error GNU Make 3.82 or later is required (found $(MAKE_VERSION)). On macOS, run "brew install make", then use "gmake")
endif

.PHONY: all test test-unit test-integration format lint build build-all build/pkg install manifest clean \
        build-demo build-demo-grpc build-demo-http format/go format/yaml lint/go lint/yaml \
        lint/action lint/makefile lint/license-header lint/license-header/fix lint/dockerfile actionlint yamlfmt gotestfmt ratchet ratchet/pin \
        ratchet/update ratchet/check golangci-lint embedmd checkmake hadolint help docs check-api-sync check-golden-files check-test-file-naming \
        test-unit/update-golden test-unit/race test-unit/tool test-unit/pkg test-unit/demo test-unit/helper \
        test-unit/coverage test-unit/tool/coverage test-unit/pkg/coverage \
        check-coverage test-integration/coverage \
        tidy/test-apps \
        adr-tools adr-new adr-list \
        benchmark/codspeed benchmark/threshold govulncheck

# Constant variables
BINARY_NAME := otelc
PLATFORMS := darwin/amd64 linux/amd64 windows/amd64 darwin/arm64 linux/arm64
TOOL_DIR := tool/cmd/otelc
API_SYNC_SOURCE = pkg/hook/context.go
API_SYNC_TARGET = tool/internal/instrument/api.tmpl
TOOLS_DIR = .tools
GO_VERSION = 1.26
INTEGRATION_TEST_RUN ?= .
TOOL_COVERAGE_THRESHOLD ?= 68
PKG_COVERAGE_THRESHOLD ?= 70
RACE ?= 0
RACE_FLAG := $(if $(filter 1 true,$(RACE)),-race,)

# Modules scanned by govulncheck for known Go CVEs (tool version pinned in
# .tools/go.mod, Renovate-managed like every other .tools binary).
# Core modules: root module (covers tool/) plus pkg/ module.
# Demos are intentionally excluded (pinned example deps).
GOVULNCHECK_CORE_MODULES := . $(shell find pkg -type f -name 'go.mod' -exec dirname {} \; | sort)

##@ Tooling

TOOLS := $(CURDIR)/.bin

# Tools built from .tools module
$(TOOLS):
	@mkdir -p $@

$(TOOLS)/%: $(TOOLS_DIR)/go.mod | $(TOOLS)
	cd $(TOOLS_DIR) && \
	GOWORK=off go build -o $@ $(PACKAGE)

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
$(EMBEDMD): PACKAGE=github.com/campoy/embedmd/v2

CHECKMAKE = $(TOOLS)/checkmake
$(CHECKMAKE): PACKAGE=github.com/checkmake/checkmake/cmd/checkmake

GOVULNCHECK = $(TOOLS)/govulncheck
$(GOVULNCHECK): PACKAGE=golang.org/x/vuln/cmd/govulncheck

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
# Newest release or pre-release tag reachable from HEAD (e.g. v1.3.0 or
# v1.3.0-rc1), excluding pkg/* module tags. Falls back to v0.0.0 when no
# matching tag exists.
VERSION := $(shell git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || echo "v0.0.0")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d')
MODULE_PATH = go.opentelemetry.io/otelc
LDFLAGS := -X $(MODULE_PATH)/tool/util.Version=$(VERSION) -X $(MODULE_PATH)/tool/util.CommitHash=$(COMMIT_HASH) -X $(MODULE_PATH)/tool/util.BuildTime=$(BUILD_TIME)
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
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \;); \
	for moddir in $$PKG_MODULES; do \
		echo "Building $$moddir..."; \
		(cd $$moddir && go mod tidy && go build ./...); \
	done
	@echo "All pkg modules built successfully"

build: build/pkg ## Build the instrumentation tool
	@echo "Building instrumentation tool..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@$(GO_BUILD_CMD) -o $(BINARY_NAME)$(EXT) ./$(TOOL_DIR)
	@./$(BINARY_NAME)$(EXT) version

build-all: build/pkg ## Build the instrumentation tool for all platforms
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

install: ## Install otelc to $$GOPATH/bin
	@echo "Installing otelc..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	go install -ldflags "-X $(MODULE_PATH)/tool/util.Version=$(VERSION) -X $(MODULE_PATH)/tool/util.CommitHash=$(COMMIT_HASH) -X $(MODULE_PATH)/tool/util.BuildTime=$(BUILD_TIME)" ./$(TOOL_DIR)

manifest: ## Generate the instrumentation manifest
	@echo "Generating instrumentation manifest..."
	@go run ./tool/cmd/gen-manifest


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
	$(YAMLFMT) -conf .tools/yamlfmt -dstar \
		-exclude '**/testdata/**' \
		'**/*.yml' '**/*.yaml'

lint: ## Run all linters (Go, YAML, GitHub Actions, Makefile, Dockerfile, typos)
lint: lint/go lint/yaml lint/action lint/makefile lint/license-header lint/dockerfile lint/typos

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
	$(YAMLFMT) -conf .tools/yamlfmt -lint -dstar \
		-exclude '**/testdata/**' \
		'**/*.yml' '**/*.yaml'

lint/dockerfile: ## Lint Dockerfiles
lint/dockerfile: hadolint
	@echo "Linting Dockerfiles..."
	@HADOLINT_CMD="hadolint"; \
	if command -v hadolint >/dev/null 2>&1 && hadolint --version >/dev/null 2>&1; then \
		HADOLINT_CMD="hadolint"; \
	elif [ -f /opt/homebrew/bin/hadolint ]; then \
		HADOLINT_CMD="/opt/homebrew/bin/hadolint"; \
	fi; \
	DOCKERFILES=$$(find demo/app -name "Dockerfile" | sort); \
	$$HADOLINT_CMD --failure-threshold warning -c .tools/hadolint.yaml $$DOCKERFILES

lint/makefile: ## Lint Makefile
lint/makefile: $(CHECKMAKE)
	@echo "Linting Makefile..."
	$(CHECKMAKE) --config .tools/checkmake Makefile

lint/license-header: ## Check license headers in source files
	@.github/scripts/license-check.sh

.PHONY: lint/license-header/fix
lint/license-header/fix: ## Add missing license headers to source files
	@.github/scripts/license-check.sh --fix

.PHONY: lint/typos
lint/typos: ## Check for typos using crate-ci/typos
	@echo "Checking for typos..."
	@if command -v typos >/dev/null 2>&1; then \
		typos --config .tools/typos.toml; \
	else \
		echo "Error: 'typos' not found on PATH."; \
		echo "Install with one of:"; \
		echo "  brew install typos-cli"; \
		echo "  cargo install typos-cli"; \
		echo "  https://github.com/crate-ci/typos/releases"; \
		echo "(The former ghcr.io/crate-ci/typos Docker image is no longer published.)"; \
		exit 1; \
	fi

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

check-api-sync: ## Verify api.tmpl is in sync with pkg/hook/context.go
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
check-golden-files:
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

check-test-file-naming: ## Verify unit test files follow naming conventions
	@echo "Checking test file naming conventions..."
	@go run ./tool/cmd/check-test-names

##@ Security

.ONESHELL:
govulncheck: $(GOVULNCHECK) ## Scan core modules (root, pkg) for known Go vulnerabilities
	@echo "Running govulncheck across $(words $(GOVULNCHECK_CORE_MODULES)) core modules..."
	@set -uo pipefail
	@status=0
	@for moddir in $(GOVULNCHECK_CORE_MODULES); do \
		echo "==> govulncheck $$moddir"; \
		(cd "$$moddir" && "$(GOVULNCHECK)" ./...) || status=1; \
	done; \
	if [ "$$status" -ne 0 ]; then echo "govulncheck: vulnerabilities found"; exit 1; fi; \
	echo "govulncheck: no reachable vulnerabilities found"

##@ Benchmarking

BENCH_DIR := test/bench
BENCH_SCENARIOS_DIR := $(BENCH_DIR)/scenarios
BENCH_TIME ?= 5x
BENCH_MAX_OVERHEAD_PCT ?= 150

benchmark/codspeed: build ## Run compile-time benchmarks using Go testing.B (for CodSpeed walltime)
	cd $(BENCH_DIR) && \
	OTELC_BIN=$(CURDIR)/$(BINARY_NAME) \
	BENCH_SCENARIOS_DIR=$(CURDIR)/$(BENCH_SCENARIOS_DIR) \
	go test -v -run=^$$ -bench=. -benchtime=$(BENCH_TIME)

benchmark/threshold: build ## Enforce absolute otelc overhead ceiling (fails if overhead exceeds BENCH_MAX_OVERHEAD_PCT)
	cd $(BENCH_DIR) && \
	OTELC_BIN=$(CURDIR)/$(BINARY_NAME) \
	BENCH_SCENARIOS_DIR=$(CURDIR)/$(BENCH_SCENARIOS_DIR) \
	BENCH_MAX_OVERHEAD_PCT=$(BENCH_MAX_OVERHEAD_PCT) \
	go test -tags=overhead_check -run=TestOverheadCeiling -v -count=1 -timeout=30m

##@ Testing

test: ## Run all tests (workflows + unit + integration)
test: test-workflows test-unit test-integration

.PHONY: test-workflows
test-workflows: ## Test GitHub Actions workflow definitions
	@GOWORK=off go -C .tools test -count=1 ./workflows

test-unit: test-unit/tool test-unit/pkg test-unit/demo test-unit/helper ## Run all unit tests (tool + pkg + demo + test helpers)

test-unit/race: ## Run all unit tests with the Go race detector enabled
	$(MAKE) test-unit RACE=1

.ONESHELL:
test-unit/update-golden: ## Run unit tests and update golden files
test-unit/update-golden:
	@echo "Running unit tests and updating golden files..."
	set -euo pipefail
	cd tool/internal/instrument && go test -v -timeout=5m -count=1 ./... -args -update

# - Does NOT use gotestfmt because v2.5.0 has a bug that causes panics when go test
#   outputs build errors (JSON lines with ImportPath but no Package field).

.ONESHELL:
test-unit/tool: build $(GOTESTFMT) ## Run unit tests for tool modules only
	@echo "Running tool unit tests..."
	set -euo pipefail
	go test $(RACE_FLAG) -json -v -shuffle=on -timeout=5m -count=1 ./tool/... 2>&1 | tee ./gotest-unit-tool.log

.ONESHELL:
test-unit/pkg: ## Run unit tests for pkg modules only
	@echo "Running pkg unit tests..."
	set -euo pipefail
	rm -f ./gotest-unit-pkg.log
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \;); \
	for moddir in $$PKG_MODULES; do \
    	if ! find "$$moddir" -name "*_test.go" -type f | grep -q .; then \
    		echo "Skipping $$moddir (no tests)..."; \
    		continue; \
    	fi; \
		echo "Testing $$moddir..."; \
		(cd "$$moddir" && go mod tidy); \
		go test -C "$$moddir" $(RACE_FLAG) -v -shuffle=on -timeout=5m -count=1 ./... 2>&1 | tee -a ./gotest-unit-pkg.log; \
	done

.ONESHELL:
test-unit/helper: ## Run unit tests for test helper packages
	@echo "Running test helper unit tests..."
	set -euo pipefail
	rm -f ./gotest-unit-helper.log
	go test -C "test" $(RACE_FLAG) -v -shuffle=on -timeout=5m -count=1 ./testutil/... 2>&1 | tee ./gotest-unit-helper.log

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
		go test -C "$$moddir" $(RACE_FLAG) -v -shuffle=on -timeout=5m -count=1 ./... 2>&1 | tee -a ./gotest-unit-demo.log; \
	done


test-unit/coverage: test-unit/tool/coverage test-unit/pkg/coverage ## Run all unit tests with coverage

.ONESHELL:
test-unit/tool/coverage: ## Run unit tests with coverage for tool modules only
	@echo "Running tool unit tests with coverage..."
	set -euo pipefail
	go test -json -v -shuffle=on -timeout=5m -count=1 ./tool/... -coverprofile=coverage-tool.txt -covermode=atomic 2>&1 | tee ./gotest-unit-tool.log

.ONESHELL:
test-unit/pkg/coverage: ## Run unit tests with coverage for pkg modules only
	@echo "Running pkg unit tests with coverage..."
	set -euo pipefail
	rm -f ./gotest-unit-pkg.log
	@PKG_MODULES=$$(find pkg -name "go.mod" -type f -exec dirname {} \;); \
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
check-coverage: test-unit/tool/coverage test-unit/pkg/coverage ## Verify the unit test coverage floor for tool and pkg modules
	@echo "Checking unit test coverage floors..."
	set -euo pipefail
	for coverage_file in coverage-tool.txt coverage-pkg.txt; do \
		case "$$coverage_file" in \
			coverage-tool.txt) threshold="$(TOOL_COVERAGE_THRESHOLD)" ;; \
			coverage-pkg.txt) threshold="$(PKG_COVERAGE_THRESHOLD)" ;; \
			*) echo "Unexpected coverage report: $$coverage_file"; exit 1 ;; \
		esac; \
		if [[ ! -f "$$coverage_file" ]]; then \
			echo "Missing coverage report: $$coverage_file"; \
			exit 1; \
		fi; \
		coverage_value=$$(awk '/^mode:/ { next } { total += $$2; if ($$3 > 0) covered += $$2 } END { if (total > 0) printf "%.1f", (covered / total) * 100; else print "0.0" }' "$$coverage_file"); \
		if [[ -z "$$coverage_value" ]]; then \
			echo "Unable to determine coverage for $$coverage_file"; \
			exit 1; \
		fi; \
		awk -v coverage="$$coverage_value" -v threshold="$$threshold" 'BEGIN { exit (coverage + 0 >= threshold + 0) ? 0 : 1 }' || { \
			echo "Coverage $$coverage_file is below $$threshold%: $$coverage_value%"; \
			exit 1; \
		}; \
		echo "$$coverage_file: $$coverage_value% (threshold $$threshold%)"; \
	done

.ONESHELL:
test-integration: go-protobuf-plugins ## Run integration tests
test-integration: build build-demo
	@echo "Running integration tests..."
	set -euo pipefail
	# 40m: linodego public-method instrumentation rewrites ~450 *Client methods per
	# instrumented build; under coverage (all tests, no shards) wall time exceeds 20m.
	go -C "test" test -json -v -shuffle=on -timeout=40m -count=1 -tags integration -run '$(value INTEGRATION_TEST_RUN)' ./integration/... 2>&1 | tee ./gotest-integration.log

.ONESHELL:
test-integration/coverage: ## Run integration tests with coverage report
test-integration/coverage: build build-demo
	@echo "Running integration tests with coverage report..."
	set -euo pipefail
	# See test-integration: linodego builds need >20m when the suite is unsharded,
	# and the unsharded suite can take more than 35m.
	# Coverage is unsharded: queued parallel tests share the suite timeout.
	# Leave headroom for late-starting cold-cache builds.
	go -C "test" test -json -v -shuffle=on -timeout=60m -count=1 -tags integration ./integration/... -coverprofile=../coverage-integration.txt -covermode=atomic 2>&1 | tee ./gotest-integration.log

.PHONY: crosslink
crosslink: $(CROSSLINK) ## Update intra-repository dependencies in all go modules
	@# Clean .otel-build directories before generating go.work to avoid parsing generated go.mod
	@find . -type d -name ".otel-build" -exec rm -rf {} + 2>/dev/null || true
	@echo "Updating intra-repository dependencies in all go modules" \
		&& $(CROSSLINK) --root=$(CURDIR) --exclude=go.opentelemetry.io/otelc/pkg --skip=go.opentelemetry.io/otelc/pkg

.PHONY: go-work
go-work: $(CROSSLINK) ## Generate go.work file for local development
	@echo "Generating go.work file for local development..."
	@$(CROSSLINK) work --root=$(CURDIR) --go=$(GO_VERSION)
	@# Fix go version to include patch version (crosslink only supports major.minor)
	@sed -i.bak 's/^go $(GO_VERSION)$$/go $(GO_VERSION).0/' go.work && rm -f go.work.bak
	@# Drop tool-only modules: their transitive deps conflict with the main modules
	@# (e.g. old monolithic genproto vs. split genproto/googleapis/rpc).
	@go work edit -dropuse ./.tools
	@go work edit -dropuse ./.github/tools
	@go work edit -dropuse ./pkg
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
	find . -type f \( -name gotest-unit-tool.log -o -name gotest-unit-pkg.log -o -name gotest-integration.log \) -delete

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

