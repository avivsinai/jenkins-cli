#!/usr/bin/env bash
set -euo pipefail

binary_path="${1:?usage: ./scripts/sign-darwin-binary.sh <binary> [target] }"
target="${2:-$(go env GOOS 2>/dev/null || echo unknown)}"
identifier="${MACOS_CODESIGN_IDENTIFIER:-io.github.avivsinai.jk}"

case "$target" in
  *darwin*)
    ;;
  *)
    exit 0
    ;;
esac

if [[ "$(uname -s)" != "Darwin" ]]; then
  exit 0
fi

if [[ ! -f "$binary_path" ]]; then
  echo "error: binary not found: $binary_path" >&2
  exit 1
fi

# Sign ad-hoc with an explicit Designated Requirement pinned to the identifier.
#
# Without -r, codesign derives a DR of the form `cdhash H"..."` which changes
# on every rebuild. Each new jk release would therefore invalidate any "Always
# Allow" the user granted in Keychain and re-prompt on the next invocation.
# Pinning the DR to the identifier makes the DR stable across rebuilds so the
# Keychain ACL keeps matching after `brew upgrade jk`.
#
# Security trade-off: any other ad-hoc binary that claims this identifier
# satisfies the DR. Acceptable because the Keychain item only stores a user-
# scoped Jenkins API token, trust is granted per-context by the user
# explicitly via `jk auth login`, and the identifier is namespaced under our
# GitHub org. Upgrading to Developer ID is tracked as separate future work.
codesign \
  --force \
  --sign - \
  --identifier "$identifier" \
  -r="designated => identifier \"$identifier\"" \
  "$binary_path"
codesign --verify --strict --verbose=2 "$binary_path"
