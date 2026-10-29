#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

main() {
  if [[ "${1:-}" == '--help' ]]; then
    printf 'Usage: %s <checkout> <base-sha>\n' "$0"
    return
  fi
  if [[ "$#" != 2 || ! "$2" =~ ^[0-9a-f]{40}$ ]]; then
    printf 'A checkout and a full base commit SHA are required.\n' >&2
    return 1
  fi

  local attributes
  attributes="$(git -C "$1" rev-parse --path-format=absolute --git-path info/attributes)"
  mkdir -p "$(dirname "${attributes}")"
  # Local attributes block contributor merge drivers, filters, and bundle conversion.
  printf '%s\n' '* !merge -filter' \
    'tool/data/otelc-bundle.tgz merge=otelc-bundle -text -ident !working-tree-encoding !eol' >"${attributes}"
  git -C "$1" -c core.hooksPath=/dev/null \
    -c user.name='github-actions[bot]' \
    -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
    -c merge.otelc-bundle.driver=true \
    merge --no-ff --no-commit "$2"
}

main "$@"
