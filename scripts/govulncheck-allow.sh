#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
#
# echoserver uses echo v4.16.3. The upstream advisories list this version as fixed.
# The Go vulnerability database still marks every echo v4 release as affected.
# The database gives no fixed version. govulncheck then reports GO-2026-6872
# and GO-2026-6875. This script ignores those two IDs for echoserver only.
# After vuln.go.dev lists v4.16.3 as fixed, delete this script. See issue 1744.
#
# Usage: govulncheck-allow.sh <app>
# Read the govulncheck text from stdin.
# When the app is echoserver and every reported ID is in the allow list, exit 0.
# Exit 1 for every other result. A report with no ID is not a match.

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $(basename "$0") <app>" >&2
  exit 2
fi

app="$1"
ids=$(sed -n 's/^Vulnerability #[0-9][0-9]*:[[:space:]]*//p' | sed 's/[[:space:]]*$//' | sort -u)

if [ "$app" != "echoserver" ]; then
  exit 1
fi
if [ -z "$ids" ]; then
  exit 1
fi

extra=$(printf '%s\n' "$ids" | grep -vxF -e 'GO-2026-6872' -e 'GO-2026-6875' || true)
if [ -n "$extra" ]; then
  exit 1
fi
