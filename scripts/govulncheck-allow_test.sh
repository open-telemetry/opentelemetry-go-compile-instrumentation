#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
#
# Feed sample govulncheck text to govulncheck-allow.sh.

set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/govulncheck-allow.sh"

fail() {
  echo "govulncheck-allow_test: $1" >&2
  exit 1
}

expect() {
  local name="$1" app="$2" want="$3" got
  shift 3
  set +e
  printf '%s\n' "$@" | "$script" "$app"
  got=$?
  set -e
  if [ "$got" -ne "$want" ]; then
    fail "$name: exit $got, want $want"
  fi
}

expect "both ids" echoserver 0 \
  "Vulnerability #1: GO-2026-6875" \
  "  Found in: github.com/labstack/echo/v4@v4.16.3" \
  "  Fixed in: N/A" \
  "Vulnerability #2: GO-2026-6872" \
  "  Found in: github.com/labstack/echo/v4@v4.16.3" \
  "  Fixed in: N/A"

expect "one id" echoserver 0 \
  "Vulnerability #1: GO-2026-6872"

expect "other id" echoserver 1 \
  "Vulnerability #1: GO-2026-6875" \
  "Vulnerability #2: GO-2026-9999"

expect "other app" gin 1 \
  "Vulnerability #1: GO-2026-6875"

expect "no id" echoserver 1 \
  "No vulnerabilities found."

expect "blank id" echoserver 1 \
  "Vulnerability #1: "

echo "govulncheck-allow_test: ok"
