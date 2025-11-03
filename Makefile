# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

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
	@go build -a -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildTime=$(BUILD_TIME)" -o $(BINARY_NAME)$(EXT) ./$(TOOL_DIR)
	@./$(BINARY_NAME)$(EXT) version


.PHONY: build-demo-grpc
build-demo-grpc:
	@echo "Building gRPC demo..."
	@cd demo/grpc/server && go generate && go build -o server .
	@cd demo/grpc/client && go build -o client .

# Run the test with instrumentation
.PHONY: test
test: build
	@echo "Running e2e test..."
	@go test -count=1 -run TestBasic ./test/...
	@echo "Running unit test..."
	@go test -count=1 ./tool/...

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)$(EXT)
	rm -f demo/basic/basic
	rm -rf demo/basic/.otel-build
	rm -f demo/grpc/server/server
	rm -rf demo/grpc/server/pb
	rm -f demo/grpc/client/client
	rm -rf demo/grpc/server/.otel-build
	rm -rf demo/grpc/client/.otel-build
