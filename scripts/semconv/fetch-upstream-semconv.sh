#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
#
# Pre-fetch the upstream OpenTelemetry semantic-conventions registry into
# `schemas/otelc/.deps/` so weaver doesn't have to clone it over the network
# on every container start. The pinned upstream version is parsed out of
# `schemas/otelc/registry_manifest.yaml` so there's a single source of truth.

set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
MANIFEST="$REPO_ROOT/schemas/otelc/registry_manifest.yaml"
DEPS_DIR="$REPO_ROOT/schemas/otelc/.deps"

# Extract the pinned upstream semconv version from the manifest. After this
# script has run once the manifest's `registry_path` points at the local
# `upstream-v<VERSION>` cache; the value always contains `upstream-v<VERSION>`.
VERSION=$(grep -oE 'upstream-v[0-9]+\.[0-9]+\.[0-9]+' "$MANIFEST" \
  | head -1 \
  | sed -E 's|.*v||')
if [ -z "$VERSION" ]; then
  echo "fetch-upstream-semconv: could not extract version from $MANIFEST" >&2
  exit 1
fi

TARGET="$DEPS_DIR/upstream-v$VERSION"
if [ -d "$TARGET/model" ]; then
  exit 0
fi

mkdir -p "$TARGET"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "fetch-upstream-semconv: fetching v$VERSION into $TARGET/model"
curl -fsSL "https://github.com/open-telemetry/semantic-conventions/archive/refs/tags/v${VERSION}.tar.gz" \
  | tar -xz -C "$TMPDIR" --strip-components=1 "semantic-conventions-${VERSION}/model"

mv "$TMPDIR/model" "$TARGET/model"
