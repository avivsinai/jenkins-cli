# Contributing to `jk`

Thanks for your interest in helping improve the Jenkins CLI! We welcome issues,
pull requests, and feedback from the community.

## Ground rules

- Be kind and follow our [Code of Conduct](CODE_OF_CONDUCT.md).
- Discuss large changes in an issue before opening a pull request.
- Keep the documentation up to date with user-facing changes.
- Add or update tests whenever behavior changes.
- Run `make lint`, `JK_E2E_DISABLE=1 make test`, and `make e2e` locally before submitting.

## Getting started

1. Fork the repository and clone your fork.
2. Install dependencies:
   ```bash
   # Go 1.25+
   go version  # should report go1.25.x

   # Dev tools (macOS)
   brew install golangci-lint gitleaks pre-commit
   ```
3. Set up pre-commit hooks:
   ```bash
   make pre-commit-install
   ```
4. Verify everything works:
   ```bash
   make security  # runs: gitleaks, pre-commit
   make lint
   make test
   ```
5. Create a feature branch for your changes.

## Pull requests

- Keep commits focused; use conventional messages (e.g., `feat:`, `fix:`).
- Include a summary of the change, tests performed, and any follow-up work.
- If your change adds a command or flag, update the relevant docs in `docs/`.
- For breaking changes, call out migration notes clearly in the PR description.

## End-to-end tests

- End-to-end coverage lives under `test/e2e` and is executed with `make e2e` (or `go test ./test/e2e -count=1`).
- The harness auto-detects Colima on macOS and will set `DOCKER_HOST` for you when needed. If Docker is still unreachable, start Colima with `colima start --network-address` and retry; as a last resort export:

  ```sh
  export DOCKER_HOST="unix://$HOME/.colima/default/docker.sock"
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE="/var/run/docker.sock"
  # Requires jq (install via 'brew install jq' on macOS).
  export TESTCONTAINERS_HOST_OVERRIDE="$(colima status --json | jq -r '.ip_address')"
  ```

- The harness keeps its configuration and keyring inside a temporary directory, so no extra setup is required on CI runners.

## Reporting issues

- Use the issue templates to provide reproduction steps and environment details.
- Include the `jk` version (`jk version`) and Jenkins version when possible.
- Attach logs or stack traces if they help illustrate the problem.

## Versioning

This project follows [Semantic Versioning](https://semver.org/):
- **MAJOR** version for incompatible API changes
- **MINOR** version for backwards-compatible functionality additions
- **PATCH** version for backwards-compatible bug fixes

### Version Formats

- **Release versions**: `v1.2.3` (tagged releases)
- **Development builds**: `dev-abc1234[-dirty]` (local builds, no tag)
- **Snapshot builds**: `1.2.4-next+abc1234` (GoReleaser snapshots for testing)

### Local Development

When building locally with `make build`:
- If on a tagged commit: version shows the tag (e.g., `v0.1.0`)
- If between tags: version shows `dev-<commit>[-dirty]` (e.g., `dev-9a63037-dirty`)

Check your build version:
```bash
./bin/jk version
```

### Testing Snapshot Releases

To test the release process without publishing:
```bash
goreleaser release --snapshot --clean
ls -la dist/
```

This creates binaries in `dist/` with versions like `0.1.1-next+abc1234`.

## Release process

Releases are automated using [GoReleaser](https://goreleaser.com/) and triggered by pushing a version tag:

1. **Create a release tag** following [semantic versioning](https://semver.org/):
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Automated steps** (via `.github/workflows/release.yml`):
   - Runs tests
   - Builds binaries for Linux, macOS, and Windows (amd64 + arm64)
   - Generates changelog from [conventional commits](https://www.conventionalcommits.org/)
   - Creates GitHub release with artifacts and checksums
   - Updates Homebrew tap formula (requires `HOMEBREW_TAP_GITHUB_TOKEN`)
   - Publishes Docker images to GitHub Container Registry

3. **Conventional commits**: Use prefixes like `feat:`, `fix:`, `deps:` for automatic changelog categorization.

**Note**: The `HOMEBREW_TAP_GITHUB_TOKEN` secret must be configured for Homebrew tap updates. Docker publishing requires appropriate registry permissions.

## Questions?

Open a GitHub issue or start a discussion in the repository. For
security-sensitive reports, please follow the [security policy](SECURITY.md).
