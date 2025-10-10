# Jenkins CLI ("jk") Specification & Technical Plan

## 1. Context & Vision
- Deliver a cross-platform command line experience that makes Jenkins approachable for day-to-day development workflows while remaining safe for production environments.
- Pair a lightweight Go CLI with Jenkins intrinsic REST APIs and a thin companion plugin that unifies inconsistent surfaces (runs, credentials, job provisioning, events).
- Provide parity with the ergonomics of GitHub CLI (`gh`) where it overlaps with CI/CD (contexts, runs, logs, secrets, extensions).

## 2. Goals
- Time-to-first-run under 60 seconds from `jk auth login` to `jk run start`.
- Uniform, scriptable coverage for Jenkins essentials: contexts, jobs/pipelines, runs, logs, artifacts, tests, credentials, nodes, queue, plugins, configuration-as-code, events, metrics.
- Developer-friendly defaults: human output with optional `--json`/`--yaml`, colorized summaries, auto-completions, and extension support.
- Safe-by-default mutations with clear capability detection, dry-run flags, and respect for Jenkins RBAC and CSRF protections.
- Modular architecture that works against stock Jenkins installs while unlocking richer UX when the companion plugin is installed.

### Confirmed baselines & policies
- **Supported Jenkins versions:** full support for three maintained LTS lines at any time — baseline `2.361.4`, baseline + ~12 months, and current LTS — validated continuously in CI. Older LTS versions receive best-effort support only.
- **Authentication:** v1.0 ships with API token + Basic auth. OIDC/SSO integrations are tracked for a post-v1.0 iteration.
- **Air-gapped & FIPS:** provide offline bundles (binaries, checksums, trust store guidance) in v1.0. FIPS-compatible builds are planned for the 1.x roadmap once Go/toolchain support is validated.
- **Proxy & CA handling:** support `--proxy`, `HTTPS_PROXY`/`NO_PROXY`, and `--ca-file`/`JK_CA_FILE`, with precedence `flag > env > context config`.
- **Telemetry:** opt-in only (`jk analytics enable|disable` or `JK_ANALYTICS=1`). Payload is limited to command name, duration, exit code, and capability hash; never transmit URLs, tokens, or identifiers.
- **Distribution:** GitHub Releases (primary), Homebrew tap, and Scoop manifest at GA. Deb/rpm repositories are 1.x backlog.
- **CLI ↔ plugin version handshake:** CLI sends `X-JK-Client: <semver>` and `X-JK-Features: runs,cred,events,…`. Plugin responds via `/jk/api/status` with `{version, features[], minClient, recommendedClient}` enabling graceful degradation messaging.

## 3. Non-Goals
- Managing non-CI/CD GitHub concerns (issues, PRs, code reviews).
- Recreating or depending on Blue Ocean specific APIs.
- Provisioning infrastructure outside Jenkins (cloud agents, Kubernetes cluster management).
- Replacing Jenkins web UI; the CLI complements it and surfaces clear links back to the UI.

## 4. Target Users & Workflows
- **Application developer:** triggers or inspects pipeline runs, watches logs, fetches artifacts, inspects test results.
- **Release engineer:** creates and maintains pipeline jobs, manages credentials, reruns failed stages.
- **DevOps/SRE:** monitors queue health, node availability, plugin versions, controller metrics.
- **Jenkins administrator:** applies configuration-as-code bundles, installs plugins, observes events.

Key workflows the CLI must make trivial:
1. Start a pipeline run with parameters and follow live stage/log progress.
2. Create or update a pipeline job from a high-level spec or raw config XML.
3. Manage secrets scoped to folders or the system store.
4. Inspect the latest runs, download artifacts, and export test results.
5. Observe queue latency, node capacity, and controller metrics in near real time.
6. Stream controller events for automation or dashboards.

## 5. Functional Requirements
- **Authentication & contexts**
  - `jk auth login <url>` stores API tokens and TLS settings.
  - `jk context ls|use|rm`, `jk whoami`.
  - Optional `jk auth status` to show crumb/token health.
- **Jobs & pipeline management**
  - List, view metadata, create (from high-level spec or config XML), copy, delete jobs.
  - Support folders and multibranch job resolution via human-readable paths (e.g., `team/service/build`).
  - Trigger multibranch scans when allowed.
- **Runs**
  - Trigger builds with parameters and optional `--follow`.
  - List and filter runs by status/branch.
  - View run details, including stage graph, parameters, SCM info, and artifacts.
  - Cancel, rebuild/replay, restart from stage when plugins support it (surface capability flags).
- **Logs & artifacts**
  - Stream console logs with robust resume (`jk log follow`).
  - List and download artifacts with glob filtering and output directory controls.
- **Tests**
  - Summarize test results (failures, flaky, pass rate) from the `testReport` API.
  - Export raw JUnit data for downstream tooling.
- **Credentials**
  - CRUD operations for common credential types in system and folder scopes.
  - Surface type metadata and last-updated timestamps.
- **Nodes & queue**
  - List nodes, cordon/uncordon, toggle temporary offline messages.
  - List queue items, inspect causes, cancel items.
- **Plugins**
  - List installed plugins, versions, updates available.
  - Install, enable/disable plugins with confirmation gates.
- **Configuration as Code (JCasC)**
  - Apply, export, reload configuration bundles with optional dry-run.
- **Events & metrics**
  - Stream events via SSE (`jk events stream --kind run,queue`).
  - Fetch Prometheus metrics snapshots or tail key gauges (queue length, executor usage).
- **Diagnostics**
  - `jk doctor` validates context configuration, reachability, authentication, crumb retrieval, permission checks for core commands, and optional capabilities (SSE, Prometheus) with both human-readable and `--json` output.
- **Extensions**
  - Install and manage exec-based extensions (`jk extension install <repo>`, `jk extension ls|rm`).
- **General UX**
  - Support `--json`/`--yaml` output and template-friendly fields.
  - Provide shell completions (bash/zsh/fish) and context-aware prompts.
  - Return normative exit codes detailed in §9.6, including specialized mappings for `jk run --follow`.
  - Telemetry is opt-in via `jk analytics enable|disable` or `JK_ANALYTICS=0|1`; default is disabled.

## 6. Non-Functional Requirements
- **Security:** TLS verification by default with explicit opt-out, secure token storage (OS keychain), automatic crumb handling, adherence to Jenkins permissions.
- **Performance:** `jk log follow` latency < 500 ms refresh; listing commands limited via pagination (`--limit`) and `tree` query parameters to minimize payloads. Outbound request concurrency defaults to 4 in-flight operations (adjustable via `--max-concurrency` or `JK_MAX_CONCURRENCY`) to protect Jenkins controllers.
- **Reliability:** Commands retry transient network errors; CLI caches crumb until expiry; operations idempotent when possible.
- **Portability:** Single static binary for macOS (amd64/arm64), Linux (amd64/arm64), Windows (amd64).
- **Observability:** Optional verbose logging (`JK_DEBUG=1`), structured logs for integration tests, plugin exposes audit logs/metrics.
- **Extensibility:** Clear public interfaces for third-party extensions and plugin endpoints; maintain semantic versioning for the CLI and plugin.

## 7. High-Level Architecture
```
+----------------+        HTTPS + Crumb/API token        +----------------------+
| Jenkins CLI jk |-------------------------------------->| Jenkins Controller   |
| (Go binary)    |                                       | (Remote API, Pipeline|
|                |<-------------- SSE/HTTP --------------| REST, Credentials)   |
+----------------+                                       +----------------------+
        |                                                           |
        | optional rich features                                    |
        |                                                           v
        |                                             +---------------------------+
        |                                             | Companion Plugin (jk-api) |
        |                                             | - Runs facade             |
        +-------------------------------------------->| - Job spec endpoints      |
                                                      | - Credentials facade      |
                                                      | - Event router            |
                                                      +---------------------------+
```
- CLI communicates directly with Jenkins core APIs for baseline functionality.
- When installed, the companion plugin exposes additional REST/SSE endpoints that the CLI auto-detects via capability checks.
- Jenkins authentication uses API token + Basic auth; crumb issuer ensures CSRF compliance.
- SSE Gateway and Prometheus endpoints provide streaming/events and metrics.

## 8. External Interfaces & API Usage
| Feature             | Primary API(s)                                                   | Notes |
|---------------------|------------------------------------------------------------------|-------|
| Job metadata        | `GET /job/<path>/api/json?tree=...`, `POST /createItem`, `PUT /job/<path>/config.xml` | Use CloudBees Folders conventions for nested paths. |
| Runs                | `POST /job/<path>/build[WithParameters]`, `GET .../api/json`, `POST .../stop` | Pipeline REST (`/wfapi/**`) for stage graph. |
| Logs                | `GET .../logText/progressiveText?start=N`, `GET .../consoleText` | Resume on 416 errors by resetting start offset. |
| Artifacts           | `GET .../artifact/*`, `GET .../api/json?tree=artifacts[fileName,relativePath]` | Supports glob filtering client-side. |
| Tests               | `GET .../testReport/api/json` (JUnit plugin) | Fail gracefully if plugin absent. |
| Credentials         | `POST /credentials/store/(system|folder)/domain/_/createCredentials`, similar update/delete | JSON payloads with `$class`. Companion plugin normalizes types. |
| Queue               | `GET /queue/api/json?tree=items[id,task[name,url],why,inQueueSince]`, `POST /queue/cancelItem?id=<id>` | |
| Nodes               | `GET /computer/api/json`, `POST /computer/<name>/toggleOffline` | For safety, creation routed via JCasC. |
| Plugins             | `GET /pluginManager/api/json?depth=1`, `POST /pluginManager/installNecessaryPlugins` | Install API requires XML list. |
| Config-as-Code      | JCasC endpoints or script console; companion plugin should add `/jk/casc/**` wrappers | |
| Events              | SSE Gateway `/sse-gateway/stats`, `/sse-gateway/stream?topic=...` | Companion plugin publishes stable topic names. |
| Metrics             | Prometheus plugin `/prometheus` | Parse text exposition format. |
| CSRF crumb          | `GET /crumbIssuer/api/json` | Cache crumbRequestField + crumb per context. |

## 9. CLI Design

### 9.1 Command Tree
| Group          | Example commands                                                | Notes |
|----------------|-----------------------------------------------------------------|-------|
| `auth`         | `jk auth login`, `jk auth status`, `jk auth logout`             | Stores contexts securely. |
| `context`      | `jk context ls`, `jk context use`, `jk context rename`          | Config stored under `$XDG_CONFIG_HOME/jk/config.yml`. |
| `job`          | `jk job ls`, `jk job view`, `jk job create`, `jk job import-config`, `jk job delete` | `jk job create` consumes high-level YAML when plugin present. |
| `run`          | `jk run start`, `jk run ls`, `jk run view`, `jk run cancel`, `jk run rerun`, `jk run restart-from` | Capability flags printed in `jk run view`. |
| `log`          | `jk log follow`, `jk log download`                              | `log follow` supports `--since` and `--plain`. |
| `artifact`     | `jk artifact ls`, `jk artifact download`                        | Glob filtering via `--pattern`. |
| `test`         | `jk test report`, `jk test junit`                               | Format options `--summary`, `--json`. |
| `cred`         | `jk cred ls`, `jk cred create`, `jk cred update`, `jk cred rm`  | Type-specific subcommands (`secret-text`, `userpass`, `ssh`). |
| `node`         | `jk node ls`, `jk node cordon`, `jk node uncordon`, `jk node delete` | Cordon optionally sets offline message. |
| `queue`        | `jk queue ls`, `jk queue cancel`                                | `jk queue ls --watch` uses SSE if available. |
| `plugin`       | `jk plugin ls`, `jk plugin install`, `jk plugin enable`         | `install` prompts for confirmation unless `--yes`. |
| `casc`         | `jk casc apply`, `jk casc export`, `jk casc reload`             | Requires admin rights. |
| `events`       | `jk events stream`, `jk events tail`                             | Fallback to polling with refresh interval when SSE missing. |
| `metrics`      | `jk metrics dump`, `jk metrics top`                             | `top` keeps refreshing selected gauges. |
| `extension`    | `jk extension install`, `jk extension ls`, `jk extension rm`    | Exec-based, loads `jk-<name>` on PATH. |
| `config`       | `jk config set|get|unset`                                       | Manage CLI preferences. |
| `analytics`    | `jk analytics enable`, `jk analytics disable`, `jk analytics status` | Manage opt-in telemetry state. |
| Global flags   | `--context`, `--url`, `--token`, `--insecure`, `--json`, `--yaml`, `--quiet`, `--color=auto|always|never`, `--trace` | CLI resolves context precedence: flag > env > active context. |

### 9.2 Configuration & State
- Config file `config.yml` holds contexts (name, URL, username), toggles (color, pager).
- Secrets (API tokens) stored in OS keychain via `go-keyring`; fallback encrypted file only when the user passes `--allow-insecure-store` and confirms interactively.
- Proxy configuration precedence is `flag (--proxy) > environment (HTTPS_PROXY/HTTP_PROXY/NO_PROXY) > context config`. CLI also honors custom CA bundles via `--ca-file` and `JK_CA_FILE`.
- Per-context cache directory stores crumb and small metadata (capabilities, plugin detection caches) with short TTL.

### 9.3 HTTP & Auth Flow
1. Resolve server URL (ensuring trailing slash trimmed) and credentials from context or flags.
2. Issue `GET /crumbIssuer/api/json` when performing first mutating request or crumb missing/expired.
3. Attach version handshake headers to every request: `X-JK-Client: <semver>` and `X-JK-Features: <capabilities>` (comma-separated).
4. All requests include `Authorization: Basic <user:token>` and `Content-Type` appropriate to method.
5. Respect Jenkins CSRF configuration; if crumb endpoint 404, assume crumbs disabled.
6. Retry on 401/403 once after refreshing crumb; propagate descriptive error if still failing.

### 9.4 Output & UX
- Human output includes concise tables or cards; use color when stdout is TTY.
- `--json` returns stable JSON schema documented per command; CLI uses struct tags and `omitempty`.
- Support pagination flags `--limit`, `--after` for list commands; CLI surfaces server pagination (if plugin adds support) via `Link` headers.
- Provide `--open` flag for commands that can open Jenkins UI in browser (optional, off by default).
- Autocomplete scripts generated via Cobra's built-in support.

### 9.5 Extension Model
- Compatible executables under PATH named `jk-<name>` or installed into `~/.config/jk/extensions/`.
- `jk extension install <git-url>` clones repo (shallow) and links binary/script, similar to `gh`.
- CLI offers environment variables to extensions (`JK_CONTEXT`, `JK_JENKINS_URL`, `JK_TOKEN_PATH`).
- Document contract for extension authors (input args, help text, JSON output expectations).

### 9.6 Exit codes & run result mapping
| Code | Meaning                                      |
|------|----------------------------------------------|
| 0    | Success                                       |
| 1    | General error (unexpected failure)            |
| 2    | Validation error (bad flags, schema issues)   |
| 3    | Not found (job/run/artifact absent)           |
| 4    | Authentication failure                        |
| 5    | Permission denied                             |
| 6    | Connectivity/DNS/TLS failure                  |
| 7    | Timeout (server or client)                    |
| 8    | Feature unsupported (capability missing)      |

`jk run --follow` adopts build-result exit codes in addition to the table above:
| Result    | Exit code |
|-----------|-----------|
| SUCCESS   | 0         |
| UNSTABLE  | 10        |
| FAILURE   | 11        |
| ABORTED   | 12        |
| NOT_BUILT | 13        |

Other commands keep the general-purpose codes, surfaced consistently in help text and docs.

### 9.7 Pagination, cursors & limits
- List commands accept `--limit`, `--cursor`, `--since`, and `--until`. `--limit` defaults to 20; `--since/--until` accept RFC3339 timestamps or relative durations (e.g., `2h`).
- When the companion plugin is available, responses follow the cursor contract:
  ```json
  {
    "items": [...],
    "nextCursor": "g2wAAA..."
  }
  ```
- CLI persists the last cursor when used interactively (`--cursor @prev`), and surfaces `nextCursor` in human-readable output.
- Against baseline Jenkins endpoints, the CLI enforces `--limit` client-side and supports `--since/--until` where timestamps are available.

### 9.8 Job path encoding
- Accept human-readable job paths such as `team/app/main`.
- Split on `/`, percent-encode each segment (spaces → `%20`, encode `%` and characters disallowed in Jenkins identifiers), and join with `/job/<segment>`.
- Reuse encoder everywhere (HTTP requests, UI links, plugin identifiers) and verify via unit tests covering nested folders, spaces, and special characters.

### 9.9 Progressive log streaming
- Poll `logText/progressiveText` every 300–500 ms (`--interval` override). Honor `X-Text-Size` to maintain offsets.
- If Jenkins returns 416 or `X-More-Data: false` after truncation, reset offset to `0` and resume gracefully.
- Support `--since <duration>` and `--plain` to trim to recent output and disable ANSI stripping.
- During queue/wait states, back off to 1s polls until the run transitions to `running`.

### 9.10 Capability detection cache
- On context activation, probe `/jk/api/status`, `/sse-gateway/`, and `/prometheus` once; cache capability flags for 60 seconds or until an operation fails with 404/403/5xx.
- Capability flags include `hasRunsFacade`, `hasCredentialFacade`, `hasEventRouter`, `hasPrometheus`, and `hasSSE`.
- Commands fall back to core APIs when a capability is absent and emit a single informational warning (suppressed with `--quiet`).

### 9.11 Artifact download semantics
- `jk artifact download` accepts `--pattern` globs (default `**/*`) using case-sensitive matching consistent with Go filepath globbing.
- Duplicate artifact names across directories are all downloaded unless `--unique` is set (warn otherwise).
- Preserve artifact-relative directory structure under the output directory unless `--flat` is supplied.
- Exit with code 3 when no artifacts match filters and `--allow-empty` is not set.

### 9.12 Error messaging standard
- First line states the human-readable cause (`Error: failed to fetch job 'team/app' (403 Forbidden)`).
- Follow with concise remediation hint (`hint: check that your token has Job/Read permission`).
- Mention `--trace` or `JK_DEBUG=1` for verbose logs. Never surface raw HTML responses; log sanitized details under trace mode only.

### 9.13 Rate limiting & concurrency controls
- Default to 4 concurrent outbound requests; configurable via `--max-concurrency` CLI flag, `JK_MAX_CONCURRENCY` env var, or `config.concurrency` per context.
- Implement token-bucket rate limiting (default 5 requests/second burst 10). Allow overrides via config for high-throughput automation with caution banner.

## 10. Companion Plugin Design

### 10.1 Overview
- Plugin name: `jk-api` (working name).
- Built with Jenkins plugin parent POM, Java 11 (matching Jenkins LTS baseline 2.361+).
- Provides REST endpoints under `/jk/api/**` and SSE topics under `/jk/events/**`.
- Packages Jenkins descriptors to register capabilities; CLI detects plugin via `GET /jk/api/status`.

### 10.2 Runs Facade
- Endpoint: `GET /jk/api/runs?jobPath=<path>&limit=<n>&after=<buildNumber>` returns normalized list of runs (id, number, status, result, startTime, duration, branch, commit, stages, artifacts, testSummary).
- Endpoint: `GET /jk/api/runs/<jobPath>/<buildId>` returns full detail, combining classic build data and Pipeline REST when available.
- Endpoint: `GET /jk/api/runs/<jobPath>/<buildId>/logs?start=<offset>` returns structured progressive log pointer.

### 10.3 Job Provisioner
- Endpoint: `POST /jk/api/jobs` with payload describing job type (`pipeline`, `multibranch`, `freestyle`), SCM, Jenkinsfile path, parameters, triggers.
- Endpoint: `PUT /jk/api/jobs/<jobPath>` updates existing job declaratively.
- Implementation internally uses Job DSL or config XML generation with validation and diff preview (optional `dryRun=true` query).
- Returns created job URL and warnings.

### 10.4 Credentials Facade
- Endpoint: `GET /jk/api/credentials?scope=system|folder&folderPath=...` returns normalized credential list.
- Endpoint: `POST /jk/api/credentials` accepts simplified JSON for common types:
  ```
  {
    "scope": "folder",
    "folderPath": "team/app",
    "type": "secretText",
    "id": "slack-token",
    "description": "Slack notifier",
    "data": {"secret": "****"}
  }
  ```
- Endpoints for update (`PUT`) and delete (`DELETE`) mirror this shape.
- Plugin maps to underlying Credentials plugin classes and handles folder RBAC checks.

### 10.5 Events Router
- Exposes SSE stream at `/jk/events/stream?topics=run,queue,node`.
- Converts Jenkins event bus notifications and SSE Gateway messages into stable JSON events:
  ```
  { "topic": "run.started", "job": "team/app/main", "build": 128, "timestamp": "...", "causes": [...] }
  ```
- Supports server-side filters and heartbeat frames.

### 10.6 Safety & Permissions
- Enforces `@RequirePOST` on mutating endpoints.
- Validates user permissions (e.g., `Job.CREATE`, `Credentials.CREATE`) before invoking operations.
- Supports `X-Client-Dry-Run: true` header to preview actions.
- Emits audit log entries to Jenkins log with user, endpoint, parameters.

### 10.7 Packaging & Dependencies
- Depends on Pipeline, Credentials, SSE Gateway, Job DSL plugins via optional dependencies; features degrade gracefully when absent.
- Ships with simple configuration page allowing admins to disable individual endpoints or require admin approval.
- Versioned REST contract; `/jk/api/status` returns `{ "version": "1.0.0", "features": ["runs","credentials","events"], "minClient": "0.3.0", "recommendedClient": "1.0.0" }`. CLI compares against `X-JK-Client` header, warns on incompatibility, and falls back to core APIs when below `minClient`.

## 11. Security Considerations
- CLI encourages dedicated service accounts with least privileges; documentation includes recommended Jenkins matrix permissions per command.
- Tokens stored in OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service). CLI warns when falling back to plaintext (requires `--allow-insecure-store`).
- Companion plugin uses Jenkins standard stapler routing with CSRF crumb requirements and Strict-Transport-Security headers (inherits from controller).
- Provide `jk auth logout --purge` to remove cached tokens and crumbs.
- Support mutual TLS by allowing custom CA bundles via `--ca-file` and `JK_CA_FILE`.
- Logging redacts tokens and secrets both client and server side.

| Command group                                       | Required Jenkins permissions                                           |
|-----------------------------------------------------|------------------------------------------------------------------------|
| `auth`, `context`, `config`                         | Client-side only                                                       |
| `job ls`, `job view`                                | `Overall/Read` + `Job/Read` (folder scoped)                            |
| `job create`, `job import-config`, `job delete`     | `Job/Create`, `Job/Configure`, `Job/Delete` (folder scoped)           |
| `run start`                                         | `Job/Build`                                                            |
| `run cancel`                                        | `Job/Cancel` (or equivalent policy)                                   |
| `run rerun`, `run restart-from`                     | Plugin-specific (`Rebuild/Build`, `Replay`, `Restart from Stage`)     |
| `log follow`, `artifact ls/download`, `test report` | `Job/Read`                                                             |
| `cred ls`                                           | `Credentials/View` (system or folder scoped)                           |
| `cred create/update/delete`                         | `Credentials/Create`, `Credentials/Update`, `Credentials/Delete`      |
| `node ls`                                           | `Overall/Read`                                                         |
| `node cordon/uncordon`, `node delete`               | `Computer/Configure` (delete also `Computer/Delete` if enabled)        |
| `queue ls`                                          | `Overall/Read`                                                         |
| `queue cancel`                                      | `Job/Cancel`                                                           |
| `plugin ls/install/enable`                          | `Overall/Administer`                                                   |
| `casc apply/export/reload`                          | `Overall/Administer`                                                   |
| `events stream`, `metrics`                          | `Overall/Read` (metrics may require `Overall/Administer` per policy)  |

## 12. Technology Selection & Tooling

### 12.1 CLI
- Language: Go 1.22+ (module mode, static builds).
- Libraries:
  - `spf13/cobra` + `spf13/pflag` for command structure.
  - `spf13/viper` for config binding and env overrides.
  - `go-resty/resty` for HTTP client with retry/backoff.
  - `99designs/keyring` for cross-platform secret storage.
  - `fatih/color` for TTY coloring (with disable flag).
  - `tidwall/gjson` (or typed structs) for JSON filtering; `tidwall/sjson` when editing JSON.
  - `r3labs/sse/v2` or `launchdarkly/eventsource` for SSE streams.
  - Lightweight table renderer (e.g., `olekukonko/tablewriter`) or custom formatting to minimize dependencies.
  - `rs/zerolog` (or similar) for structured logging.
  - `spf13/afero` for test-friendly filesystem abstraction.
- Testing:
  - `stretchr/testify` for assertions.
  - `h2non/gock` for HTTP mocking.
  - Integration tests using Dockerized Jenkins via `testcontainers-go`.
- Build & release:
  - `make` or `mage` tasks for lint/test/build.
  - `golangci-lint` for linting.
  - `goreleaser` to produce binaries and release artifacts (tarballs, checksums, SBOM).
  - Provide Docker image for CI pipelines (Go + required tools).

### 12.2 Companion Plugin
- Language: Java 11 (optionally Kotlin for DSL-heavy areas).
- Build: Maven with Jenkins plugin parent POM (baseline matching LTS).
- Testing:
  - `jenkinsci/jenkins-test-harness` (`JenkinsRule`) unit/integration tests.
  - Contract tests verifying REST endpoints with CLI fixtures.
- Dependencies:
  - `workflow-api`, `workflow-job`, `workflow-cps` for pipeline data.
  - `credentials`, `credentials-binding`.
  - `job-dsl`.
  - `sse-gateway`, `pubsub-light` optional.
- Release: hosted on internal update center or artifacts repository (Artifactory), packaged as `.hpi`.

### 12.3 Development Workflow
- Mono-repo layout with `/cmd/jk`, `/pkg/...`, `/plugin`.
- Use GitHub Actions or Jenkins pipelines for CI: run Go tests, plugin Maven tests, integration suites.
- Manage dependencies with Renovate or Dependabot.
- Documentation site generated via MkDocs or Docusaurus (optional) fed by `docs/`.

## 13. Observability & Telemetry
- CLI:
  - `--trace` flag outputs HTTP request/response summaries (headers sanitized).
  - Optional OpenTelemetry exporter via `JK_OTEL_EXPORTER` for command metrics (duration, exit code) when teams opt in.
  - `jk analytics enable|disable` (or `JK_ANALYTICS=0|1`) controls telemetry; when enabled, CLI emits command name, duration, exit code, Jenkins capability hash, and anonymized client identifier.
- Plugin:
  - Exposes metrics under `/jk/metrics` (or integrate with Prometheus registry) showing API usage counts, error counts, latency histograms.
  - Logs structured audit events to Jenkins logs (JSON line format).
  - Supports configurable log level via Jenkins system configuration.

## 14. Testing Strategy

### 14.1 CLI
- Unit tests per package with coverage target 80%.
- Contract tests using recorded fixtures from Jenkins LTS to guard JSON schema stability.
- Integration tests:
  - Spin up Jenkins LTS container with required plugins via Docker Compose or Testcontainers.
  - Run scenarios: login, create job, trigger run, follow logs, download artifact.
  - Ensure tests cover crumb logic, SSE fallback, plugin detection.

### 14.2 Companion Plugin
- JenkinsRule tests for each endpoint validating permission enforcement, payload schema, and error paths.
- Smoke tests deploying plugin to container and executing CLI integration suite.
- Performance tests ensuring endpoints respond within acceptable latency under load (simulate ~100 concurrent clients).

### 14.3 Continuous Integration
- Pipeline stages: lint -> unit tests (Go + Java) -> build binaries -> integration tests (matrix by OS) -> package artifacts -> publish preview docs.
- Nightly job to run extended integration matrix (multiple Jenkins versions, plugin combinations).
- Maintain continuous integration matrix for `2.361.4`, `baseline+~1yr`, and latest LTS, ensuring CLI + plugin tests run across all three.

## 15. Implementation Roadmap

### Phase 0 – Project Bootstrap (Week 0-1)
- Initialize repository structure (`cmd/jk`, `pkg/...`, `/plugin`).
- Set up Go module, basic Cobra scaffold, logging, configuration plumbing.
- Establish CI pipeline (lint, unit tests) and coding standards.
- Document contributing guidelines and decision log format.

### Phase 1 – CLI MVP (Week 2-6)
- Implement contexts/auth storage, crumb handling, HTTP client abstraction.
- Deliver commands: `auth`, `context`, `job ls/view`, `run start/ls/view`, `log follow`, `artifact ls/download`, `queue ls/cancel`, `test report`.
- Add `--json` output and table renderer.
- Build integration test harness against Jenkins LTS without companion plugin.
- Ship `examples/parity-smoke.sh` demonstrating `jk` parity with `gh` workflows (login, job ls, run follow exit codes, artifact download, credential create).
- Publish v0.1.0 preview release with documentation for supported commands.

### Phase 2 – Core Admin Features (Week 7-11)
- Add credentials CRUD (direct Credentials API), node cordon/uncordon, plugin list/install (with confirmations), JCasC apply/export (existing endpoints).
- Implement events stream (SSE + polling fallback) and metrics fetch.
- Harden error handling, add retries and capability detection logic.
- Ship autocompletion scripts and extension framework skeleton.
- Release v0.2.0 with updated docs and changelog.

### Phase 3 – Companion Plugin (Week 12-18)
- Scaffold plugin project, implement `/jk/api/status`.
- Deliver runs facade endpoints and integrate CLI to consume them (stage graph parity for freestyle/matrix jobs).
- Implement job provisioner with high-level YAML spec support.
- Implement credentials facade and event router (SSE topics).
- Add audit logging and admin configuration UI.
- Publish plugin beta and CLI v0.3.0 toggling enhanced features automatically.

### Phase 4 – Polish & Extensions (Week 19-22)
- Finalize extension tooling (`jk extension install/ls/rm`).
- Add CLI telemetry opt-in and improved error messages.
- Improve artifact filtering, test summaries, queue watch using SSE.
- Package installers (Homebrew tap, Scoop manifest, Debian/RPM optional).
- Document plugin installation and security practices.
- Release v1.0.0.

### Phase 5 – Maintenance & Scale (post GA)
- Add TUI variant for `jk run watch`.
- Support additional credential types (certificates, Kubernetes service account tokens).
- Evaluate caching strategies and offline modes for metadata.
- Track Jenkins upstream changes and update compatibility matrix.

Each phase is gated by acceptance criteria (functional coverage, test suite passing, documentation updates).

## 16. Release & Distribution Plan
- Use semantic versioning for CLI (`vMAJOR.MINOR.PATCH`).
- Publish binaries to GitHub Releases with checksums and SBOM (CycloneDX via `syft`).
- Homebrew: maintain tap repository to install `jk`.
- Windows: deliver Scoop manifest and optional MSI installer (WiX) for enterprise distribution.
- Provide offline bundles (CLI binaries, checksums, trusted CA instructions) for air-gapped environments.
- Companion plugin distributed via internal update center (Jenkins plugin manager) with release notes.
- Provide `jk version --check` to notify users of updates (opt-in).

## 17. Risks & Mitigations
- **API inconsistency across Jenkins versions:** test against supported LTS versions; implement feature flags and graceful degradation messages.
- **CSRF or crumb failures:** fallback detection, clear remediation guidance, ability to disable crumb usage if server configured accordingly.
- **Plugin optional dependency drift:** maintain compatibility matrix, adopt Jenkins plugin BOM, run nightly compatibility tests.
- **Performance impact on controller:** ensure CLI uses `tree` filters and rate limits; plugin caches heavy data and enforces paging.
- **Security of stored tokens:** default to OS keyring, warn and require explicit confirmation before plaintext storage.
- **User expectation mismatch:** invest in documentation, `jk help` examples, `jk doctor` command to validate environment.

## 18. API Contracts Reference
- Canonical JSON schemas, cursor contracts, and status payloads live in `docs/api.md`. Changes to these artifacts require a semver-major or explicit compatibility notes.
- Fixtures used in integration and contract tests mirror the samples in `docs/api.md` to guarantee parity between CLI and plugin implementations.

## 19. Open Follow-Ups
- Schedule FIPS-compatible build validation during the 1.x cycle and document cryptography constraints.
- Gauge demand and prioritize timeline for official deb/rpm repositories after GA.
- Reassess priority of OIDC/SSO support post-v1.0 based on customer feedback.
- Monitor usage of `jk analytics` opt-in telemetry and publish governance documentation alongside release notes.

## 20. Ready-to-code checklist
- [x] Normative exit codes and `jk run --follow` result mappings captured (§9.6).
- [x] Pagination, cursor semantics, and `--limit/--cursor/--since/--until` behavior documented (§9.7, docs/api.md).
- [x] JSON schemas for runs, credentials, events, and status handshake published (`docs/api.md`).
- [x] Jenkins permission map per command group defined (§11).
- [x] Supported Jenkins LTS matrix and compatibility policy finalized (§2, §14).
- [x] CLI ↔ plugin handshake headers and `/jk/api/status` response specified (§2, §9.3, §10.7).
- [x] Job path encoding algorithm detailed (§9.8).
- [x] Proxy precedence, CA handling, and telemetry policy outlined (§2, §9.2, §13).
- [x] `jk doctor` scope and outputs defined (§5).
- [x] Artifact download semantics and concurrency controls captured (§9.11, §9.13).
- [x] Error messaging style and trace guidance documented (§9.12).
- [x] Capability detection cache strategy described (§9.10).

---

This specification provides the shared blueprint needed to begin implementation while leaving room for iterative discovery. Update as decisions solidify during development.
