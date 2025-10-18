# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2025-10-18

- Added discovery-focused flags to `jk run ls` (`--filter`, `--since`, `--select`, `--group-by`, `--with-meta`) with a versioned JSON schema and metadata payload for agents.
- Introduced `jk run params` for parameter discovery (config- and run-based sources) with frequency and sample heuristics.
- Added `jk run search` for cross-job discovery with folder/glob support and schema-aligned output.
- Exposed `jk help --json` to emit a versioned command/flag catalog and documented exit codes.
- Factored reusable filter parsing/evaluation utilities for parameter/artifact/cause matching and expanded test coverage.
- Initial open-source preparation (community health files, README refresh).
- Improved `jk run --fuzzy` ranking by wiring the command to the new fuzzy scorer and adding regression tests.
