# jk - Jenkins CLI for developers

`jk` is a GitHub CLI–style command line for Jenkins controllers. The CLI is written in Go and targets macOS, Linux, and Windows with a single static binary.

## Project status

Phase 0 + Phase 1 MVP scaffolding is now in place:

- Auth and context management backed by the OS keyring (`jk auth login`, `jk context ls/use/rm`).
- Shared configuration, logging, and Jenkins path utilities.
- HTTP client with CSRF crumb handling, capability detection, and version handshakes.
- Core developer workflows (`job ls/view`, `run start/ls/view/cancel/rerun`, `log follow`, `artifact ls/download`, `queue ls/cancel`, `test report`).
- Basic unit tests and `make` targets for build/test/tidy.

Refer to `docs/spec.md` for the full technical plan and `docs/api.md` for JSON contracts.

## Building from source

```bash
make build   # produces ./bin/jk
make test    # runs go test ./...
```

> Ensure Go 1.25 or newer is available (`go env GOVERSION` should report `go1.25.x`). The `go` tool on your `PATH` is used directly.

## Quick start (local development)

```bash
# Authenticate and create a context
jk auth login https://jenkins.company.example

# Inspect configuration
jk auth status
jk context ls

# Explore jobs and runs
jk job ls --folder team
jk run ls team/app/pipeline
jk run view team/app/pipeline 128
jk run start team/app/pipeline --follow
jk run rerun team/app/pipeline 128 --follow
jk run cancel team/app/pipeline 129

# Stream logs and fetch artifacts
jk log team/app/pipeline 128
jk log team/app/pipeline 128 --follow
jk artifact ls team/app/pipeline 128
jk artifact download team/app/pipeline 128 -p "**/*.xml" -o out/

# Queue and test insights
jk queue ls
jk test report team/app/pipeline 128

# Credentials
jk cred ls --scope system

# Nodes and plugins
jk node ls
jk node cordon linux-agent-1 --message "Maintenance"
jk plugin ls

# Fetch structured output
jk run ls team/app/pipeline --limit 5 --json
jk run start team/app/pipeline --json --param version=1.4.0
```

`examples/parity-smoke.sh` outlines the acceptance flow we will automate during Phase 1 to demonstrate `gh` parity.

## Repository structure

```
cmd/jk               # entry point calling internal/jkcmd
internal/jkcmd       # gh-style command runner and exit handling
internal/jenkins     # Jenkins client and helpers
internal/config      # Config file model
internal/secret      # Keyring integration
internal/log         # Logging helpers
pkg/cmd              # Command packages (auth, context, run, log, etc.)
pkg/cmd/shared       # Shared command helpers (output, test reports, logs)
pkg/cmdutil          # Factory and error helpers mirroring gh
pkg/iostreams        # Terminal IO abstraction (ported from gh)
plugin/              # Placeholder for Phase 3 companion plugin
```

## Testing

`go test ./...` exercises helper utilities today (job path encoding, duration formatting, exit-code mapping). Integration and contract tests against a live Jenkins matrix will arrive in later phases per the spec.

## Contributing

1. Fork and clone the repo.
2. Install Go 1.25.
3. Run `make build` and `make test` before submitting changes.
4. Keep the specification (`docs/spec.md`) and API contract (`docs/api.md`) up to date with behavioral changes.
