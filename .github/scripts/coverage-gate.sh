#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# coverage-gate.sh - enforce a minimum unit-test coverage threshold.
#
# Usage:
#   coverage-gate.sh <coverprofile> [threshold]
#
# Arguments:
#   coverprofile   Path to a Go coverprofile file produced by go test -coverprofile.
#   threshold      Minimum acceptable total coverage percentage (default: 70.0).
#
# Exit codes:
#   0 - coverage meets or exceeds the threshold.
#   1 - coverage is below the threshold, or the input file is missing/unreadable.
#
# Example:
#   .github/scripts/coverage-gate.sh coverage-tool.txt 70.0

set -euo pipefail

COVERPROFILE="${1:-}"
THRESHOLD="${2:-70.0}"

if [[ -z "$COVERPROFILE" ]]; then
    echo "Usage: $0 <coverprofile> [threshold]" >&2
    exit 1
fi

if [[ ! -f "$COVERPROFILE" ]]; then
    echo "Error: coverage file '$COVERPROFILE' not found." >&2
    exit 1
fi

# Extract the total coverage percentage from `go tool cover -func` output.
# The last line looks like:  total:  (statements)    73.4%
TOTAL=$(go tool cover -func="$COVERPROFILE" | awk '/^total:/{gsub(/%/, "", $NF); print $NF}')

if [[ -z "$TOTAL" ]]; then
    echo "Error: could not parse total coverage from '$COVERPROFILE'." >&2
    exit 1
fi

echo "Coverage report: ${COVERPROFILE}"
echo "  Total coverage : ${TOTAL}%"
echo "  Threshold      : ${THRESHOLD}%"

# awk is used for the float comparison because bash does not support floats natively.
if awk "BEGIN { exit !(${TOTAL} + 0 < ${THRESHOLD} + 0) }"; then
    echo ""
    echo "FAIL: coverage ${TOTAL}% is below the required ${THRESHOLD}% threshold." >&2
    echo "   Improve unit-test coverage and re-run 'make test-unit/coverage'." >&2
    exit 1
fi

echo ""
echo "PASS: coverage ${TOTAL}% meets the required ${THRESHOLD}% threshold."
