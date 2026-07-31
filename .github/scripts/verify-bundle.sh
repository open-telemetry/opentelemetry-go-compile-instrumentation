#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

archive="${1:-tool/data/otelc-bundle.tgz}"

make package

if git diff --exit-code -- "$archive"; then
    echo "archive verified successfully"
else
    echo 'archive verification failed.'
    echo 'To fix: rebase your branch on main, run "make package", and commit the updated archive.'
    echo 'If you are a maintainer, you can also comment "/regenerate-bundle" on the PR to auto-fix.'
    exit 1
fi
