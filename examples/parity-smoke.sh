#!/usr/bin/env bash
set -euo pipefail

# Placeholder for the Phase 1 parity demo. This script will be fleshed out once
# the Phase 1 commands (auth, job, run, artifact, cred) are implemented.

cat <<'MSG'
This script will exercise jk parity with gh once the Phase 1 commands land:
  jk auth login https://jenkins.example
  jk job ls --limit 5
  jk run start team/app/main -p version=1.2.3 --follow
  jk artifact download team/app/main/$(jk run ls team/app/main --limit 1 --json | jq -r '.[0].number') -p "**/*.xml" -o out/
  jk cred create secret-text --id slack-token --value "***" --folder team/app
MSG
