# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Variables
BINARY_NAME := otel
DEMO_DIR := demo
TOOL_DIR := tool/cmd
INST_PKG_GZIP = otel-pkg.gz
INST_PKG_TMP = pkg_temp
API_SYNC_SOURCE = pkg/inst/context.go
API_SYNC_TARGET = tool/internal/instrument/api.tmpl

# Version variables
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d')

# Default target
.PHONY: all
all: build

# Package the instrumentation code into binary
.PHONY: package
package:
	@echo "Packaging instrumentation code into binary..."
	@rm -rf $(INST_PKG_TMP)
	@cp -a pkg $(INST_PKG_TMP)
	@cd $(INST_PKG_TMP) && go mod tidy
	@tar -czf $(INST_PKG_GZIP) --exclude='*.log' $(INST_PKG_TMP)
	@mv $(INST_PKG_GZIP) tool/data/
	@rm -rf $(INST_PKG_TMP)

# Build the instrumentation tool
.PHONY: build
build: package
	@echo "Building instrumentation tool..."
	@cp $(API_SYNC_SOURCE) $(API_SYNC_TARGET)
	@go mod tidy
	@go build -a -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" -o $(BINARY_NAME) ./$(TOOL_DIR)
	@./$(BINARY_NAME) version

# Run the demo with instrumentation
.PHONY: demo
demo: build
	@echo "Building demo with instrumentation..."
	@rm -rf $(DEMO_DIR)/otel.runtime.go
	@cd $(DEMO_DIR) && ../$(BINARY_NAME) go build -a
	@echo "Running demo..."
	@./$(DEMO_DIR)/demo

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)
	rm -f $(DEMO_DIR)/demo
	rm -rf $(DEMO_DIR)/.otel-build
