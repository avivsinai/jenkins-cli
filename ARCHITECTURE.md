# Architecture

`jk` is a CLI for Jenkins controllers modeled after the GitHub CLI (`gh`).
It is written in Go 1.25+ using [Cobra](https://github.com/spf13/cobra) for
command routing and [go-resty](https://github.com/go-resty/resty) for HTTP.

## Directory layout

```
cmd/jk/                  Entry point — calls internal/jkcmd.Main()
internal/
  jkcmd/                 CLI bootstrap, factory wiring
  jenkins/               HTTP client, CSRF crumb handling, path helpers
  config/                YAML configuration (~/.config/jk/config.yaml)
  secret/                OS keyring token storage (fallback: encrypted file)
  build/                 Version info injected via ldflags
  log/                   Structured logging (zerolog)
  filter/                JQ-style JSON filtering
  fuzzy/                 Fuzzy-match helpers for interactive selection
  terminal/              TTY detection utilities
  i18n/                  Internationalisation stubs
pkg/
  cmd/<command>/         One package per top-level command:
                           artifact, auth, context, cred, job, log,
                           node, plugin, queue, run, search, test, version
  cmd/root/              Root command and help renderer
  cmd/shared/            Shared helpers — output formatting, log streaming,
                           jq integration, template rendering, time formatting
  cmd/factory/           Factory constructor (builds the dependency graph)
  cmdutil/               Factory interface, ExitError, exit codes
  iostreams/             I/O abstraction — TTY, colour, pager, progress bars
docs/                    User-facing specification (docs/spec.md)
test/e2e/                End-to-end tests using testcontainers-go (Jenkins in Docker)
hack/                    Developer scripts
examples/                Example usage files
```

## Execution flow

```
main.go
  → jkcmd.Main()
    → jkfactory.New(version)          build Factory
    → root.NewCmdRoot(factory)         register all sub-commands
    → rootCmd.Execute()                Cobra dispatch
      → pkg/cmd/<command>/*.go         command handler
        → shared.JenkinsClient(…)      resolve context + build client
          → jenkins.Client.Do(req)     HTTP call with CSRF crumb
```

## Key abstractions

**Factory** (`pkg/cmdutil/Factory`) — dependency-injection root providing
IOStreams, Config loader, and a lazy JenkinsClient builder.

**Jenkins client** (`internal/jenkins/Client`) — created per-context; handles
CSRF crumbs, TLS/proxy settings, and capability detection via `/api/json`.

**Multi-context** — contexts stored in `~/.config/jk/config.yaml`; tokens in
the OS keyring.  Resolution order: `--context` flag → `JK_CONTEXT` env →
active context in config.

**IOStreams** (`pkg/iostreams`) — wraps stdin/stdout/stderr with TTY detection,
colour support, pager piping, and progress indicators.

## Testing strategy

| Layer | Count | Runner |
|-------|-------|--------|
| Unit tests | ~230 (23 test files) | `go test ./...` with `JK_E2E_DISABLE=1` |
| End-to-end | ~60 (test/e2e/) | testcontainers-go — spins up a real Jenkins in Docker |

CI runs lint → unit → e2e in sequence.  The e2e suite uses git worktrees
(`.tmp-jk-e2e/`) to isolate each test's Jenkins home directory.
