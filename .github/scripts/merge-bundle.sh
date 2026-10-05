#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

# Git merge driver for tool/data/otelc-bundle.tgz.
#
# The bundle is a reproducible binary archive derived from pkg/ and
# instrumentation/ (see .tools/bundle/main.go). Because it is binary, git cannot
# 3-way merge it, so every branch that touches the sources conflicts with the
# base on this one file and halts an otherwise-clean rebase/merge.
#
# We deliberately do NOT regenerate the bundle here: when git invokes a merge
# driver it has not yet materialized the *other* merged source files into the
# working tree, so `make package` would read stale sources and embed the wrong
# bytes (missing the incoming changes). Instead we keep the current ("ours")
# version so the rebase/merge completes without stopping, and remind the user to
# regenerate the bundle from the fully-merged sources afterwards with
# `make package`. The verify-bundle CI enforces that the committed bundle
# matches the sources, so a forgotten regeneration cannot slip through.
#
# Register it once per clone with `make setup-git`.
#
# Git invokes this as: merge-bundle.sh %A
#   $1 = %A  current/ours version, which is already the result git will keep.

set -euo pipefail

cat >&2 <<'EOF'
------------------------------------------------------------------------
otelc-bundle merge driver: kept the current bundle to let the merge/rebase
proceed. tool/data/otelc-bundle.tgz is now likely STALE. Regenerate and
amend it from the fully-merged sources before pushing:

    make package
    git add tool/data/otelc-bundle.tgz && git commit --amend --no-edit

(verify-bundle CI will fail if you skip this.)
------------------------------------------------------------------------
EOF

# Leave %A untouched (keep "ours") and report success so git accepts it.
exit 0
