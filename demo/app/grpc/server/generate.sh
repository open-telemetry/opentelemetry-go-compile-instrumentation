#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0


# Generate Go code from proto file
# Note: This requires protoc to be installed on the system
# On macOS: brew install protobuf
# On Linux: apt-get install -y protobuf-compiler

# Create the pb directory if it doesn't exist
mkdir -p pb

# Generate protobuf and gRPC code in the pb directory
protoc --go_out=pb --go_opt=paths=source_relative \
       --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
       greeter.proto

echo "Generated files in pb/ directory:"
ls -la pb/*.go