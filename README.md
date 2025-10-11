# jk

<p align="center"><em>GitHub CLI–style workflows for Jenkins controllers</em></p>

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](go.mod)

`jk` gives developers and operators a modern, scriptable interface to Jenkins: inspect runs, stream logs, manage credentials, and administer controllers from a single cross-platform binary.

## Features

- **Context-aware auth** – store multiple controllers, switch with `jk context use`, or pin a context via `JK_CONTEXT`.
- **Friendly pipelines** – trigger, rerun, follow, and summarize jobs with human or JSON/YAML output.
- **Artifacts & tests** – browse artifacts, download filtered sets, and surface aggregated test reports.
- **Platform operations** – cordon nodes, manage credentials, inspect queues, and view installed plugins.
- **GitHub CLI parity** – command structure and UX mirror `gh`, easing adoption in developer toolchains.

## Installation

### From source

```bash
go install github.com/your-org/jenkins-cli/cmd/jk@latest
```

or clone and build:

```bash
make build   # produces ./bin/jk
make test    # runs go test ./...
```

Prebuilt release archives will land with the first tagged version.

## Quickstart

```bash
jk auth login https://jenkins.company.example      # authenticate and create a context
jk context ls                                      # list available contexts
jk run ls team/app/pipeline                        # inspect recent runs
jk run view team/app/pipeline 128 --follow         # stream logs until completion
jk artifact download team/app/pipeline 128 -p "**/*.xml" -o out/
```

Add `--json` or `--yaml` to supported commands for machine-readable output.

## Documentation

- [Specification](docs/spec.md) – architecture, scope, and design decisions.
- [API contracts](docs/api.md) – JSON schemas for structured output.
- [Changelog](CHANGELOG.md) – release notes and migration guidance.

## Community

- Read the [code of conduct](CODE_OF_CONDUCT.md) and [contributing guide](CONTRIBUTING.md).
- Ask questions or propose ideas via GitHub Discussions (coming soon) or issues.
- Follow the [support guidelines](SUPPORT.md) for help, and our [security policy](SECURITY.md) for private reports.

## Development

Run the full test/build suite before opening a PR:

```bash
make build
make test
make e2e    # spins up a disposable Jenkins controller and runs dogfood scenarios
make e2e-up # launches the same controller for manual exploration (default port 28080)
```

Update docs in `docs/` whenever behavior changes. See the [contributing guide](CONTRIBUTING.md) for release steps and review expectations.

## License

`jk` is available under the [MIT License](LICENSE).
