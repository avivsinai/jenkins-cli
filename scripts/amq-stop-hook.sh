#!/bin/bash
# AMQ Co-op Stop Hook
# Blocks stop if there are pending messages in inbox
# Safe fallback: approves if amq unavailable or co-op not configured

DEFAULT_ROOT=".agent-mail"
if [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
    DEFAULT_ROOT="${CLAUDE_PROJECT_DIR}/.agent-mail"
fi
ROOT="${AM_ROOT:-$DEFAULT_ROOT}"
ME="${AM_ME:-claude}"

# Fast path: approve immediately if co-op not set up
if [ ! -d "$ROOT/agents/$ME/inbox/new" ]; then
    echo '{"decision": "approve"}'
    exit 0
fi

# Fast path: if inbox/new has no message files, approve without invoking amq
inbox_new="$ROOT/agents/$ME/inbox/new"
shopt -s nullglob
files=("$inbox_new"/*.md)
shopt -u nullglob
if [ ${#files[@]} -eq 0 ]; then
    echo '{"decision": "approve"}'
    exit 0
fi

# Safe fallback if dependencies missing
if ! command -v amq &> /dev/null; then
    echo '{"decision": "approve"}'
    exit 0
fi

if command -v jq &> /dev/null; then
    # Check for pending messages (safe fallback on any error)
    COUNT=$(amq list --root "$ROOT" --me "$ME" --new --json 2>/dev/null | jq -r 'length // 0' 2>/dev/null || echo "0")

    # Sanitize COUNT to ensure it's a number
    if ! [[ "$COUNT" =~ ^[0-9]+$ ]]; then
        COUNT=0
    fi

    if [ "$COUNT" -gt 0 ]; then
        echo '{"decision": "block", "reason": "You have '"$COUNT"' pending message(s). Ask me to drain the inbox before stopping."}'
        exit 0
    fi
else
    OUT=$(amq list --root "$ROOT" --me "$ME" --new 2>/dev/null || true)
    if echo "$OUT" | grep -q "^No messages\\.$"; then
        echo '{"decision": "approve"}'
        exit 0
    fi
    if [ -n "$OUT" ]; then
        echo '{"decision": "block", "reason": "You have pending message(s). Ask me to drain the inbox before stopping."}'
        exit 0
    fi
fi

echo '{"decision": "approve"}'
