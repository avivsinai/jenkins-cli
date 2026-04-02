# CLAUDE.md

This is the master agent instruction file for this repository. Keep repository policy here. `AGENTS.md` exists only as a Codex compatibility shim and should contain only Codex-specific notes.

## Build & Test Commands

```bash
make build          # Build binary to ./bin/jk (injects version via ldflags)
make test           # Run unit tests (228+ tests)
make lint           # Run golangci-lint
make e2e            # Run end-to-end tests (requires Docker)
make fmt            # Format code with gofmt

# Run a single test
go test -run TestFunctionName ./path/to/package

# Skip e2e tests during unit testing
JK_E2E_DISABLE=1 make test

# E2E with Colima on macOS (if Docker is unreachable)
colima start --network-address
export DOCKER_HOST="unix://$HOME/.colima/default/docker.sock"
make e2e
```

## Required Checks

- Run `make build` and `make test` before commit.
- Run `make e2e` when Jenkins-facing behavior changes and Docker is available.
- Run `gofmt -w` on touched Go packages.

## Architecture

This is `jk`, a GitHub CLI–style interface for Jenkins controllers written in Go 1.25+.

### Code Layout (mirrors `gh` CLI)

```
cmd/jk/              → Entry point only; calls internal/jkcmd.Main()
internal/
  jkcmd/             → CLI initialization and factory wiring
  jenkins/           → HTTP client (go-resty), CSRF crumb handling, capability detection
  config/            → YAML configuration (~/.config/jk/config.yaml)
  secret/            → OS keyring token storage (fallback: encrypted file)
  build/             → Version info (injected via ldflags)
  log/               → Structured logging (zerolog)
pkg/
  cmd/<command>/     → Cobra command implementations (auth, run, job, artifact, etc.)
  cmd/shared/        → Shared helpers (output formatting, client resolution)
  cmdutil/           → Factory pattern, error handling, exit codes
  iostreams/         → I/O abstraction (TTY detection, color, progress)
test/e2e/            → End-to-end tests using testcontainers-go
```

### Key Patterns

**Factory Pattern**: `pkg/cmdutil/Factory` provides dependency injection for all commands—IOStreams, Config loader, and JenkinsClient factory.

**Jenkins Client**: Created per-context via `internal/jenkins/Client`. Auto-handles CSRF crumbs, TLS/proxy settings, and capability detection via `/jk/api/status`.

**Command Execution Flow**:
```
main.go → jkcmd.Main() → root.NewCmdRoot(factory) → Cobra Execute()
                                  ↓
                         pkg/cmd/<command>/*.go
                                  ↓
                         shared.JenkinsClient(cmd, factory)
                                  ↓
                         jenkins.Client.Do(request)
```

### Multi-Context Support

- Contexts stored in `~/.config/jk/config.yaml`
- Tokens in OS keyring (macOS Keychain, Linux secretservice, Windows credential manager)
- Resolution: `--context` flag → `JK_CONTEXT` env → active context in config

## Conventions

- **Commit messages**: Use conventional commits (`feat:`, `fix:`, `deps:`, `ci:`, `chore:`)
- **Formatting**: Run `gofmt -w` on touched packages
- **Documentation**: Update `README.md` and `docs/spec.md` for user-facing changes
- **New directories**: Update `docs/spec.md` before adding top-level directories

## Release Contract

- Release from `main` only through `./scripts/release.sh X.Y.Z` and the resulting release PR; do not create manual tags or GitHub releases.
- A push to `main` updates the AvivSinai marketplace immediately for the `jk` skill.
- Keep `CHANGELOG.md` and skill/plugin metadata on one version in the release commit; after the release PR merges, CI validates the merged commit, creates the tag, publishes GitHub/Homebrew artifacts, and uses the committed changelog entry as the GitHub release notes.

## Documentation Expectations

- User-facing changes should update `README.md` and, when relevant, `docs/spec.md` or `docs/api.md`.
- Avoid adding new top-level directories without updating the docs first.

## Agent Discovery Patterns

When automating with `jk`:
- `jk search --job-glob '*pattern*' --json` for cross-folder job discovery
- `jk run ls <job> --filter result=SUCCESS --since 7d --json` for filtered run queries
- `jk run params <job>` to inspect parameter metadata before triggering builds
- `jk help --json` for programmatic command enumeration
