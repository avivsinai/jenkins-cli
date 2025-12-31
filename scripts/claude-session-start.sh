#!/bin/bash
# AMQ Claude SessionStart hook
# Sets AM_ROOT/AM_ME for Claude Code by appending to CLAUDE_ENV_FILE.

set -euo pipefail

if [ -z "${CLAUDE_ENV_FILE:-}" ]; then
    exit 0
fi

DEFAULT_ROOT=".agent-mail"
if [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
    DEFAULT_ROOT="${CLAUDE_PROJECT_DIR}/.agent-mail"
fi

ROOT="${AM_ROOT:-$DEFAULT_ROOT}"
ME="${AM_ME:-claude}"

touch "$CLAUDE_ENV_FILE"

if ! grep -q '^export AM_ROOT=' "$CLAUDE_ENV_FILE"; then
    printf 'export AM_ROOT=%q\n' "$ROOT" >> "$CLAUDE_ENV_FILE"
fi
if ! grep -q '^export AM_ME=' "$CLAUDE_ENV_FILE"; then
    printf 'export AM_ME=%q\n' "$ME" >> "$CLAUDE_ENV_FILE"
fi
