#!/usr/bin/env bash
set -euo pipefail

if git ls-files --error-unmatch jk >/dev/null 2>&1; then
  echo "error: root-level jk binary is tracked; build outputs belong under ./bin/ or ./dist/" >&2
  exit 1
fi

echo "✓ no root-level jk binary tracked"
