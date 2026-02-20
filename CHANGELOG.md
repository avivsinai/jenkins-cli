# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
