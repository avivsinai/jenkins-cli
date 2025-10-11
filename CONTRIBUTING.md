# Contributing to `jk`

Thanks for your interest in helping improve the Jenkins CLI! We welcome issues,
pull requests, and feedback from the community.

## Ground rules

- Be kind and follow our [Code of Conduct](CODE_OF_CONDUCT.md).
- Discuss large changes in an issue before opening a pull request.
- Keep the documentation up to date with user-facing changes.
- Add or update tests whenever behavior changes.
- Run `make build` and `make test` locally before submitting.

## Getting started

1. Fork the repository and clone your fork.
2. Install Go 1.25 and ensure `go env GOVERSION` reports `go1.25.x`.
3. Run `make build` and `make test` to verify the baseline passes.
4. Create a feature branch for your changes.

## Pull requests

- Keep commits focused; use conventional messages (e.g., `feat:`, `fix:`).
- Include a summary of the change, tests performed, and any follow-up work.
- If your change adds a command or flag, update the relevant docs in `docs/`.
- For breaking changes, call out migration notes clearly in the PR description.

## Reporting issues

- Use the issue templates to provide reproduction steps and environment details.
- Include the `jk` version (`jk version`) and Jenkins version when possible.
- Attach logs or stack traces if they help illustrate the problem.

## Release process

- Maintainers update `CHANGELOG.md` each release following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
- Tag releases with semantic versioning (`vMAJOR.MINOR.PATCH`).
- Upload compiled binaries for macOS, Linux, and Windows to the release page.
- Announce releases in the repository discussions or mailing list when available.

## Questions?

Open a GitHub issue or start a discussion in the repository. For
security-sensitive reports, please follow the [security policy](SECURITY.md).
