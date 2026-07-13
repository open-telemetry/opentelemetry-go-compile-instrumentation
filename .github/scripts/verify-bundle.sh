#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

archive="${1:-tool/data/otelc-bundle.tgz}"

make package

if git diff --exit-code -- "$archive"; then
    echo "archive verified successfully"
else
    echo 'archive verification failed; run "make package" and commit the updated archive'
    exit 1
fi
