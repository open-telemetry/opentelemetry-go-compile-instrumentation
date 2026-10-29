#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Copy the bundled source trees from an untrusted checkout into a trusted one.
# Only pkg/ and instrumentation/ are copied, and only as plain files and
# directories. The Makefile and the bundle tool stay those of the trusted
# checkout.

set -o errexit
set -o nounset
set -o pipefail

readonly TREES=(pkg instrumentation)

check_tree() {
  local root="$1/$2" special
  if [[ -L "${root}" ]]; then
    printf '%s is a symbolic link.\n' "${root}" >&2
    return 1
  fi
  if [[ ! -d "${root}" ]]; then
    printf '%s is not a directory.\n' "${root}" >&2
    return 1
  fi
  # find does not follow links, so a link is never a regular file or a directory.
  special="$(find "${root}" ! -type f ! -type d -print -quit)"
  if [[ -n "${special}" ]]; then
    printf '%s is not a regular file or a directory.\n' "${special}" >&2
    return 1
  fi
}

main() {
  if [[ "${1:-}" == '--help' ]]; then
    printf 'Usage: %s <branch-dir> <trusted-dir>\n' "$0"
    return
  fi
  if [[ "$#" != 2 || ! -d "$1" || ! -d "$2" ]]; then
    printf 'A branch directory and a trusted directory are required.\n' >&2
    return 1
  fi

  local tree
  for tree in "${TREES[@]}"; do
    check_tree "$1" "${tree}"
  done
  for tree in "${TREES[@]}"; do
    rm -rf "${2:?}/${tree}"
  done
  for tree in "${TREES[@]}"; do
    cp -R "$1/${tree}" "$2/${tree}"
  done
}

main "$@"
