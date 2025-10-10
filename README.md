# jk — Jenkins CLI for developers

`jk` brings a GitHub CLI–style experience to Jenkins. It lets you inspect jobs, follow builds, manage credentials, and administer nodes from a single cross-platform binary.

## Highlights

- **Context aware authentication** – log in once and switch between controllers (`jk auth`, `jk context`).
- **Pipeline ergonomics** – trigger, rerun, follow, and summarize jobs with human or JSON/YAML output (`jk run`, `jk log`).
- **Artifacts & tests** – enumerate artifacts, download filtered sets, and inspect aggregated test reports.
- **Platform administration** – list queue items, manage credentials, cordon/delete nodes, and view installed plugins.
- **GitHub CLI parity** – command layout, help UX, and flag semantics mirror `gh`, easing adoption for developers.

## Installation

Pre-built releases are not published yet. Build from source with Go 1.25 or newer:

```bash
make build   # produces ./bin/jk
make test    # runs go test ./...
```

> Ensure `go env GOVERSION` reports `go1.25.x`. The toolchain on your `PATH` is used directly.

## Getting started

```bash
# Authenticate and store a context
jk auth login https://jenkins.company.example

# Inspect active context
jk auth status
jk context ls

# Explore jobs and runs
jk job ls --folder team
jk run ls team/app/pipeline
jk run view team/app/pipeline 128
jk run start team/app/pipeline --follow

# Stream logs & artifacts
jk log team/app/pipeline 128 --follow
jk artifact download team/app/pipeline 128 -p "**/*.xml" -o out/

# Platform operations
jk queue ls
jk cred ls --scope system
jk cred create-secret --id slack-token --description "Slack notifier" --from-stdin < token.txt
jk node cordon linux-agent-1 --message "Maintenance"
jk plugin install warnings-ng
```

Use `--json` or `--yaml` on supported commands for machine-readable output.

## Command quick reference

| Area        | Examples |
|-------------|----------|
| Auth & context | `jk auth login`, `jk context use`, `jk context ls` |
| Jobs & runs | `jk job ls`, `jk run ls`, `jk run rerun`, `jk run cancel` |
| Logs & artifacts | `jk log`, `jk log --follow`, `jk artifact ls/download` |
| Tests | `jk test report` |
| Credentials | `jk cred ls`, `jk cred create-secret`, `jk cred rm` |
| Nodes | `jk node ls`, `jk node cordon`, `jk node rm` |
| Queue | `jk queue ls`, `jk queue cancel` |
| Plugins | `jk plugin ls`, `jk plugin install`, `jk plugin enable/disable` |

Run `jk <command> --help` for detailed flags and examples.

## Documentation

- [Specification](docs/spec.md) — roadmap, scope, and design decisions.
- [API contracts](docs/api.md) — JSON schemas for structured output.
- [Changelog](docs/CHANGELOG.md) — release notes when versioning begins.

## Development notes

Project layout mirrors `gh`:

```
cmd/jk               # entry point delegating to internal/jkcmd
internal/jkcmd       # command runner / exit handling
internal/jenkins     # Jenkins REST client
pkg/cmd              # Cobra command packages
pkg/cmd/shared       # Common helpers (output, logs, test reports)
pkg/cmdutil          # Factory/error utilities
y pkg/iostreams        # Terminal abstraction borrowed from gh
```

We run `go test ./...` and `go build ./...` in CI. Keep the spec and API docs synchronized with behavior changes.

## Contributing

1. Fork and clone the repo.
2. Install Go 1.25.
3. Run `make build` & `make test` before submitting changes.
4. Update `docs/spec.md` and `docs/api.md` when altering contracts or workflows.
5. Open a pull request describing the user-facing impact (command, flag, behavior).

---

We’re actively iterating toward a 1.0 release; feedback and contributions are welcome.
