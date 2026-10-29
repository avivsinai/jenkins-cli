# `jk` API & JSON Contract Reference

This document is normative for the Jenkins CLI (`jk`) JSON output modes and the companion plugin REST surfaces. Breaking changes to these contracts require a semver-major release of the CLI and/or plugin.

> **Status:** the companion plugin is not shipped yet (`plugin/` is a stub). Every `jk ... --json` shape below is produced today from baseline Jenkins endpoints (`/api/json`, `/logText/progressiveText`, `/credentials/store/...`). The `/jk/api/*` and `/jk/events/*` endpoints are the design contract the plugin will implement; the CLI currently consumes only `/jk/api/status` (capability probe) and `/jk/api/credentials` (`cred ls`, with fallback to core).

## 1. JSON output conventions
- All timestamps use RFC3339 (`2006-01-02T15:04:05Z07:00`) in UTC unless otherwise noted.
- Optional fields are omitted when empty (`omitempty`); arrays default to `[]`.
- Enumerations are uppercase strings (e.g., `SUCCESS`, `FAILURE`).
- Cursor-based pagination objects follow `{ "items": [...], "nextCursor": "<opaque>" }`. Absent `nextCursor` means the end of the collection.

## 2. Runs

### 2.1 Run detail (`jk run view --json`; planned plugin endpoint `/jk/api/runs/<jobPath>/<build>`)
```json
{
  "id": "team/app/main/128",
  "number": 128,
  "jobPath": "team/app/main",
  "url": "https://jenkins.example/job/team/job/app/job/main/128/",
  "status": "completed",                 // queued | running | completed
  "result": "SUCCESS",                   // SUCCESS | UNSTABLE | FAILURE | ABORTED | NOT_BUILT | null
  "startTime": "2025-08-12T18:24:03Z",
  "durationMs": 92345,
  "estimatedDurationMs": 110000,
  "parameters": [
    {"name": "version", "value": "1.2.3"},
    {"name": "deploy", "value": true}
  ],
  "scm": {
    "branch": "refs/heads/main",
    "commit": "abc123def456",
    "repo": "github.com/org/app",
    "author": "jane@example.com"
  },
  "causes": [
    {"type": "user", "userId": "jane", "userName": "Jane Doe"},
    {"type": "scm", "description": "Branch indexing triggered this build"}
  ],
  "stages": [
    {
      "name": "Build",
      "status": "completed",
      "result": "SUCCESS",
      "durationMs": 12000,
      "startTime": "2025-08-12T18:24:10Z",
      "pauseDurationMs": 0
    },
    {
      "name": "Test",
      "status": "completed",
      "result": "UNSTABLE",
      "durationMs": 63000,
      "startTime": "2025-08-12T18:24:22Z",
      "pauseDurationMs": 0
    }
  ],
  "artifacts": [
    {"fileName": "report.xml", "relativePath": "reports/report.xml", "size": 1234},
    {"fileName": "logs.tar.gz", "relativePath": "logs/logs.tar.gz", "size": 98765}
  ],
  "tests": {"total": 420, "failed": 3, "skipped": 5},
  "queue": {"id": 1357, "queuedAt": "2025-08-12T18:23:55Z"},
  "node": {"displayName": "linux-agent-1", "executor": 0},
  "description": "Deploy 1.2.3 to staging",
  "displayName": "#128"
}
```

`description` and `displayName` are omitted when Jenkins does not set them.

Run `commit`/`scm.commit` prefer the job's SCM API revision; GitHub PR merge
builds report the PR head rather than the synthetic merge commit. Branch and
repository are omitted if no current checkout matches. Unsupported SCM revision
types retain legacy selection, while revision lookup failures are errors.

### 2.2 Run list (`jk run ls --json`; planned plugin endpoint `/jk/api/runs`)
```json
{
  "schemaVersion": "1.0",
  "items": [
    {
      "id": "team/app/main/128",
      "number": 128,
      "status": "completed",
      "result": "FAILURE",
      "durationMs": 45000,
      "startTime": "2025-08-12T18:24:03Z",
      "branch": "main",
      "commit": "abc123def456",
      "fields": {
        "parameters": {
          "CHART_NAME": "nova-video-prod"
        }
      }
    }
  ],
  "groups": [
    {
      "key": "param.CHART_NAME",
      "value": "nova-video-prod",
      "count": 3,
      "last": {
        "id": "team/app/main/128",
        "number": 128,
        "status": "completed",
        "result": "FAILURE",
        "durationMs": 45000,
        "startTime": "2025-08-12T18:24:03Z"
      }
    }
  ],
  "nextCursor": "g2wAAAABbQAAAGp...",
  "metadata": {
    "filters": {
      "available": ["result", "status", "branch", "commit", "cause.type", "cause.user", "queue.id", "started", "duration", "param.*", "artifact.*", "cause.*"],
      "operators": [">=", "<=", "!=", "~=", "~", "=", "^", "$", ">", "<"]
    },
    "parameters": [
      {
        "name": "CHART_NAME",
        "isSecret": false,
        "sampleValues": ["nova-video-prod"],
        "frequency": 1
      }
    ],
    "fields": ["artifacts", "branch", "causes", "commit", "durationms", "estimateddurationms", "number", "parameters", "queueid", "result", "starttime", "status", "url"],
    "selection": ["parameters"],
    "groupBy": "param.CHART_NAME",
    "aggregation": "last"
  }
}
```

`groups` is omitted when no aggregation is requested, and `metadata` is present only when `--with-meta` is supplied.

### 2.3 Run search (`jk search --json`, `jk run search --json`)
```json
{
  "schemaVersion": "1.0",
  "items": [
    {
      "jobPath": "releases/prod/deploy",
      "id": "releases/prod/deploy/582",
      "number": 582,
      "status": "completed",
      "result": "SUCCESS",
      "durationMs": 540000,
      "startTime": "2025-10-14T17:25:12Z",
      "branch": "main",
      "commit": "4af3d8b0",
      "fields": {
        "parameters": {
          "CHART_NAME": "nova-video-prod"
        }
      }
    }
  ],
  "metadata": {
    "folder": "releases",
    "jobGlob": "*/deploy-*",
    "filters": [
      "param.CHART_NAME=nova-video-prod"
    ],
    "since": "2025-10-13T17:25:12Z",
    "jobsScanned": 6,
    "maxScan": 500,
    "selection": ["parameters"]
  }
}
```

Unlike `jk run ls`, structured search output always includes this lightweight `metadata` block. `--with-meta` is accepted for CLI compatibility, but it is not required.

### 2.4 Progressive log pointer (planned plugin endpoint `/jk/api/runs/<jobPath>/<build>/logs`; the CLI streams via `/logText/progressiveText` today)
```json
{
  "text": "Running on linux-agent-1...\n",
  "nextOffset": 1472,
  "hasMore": true
}
```

### 2.5 Trigger acknowledgement (`jk run start --json` or `jk run rerun --json` without `--follow`)
```json
{
  "jobPath": "team/app/main",
  "message": "run requested",
  "queueLocation": "https://jenkins.example/queue/item/1357/"
}
```

`jk run rerun` uses `"message": "rerun requested"`. When `--follow` is supplied with `--json`/`--yaml`, the CLI suppresses live log output and, after the run finishes, emits the run detail payload described in §2.1 instead of the acknowledgement.

### 2.6 Cancel acknowledgement (`jk run cancel --json`)
```json
{
  "jobPath": "team/app/main",
  "build": 128,
  "action": "stop",
  "status": "requested"
}
```

`action` mirrors `--mode` (`stop` by default, `term`, or `kill`). The CLI exits successfully once the cancellation request is accepted by Jenkins; it does not wait for the build to terminate.

### 2.7 Run parameter discovery (`jk run params --json`)
```json
{
  "jobPath": "team/app/main",
  "source": "runs",
  "parameters": [
    {
      "name": "CHART_NAME",
      "type": "string",
      "isSecret": false,
      "sampleValues": ["nova"],
      "frequency": 1
    },
    {
      "name": "API_TOKEN",
      "type": "password",
      "isSecret": true,
      "frequency": 1
    }
  ]
}
```

`source` reflects the discovery path actually used (`config` or `runs`; `--source auto`, the default, tries config first and falls back to runs), and `sampleValues`/`frequency` are derived from recent runs. Secret parameters omit defaults and sample values.

### 2.8 JUnit report (`jk test report --json`)

```json
{
  "totalCount": 3,
  "passCount": 1,
  "failCount": 1,
  "skipCount": 1,
  "suites": [{
    "name": "example.Calculator",
    "cases": [{
      "className": "example.Calculator",
      "name": "test_sum",
      "status": "FAILED",
      "duration": 0.1,
      "errorDetails": "expected 4, got 5",
      "errorStackTrace": "AssertionError: expected 4, got 5\n  at Calculator.test_sum",
      "age": 2,
      "failedSince": 41
    }]
  }]
}
```

The suite and case arrays contain every reported case by default; the example
abbreviates them. `--failed` filters cases to `FAILED` and `REGRESSION`, omits
empty suites and keeps the complete report's aggregate counts. No matches produce
`"suites": []`. `passCount` is omitted when zero; failure detail/history fields
are optional and omitted when empty or zero. Full stack traces are preserved.
If Jenkins omits or returns null for `totalCount`, the CLI derives it from
pass/fail/skip counts; an explicit total, including zero, is preserved. A missing
report retains the existing human message, including when `--json` is selected.
`run view --json` retains its existing `tests: {total, failed, skipped}` shape;
failure details are displayed only by its human `--summary` mode.

### 2.5 Commit watch (`jk run watch --json`)

`jk run watch <jobPath> --commit <full-source-sha>` prints progress to stderr and
one final JSON/YAML object to stdout. It selects the newest retained matching
build, then follows that fixed build number. The full SHA must contain 40 or 64
hexadecimal characters; abbreviated hashes are rejected. Queue entries before
discovery are job context rather than commit attribution.

```json
{
  "jobPath": "team/app/main",
  "commit": "2222222222222222222222222222222222222222",
  "number": 128,
  "url": "https://jenkins.example/job/team/job/app/job/main/128/",
  "status": "completed",
  "result": "SUCCESS",
  "durationMs": 92345
}
```

All fields are present. `status` is `completed`, `timed_out`, or `cancelled` in a
final result. Before a build is identified, `number` is 0, `url` and `result` are
empty strings, and `durationMs` is 0. If a selected build is still running at
timeout/cancellation, the result contains its last observed URL and duration;
`result` may still be empty. An empty terminal Jenkins result becomes `UNKNOWN`.

`--timeout` exits 124 and cancellation exits 130, regardless of `--exit-status`.
With `--exit-status`, build results map to SUCCESS=0, UNSTABLE=10, FAILURE=11,
ABORTED=12, NOT_BUILT=13; unknown terminal results exit 1. Without it, watching a
completed failed build exits 0. HTTP/authentication failures propagate as errors
and do not produce a successful watch result.

### 2.8 Pipeline stage and step inspection (`jk run steps --json`)

Requires the optional Pipeline Graph View plugin and run read permission. The CLI decodes the plugin's `status/data` envelope; its own output is:

```json
{
  "jobPath": "team/app/main",
  "build": 128,
  "complete": true,
  "stages": [{"id":"2","name":"Test","state":"failure","type":"STAGE"}],
  "steps": [{"id":"3","name":"unit checks","state":"failure","type":"STEP","title":"Shell Script","stageId":"2"}]
}
```

Stages may contain `children` and a `nextSibling` stage. Step labels may repeat; `id` is the node identity, and `stageId` links each step to a stage. States are passed through from the plugin (for example `running`, `paused`, `success`, `failure`, `skipped`). Empty collections are arrays. `complete` is true only when both plugin snapshots report completion.

## 3. Logs

### 3.1 Run log snapshot (`jk log --json`)
```json
{
  "jobPath": "team/app/main",
  "build": 128,
  "status": "completed",
  "result": "SUCCESS",
  "startTime": "2025-08-12T18:24:03Z",
  "duration": "1m32s",
  "log": "Started by user Jane Doe\nRunning on linux-agent-1...\n...",
  "truncated": false,
  "complete": true
}
```

Snapshot output is capped at 2 MiB, including completed logs returned in a single response. `truncated: true` means available selected output was omitted. `complete` is always present and is true only when the completed build's requested selection is exact; running snapshots set it to false even when all currently available bytes fit. The false `truncated` value may be omitted for backward compatibility. `--follow` emits live text only and does not support JSON/YAML.

`jk log <job> <build> --tail N --grep <pattern> --json` scans the complete console of a completed build, applies the Go regular expression to each line (excluding its LF/CRLF ending), then retains the last N matching lines. `--tail` must be positive; an explicitly empty pattern matches all lines. Either flag works alone, and both reject running builds and `--follow`. JSON/YAML preserves line endings and the absence of a final newline.

The selected output budget is 2 MiB, independent of N. If it cannot hold the requested selection, tail output keeps the newest whole lines that fit; grep-only output keeps a prefix of whole matching lines. These cases set `truncated: true, complete: false`. Intentional exclusion by grep or tail does not count as truncation. Lines over 1 MiB including the line ending fail the command explicitly rather than yielding an inexact result. `--plain` suppresses metadata and notices, so callers that must detect truncation should use JSON/YAML.

### 3.2 Pipeline node log (`jk log --step <id> --json`)

The snapshot schema in §3.1 gains `stepId` and `complete` for an isolated Pipeline stage or step log. `truncated` is true when the response exceeds 2 MiB; `complete` is false if capped, the build is still running, or plugin metadata reports incomplete execution. YAML uses the same keys. A stage ID returns the stage's aggregated step log. Unknown IDs are rejected before requesting the log, since the plugin can otherwise return HTTP 200 for an invalid node. `--step` cannot be combined with `--follow`, `--tail` or `--grep`.

## 4. Credentials

### 4.1 List (`jk cred ls --json` and `/jk/api/credentials`)
```json
{
  "items": [
    {
      "id": "slack-token",
      "type": "secretText",
      "scope": "folder",
      "path": "team/app",
      "description": "Slack notifier",
      "updatedAt": "2025-09-03T12:44:00Z"
    },
    {
      "id": "docker-hub",
      "type": "usernamePassword",
      "scope": "system",
      "description": "Docker Hub service account",
      "updatedAt": "2025-08-01T09:10:11Z"
    }
  ],
  "nextCursor": null
}
```

### 4.2 Create/update request payload (planned plugin contract)

The sample below is the planned plugin payload. Today `jk cred create-secret` posts a JSON body `{"": "0", "credentials": {"scope": "GLOBAL", "id": ..., "description": ..., "secret": ..., "$class": "org.jenkinsci.plugins.plaincredentials.impl.StringCredentialsImpl"}}` to the Jenkins core endpoint `/credentials/store/system/domain/_/createCredentials` (or `/<folder>/credentials/store/folder/domain/_/createCredentials`).

```json
{
  "scope": "folder",
  "folderPath": "team/app",
  "type": "secretText",
  "id": "slack-token",
  "description": "Slack notifier token",
  "data": {
    "secret": "****"
  }
}
```

## 5. CLI Introspection

### 5.1 Command catalog (`jk help --json`)
```json
{
  "schemaVersion": "1.0",
  "commands": [
    {
      "name": "jk",
      "use": "jk",
      "description": "jk is the Jenkins CLI for developers",
      "flags": [
        {"name": "context", "shorthand": "c", "type": "string", "description": "Active Jenkins context name", "default": "", "persistent": true},
        {"name": "json", "type": "bool", "description": "Output in JSON format when supported", "default": "false", "persistent": true},
        {"name": "yaml", "type": "bool", "description": "Output in YAML format when supported", "default": "false", "persistent": true},
        {"name": "jq", "type": "string", "description": "Filter JSON output using jq expression (requires --json or --format=json)", "default": "", "persistent": true},
        {"name": "quiet", "shorthand": "q", "type": "bool", "description": "Suppress non-essential output", "default": "false", "persistent": true},
        {"name": "format", "type": "string", "description": "Output format: json, yaml", "default": "", "persistent": true},
        {"name": "template", "shorthand": "t", "type": "string", "description": "Format output using Go template (requires --json or --format=json)", "default": "", "persistent": true}
      ],
      "subcommands": [
        {
          "name": "run",
          "use": "run",
          "description": "Interact with job runs",
          "subcommands": [
            {"name": "ls", "use": "run ls <jobPath>", "description": "List recent runs"},
            {"name": "search", "use": "run search", "description": "Search runs across jobs"}
          ]
        }
      ]
    }
  ],
  "exitCodes": {
    "0": "Success",
    "1": "General error",
    "2": "Validation error",
    "3": "Not found",
    "4": "Authentication failure",
    "5": "Permission denied",
    "6": "Connectivity/DNS/TLS failure",
    "7": "Timeout",
    "8": "Feature unsupported",
    "10": "Build result: UNSTABLE",
    "11": "Build result: FAILURE",
    "12": "Build result: ABORTED",
    "13": "Build result: NOT_BUILT",
    "14": "Build result: RUNNING (in-progress)"
  },
  "environmentVariables": {
    "JK_CONTEXT": "Override the active Jenkins context (same as --context/-c flag)",
    "JK_TOKEN": "Use an API token from the environment without opening the secret store",
    "JK_URL": "Controller URL for an ephemeral context (requires JK_USER and JK_TOKEN)",
    "JK_USER": "Jenkins user ID for an ephemeral context (requires JK_URL and JK_TOKEN)",
    "JK_QUIET": "Enable quiet mode for supported commands (same as --quiet/-q flag)"
  }
}
```

## 6. Events (SSE) — planned plugin contract

No `jk` command consumes this stream yet; the CLI only probes `/sse-gateway/stats` to advertise the capability.

- Endpoint: `/jk/events/stream?topics=run,queue,node`
- Frames are JSON objects encoded as UTF-8 text events.

```json
{
  "topic": "run.completed",              // run.started | run.progress | queue.entered | node.offline | ...
  "jobPath": "team/app/main",
  "number": 128,
  "status": "completed",
  "result": "SUCCESS",
  "timestamp": "2025-08-12T18:25:35Z"
}
```

## 7. Plugin status handshake

- Endpoint: `GET /jk/api/status`
- Response schema:
```json
{
  "version": "1.0.0",
  "features": ["runs", "credentials", "events"],
  "minClient": "0.3.0",
  "recommendedClient": "1.0.0"
}
```
- Clients send `X-JK-Client: <semver>` and `X-JK-Features: <csv>` headers. The CLI parses `minClient`/`recommendedClient` but does not act on them yet; the planned behaviour is to fall back to baseline Jenkins APIs and warn when the CLI is below `minClient`.

## 8. Pagination cursors

- Cursors are opaque URL-safe base64 strings produced by the server; clients cannot introspect them.
- Requests accept `cursor=<value>` and `limit=<n>`. Servers may ignore `limit` in favor of their own defaults but must not return more than requested.
- When `nextCursor` is omitted or `null`, the collection is exhausted. Pass the value back verbatim with `--cursor <value>`.

## 9. Enumerations

| Field    | Allowed values                                                       |
|----------|---------------------------------------------------------------------|
| `status` | `queued`, `running`, `completed`                                    |
| `result` | `SUCCESS`, `UNSTABLE`, `FAILURE`, `ABORTED`, `NOT_BUILT`, `null`    |
| `topic`  | `run.started`, `run.progress`, `run.completed`, `queue.entered`, `queue.left`, `node.online`, `node.offline` |

All enumerations are case-sensitive.

---

These schemas back the CLI contract tests and companion plugin REST responses. Update this document and increment the relevant versions before changing any field shape or enumeration.
