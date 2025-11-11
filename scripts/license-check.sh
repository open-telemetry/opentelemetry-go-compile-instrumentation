#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# License header check exclusions
EXCLUDE_PATHS=(
    -path '**/vendor/*'
    -o -path './.git/*'
    -o -path './tmp/*'
    -o -path './.otel-build/*'
    -o -path '**/pkg_temp/*'
    -o -path '**/pb/*'
    -o -path './LICENSE'
    -o -path './LICENSES/*'
    -o -path './.github/workflows/*'
    -o -path './scripts/*'
)

# File patterns that require license headers (source code files only)
FILE_PATTERNS=('*.go' '*.sh')

# Parse arguments
FIX_MODE=false
if [[ "${1:-}" == "--fix" ]]; then
    FIX_MODE=true
fi

if [ "$FIX_MODE" = true ]; then
    echo "Adding missing license headers..."

    # Fix Go files
    while IFS= read -r file; do
        if ! grep -q "Copyright The OpenTelemetry Authors" "$file"; then
            echo "Adding license header to $file"
            tmpfile=$(mktemp)
            {
                echo "// Copyright The OpenTelemetry Authors"
                echo "// SPDX-License-Identifier: Apache-2.0"
                echo ""
                cat "$file"
            } > "$tmpfile"
            mv "$tmpfile" "$file"
        fi
    done < <(find . -type f -iname '*.go' ! \( "${EXCLUDE_PATHS[@]}" \) 2>/dev/null)

    # Fix Shell files
    while IFS= read -r file; do
        if ! grep -q "Copyright The OpenTelemetry Authors" "$file"; then
            echo "Adding license header to $file"
            tmpfile=$(mktemp)
            if head -n 1 "$file" | grep -q '^#!/'; then
                {
                    head -n 1 "$file"
                    echo ""
                    echo "# Copyright The OpenTelemetry Authors"
                    echo "# SPDX-License-Identifier: Apache-2.0"
                    echo ""
                    tail -n +2 "$file"
                } > "$tmpfile"
            else
                {
                    echo "# Copyright The OpenTelemetry Authors"
                    echo "# SPDX-License-Identifier: Apache-2.0"
                    echo ""
                    cat "$file"
                } > "$tmpfile"
            fi
            mv "$tmpfile" "$file"
        fi
    done < <(find . -type f -iname '*.sh' ! \( "${EXCLUDE_PATHS[@]}" \) 2>/dev/null)

    echo "License headers added successfully"
else
    echo "Checking license headers..."

    missing_files=()

    for pattern in "${FILE_PATTERNS[@]}"; do
        while IFS= read -r file; do
            if ! grep -q "Copyright The OpenTelemetry Authors" "$file" || \
               ! grep -q "SPDX-License-Identifier: Apache-2.0" "$file"; then
                missing_files+=("$file")
            fi
        done < <(find . -type f -iname "$pattern" ! \( "${EXCLUDE_PATHS[@]}" \) 2>/dev/null)
    done

    if [ ${#missing_files[@]} -gt 0 ]; then
        echo "Missing license header in the following files:"
        printf '%s\n' "${missing_files[@]}"
        exit 1
    fi

    echo "All files have proper license headers"
fi
