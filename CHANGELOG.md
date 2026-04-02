# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.27] - 2026-04-02
### Fixed
- Wrote generated GitHub release notes to the runner temp directory so GoReleaser can publish without dirtying the checked-out tree.


## [0.0.26] - 2026-04-02
### Changed
- Switched releases to the shared PR-based `scripts/release.sh` flow, with `CHANGELOG.md` supplying the GitHub release notes and CI creating the version tag only after the merged release commit verifies.

### Fixed
- Removed deprecated release shims so there is exactly one supported release entrypoint.


## [0.0.25] - 2026-04-01

### Fixed
- Keyed manual release rerun concurrency by tag so rerunning one release tag cannot cancel a different release recovery run.

## [0.0.24] - 2026-04-01

### Fixed
- Treated `Version already exists` as success when a skill publish reruns after retrying without an alias, preventing false-negative publish failures on release reruns.

## [0.0.23] - 2026-04-01

### Changed
- Simplified the GitHub release workflow to use `actions/setup-go`'s built-in module and build caching instead of a separate cache step, making tag reruns more reliable.

## [0.0.22] - 2026-04-01

### Fixed
- Reworked macOS signing invocation so Linux CI, Jenkins-based end-to-end tests, and GoReleaser all call the helper through `bash` instead of relying on executable mode propagation through every checkout path.

## [0.0.21] - 2026-04-01

### Fixed
- Serialized macOS keychain reads and writes behind an inter-process lock to prevent prompt storms when multiple `jk` processes access the same token concurrently.
- Ad-hoc signed macOS binaries with the stable identifier `io.github.avivsinai.jk` in both local builds and GoReleaser artifacts so Keychain approvals survive Homebrew upgrades.

## [0.0.20] - 2026-03-31

### Changed
- Bumped `github.com/rs/zerolog` from `1.34.0` to `1.35.0`.

### Fixed
- Release and skill publishing now fail fast when the tag version does not match the skill and plugin manifests, preventing another partially aligned tag.

## [0.0.19] - 2026-03-31

### Fixed
- `jk search` and `jk run search` now accept `--with-meta` as a compatibility alias, while the docs now explain that structured search output already includes lightweight metadata by default.

## [0.0.16] - 2026-02-25

### Changed
- `--quiet`/`-q` now also suppresses resty HTTP client warnings (e.g. "Using Basic Auth in HTTP mode") for plain-HTTP Jenkins instances.

## [0.0.15] - 2026-01-27

### Fixed
- Fixed `-p/--param` flag incorrectly splitting comma-separated values. Parameters like `SERVICES=a,b,c` are now preserved as a single value instead of being split into multiple parameters. Changed from `StringSliceVarP` to `StringArrayVarP` in cobra flag definition.

## [0.0.13] - 2025-12-31

### Changed
- `--format` now validates allowed values (`json`, `yaml`) and rejects unsupported formats.
- `--json`/`--yaml` and `--format` conflicts are validated up front with gh-style errors.
- `--jq`/`--template` validations run before command execution and accept `--format=json`.
- JSON and jq outputs are pretty-printed only when stdout is a TTY; piped output is compact.

## [0.0.12] - 2025-12-31

### Added
- `--jq` flag for filtering JSON output using jq expressions (`jk run view --json --jq '.result'`)
- `--result` flag to output only the build result (SUCCESS, FAILURE, etc.)
- `--exit-status` flag to exit with code based on build result
- `--wait` flag for blocking until build completes without log streaming
- `--quiet`/`-q` flag to suppress non-essential output (outputs build number in scripting mode)
- `--summary` flag for human-readable build summary with colors and status symbols
- `--template`/`-t` flag for Go template output formatting
- `--format` flag for output format selection (json, yaml, table)
- `JK_QUIET` environment variable support
- Documented `JK_CONTEXT` environment variable in help text

### Fixed
- Interval validation in `--wait` to prevent ticker panic on zero/negative values

## [0.0.11] - 2025-12-14
- CI: upgraded golangci-lint-action to v9 and golangci-lint to v2.
- CI: stabilized required check names for branch protection.

## [0.0.10] - 2025-12-10
- Updated dependencies (go-gh, docker, resty, cobra, x/term).

## [0.0.9] - 2025-10-26
- Fixed version information display for `go install` users by adding runtime/debug.ReadBuildInfo() fallback, showing proper version instead of "dev".

## [0.0.8] - 2025-10-20
- Fixed authentication streaming issues.

## [0.0.7] - 2025-10-20
- Fixed macOS release binaries by enabling cgo so the CLI can access the system Keychain again.

## [0.0.6] - 2025-10-20
- Restored the encrypted file keyring fallback for existing contexts without `allow_insecure_store`, fixing authentication for headless hosts upgrading from earlier releases.

## [0.0.5] - 2025-10-20
- Added top-level `jk search` alias (wrapping `jk run search`) with refreshed help text, documentation, and CLI hints for job discovery ergonomics.
- Redesigned `jk --help` output with gh-style command groups, flag summaries, and examples for improved discoverability.

## [0.0.4] - 2025-10-18

- Added discovery-focused flags to `jk run ls` (`--filter`, `--since`, `--select`, `--group-by`, `--with-meta`) with a versioned JSON schema and metadata payload for agents.
- Introduced `jk run params` for parameter discovery (config- and run-based sources) with frequency and sample heuristics.
- Added `jk run search` for cross-job discovery with folder/glob support and schema-aligned output.
- Exposed `jk help --json` to emit a versioned command/flag catalog and documented exit codes.
- Factored reusable filter parsing/evaluation utilities for parameter/artifact/cause matching and expanded test coverage.
- Initial open-source preparation (community health files, README refresh).
- Improved `jk run --fuzzy` ranking by wiring the command to the new fuzzy scorer and adding regression tests.
