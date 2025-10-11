#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: up.sh [command]

Commands:
  up [docker-compose args]    Start the Jenkins dogfood controller (default)
  down [docker-compose args]  Stop the controller and remove containers
EOF
}

repo_root() {
  git rev-parse --show-toplevel
}

prepare_bare_repo() {
  local root="$1"
  local workdir="${root}/hack/e2e/.tmp"
  local repo="${workdir}/jenkins-cli.git"
  local snapshot="${workdir}/snapshot"

  rm -rf "${workdir}"
  mkdir -p "${workdir}"

  git init --bare "${repo}" >/dev/null

  mkdir -p "${snapshot}"
  git init --initial-branch=main "${snapshot}" >/dev/null
  rsync -a --exclude '.git' "${root}/" "${snapshot}/"
  git -C "${snapshot}" add . >/dev/null
  git -C "${snapshot}" config user.email jk-e2e@example.com >/dev/null
  git -C "${snapshot}" config user.name "jk e2e" >/dev/null
  git -C "${snapshot}" commit -m "jk e2e snapshot" >/dev/null
  git -C "${snapshot}" remote add origin "${repo}" >/dev/null
  git -C "${snapshot}" push --force origin main >/dev/null
  git --git-dir "${repo}" update-server-info >/dev/null

  echo "${repo}"
}

compose() {
  local root="$1"
  shift
  docker compose -f "${root}/hack/e2e/docker-compose.yaml" "$@"
}

main() {
  local cmd="${1:-up}"
  shift || true

  local root
  root="$(repo_root)"

  case "${cmd}" in
    up)
      local repo_path
      repo_path="$(prepare_bare_repo "${root}")"
      export JK_E2E_BARE_REPO="${repo_path}"
      compose "${root}" up --build --detach "$@"
      echo "Jenkins controller is available at http://localhost:${JK_E2E_HTTP_PORT:-28080}"
      echo "Username: admin  Password: admin123"
      ;;
    down)
      compose "${root}" down "$@"
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
