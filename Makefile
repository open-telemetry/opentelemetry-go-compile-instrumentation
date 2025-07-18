# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Variables
BINARY_NAME := otel
DEMO_DIR := demo
TOOL_DIR := tool/cmd

# Version variables
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.1.0")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d')

# Default target
.PHONY: all
all: build

# Build the instrumentation tool
.PHONY: build
build:
	@echo "Building instrumentation tool..."
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT_HASH)"
	@echo "Time: $(BUILD_TIME)"
	@go mod tidy
	@go build -a -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" -o $(BINARY_NAME) ./$(TOOL_DIR)

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
