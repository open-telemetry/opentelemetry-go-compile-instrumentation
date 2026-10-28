#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
#
# Generate pkg/otelcconv from the attributes otelc declares itself with `id:`
# in `schemas/otelc/`, using the templates under `schemas/templates/registry/go`
# (see docs/adr/0006-semconv-telemetry-contract-organization.md).
#
# weaver exits non-zero when the registry has diagnostics, even after it has
# written the output. The registry always reports the expected otelc/upstream
# DuplicateMetricName mirrors, so, like lint-schema.sh, we capture the JSON
# diagnostics and fail only on what survives `lint-schema-filter.jq`. The
# output is then run through gofmt, since weaver does not format Go code.
#
# Usage: generate.sh <oci-bin> <weaver-image> <registry-host-path> <templates-host-path> <output-host-path>

set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: $(basename "$0") <oci-bin> <weaver-image> <registry-host-path> <templates-host-path> <output-host-path>" >&2
  exit 2
fi

OCI_BIN="$1"
WEAVER_IMAGE="$2"
REGISTRY_PATH="$3"
TEMPLATES_PATH="$4"
OUTPUT_PATH="$5"
FILTER="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lint-schema-filter.jq"

mkdir -p "$OUTPUT_PATH"

stderr=$(mktemp)
trap 'rm -f "$stderr"' EXIT

rc=0
out=$($OCI_BIN run --rm \
  -u "$(id -u):$(id -g)" \
  -v "${REGISTRY_PATH}:/otelc-registry:ro" \
  -v "${TEMPLATES_PATH}:/otelc-templates:ro" \
  -v "${OUTPUT_PATH}:/otelc-output" \
  -w /otelc-registry \
  "$WEAVER_IMAGE" registry generate \
    --registry /otelc-registry \
    --templates /otelc-templates \
    --skip-policies \
    --future \
    --diagnostic-format json \
    --diagnostic-stdout \
    go /otelc-output 2>"$stderr") || rc=$?

if [ "$rc" -ne 0 ] && ! printf '%s' "$out" | jq empty >/dev/null 2>&1; then
  echo "weaver registry generate failed to run (exit $rc):" >&2
  cat "$stderr" >&2
  printf '%s\n' "$out" >&2
  exit 1
fi

remaining=$(printf '%s' "${out:-[]}" | jq -f "$FILTER")
if [ "$remaining" != "[]" ]; then
  echo "weaver registry generate produced diagnostics:" >&2
  printf '%s\n' "$remaining" >&2
  exit 1
fi

gofmt -w "$OUTPUT_PATH"
echo "generated $OUTPUT_PATH"
