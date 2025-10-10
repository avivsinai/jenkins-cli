# Changelog

## [0.1.0-dev] - 2024-10-10
- Bootstrap Go module, Cobra root command, and project scaffolding.
- Implement auth/context management backed by OS keyring.
- Add Jenkins REST client with crumb handling and capability detection.
- Deliver Phase 1 MVP commands (`job`, `run`, `log`, `artifact`, `queue`, `test`) with `jk run start --follow` streaming logs and returning result-based exit codes.
- Introduce initial unit tests and Makefile targets (`build`, `test`, `tidy`, `fmt`).
