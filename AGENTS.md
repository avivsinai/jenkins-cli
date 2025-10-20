# Agent Guidelines

This repository is optimized for scripted and human contributors who follow a GitHub CLI–style workflow. Use these conventions when automating tasks or reviewing changes.

## Environment

- Go 1.25 (the `go` tool on `PATH` must report `go1.25.x`).
- `make` for the helper targets defined in `Makefile`.
- Jenkins controller access is not required for unit tests; integration tests will be layered in later.

## Required checks

Run the following before opening or merging a pull request:

```bash
make build   # go build ./...
make test    # go test ./...
```

When editing Go files manually, run `gofmt -w` on touched packages.

## Repository layout

- Command entry point: `cmd/jk`
- Shared command wiring: `internal/jkcmd`, `pkg/cmdutil`, `pkg/cmd`
- Jenkins client & helpers: `internal/jenkins`
- Documentation: `docs/spec.md`, `docs/api.md`, `README.md`

Avoid creating new top-level directories without first updating `docs/spec.md` and the quick start section in `README.md`.

## Agent discovery patterns

- Use `jk run ls <job>` with `--filter`, `--since`, `--select`, and `--with-meta` to retrieve focused run slices plus metadata for schema-aware automation.
- Reach across folders with `jk search` (alias: `jk run search`) (supports `--folder`, `--job-glob`, and the same filter syntax) before falling back to bespoke scripts.
- Harvest parameter definitions via `jk run params <job>` (`--source auto|config|runs`) ahead of composing trigger payloads.
- Prefer `jk help --json` when programmatically enumerating commands, flags, and exit codes.

## Documentation expectations

- User-facing changes must mention affected commands in `README.md` and, when relevant, update `docs/spec.md` / `docs/api.md`.
- Keep documentation user-focused; implementation details belong in code comments or developer notes, not the README.

## Commit hygiene

- Write conventional, descriptive commit messages (e.g. `feat: add jk node cordon command`).
- `go fmt`, `go vet`, and `go test` should be clean before commit.

## Issue triage

- Link design changes to the relevant section in `docs/spec.md`.
- Label feature work by command area (`auth`, `run`, `cred`, etc.).

Following these conventions keeps automation, maintainers, and external contributors aligned while we iterate toward 1.0.
