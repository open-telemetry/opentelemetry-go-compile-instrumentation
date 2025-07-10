# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Variables
BINARY_NAME := otel
DEMO_DIR := demo
TOOL_DIR := tool/cmd

# Default target
.PHONY: all
all: build

# Build the instrumentation tool
.PHONY: build
build:
	@echo "Building instrumentation tool..."
	@go mod tidy
	@go build -a -o $(BINARY_NAME) ./$(TOOL_DIR)

# Run the demo with instrumentation
.PHONY: demo
demo: build
	@echo "Running demo with instrumentation..."
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